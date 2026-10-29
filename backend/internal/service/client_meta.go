package service

import "context"

type clientMetaKey struct{}

// WithClientMeta records where a request came from, for code that limits or
// logs by sender without taking the request as a parameter.
func WithClientMeta(ctx context.Context, meta ClientMeta) context.Context {
	return context.WithValue(ctx, clientMetaKey{}, meta)
}

// ClientMetaFromContext returns the sender recorded by WithClientMeta, or
// the zero value when none was.
func ClientMetaFromContext(ctx context.Context) ClientMeta {
	meta, _ := ctx.Value(clientMetaKey{}).(ClientMeta)
	return meta
}
