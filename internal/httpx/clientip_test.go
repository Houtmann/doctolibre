package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"doctolibre/internal/config"
)

func TestTrustedProxiesClientIP(t *testing.T) {
	t.Parallel()
	trusted, err := config.ParseCIDRs("172.18.0.0/16, fd00::/8")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		remote string
		xff    []string
		want   string
	}{
		{"pair non fiable : XFF forgé ignoré", "203.0.113.9:4000", []string{"1.2.3.4"}, "203.0.113.9"},
		{"proxy de confiance, un client", "172.18.0.2:5555", []string{"198.51.100.7"}, "198.51.100.7"},
		{"client qui forge XFF derrière le proxy", "172.18.0.2:5555", []string{"1.1.1.1, 198.51.100.7"}, "198.51.100.7"},
		{"plusieurs en-têtes XFF", "172.18.0.2:5555", []string{"1.1.1.1", "198.51.100.7"}, "198.51.100.7"},
		{"proxy intermédiaire fiable", "172.18.0.2:5555", []string{"198.51.100.7, 172.18.0.9"}, "198.51.100.7"},
		{"tout est fiable : la plus à gauche", "172.18.0.2:5555", []string{"172.18.0.5, 172.18.0.9"}, "172.18.0.5"},
		{"espaces", "172.18.0.2:5555", []string{"  198.51.100.7  ,   172.18.0.9 "}, "198.51.100.7"},
		{"XFF invalide", "172.18.0.2:5555", []string{"n'importe quoi"}, "172.18.0.2"},
		{"XFF invalide dans une liste", "172.18.0.2:5555", []string{"198.51.100.7, zzz"}, "172.18.0.2"},
		{"pas de XFF", "172.18.0.2:5555", nil, "172.18.0.2"},
		{"IPv6 client", "172.18.0.2:5555", []string{"2001:db8::1"}, "2001:db8::1"},
		{"IPv6 proxy de confiance", "[fd00::2]:5555", []string{"2001:db8::1"}, "2001:db8::1"},
		{"IPv4 mappée en IPv6", "172.18.0.2:5555", []string{"::ffff:198.51.100.7"}, "198.51.100.7"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var got string
			h := TrustedProxies(trusted)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = ClientIP(r) }))
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tt.remote
			for _, v := range tt.xff {
				req.Header.Add("X-Forwarded-For", v)
			}
			req.Header.Set("X-Real-IP", "9.9.9.9")
			h.ServeHTTP(httptest.NewRecorder(), req)
			if got != tt.want {
				t.Fatalf("ClientIP = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestClientIPWithoutMiddleware(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "172.18.0.2:1"
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	if got := ClientIP(req); got != "172.18.0.2" {
		t.Fatalf("got %q", got)
	}
}
