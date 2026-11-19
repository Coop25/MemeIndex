package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// testTrustedProxies parses a comma-separated MEMEINDEX_TRUSTED_PROXIES value
// with hostname lookups answered from hosts instead of DNS. A name missing from
// hosts does not resolve.
func testTrustedProxies(t *testing.T, value string, hosts map[string][]string) *trustedProxies {
	t.Helper()
	trusted, err := parseTrustedProxies(strings.Split(value, ","))
	if err != nil {
		t.Fatalf("parseTrustedProxies(%q): %v", value, err)
	}
	trusted.lookup = func(_ context.Context, host string) ([]netip.Addr, error) {
		var addrs []netip.Addr
		for _, raw := range hosts[host] {
			addrs = append(addrs, netip.MustParseAddr(raw))
		}
		if len(addrs) == 0 {
			return nil, errors.New("no such host")
		}
		return addrs, nil
	}
	return trusted
}

func TestParseTrustedProxiesAcceptsRangesAddressesAndHostnames(t *testing.T) {
	trusted := testTrustedProxies(t, " 10.1.0.0/16 , 192.0.2.7,2001:db8::/32,::ffff:198.51.100.1, cloudflared,proxy.internal ,,", map[string][]string{
		"cloudflared":    {"172.18.0.5"},
		"proxy.internal": {"fd00::9"},
	})
	for _, addr := range []string{"10.1.200.3", "192.0.2.7", "2001:db8::1", "198.51.100.1", "::ffff:10.1.0.1", "172.18.0.5", "fd00::9"} {
		if !trusted.contains(netip.MustParseAddr(addr)) {
			t.Errorf("%s should be trusted", addr)
		}
	}
	for _, addr := range []string{"10.2.0.1", "192.0.2.8", "2001:db9::1", "172.18.0.1", "127.0.0.1"} {
		if trusted.contains(netip.MustParseAddr(addr)) {
			t.Errorf("%s should not be trusted", addr)
		}
	}

	for _, bad := range []string{"10.0.0.0/33", "10.0.0.300", "300", "not a host", "evil.example/24", "a..b"} {
		if _, err := parseTrustedProxies([]string{bad}); err == nil {
			t.Errorf("parseTrustedProxies(%q) succeeded, want an error", bad)
		}
	}

	if empty, err := parseTrustedProxies(nil); err != nil || empty.contains(netip.MustParseAddr("127.0.0.1")) {
		t.Errorf("an empty list must trust nobody (err %v)", err)
	}
	var none *trustedProxies
	if none.contains(netip.MustParseAddr("127.0.0.1")) {
		t.Error("a nil list must trust nobody")
	}
}

// cloudflared starts after the app (compose waits for the app to be healthy)
// and gets a new address whenever its container is recreated, so a hostname
// entry has to be looked up again rather than resolved once at startup - but
// not on every request a spoofing client sends.
func TestTrustedProxyHostnamesResolveLazilyAndRefresh(t *testing.T) {
	var mu sync.Mutex
	current := map[string][]string{}
	lookups := 0
	now := time.Unix(1_700_000_000, 0)

	trusted, err := parseTrustedProxies([]string{"cloudflared"})
	if err != nil {
		t.Fatal(err)
	}
	trusted.now = func() time.Time { return now }
	trusted.lookup = func(_ context.Context, host string) ([]netip.Addr, error) {
		mu.Lock()
		defer mu.Unlock()
		lookups++
		var addrs []netip.Addr
		for _, raw := range current[host] {
			addrs = append(addrs, netip.MustParseAddr(raw))
		}
		if len(addrs) == 0 {
			return nil, errors.New("no such host")
		}
		return addrs, nil
	}
	tunnel := netip.MustParseAddr("172.18.0.5")
	gateway := netip.MustParseAddr("172.18.0.1")

	// The tunnel is not running yet.
	if trusted.contains(tunnel) {
		t.Fatal("trusted an address before the name resolved to it")
	}
	if lookups != 1 {
		t.Fatalf("lookups = %d, want 1 on first use", lookups)
	}

	// A burst of forged headers shortly afterwards does not turn into a
	// lookup per request.
	for i := 0; i < 20; i++ {
		trusted.contains(gateway)
	}
	if lookups != 1 {
		t.Fatalf("lookups = %d after a burst of misses inside the refresh floor, want 1", lookups)
	}

	// cloudflared comes up; the next miss after the floor finds it.
	current["cloudflared"] = []string{"172.18.0.5"}
	now = now.Add(trustedProxyMissRefresh)
	if !trusted.contains(tunnel) {
		t.Fatal("cloudflared not trusted once it resolves")
	}
	if trusted.contains(gateway) {
		t.Fatal("the gateway became trusted")
	}
	lookupsBefore := lookups
	for i := 0; i < 20; i++ {
		if !trusted.contains(tunnel) {
			t.Fatal("cloudflared stopped being trusted inside the TTL")
		}
	}
	if lookups != lookupsBefore {
		t.Fatalf("hits inside the TTL looked the name up again (%d -> %d)", lookupsBefore, lookups)
	}

	// The container is recreated on a new address and the old one is
	// handed to something else.
	current["cloudflared"] = []string{"172.18.0.9"}
	now = now.Add(trustedProxyResolveTTL)
	if trusted.contains(tunnel) {
		t.Fatal("the old cloudflared address is still trusted after the TTL")
	}
	if !trusted.contains(netip.MustParseAddr("172.18.0.9")) {
		t.Fatal("the new cloudflared address is not trusted")
	}
}

