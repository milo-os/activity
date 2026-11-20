// Package filterparam passes the `filter` query parameter to REST handlers.
//
// The generic apiserver decodes list and watch parameters into ListOptions and
// drops unknown ones, so handlers can't read `filter` from the request.
package filterparam

import (
	"context"
	"net/http"
)

type contextKey struct{}

// Handler stores the request's `filter` query parameter in its context.
func Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if f := r.URL.Query().Get("filter"); f != "" {
			r = r.WithContext(NewContext(r.Context(), f))
		}
		next.ServeHTTP(w, r)
	})
}

// NewContext returns a copy of ctx carrying filter.
func NewContext(ctx context.Context, filter string) context.Context {
	return context.WithValue(ctx, contextKey{}, filter)
}

// FromContext returns the filter stored in ctx, or "" if there is none.
func FromContext(ctx context.Context) string {
	f, _ := ctx.Value(contextKey{}).(string)
	return f
}
