package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"golang.org/x/oauth2"
	"google.golang.org/api/googleapi"
	grpcstatus "google.golang.org/grpc/status"
)

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
	raw = bytes.TrimSpace(raw)
	if err == nil && len(raw) > 0 && raw[0] == '{' {
		d := json.NewDecoder(bytes.NewReader(raw))
		d.DisallowUnknownFields()
		if d.Decode(v) == nil && d.Decode(new(any)) == io.EOF {
			return true
		}
	}
	writeError(w, 400, errors.New("入力の形式を確認してください"))
	return false
}
func respond(w http.ResponseWriter, value any, err error) {
	if err != nil {
		var limit *AILimitError
		if errors.As(err, &limit) {
			seconds := max(1, int((time.Until(limit.RetryAt)+time.Second-1)/time.Second))
			w.Header().Set("Retry-After", strconv.Itoa(seconds))
			writeError(w, 429, err)
			return
		}
		if errors.Is(err, ErrAIUsageUnavailable) {
			writeError(w, 503, err)
			return
		}
		status := 400
		if errors.Is(err, ErrConflict) {
			status = 409
		}
		if errors.Is(err, ErrNotFound) {
			status = 404
		}
		LogFailure("operation failed", err)
		var apiErr *googleapi.Error
		var oauthErr *oauth2.RetrieveError
		var transportErr *url.Error
		if _, grpcError := grpcstatus.FromError(err); grpcError || errors.As(err, &apiErr) || errors.As(err, &oauthErr) || errors.As(err, &transportErr) {
			writeError(w, 500, errors.New("外部サービスとの通信に失敗しました。少し待って再試行してください"))
			return
		}
		writeError(w, status, err)
		return
	}
	writeJSON(w, 200, value)
}

// SDK errors can contain response bodies, credentials, URLs and personal input.
func LogFailure(operation string, err error) {
	var apiErr *googleapi.Error
	code := 0
	if errors.As(err, &apiErr) {
		code = apiErr.Code
	}
	slog.Error(operation, "error_type", fmt.Sprintf("%T", err), "grpc_code", grpcstatus.Code(err).String(), "http_status", code)
}
