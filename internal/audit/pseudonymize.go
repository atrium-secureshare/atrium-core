package audit

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// hashLen is the number of hex characters kept from the SHA-256 sum, enough to
// distinguish recipients while keeping audit lines readable.
const hashLen = 16

// Canonical normalizes an email to NFKC and lower case, the single
// canonicalization used before hashing so the same address always maps to the
// same token. The tos acceptance cookie shares it.
func Canonical(email string) string {
	return strings.ToLower(norm.NFKC.String(email))
}

// hashEmail returns the short hex SHA-256 of the canonicalized email, optionally
// salted. An empty email hashes to "" so absent identities stay absent.
func hashEmail(salt []byte, email string) string {
	if email == "" {
		return ""
	}
	h := sha256.New()
	if len(salt) > 0 {
		h.Write(salt)
	}
	h.Write([]byte(Canonical(email)))
	return hex.EncodeToString(h.Sum(nil))[:hashLen]
}
