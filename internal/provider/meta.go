package provider

import (
	"context"
	"net"
	"net/http"
)

// ClientMeta carries the recipient's network origin into provider tokens as audit
// context. It is advisory (the provider authorizes on the signed claims), so both
// fields are optional.
type ClientMeta struct {
	// IP is the recipient's source address (host only, no port).
	IP string
	// XFF is the verbatim X-Forwarded-For header, preserving the proxy chain.
	XFF string
}

type metaContextKey struct{}

// ContextWithMeta returns a copy of ctx carrying meta, so downstream provider
// calls can attach it to the request token.
func ContextWithMeta(ctx context.Context, meta ClientMeta) context.Context {
	return context.WithValue(ctx, metaContextKey{}, meta)
}

// MetaFromContext returns the ClientMeta stored in ctx, or the zero value.
func MetaFromContext(ctx context.Context) ClientMeta {
	meta, _ := ctx.Value(metaContextKey{}).(ClientMeta)
	return meta
}

// MetaFromRequest extracts the audit metadata (source IP, X-Forwarded-For) from
// an inbound request.
func MetaFromRequest(r *http.Request) ClientMeta {
	ip := r.RemoteAddr
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		ip = host
	}
	return ClientMeta{IP: ip, XFF: r.Header.Get("X-Forwarded-For")}
}
