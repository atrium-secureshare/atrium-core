package audit

import "testing"

func TestCanonicalNormalizesCaseAndUnicode(t *testing.T) {
	// U+FF55 FULLWIDTH LATIN SMALL LETTER U NFKC-normalizes to "u".
	if got, want := Canonical("ｕSER@Example.com"), "user@example.com"; got != want {
		t.Errorf("Canonical = %q, want %q", got, want)
	}
}

func TestHashEmailDeterministic(t *testing.T) {
	if hashEmail(nil, "user@example.com") != hashEmail(nil, "user@example.com") {
		t.Error("hashEmail should be deterministic")
	}
}

func TestHashEmailCaseAndUnicodeInsensitive(t *testing.T) {
	if hashEmail(nil, "ｕSER@Example.com") != hashEmail(nil, "user@example.com") {
		t.Error("canonical-equal emails should hash equally")
	}
}

func TestHashEmailLength(t *testing.T) {
	if got := len(hashEmail(nil, "user@example.com")); got != hashLen {
		t.Errorf("hash length = %d, want %d", got, hashLen)
	}
}

func TestHashEmailSaltChangesOutput(t *testing.T) {
	if hashEmail(nil, "user@example.com") == hashEmail([]byte("pepper"), "user@example.com") {
		t.Error("salted and unsalted hashes should differ")
	}
}

func TestHashEmailEmptyStaysEmpty(t *testing.T) {
	if got := hashEmail([]byte("pepper"), ""); got != "" {
		t.Errorf("hashEmail(\"\") = %q, want empty", got)
	}
}
