package httputil

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

const (
	headerForwardedFor = "X-Forwarded-For"
	headerRealIP       = "X-Real-IP"

	testForwardedIP = "203.0.113.5"
	testRealIP      = "198.51.100.7"
	testRemoteIP    = "10.0.0.1"
	testRemoteAddr  = testRemoteIP + ":1234"
)

func TestClientIP(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		headers    map[string]string
		remoteAddr string
		want       string
	}{
		{
			name:       "X-Forwarded-For single entry",
			headers:    map[string]string{headerForwardedFor: testForwardedIP},
			remoteAddr: testRemoteAddr,
			want:       testForwardedIP,
		},
		{
			name:       "X-Forwarded-For multiple entries uses first",
			headers:    map[string]string{headerForwardedFor: testForwardedIP + ", 10.0.0.2, 10.0.0.3"},
			remoteAddr: testRemoteAddr,
			want:       testForwardedIP,
		},
		{
			name:       "X-Real-IP used when no X-Forwarded-For",
			headers:    map[string]string{headerRealIP: testRealIP},
			remoteAddr: testRemoteAddr,
			want:       testRealIP,
		},
		{
			name:       "X-Forwarded-For preferred over X-Real-IP",
			headers:    map[string]string{headerForwardedFor: testForwardedIP, headerRealIP: testRealIP},
			remoteAddr: testRemoteAddr,
			want:       testForwardedIP,
		},
		{
			name:       "falls back to RemoteAddr with port",
			remoteAddr: testRemoteAddr,
			want:       testRemoteIP,
		},
		{
			name:       "falls back to RemoteAddr without port",
			remoteAddr: testRemoteIP,
			want:       testRemoteIP,
		},
		{
			name:       "invalid X-Forwarded-For falls back to RemoteAddr",
			headers:    map[string]string{headerForwardedFor: "not-an-ip"},
			remoteAddr: testRemoteAddr,
			want:       testRemoteIP,
		},
		{
			name:       "IPv6 RemoteAddr",
			remoteAddr: "[::1]:1234",
			want:       "::1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
			r.RemoteAddr = tt.remoteAddr

			for k, v := range tt.headers {
				r.Header.Set(k, v)
			}

			got := ClientIP(r)

			want := net.ParseIP(tt.want)
			if !got.Equal(want) {
				t.Errorf("ClientIP() = %v, want %v", got, want)
			}
		})
	}

	t.Run("unparseable RemoteAddr with no headers returns nil", func(t *testing.T) {
		t.Parallel()

		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
		r.RemoteAddr = "not-an-address:not-a-port:extra"

		if got := ClientIP(r); got != nil {
			t.Errorf("ClientIP() = %v, want nil", got)
		}
	})
}

func TestIPInSubnets(t *testing.T) {
	t.Parallel()

	mustCIDR := func(s string) *net.IPNet {
		_, n, err := net.ParseCIDR(s)
		if err != nil {
			t.Fatalf("ParseCIDR(%q): %v", s, err)
		}

		return n
	}

	tests := []struct {
		name    string
		ip      net.IP
		subnets []*net.IPNet
		want    bool
	}{
		{
			name:    "empty subnets is unrestricted",
			ip:      net.ParseIP("203.0.113.5"),
			subnets: nil,
			want:    true,
		},
		{
			name:    "nil IP never matches non-empty subnets",
			ip:      nil,
			subnets: []*net.IPNet{mustCIDR("10.0.0.0/8")},
			want:    false,
		},
		{
			name:    "IPv4 match",
			ip:      net.ParseIP("10.1.2.3"),
			subnets: []*net.IPNet{mustCIDR("10.0.0.0/8")},
			want:    true,
		},
		{
			name:    "IPv4 no match",
			ip:      net.ParseIP("192.168.1.1"),
			subnets: []*net.IPNet{mustCIDR("10.0.0.0/8")},
			want:    false,
		},
		{
			name:    "matches second of multiple subnets",
			ip:      net.ParseIP("192.168.1.1"),
			subnets: []*net.IPNet{mustCIDR("10.0.0.0/8"), mustCIDR("192.168.0.0/16")},
			want:    true,
		},
		{
			name:    "IPv6 match",
			ip:      net.ParseIP("::1"),
			subnets: []*net.IPNet{mustCIDR("::1/128")},
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := IPInSubnets(tt.ip, tt.subnets); got != tt.want {
				t.Errorf("IPInSubnets() = %v, want %v", got, tt.want)
			}
		})
	}
}
