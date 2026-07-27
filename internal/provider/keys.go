package provider

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/x509"
	"encoding/pem"
	"fmt"
)

// LoadPrivateKey decodes a PEM ECDSA P-256 private key (SEC1 or PKCS#8). The
// curve must be P-256 to match ES256; any other curve is rejected so
// misconfiguration fails fast at startup.
func LoadPrivateKey(pemStr string) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, fmt.Errorf("provider private key: no PEM block found")
	}

	key, err := parseECPrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("provider private key: %w", err)
	}
	if key.Curve != elliptic.P256() {
		return nil, fmt.Errorf("provider private key: curve must be P-256 for ES256, got %s", key.Curve.Params().Name)
	}
	return key, nil
}

// parseECPrivateKey parses DER key bytes as SEC1 first, then PKCS#8, returning
// the ECDSA key or an error when the bytes are neither an EC key.
func parseECPrivateKey(der []byte) (*ecdsa.PrivateKey, error) {
	if key, err := x509.ParseECPrivateKey(der); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return nil, fmt.Errorf("not a valid SEC1 or PKCS#8 EC key: %w", err)
	}
	key, ok := parsed.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("not an ECDSA key: %T", parsed)
	}
	return key, nil
}

// PublicKeyPEM returns the PKIX/SPKI PEM of key's public half — the value an
// operator installs on the provider to establish the trust relationship.
func PublicKeyPEM(key *ecdsa.PrivateKey) (string, error) {
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return "", fmt.Errorf("marshal provider public key: %w", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})), nil
}