func TestForwardedClientIPSkipsTrustedHopsFromTheRight(t *testing.T) {
	trusted := testTrustedProxies(t, "10.0.0.0/8", nil)
	for _, tc := range []struct {
		name   string
		values []string
		want   string
		ok     bool
	}{
		{"single value", []string{"203.0.113.7"}, "203.0.113.7", true},
		{"client-supplied prefix", []string{"6.6.6.6, 203.0.113.7"}, "203.0.113.7", true},
		{"inner proxy hop skipped", []string{"6.6.6.6, 203.0.113.7, 10.0.0.2"}, "203.0.113.7", true},
		{"repeated header lines", []string{"6.6.6.6", "203.0.113.7"}, "203.0.113.7", true},
		{"all hops trusted", []string{"10.0.0.3, 10.0.0.2"}, "10.0.0.3", true},
		{"ipv6", []string{"2001:db8::1"}, "2001:db8::1", true},
		{"ipv4-mapped", []string{"::ffff:203.0.113.7"}, "203.0.113.7", true},
		{"garbage", []string{"not-an-ip"}, "", false},
		{"garbage before a trusted hop", []string{"203.0.113.7, junk, 10.0.0.2"}, "", false},
		{"empty", []string{" , "}, "", false},
	} {
		got, ok := forwardedClientIP(tc.values, trusted)
		if got != tc.want || ok != tc.ok {
			t.Errorf("%s: forwardedClientIP(%q) = %q, %v; want %q, %v", tc.name, tc.values, got, ok, tc.want, tc.ok)
		}
	}
}

func TestClientIPHonoursConfiguredHeaderOnlyFromTrustedProxies(t *testing.T) {
	server := &Server{config: Config{
		ClientIPHeader: "X-Forwarded-For",
		TrustedProxies: testTrustedProxies(t, "127.0.0.0/8,::1/128,proxy", map[string][]string{"proxy": {"172.20.0.4"}}),
	}}
	request := func(remote, forwarded string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = remote
		r.Header.Set("X-Forwarded-For", forwarded)
		// Not the configured header, so it never counts, even from the proxy.
		r.Header.Set("CF-Connecting-IP", "192.0.2.99")
		return r
	}
	for _, tc := range []struct {
		name, remote, forwarded, want string
	}{
		{"named proxy", "172.20.0.4:5000", "198.51.100.7", "198.51.100.7"},
		{"named proxy, client prepended a fake hop", "172.20.0.4:5000", "1.1.1.1, 198.51.100.7", "198.51.100.7"},
		{"ipv6 loopback", "[::1]:5000", "198.51.100.7", "198.51.100.7"},
		{"ipv4-mapped loopback", "[::ffff:127.0.0.1]:5000", "198.51.100.7", "198.51.100.7"},
		{"untrusted private peer", "172.20.0.1:5000", "198.51.100.7", "172.20.0.1"},
		{"untrusted public peer", "203.0.113.9:5000", "198.51.100.7", "203.0.113.9"},
		{"trusted peer, malformed header", "172.20.0.4:5000", "nope", "172.20.0.4"},
	} {
		if got := server.clientIP(request(tc.remote, tc.forwarded)); got != tc.want {
			t.Errorf("%s: clientIP = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestLoadConfigTrustedProxies(t *testing.T) {
	t.Setenv("MEMEINDEX_ALLOW_ANONYMOUS", "true")
	t.Setenv("MEMEINDEX_DATABASE_URL", "postgres://memeindex@localhost/memeindex")
	t.Setenv("MEMEINDEX_TRUSTED_PROXIES", "")
	os.Unsetenv("MEMEINDEX_TRUSTED_PROXIES")

	field, ok := reflect.TypeOf(rawConfig{}).FieldByName("TrustedProxies")
	if !ok || field.Tag.Get("default") != defaultTrustedProxies {
		t.Fatalf("rawConfig.TrustedProxies default = %q, want defaultTrustedProxies %q", field.Tag.Get("default"), defaultTrustedProxies)
	}
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !cfg.TrustedProxies.contains(netip.MustParseAddr("127.0.0.1")) || !cfg.TrustedProxies.contains(netip.MustParseAddr("::1")) {
		t.Error("the default does not trust loopback")
	}
	if cfg.TrustedProxies.contains(netip.MustParseAddr("192.168.1.20")) || cfg.TrustedProxies.contains(netip.MustParseAddr("172.17.0.1")) {
		t.Error("the default trusts a private address that is not the tunnel")
	}
	if !reflect.DeepEqual(cfg.TrustedProxies.hosts, []string{"cloudflared"}) {
		t.Errorf("default hostnames = %q, want the cloudflared service", cfg.TrustedProxies.hosts)
	}

	t.Setenv("MEMEINDEX_TRUSTED_PROXIES", "10.0.0.0/8,10.0.0.300")
	if _, err := LoadConfig(); err == nil || !strings.Contains(err.Error(), "MEMEINDEX_TRUSTED_PROXIES") {
		t.Fatalf("LoadConfig with a bad entry: err = %v, want a MEMEINDEX_TRUSTED_PROXIES error", err)
	}
}
