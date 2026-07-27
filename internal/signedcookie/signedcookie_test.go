package signedcookie

import (
	"bytes"
	"testing"
)

var testKey = []byte("0123456789abcdef0123456789abcdef")

func TestSealOpenRoundTrip(t *testing.T) {
	payload := []byte(`{"hello":"world"}`)
	got, err := Open(testKey, Seal(testKey, payload))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("payload = %q, want %q", got, payload)
	}
}

func TestOpenRejectsTampered(t *testing.T) {
	token := Seal(testKey, []byte("payload"))
	tampered := "A" + token[1:]
	if _, err := Open(testKey, tampered); err != ErrInvalid {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
}

func TestOpenRejectsWrongKey(t *testing.T) {
	token := Seal(testKey, []byte("payload"))
	other := []byte("ffffffffffffffffffffffffffffffff")
	if _, err := Open(other, token); err != ErrInvalid {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
}

func TestOpenRejectsMalformed(t *testing.T) {
	for _, tok := range []string{"", "no-dot", "not base64!.sig", "cGF5.bad sig"} {
		if _, err := Open(testKey, tok); err != ErrInvalid {
			t.Errorf("Open(%q) err = %v, want ErrInvalid", tok, err)
		}
	}
}
