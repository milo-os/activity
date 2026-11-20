package filterparam

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestHandler(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  string
	}{
		{name: "filter present", query: "watch=true&filter=" + url.QueryEscape("spec.resource.name == 'a'"), want: "spec.resource.name == 'a'"},
		{name: "filter absent", query: "watch=true", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got string
			h := Handler(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				got = FromContext(r.Context())
			}))
			h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/apis/activity.miloapis.com/v1alpha1/activities?"+tt.query, nil))

			if got != tt.want {
				t.Errorf("FromContext() = %q, want %q", got, tt.want)
			}
		})
	}
}
