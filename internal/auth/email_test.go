package auth

import "testing"

func TestSafeNextPath(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"simple relative", "/files", "/files"},
		{"relative with query", "/files?id=1", "/files?id=1"},
		{"root", "/", "/"},
		{"empty", "", ""},
		{"absolute http", "http://evil.com/x", ""},
		{"absolute https", "https://evil.com/x", ""},
		{"protocol relative", "//evil.com", ""},
		{"no leading slash", "files", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := safeNextPath(tt.in); got != tt.want {
				t.Errorf("safeNextPath(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
