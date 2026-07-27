package provider

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"strings"
	"testing"
)

func rsaPKCS8(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal RSA PKCS#8: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
}

func sec1PEM(t *testing.T, key *ecdsa.PrivateKey) string {
	t.Helper()
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal SEC1: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}))
}

func pkcs8PEM(t *testing.T, key *ecdsa.PrivateKey) string {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal PKCS#8: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
}

func newP256(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return key
}

func TestLoadPrivateKeyAcceptsP256(t *testing.T) {
	key := newP256(t)
	for name, encoded := range map[string]string{
		"SEC1":   sec1PEM(t, key),
		"PKCS#8": pkcs8PEM(t, key),
	} {
		t.Run(name, func(t *testing.T) {
			got, err := LoadPrivateKey(encoded)
			if err != nil {
				t.Fatalf("LoadPrivateKey: %v", err)
			}
			if !got.Equal(key) {
				t.Error("loaded key does not equal the original")
			}
		})
	}
}

func TestLoadPrivateKeyRejectsWrongCurve(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatalf("generate P-384: %v", err)
	}
	if _, err := LoadPrivateKey(sec1PEM(t, key)); err == nil {
		t.Fatal("expected error for non-P-256 curve")
	}
}

func TestLoadPrivateKeyRejectsGarbage(t *testing.T) {
	for name, in := range map[string]string{
		"empty":     "",
		"no PEM":    "not a pem",
		"bad DER":   "-----BEGIN EC PRIVATE KEY-----\nZm9v\n-----END EC PRIVATE KEY-----",
		"RSA PKCS8": rsaPKCS8(t),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := LoadPrivateKey(in); err == nil {
				t.Fatalf("expected error for %s", name)
			}
		})
	}
}

func TestPublicKeyPEMRoundTrips(t *testing.T) {
	key := newP256(t)
	pemStr, err := PublicKeyPEM(key)
	if err != nil {
		t.Fatalf("PublicKeyPEM: %v", err)
	}
	if !strings.Contains(pemStr, "BEGIN PUBLIC KEY") {
		t.Errorf("expected SPKI PUBLIC KEY block, got %q", pemStr)
	}
	if pub := parsePublicKeyPEM(t, pemStr); !key.PublicKey.Equal(pub) {
		t.Error("round-tripped public key does not match")
	}
}
