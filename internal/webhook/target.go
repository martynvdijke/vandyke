package webhook

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ErrTargetDenied is returned when a target resolves to an address range the
// endpoint is not allowed to reach.
var ErrTargetDenied = errors.New("target address is not allowed")

// ValidateTarget checks an endpoint URL at save and test time: the scheme must
// be http or https, credentials are forbidden, and every resolved address must
// be permitted. A DNS failure does not block a save because a LAN receiver may
// be temporarily offline; the dialer re-checks at delivery time.
func ValidateTarget(ctx context.Context, raw string, allowPrivate bool) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return errors.New("URL must be an absolute http or https URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("URL must use http or https")
	}
	if parsed.User != nil {
		return errors.New("URL must not contain credentials")
	}
	host := parsed.Hostname()
	if host == "" {
		return errors.New("URL must include a host")
	}
	if ip := net.ParseIP(host); ip != nil {
		if !IsAddressAllowed(ip, allowPrivate) {
			return ErrTargetDenied
		}
		return nil
	}

	lookupCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupIPAddr(lookupCtx, host)
	if err != nil {
		return nil
	}
	for _, addr := range addrs {
		if !IsAddressAllowed(addr.IP, allowPrivate) {
			return ErrTargetDenied
		}
	}
	return nil
}

// IsAddressAllowed applies the SSRF range policy. Loopback and private ranges
// require allowPrivate; link-local, unspecified and multicast are never
// allowed. IPv4-mapped IPv6 addresses are treated as their IPv4 equivalent.
func IsAddressAllowed(ip net.IP, allowPrivate bool) bool {
	if ip == nil {
		return false
	}
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	} else if v6 := ip.To16(); v6 != nil {
		ip = v6
	} else {
		return false
	}

	if ip.IsUnspecified() || ip.IsMulticast() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return false
	}

	// The metadata service and every other link-local target stay blocked even
	// when the endpoint opts into private addresses.
	if v4 := ip.To4(); v4 != nil && v4[0] == 169 && v4[1] == 254 {
		return false
	}

	if ip.IsLoopback() || ip.IsPrivate() {
		return allowPrivate
	}
	// Carrier-grade NAT 100.64.0.0/10, which covers Tailscale addresses.
	if v4 := ip.To4(); v4 != nil && v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127 {
		return allowPrivate
	}
	return true
}

// TargetHost returns the URL host, never the path, query or userinfo.
func TargetHost(raw string) string { return hostOf(raw) }

// SanitiseError turns a delivery error into a short reason prefixed by the
// target host. Paths, query strings and provider tokens are never included.
func SanitiseError(err error, raw string) string {
	reason := "request failed"
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrTargetDenied):
		reason = "target address denied"
	case errors.Is(err, context.DeadlineExceeded):
		reason = "request timed out"
	default:
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			reason = "request timed out"
		}
	}
	if host := TargetHost(raw); host != "" {
		return host + ": " + reason
	}
	return reason
}

// newHTTPClient builds a client that resolves the host itself, refuses the
// attempt when any resolved address is disallowed, and dials the validated
// address while the Transport keeps the URL host for TLS SNI and certificate
// verification. Redirects are not followed.
func newHTTPClient(allowPrivate bool) *http.Client {
	dialer := &net.Dialer{Timeout: Timeout}
	transport := &http.Transport{
		Proxy:                 nil,
		DisableKeepAlives:     true,
		TLSHandshakeTimeout:   Timeout,
		ResponseHeaderTimeout: Timeout,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, fmt.Errorf("split target address: %w", err)
			}
			addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, err
			}
			var chosen net.IP
			for _, candidate := range addrs {
				if !IsAddressAllowed(candidate.IP, allowPrivate) {
					return nil, ErrTargetDenied
				}
				if chosen == nil {
					chosen = candidate.IP
				}
			}
			if chosen == nil {
				return nil, errors.New("target resolved to no address")
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(chosen.String(), port))
		},
	}
	return &http.Client{
		Transport: transport,
		Timeout:   Timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}
