package tos

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"

	"github.com/atrium-secureshare/atrium-core/internal/audit"
	"github.com/atrium-secureshare/atrium-core/internal/signedcookie"
)

// acceptanceTTL is the lifetime of the acceptance cookie. Consent is durable;
// re-consent is driven instead by a change to the ToS content hash.
const acceptanceTTL = 365 * 24 * time.Hour

// tosCookieData is the payload of the signed acceptance cookie. The recipient's
// email is stored only as a hash so the cookie carries no plaintext PII.
type tosCookieData struct {
	EmailHash  string    `json:"eh"`
	TOSHash    string    `json:"th"`
	AcceptedAt time.Time `json:"at"`
}

// CheckAcceptance reports whether the request carries a valid acceptance cookie
// for the given email and current ToS. Any failure returns false, forcing
// (re-)consent.
func (m *Manager) CheckAcceptance(r *http.Request, email string) bool {
	c, err := r.Cookie(acceptanceCookieName)
	if err != nil {
		return false
	}
	data, err := m.verifyCookie(c.Value)
	if err != nil {
		return false
	}
	return hmac.Equal([]byte(data.EmailHash), []byte(hashEmail(email))) &&
		hmac.Equal([]byte(data.TOSHash), []byte(m.contentHash))
}

// setAcceptanceCookie writes a signed, long-lived acceptance cookie binding the
// email hash to the current ToS content hash.
func (m *Manager) setAcceptanceCookie(w http.ResponseWriter, email string, now time.Time) error {
	token, err := m.signCookie(tosCookieData{
		EmailHash:  hashEmail(email),
		TOSHash:    m.contentHash,
		AcceptedAt: now.UTC(),
	})
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     acceptanceCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(acceptanceTTL.Seconds()),
		HttpOnly: true,
		Secure:   m.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

// signCookie serializes data to JSON and seals it with the shared signing key.
func (m *Manager) signCookie(data tosCookieData) (string, error) {
	payload, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	return signedcookie.Seal(m.signingKey, payload), nil
}

// verifyCookie reverses signCookie, verifying the signature before decoding.
func (m *Manager) verifyCookie(token string) (tosCookieData, error) {
	payload, err := signedcookie.Open(m.signingKey, token)
	if err != nil {
		return tosCookieData{}, err
	}
	var data tosCookieData
	if err := json.Unmarshal(payload, &data); err != nil {
		return tosCookieData{}, err
	}
	return data, nil
}

// hashEmail returns the full hex SHA-256 of the canonicalized email, using the
// same canonicalization as the audit trail (audit.Canonical) so the two agree.
func hashEmail(email string) string {
	sum := sha256.Sum256([]byte(audit.Canonical(email)))
	return hex.EncodeToString(sum[:])
}
