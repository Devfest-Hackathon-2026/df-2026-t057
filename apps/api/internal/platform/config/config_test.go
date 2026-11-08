package config

import (
	"strings"
	"testing"
)

func env(values map[string]string) func(string) string {
	return func(k string) string { return values[k] }
}

func TestDemoIsDefault(t *testing.T) {
	c, err := Load(env(nil))
	if err != nil || c.Mode != ModeDemo || c.Port != 8080 || c.AppURL != "http://127.0.0.1:4200" || c.Project != "" || c.AIEnabled() {
		t.Fatalf("%+v %v", c, err)
	}
}

func TestLiveFailsFastWithoutRequiredSettings(t *testing.T) {
	_, err := Load(env(map[string]string{"APP_MODE": "live"}))
	for _, want := range []string{"GOOGLE_CLOUD_PROJECT", "APP_URL", "FIREBASE_WEB_CONFIG"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("missing %s: %v", want, err)
		}
	}
	_, err = Load(env(map[string]string{"APP_MODE": "live", "GOOGLE_CLOUD_PROJECT": "p", "APP_URL": "https://app.example", "FIREBASE_WEB_CONFIG": `{"apiKey":"k"}`, "GOOGLE_OAUTH_CLIENT_ID": "c"}))
	if err == nil || !strings.Contains(err.Error(), "CLOUD_TASKS_QUEUE") {
		t.Fatalf("calendar settings: %v", err)
	}
	c, err := Load(env(map[string]string{"APP_MODE": "live", "GOOGLE_CLOUD_PROJECT": "p", "APP_URL": "https://app.example/", "FIREBASE_WEB_CONFIG": `{"authDomain":"p.firebaseapp.com"}`, "EXTENSION_IDS": "abcdefghijklmnopabcdefghijklmnop"}))
	if err != nil || c.AppURL != "https://app.example" || c.AuthDomain != "p.firebaseapp.com" || len(c.ExtensionIDs) != 1 || c.CalendarEnabled() {
		t.Fatalf("%+v %v", c, err)
	}
}

func TestRejectsBadValues(t *testing.T) {
	cases := []map[string]string{
		{"APP_MODE": "prod"},
		{"PORT": "x"},
		{"AI_USER_PER_DAY": "0"},
		{"EXTENSION_IDS": "not-an-id"},
		{"FIREBASE_WEB_CONFIG": `{"authDomain":"evil.example/path"}`},
		{"APP_MODE": "live", "DEV_API_ONLY": "true"},
	}
	for _, c := range cases {
		if _, err := Load(env(c)); err == nil {
			t.Fatalf("accepted %v", c)
		}
	}
}
