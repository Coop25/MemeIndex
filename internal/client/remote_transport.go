package client

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

// Match DNS label boundaries, not the downloader dependency's substring match.
func supportedSourceURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	if port := u.Port(); port != "" && !((u.Scheme == "http" && port == "80") || (u.Scheme == "https" && port == "443")) {
		return false
	}
	host := strings.ToLower(u.Hostname())
	for _, domain := range []string{"facebook.com", "fb.com", "fb.watch", "instagram.com", "instagr.am", "tiktok.com", "twitter.com", "x.com", "youtube.com", "youtu.be", "reddit.com", "redd.it"} {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return true
		}
	}
	return false
}

var nonPublicRanges = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"), netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("100::/64"), netip.MustParsePrefix("2001::/32"),
	netip.MustParsePrefix("2001:db8::/32"), netip.MustParsePrefix("2002::/16"),
}

func publicRemoteIP(ip net.IP) bool {
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	addr = addr.Unmap()
	for _, prefix := range nonPublicRanges {
		if prefix.Contains(addr) {
			return false
		}
	}
	return true
}

type publicRemoteDialer struct {
	lookup func(context.Context, string) ([]net.IPAddr, error)
	dial   func(context.Context, string, string) (net.Conn, error)
}

func (d publicRemoteDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	addresses, err := d.lookup(ctx, host)
	if err != nil {
		return nil, err
	}
	if len(addresses) == 0 {
		return nil, errors.New("remote host did not resolve")
	}
	for _, candidate := range addresses {
		if candidate.Zone != "" || !publicRemoteIP(candidate.IP) {
			return nil, errors.New("private and reserved network addresses are not allowed")
		}
	}
	for _, candidate := range addresses {
		// Dial the validated literal; never perform a second DNS lookup.
		conn, dialErr := d.dial(ctx, network, net.JoinHostPort(candidate.IP.String(), port))
		if dialErr == nil {
			return conn, nil
		}
		err = dialErr
	}
	return nil, err
}

func newPublicRemoteTransport() *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	transport.DialContext = (publicRemoteDialer{lookup: net.DefaultResolver.LookupIPAddr, dial: dialer.DialContext}).DialContext
	return transport
}
