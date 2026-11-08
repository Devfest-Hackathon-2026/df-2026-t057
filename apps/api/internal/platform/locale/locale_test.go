package locale

import "testing"

func TestValid(t *testing.T) {
	tests := []struct {
		in   Locale
		want bool
	}{
		{Ja, true},
		{En, true},
		{"", false},
		{"JA", false},
		{"en-US", false},
		{"fr", false},
	}
	for _, tt := range tests {
		if got := tt.in.Valid(); got != tt.want {
			t.Errorf("Locale(%q).Valid() = %v, want %v", tt.in, got, tt.want)
		}
	}
}
