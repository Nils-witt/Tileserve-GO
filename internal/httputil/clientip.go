package httputil

import (
	"net"
	"net/http"
	"strings"
)

// ClientIP returns the request's client IP, preferring the X-Forwarded-For
// header (its first, left-most entry — the original client, assuming a
// trusted reverse proxy appends rather than lets clients spoof it), then
// X-Real-IP, and finally falling back to the underlying connection's
// RemoteAddr. It returns nil if no candidate parses as an IP address.
func ClientIP(r *http.Request) net.IP {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		first, _, _ := strings.Cut(fwd, ",")

		if ip := net.ParseIP(strings.TrimSpace(first)); ip != nil {
			return ip
		}
	}

	if realIP := r.Header.Get("X-Real-IP"); realIP != "" {
		if ip := net.ParseIP(strings.TrimSpace(realIP)); ip != nil {
			return ip
		}
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return net.ParseIP(r.RemoteAddr)
	}

	return net.ParseIP(host)
}

// IPInSubnets reports whether ip falls within one of subnets. An empty
// subnets list means unrestricted and always reports true; a nil ip never
// matches a non-empty list.
func IPInSubnets(ip net.IP, subnets []*net.IPNet) bool {
	if len(subnets) == 0 {
		return true
	}

	if ip == nil {
		return false
	}

	for _, subnet := range subnets {
		if subnet.Contains(ip) {
			return true
		}
	}

	return false
}
