// Package signedcookie seals and opens HMAC-SHA256 authenticated values, the
// single primitive behind every signed cookie in the gateway so the envelope
// lives in one place.
package signedcookie

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
)

// ErrInvalid indicates a malformed or tampered token.
var ErrInvalid = errors.New("invalid signed cookie")

// Seal returns "base64url(payload).base64url(HMAC-SHA256(payload, key))".
func Seal(key, payload []byte) string {
	enc := base64.RawURLEncoding
	return enc.EncodeToString(payload) + "." + enc.EncodeToString(mac(key, payload))
}

// Open verifies the signature in constant time and returns the payload, or
// ErrInvalid if the token is malformed or the signature does not match.
func Open(key []byte, token string) ([]byte, error) {
	payloadPart, sigPart, ok := strings.Cut(token, ".")
	if !ok {
		return nil, ErrInvalid
	}
	enc := base64.RawURLEncoding
	payload, err := enc.DecodeString(payloadPart)
	if err != nil {
		return nil, ErrInvalid
	}
	sig, err := enc.DecodeString(sigPart)
	if err != nil {
		return nil, ErrInvalid
	}
	if !hmac.Equal(sig, mac(key, payload)) {
		return nil, ErrInvalid
	}
	return payload, nil
}

// mac returns the HMAC-SHA256 of payload under key.
func mac(key, payload []byte) []byte {
	m := hmac.New(sha256.New, key)
	m.Write(payload)
	return m.Sum(nil)
}
