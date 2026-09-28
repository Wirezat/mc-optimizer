package api

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

var trustedProxies []netip.Prefix

// ConfigureTrustedProxies parses a comma-separated list of IPs or CIDRs whose
// X-Forwarded-For headers are believed. Empty means no proxy is trusted.
func ConfigureTrustedProxies(spec string) error {
	var out []netip.Prefix
	for part := range strings.SplitSeq(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if p, err := netip.ParsePrefix(part); err == nil {
			out = append(out, p.Masked())
			continue
		}
		a, err := netip.ParseAddr(part)
		if err != nil {
			return fmt.Errorf("TRUSTED_PROXIES: invalid entry %q", part)
		}
		a = a.Unmap()
		out = append(out, netip.PrefixFrom(a, a.BitLen()))
	}
	trustedProxies = out
	return nil
}

func isTrustedProxy(s string) bool {
	a, err := netip.ParseAddr(strings.TrimSpace(s))
	if err != nil {
		return false
	}
	a = a.Unmap()
	for _, p := range trustedProxies {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// clientIP returns the address of the real client: the TCP peer, or, when the
// peer is a trusted proxy, the rightmost untrusted X-Forwarded-For entry.
func clientIP(r *http.Request) string {
	peer, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		peer = r.RemoteAddr
	}
	if !isTrustedProxy(peer) {
		return peer
	}
	hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	for i := len(hops) - 1; i >= 0; i-- {
		hop := strings.TrimSpace(hops[i])
		if hop == "" {
			continue
		}
		if !isTrustedProxy(hop) {
			return hop
		}
		peer = hop
	}
	return peer
}
