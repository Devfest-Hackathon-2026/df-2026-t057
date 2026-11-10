// Package config は環境変数を読み、検証した設定を返す。
//
// 環境変数を読むのは cmd/server とこのパッケージだけ（AGENTS.md）。本番のプロジェクトIDなどを既定値に書かない。
// live モードで必要な値が欠けていれば、起動を止める。
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

// Mode は起動モード。
type Mode string

const (
	ModeDemo Mode = "demo"
	ModeLive Mode = "live"
)

// AILimits は AI の利用上限（§2.14）。
type AILimits struct {
	UserPerMinute   int
	UserPerDay      int
	GlobalPerMinute int
	GlobalPerDay    int
}

// Calendar は Google カレンダー連携の設定。ClientID が空なら連携を使わない。
type Calendar struct {
	ClientID     string
	ClientSecret string
	// Queue は Cloud Tasks のキュー（projects/<p>/locations/<l>/queues/<q>）。
	Queue string
	// TasksInvoker は Cloud Tasks が OIDC トークンに使うサービスアカウントのメール。
	TasksInvoker string
	// SchedulerInvoker は Cloud Scheduler が OIDC トークンに使うサービスアカウントのメール。
	SchedulerInvoker string
}

// Enabled はカレンダー連携を使うか。
func (c Calendar) Enabled() bool { return c.ClientID != "" }

// Config は検証済みの設定。
type Config struct {
	AccountCleanup bool
	Mode           Mode
	Port           int
	AppURL         string
	Project        string
	Location       string
	// Firebase は管理画面に返す Firebase の Web 設定。
	Firebase map[string]string
	// AuthDomain は Firebase の authDomain（CSP に加える）。
	AuthDomain   string
	GeminiModel  string
	AI           AILimits
	Calendar     Calendar
	ExtensionIDs []string
	StaticDir    string
	// APIOnly は管理画面を配信せず、API だけを配信する（demo の開発用）。
	APIOnly bool
	// Public は demo でも 0.0.0.0 で待ち受ける。
	Public bool
	// OIDCAudience は Cloud Tasks / Scheduler の OIDC トークンの audience。
	OIDCAudience string
}

// AIEnabled は Gemini を使えるか。demo は外部に接続しない。
func (c Config) AIEnabled() bool { return c.Mode == ModeLive }

// CalendarEnabled はカレンダー連携を使えるか。
func (c Config) CalendarEnabled() bool { return c.Mode == ModeLive && c.Calendar.Enabled() }

