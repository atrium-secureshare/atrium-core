package api

import (
	"net/http"
	"strings"
)

// hstsValue is the Strict-Transport-Security policy (one year, subdomains). It is
// emitted only over TLS, so a browser is never told to force HTTPS over plain http.
const hstsValue = "max-age=31536000; includeSubDomains"

// securityHeaders sets the gateway's HTTP security headers on every response.
// Strict-Transport-Security is added only when tls is true, since HSTS is
// meaningful only over a secure transport.
func securityHeaders(next http.Handler, csp string, tls bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", csp)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		if tls {
			h.Set("Strict-Transport-Security", hstsValue)
		}
		next.ServeHTTP(w, r)
	})
}

// contentSecurityPolicy builds the CSP. Everything is same-origin, so the base is
// default-src 'self'. Inline scripts are allowed only by the shell's script
// hashes, never 'unsafe-inline'; styles keep 'unsafe-inline' because the app sets
// dynamic style attributes no static hash can cover.
func contentSecurityPolicy(scriptHashes []string) string {
	scriptSrc := "script-src 'self'"
	if len(scriptHashes) > 0 {
		scriptSrc += " " + strings.Join(scriptHashes, " ")
	}
	return strings.Join([]string{
		"default-src 'self'",
		scriptSrc,
		"style-src 'self' 'unsafe-inline'",
		"object-src 'none'",
		"base-uri 'none'",
		"frame-ancestors 'none'",
	}, "; ")
}
