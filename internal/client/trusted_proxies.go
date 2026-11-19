package client

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// defaultTrustedProxies is MEMEINDEX_TRUSTED_PROXIES when it is unset:
// loopback, plus the cloudflared container from the bundled compose file,
// found by its service name. cloudflared reaches the app over the compose
// network, so the app sees that container's own address. Trusting the whole
// private range instead (the old behaviour) also trusted the Docker gateway,
// which is where a connection to the published port appears to come from, so
// anyone who could reach the port directly could pick their own rate-limit
// bucket.
const defaultTrustedProxies = "127.0.0.0/8,::1/128,cloudflared"

const (
	// Hostname entries are re-resolved this often while they keep matching,
	// so a recreated cloudflared container is picked up on its new address.
	trustedProxyResolveTTL = 30 * time.Second
	// A peer that matches nothing triggers an early re-resolve, at most this
	// often, so a tunnel that starts after the app (compose waits for the app
	// to be healthy first) is trusted within seconds rather than a full TTL.
	// The floor stops direct clients sending a forged header from turning
	// every request into a DNS lookup.
	trustedProxyMissRefresh = 5 * time.Second
	// Bounds the one request that waits on a lookup.
	trustedProxyLookupTimeout = 2 * time.Second
)

// trustedProxyHostname matches a DNS name or compose service name. Underscores
// are allowed because compose service names may contain them.
var trustedProxyHostname = regexp.MustCompile(`^[A-Za-z0-9_]([A-Za-z0-9_-]{0,61}[A-Za-z0-9_])?(\.[A-Za-z0-9_]([A-Za-z0-9_-]{0,61}[A-Za-z0-9_])?)*\.?$`)

// trustedProxies is the set of peers (MEMEINDEX_TRUSTED_PROXIES) whose
// forwarded client-IP header clientIP believes. Entries are CIDR ranges, bare
// addresses, or hostnames. Hostnames are resolved lazily and cached, because
// the proxy they name (cloudflared in compose) may start after the app and
// get a new address whenever its container is recreated. A nil set trusts
// nobody.
type trustedProxies struct {
	prefixes []netip.Prefix
	hosts    []string

	lookup func(ctx context.Context, host string) ([]netip.Addr, error) // overridable in tests
	now    func() time.Time                                             // overridable in tests

	mu         sync.Mutex
	resolved   []netip.Addr
	resolvedAt time.Time
	refreshing atomic.Bool
}

// parseTrustedProxies builds the set from MEMEINDEX_TRUSTED_PROXIES entries,
// rejecting anything that is not a CIDR range, an address or a hostname so a
// typo fails at startup instead of silently trusting nothing.
func parseTrustedProxies(entries []string) (*trustedProxies, error) {
	t := &trustedProxies{
		lookup: func(ctx context.Context, host string) ([]netip.Addr, error) {
			return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		},
		now: time.Now,
	}
	for _, raw := range entries {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			continue
		}
		if prefix, err := netip.ParsePrefix(entry); err == nil {
			t.prefixes = append(t.prefixes, prefix.Masked())
			continue
		}
		if addr, err := netip.ParseAddr(entry); err == nil {
			addr = addr.Unmap().WithZone("")
			t.prefixes = append(t.prefixes, netip.PrefixFrom(addr, addr.BitLen()))
			continue
		}
		if validTrustedProxyHostname(entry) {
			t.hosts = append(t.hosts, strings.TrimSuffix(entry, "."))
			continue
		}
		return nil, fmt.Errorf("%q is not a CIDR range, IP address or hostname", entry)
	}
	return t, nil
}

// validTrustedProxyHostname accepts a DNS name, but not one whose last label
// is all digits: that is a mistyped address such as 10.0.0.300, which would
// otherwise be looked up as a name and quietly trust nothing.
func validTrustedProxyHostname(entry string) bool {
	if len(entry) > 253 || !trustedProxyHostname.MatchString(entry) {
		return false
	}
	labels := strings.Split(strings.TrimSuffix(entry, "."), ".")
	return strings.Trim(labels[len(labels)-1], "0123456789") != ""
}

// contains reports whether addr is one of the trusted proxies.
func (t *trustedProxies) contains(addr netip.Addr) bool {
	if t == nil || !addr.IsValid() {
		return false
	}
	addr = addr.Unmap().WithZone("")
	for _, prefix := range t.prefixes {
		if prefix.Contains(addr) {
			return true
		}
	}
	if len(t.hosts) == 0 {
		return false
	}

	hit, age := t.matchResolved(addr)
	if hit && age < trustedProxyResolveTTL {
		return true
	}
	if !hit && age < trustedProxyMissRefresh {
		return false
	}
	// One request re-resolves while the rest carry on with what is cached
	// rather than queueing behind the lookup.
	if !t.refreshing.CompareAndSwap(false, true) {
		return hit
	}
	defer t.refreshing.Store(false)
	t.resolve()
	hit, _ = t.matchResolved(addr)
	return hit
}

// matchResolved checks addr against the last resolution of the hostname
// entries and reports how old that resolution is.
func (t *trustedProxies) matchResolved(addr netip.Addr) (bool, time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	age := t.now().Sub(t.resolvedAt)
	for _, candidate := range t.resolved {
		if candidate == addr {
			return true, age
		}
	}
	return false, age
}

// resolve looks every hostname entry up again. A name that does not resolve
// (the tunnel profile is not running) contributes nothing, so its old address
// stops being trusted.
func (t *trustedProxies) resolve() {
	ctx, cancel := context.WithTimeout(context.Background(), trustedProxyLookupTimeout)
	defer cancel()
	var resolved []netip.Addr
	for _, host := range t.hosts {
		addrs, err := t.lookup(ctx, host)
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			resolved = append(resolved, addr.Unmap().WithZone(""))
		}
	}
	t.mu.Lock()
	t.resolved = resolved
	t.resolvedAt = t.now()
	t.mu.Unlock()
}

// forwardedClientIP picks the client address out of a forwarding header sent
// by a trusted proxy. Header values are read right to left, skipping hops that
// are themselves trusted proxies: each proxy appends the address it saw, so the
// rightmost untrusted entry is the last one a trusted party vouched for, and
// anything left of it may have been made up by the client. A single-value
// header such as CF-Connecting-IP is just that value. An unparseable entry
// stops the walk, since nothing beyond it can be attributed.
func forwardedClientIP(values []string, trusted *trustedProxies) (string, bool) {
	var hops []string
	for _, value := range values {
		hops = append(hops, strings.Split(value, ",")...)
	}
	var leftmost netip.Addr
	for i := len(hops) - 1; i >= 0; i-- {
		hop := strings.TrimSpace(hops[i])
		if hop == "" {
			continue
		}
		addr, err := netip.ParseAddr(hop)
		if err != nil {
			return "", false
		}
		addr = addr.Unmap().WithZone("")
		if !trusted.contains(addr) {
			return addr.String(), true
		}
		leftmost = addr
	}
	// Every hop was a trusted proxy: the first one is as close to the client
	// as the chain gets.
	if leftmost.IsValid() {
		return leftmost.String(), true
	}
	return "", false
}