// Load は getenv から設定を読む。getenv は os.Getenv を渡す（テストでは差し替える）。
func Load(getenv func(string) string) (Config, error) {
	env := func(key, fallback string) string {
		if v := strings.TrimSpace(getenv(key)); v != "" {
			return v
		}
		return fallback
	}
	var errs []error
	c := Config{
		Mode:        Mode(env("APP_MODE", string(ModeDemo))),
		Project:     env("GOOGLE_CLOUD_PROJECT", ""),
		Location:    env("GOOGLE_CLOUD_LOCATION", "global"),
		GeminiModel: env("GEMINI_MODEL", "gemini-3.8-flash"),
		StaticDir:   env("STATIC_DIR", "../web/dist/web/browser"),
		Calendar: Calendar{
			ClientID:         env("GOOGLE_OAUTH_CLIENT_ID", ""),
			ClientSecret:     env("GOOGLE_OAUTH_CLIENT_SECRET", ""),
			Queue:            env("CLOUD_TASKS_QUEUE", ""),
			TasksInvoker:     env("CLOUD_TASKS_INVOKER", ""),
			SchedulerInvoker: env("SCHEDULER_INVOKER", ""),
		},
	}
	cleanup, cleanupErr := strconv.ParseBool(env("ACCOUNT_CLEANUP_ENABLED", "false"))
	if cleanupErr != nil {
		errs = append(errs, errors.New("ACCOUNT_CLEANUP_ENABLED は true か false にしてください"))
	}
	c.AccountCleanup = cleanup
	if c.Mode != ModeDemo && c.Mode != ModeLive {
		errs = append(errs, fmt.Errorf("APP_MODE は demo か live にしてください（%q）", c.Mode))
	}
	port, err := strconv.Atoi(env("PORT", "8080"))
	if err != nil || port < 1 || port > 65535 {
		errs = append(errs, errors.New("PORT は 1〜65535 の整数にしてください"))
	}
	c.Port = port

	defaultURL := ""
	if c.Mode == ModeDemo {
		defaultURL = "http://127.0.0.1:4200"
	}
	c.AppURL = strings.TrimRight(env("APP_URL", defaultURL), "/")
	if c.AppURL == "" {
		errs = append(errs, errors.New("APP_URL が必要です"))
	} else if u, err := url.Parse(c.AppURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Path != "" {
		errs = append(errs, fmt.Errorf("APP_URL はオリジン（例 https://example.com）にしてください（%q）", c.AppURL))
	}
	c.OIDCAudience = env("OIDC_AUDIENCE", c.AppURL)

	c.APIOnly = getenv("DEV_API_ONLY") == "true"
	c.Public = getenv("DEMO_PUBLIC") == "true"
	if c.Mode == ModeLive && c.APIOnly {
		errs = append(errs, errors.New("DEV_API_ONLY は demo でだけ使えます"))
	}

	for _, id := range strings.Split(getenv("EXTENSION_IDS"), ",") {
		if id = strings.TrimSpace(id); id != "" {
			if !isExtensionID(id) {
				errs = append(errs, fmt.Errorf("EXTENSION_IDS に拡張IDでない値があります（%q）", id))
				continue
			}
			c.ExtensionIDs = append(c.ExtensionIDs, id)
		}
	}

	limits := []struct {
		key    string
		target *int
		def    int
	}{
		{"AI_USER_PER_MINUTE", &c.AI.UserPerMinute, 10},
		{"AI_USER_PER_DAY", &c.AI.UserPerDay, 500},
		{"AI_GLOBAL_PER_MINUTE", &c.AI.GlobalPerMinute, 60},
		{"AI_GLOBAL_PER_DAY", &c.AI.GlobalPerDay, 2000},
	}
	for _, l := range limits {
		n, err := strconv.Atoi(env(l.key, strconv.Itoa(l.def)))
		if err != nil || n < 1 {
			errs = append(errs, fmt.Errorf("%s は1以上の整数にしてください", l.key))
		}
		*l.target = n
	}

	if raw := getenv("FIREBASE_WEB_CONFIG"); strings.TrimSpace(raw) != "" {
		if err := json.Unmarshal([]byte(raw), &c.Firebase); err != nil {
			errs = append(errs, errors.New("FIREBASE_WEB_CONFIG は文字列の値を持つJSONオブジェクトにしてください"))
		}
		c.AuthDomain = c.Firebase["authDomain"]
		if c.AuthDomain != "" && !isBareHost(c.AuthDomain) {
			errs = append(errs, errors.New("FIREBASE_WEB_CONFIG の authDomain はホスト名だけにしてください"))
		}
	}

	if c.Mode == ModeLive {
		if c.AccountCleanup && !c.Calendar.Enabled() {
			errs = append(errs, errors.New("ACCOUNT_CLEANUP_ENABLED にはカレンダーと保守OIDCの設定が必要です"))
		}
		if c.Project == "" {
			errs = append(errs, errors.New("live では GOOGLE_CLOUD_PROJECT が必要です"))
		}
		if len(c.Firebase) == 0 {
			errs = append(errs, errors.New("live では FIREBASE_WEB_CONFIG が必要です"))
		}
		if c.Calendar.Enabled() {
			missing := []string{}
			for key, v := range map[string]string{
				"GOOGLE_OAUTH_CLIENT_SECRET": c.Calendar.ClientSecret,
				"CLOUD_TASKS_QUEUE":          c.Calendar.Queue,
				"CLOUD_TASKS_INVOKER":        c.Calendar.TasksInvoker,
				"SCHEDULER_INVOKER":          c.Calendar.SchedulerInvoker,
			} {
				if v == "" {
					missing = append(missing, key)
				}
			}
			if len(missing) > 0 {
				errs = append(errs, fmt.Errorf("カレンダー連携（GOOGLE_OAUTH_CLIENT_ID）には %s が必要です", strings.Join(sorted(missing), ", ")))
			}
		}
	}
	if err := errors.Join(errs...); err != nil {
		return Config{}, fmt.Errorf("設定を確認してください: %w", err)
	}
	return c, nil
}

// isExtensionID は Chrome 拡張のID（a〜p の32文字）か。
func isExtensionID(id string) bool {
	if len(id) != 32 {
		return false
	}
	for _, r := range id {
		if r < 'a' || r > 'p' {
			return false
		}
	}
	return true
}

func isBareHost(host string) bool {
	u, err := url.Parse("https://" + host)
	return err == nil && u.Host == host && u.User == nil && u.Path == "" && u.RawQuery == "" && u.Fragment == ""
}

func sorted(values []string) []string {
	slices.Sort(values)
	return values
}
