// Package gemini は Vertex AI の Gemini を使う AI の実装（課題の具体化・抽出・調整、相棒の一言、AI 相談）。
//
// AI を呼ぶ前に必ず利用回数を1回分確保する。SDK の再試行は使わない（失敗した呼び出しも1回に数え、確保した回数を超えて呼ばない）。
// AI の出力は信頼できない入力として形を確かめてから返す。外部の応答本文・プロンプト・本人の入力はエラーにもログにも出さない。
package gemini

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"google.golang.org/genai"

	"github.com/a-sumikawa/devfest-2026/apps/api/internal/assistant"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/companion"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/apperr"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/platform/locale"
	"github.com/a-sumikawa/devfest-2026/apps/api/internal/task"
)

// companionNames はどの場面でも話す、ひとりの相棒の名前。本人の言語で呼ぶ。
var companionNames = map[locale.Locale]string{locale.Ja: "相棒", locale.En: "Buddy"}

// languageNames は、本人に向けた文を書く言語の名前（AI への指示に使う）。
var languageNames = map[locale.Locale]string{locale.Ja: "日本語", locale.En: "英語（English）"}

//go:embed companion_persona.md
var companionPersona string

// Quota は AI の利用回数を確保する（aiquota.Quota が実装する）。上限なら ai.rate_limited、確保できなければ ai.unavailable。
type Quota interface {
	Take(ctx context.Context, uid string) error
}

// Config は接続先。BaseURL と HTTPClient はテストで偽のサーバーに向けるときだけ使う（HTTPClient が nil なら ADC で認証する）。
type Config struct {
	Project    string
	Location   string
	Model      string
	BaseURL    string
	HTTPClient *http.Client
}

// Client は Gemini を呼ぶ。
type Client struct {
	models *genai.Models
	model  string
	quota  Quota
}

var (
	_ task.Planner        = (*Client)(nil)
	_ companion.Generator = (*Client)(nil)
	_ assistant.Model     = (*Client)(nil)
)

// New は Vertex AI の Gemini に接続する Client を作る。
func New(ctx context.Context, cfg Config, quota Quota) (*Client, error) {
	if cfg.Project == "" || cfg.Location == "" || cfg.Model == "" || quota == nil {
		return nil, errors.New("gemini: project, location, model and quota are required")
	}
	// 確保した1回分を超えて呼ばないよう、SDK の再試行を止める。
	attempts := int32(1)
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		Backend:    genai.BackendVertexAI,
		Project:    cfg.Project,
		Location:   cfg.Location,
		HTTPClient: cfg.HTTPClient,
		HTTPOptions: genai.HTTPOptions{
			BaseURL:      cfg.BaseURL,
			RetryOptions: &genai.HTTPRetryOptions{Attempts: &attempts},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("gemini: new client: %w", err)
	}
	return &Client{models: client.Models, model: cfg.Model, quota: quota}, nil
}

func persona(lang locale.Locale) *genai.Content {
	text := strings.NewReplacer("{NAME}", companionNames[lang], "{LANGUAGE}", languageNames[lang]).Replace(companionPersona)
	return genai.NewContentFromText(text, genai.RoleUser)
}

// generate は利用回数を1回分確保してから Gemini を1回呼ぶ。
func (c *Client) generate(ctx context.Context, uid string, contents []*genai.Content, config *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error) {
	if err := c.quota.Take(ctx, uid); err != nil {
		return nil, err
	}
	res, err := c.models.GenerateContent(ctx, c.model, contents, config)
	if err != nil {
		return nil, apperr.External("ai.failed", sanitize(err))
	}
	return res, nil
}

// sanitize は外部のエラーから、応答本文やメッセージを除いた原因だけを残す。
func sanitize(err error) error {
	var api genai.APIError
	if errors.As(err, &api) {
		return fmt.Errorf("gemini: http %d %s", api.Code, api.Status)
	}
	var apiPtr *genai.APIError
	if errors.As(err, &apiPtr) && apiPtr != nil {
		return fmt.Errorf("gemini: http %d %s", apiPtr.Code, apiPtr.Status)
	}
	for _, known := range []error{context.DeadlineExceeded, context.Canceled} {
		if errors.Is(err, known) {
			return fmt.Errorf("gemini: %w", known)
		}
	}
	return errors.New("gemini: request failed")
}

// responseText は最初の候補の、考えの過程を除いた文。
func responseText(res *genai.GenerateContentResponse) string {
	if res == nil || len(res.Candidates) == 0 || res.Candidates[0].Content == nil {
		return ""
	}
	var b strings.Builder
	for _, part := range res.Candidates[0].Content.Parts {
		if part != nil && part.Text != "" && !part.Thought {
			b.WriteString(part.Text)
		}
	}
	return b.String()
}

func unreadable() error { return apperr.Unavailable("ai.unreadable") }
