package http

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/adapter/http/oapi"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/apperr"
)

// authKind は操作の認証の方式（仕様の security から決める）。
type authKind int

const (
	authUser   authKind = iota // Firebase の ID トークン、またはデモ
	authNone                   // 認証なし
	authCustom                 // ハンドラーが自分で確かめる（ショートカットのキー、OIDC）
)

// route は仕様の1つの操作。
type route struct {
	method      string
	template    string
	segments    []string
	auth        authKind
	idempotent  bool
	bodySchema  *openapi3.Schema
	operationID string
}

// routes は仕様から作った操作の一覧。
type routes []route

func loadRoutes() (routes, error) {
	spec, err := oapi.GetSpec()
	if err != nil {
		return nil, fmt.Errorf("load spec: %w", err)
	}
	var out routes
	for path, item := range spec.Paths.Map() {
		for method, op := range item.Operations() {
			r := route{method: method, template: path, segments: strings.Split(strings.Trim(path, "/"), "/"), operationID: op.OperationID}
			r.auth = authUser
			if op.Security != nil {
				switch {
				case len(*op.Security) == 0:
					r.auth = authNone
				default:
					r.auth = authCustom
				}
			}
			for _, p := range op.Parameters {
				if p.Value != nil && p.Value.In == "header" && p.Value.Name == "Idempotency-Key" {
					r.idempotent = true
				}
			}
			if op.RequestBody != nil && op.RequestBody.Value != nil {
				if mt := op.RequestBody.Value.Content.Get("application/json"); mt != nil && mt.Schema != nil {
					r.bodySchema = mt.Schema.Value
				}
			}
			out = append(out, r)
		}
	}
	// 固定の区間を持つパスを、パラメーターのパスより先に比べる（/tasks/extract と /tasks/{taskId}）。
	slices.SortFunc(out, func(a, b route) int { return strings.Compare(sortKey(a), sortKey(b)) })
	return out, nil
}

func sortKey(r route) string {
	return strings.NewReplacer("{", "~").Replace(r.template) + " " + r.method
}

// match は API のパス（/api を除いた部分）とメソッドに合う操作を返す。
func (rs routes) match(method, path string) *route {
	segs := strings.Split(strings.Trim(path, "/"), "/")
	for i := range rs {
		if segmentsMatch(rs[i].segments, segs) && (rs[i].method == method || (method == "HEAD" && rs[i].method == "GET")) {
			return &rs[i]
		}
	}
	return nil
}

func segmentsMatch(template, segs []string) bool {
	if len(template) != len(segs) {
		return false
	}
	for i, t := range template {
		if strings.HasPrefix(t, "{") {
			if segs[i] == "" {
				return false
			}
			continue
		}
		if t != segs[i] {
			return false
		}
	}
	return true
}

// checkBody は本文が1つの JSON オブジェクトで、仕様に無い項目が無く、必須の項目があるかを確かめる。
// 値の範囲などは機能のルールで確かめる（項目ごとの code を返すため）。
func checkBody(body []byte, schema *openapi3.Schema) error {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return apperr.Invalid("request.malformed")
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return apperr.Invalid("request.malformed")
	}
	if _, err := dec.Token(); err != io.EOF {
		return apperr.Invalid("request.malformed")
	}
	return checkValue(value, schema, "")
}

func checkValue(value any, schema *openapi3.Schema, path string) error {
	if schema == nil {
		return nil
	}
	if value == nil {
		if allowsNull(schema) {
			return nil
		}
		return apperr.Invalid("request.malformed")
	}
	if len(schema.OneOf) > 0 || len(schema.AnyOf) > 0 {
		var firstErr error
		for _, s := range append(slices.Clone(schema.OneOf), schema.AnyOf...) {
			if s.Value == nil || (s.Value.Type != nil && s.Value.Type.Is("null")) {
				continue
			}
			err := checkValue(value, s.Value, path)
			if err == nil {
				return nil
			}
			if firstErr == nil {
				firstErr = err
			}
		}
		return firstErr
	}
	properties, required := flatten(schema)
	switch v := value.(type) {
	case map[string]any:
		if len(properties) == 0 && len(schema.AllOf) == 0 && !schema.Type.Is("object") {
			return apperr.Invalid("request.malformed")
		}
		for key := range v {
			if _, ok := properties[key]; !ok {
				return apperr.Invalid("request.malformed")
			}
		}
		for _, key := range required {
			if _, ok := v[key]; !ok {
				return apperr.Invalid("request.invalid", apperr.Field(join(path, key), "required"))
			}
		}
		for key, item := range v {
			if err := checkValue(item, properties[key], join(path, key)); err != nil {
				return err
			}
		}
	case []any:
		if schema.Items != nil {
			for i, item := range v {
				if err := checkValue(item, schema.Items.Value, join(path, fmt.Sprint(i))); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func allowsNull(s *openapi3.Schema) bool {
	if s.Nullable || (s.Type != nil && s.Type.Is("null")) {
		return true
	}
	for _, x := range s.OneOf {
		if x.Value != nil && x.Value.Type != nil && x.Value.Type.Is("null") {
			return true
		}
	}
	return false
}

// flatten は allOf を合わせた項目と必須の項目。
func flatten(s *openapi3.Schema) (map[string]*openapi3.Schema, []string) {
	props := map[string]*openapi3.Schema{}
	required := slices.Clone(s.Required)
	for name, p := range s.Properties {
		props[name] = p.Value
	}
	for _, part := range s.AllOf {
		if part.Value == nil {
			continue
		}
		p, r := flatten(part.Value)
		for k, v := range p {
			props[k] = v
		}
		required = append(required, r...)
	}
	return props, required
}

func join(path, key string) string {
	if path == "" {
		return key
	}
	return path + "/" + key
}
