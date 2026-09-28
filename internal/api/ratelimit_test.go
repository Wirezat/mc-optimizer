package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClientIP(t *testing.T) {
	cases := []struct {
		name    string
		trusted string
		remote  string
		xff     []string
		want    string
	}{
		{"no proxy trusted ignores header", "", "203.0.113.5:1234", []string{"1.2.3.4"}, "203.0.113.5"},
		{"untrusted peer ignores header", "10.0.0.0/8", "203.0.113.5:1234", []string{"1.2.3.4"}, "203.0.113.5"},
		{"trusted peer uses header", "127.0.0.1", "127.0.0.1:1234", []string{"198.51.100.7"}, "198.51.100.7"},
		{"spoofed left entry is skipped", "127.0.0.1", "127.0.0.1:1234", []string{"6.6.6.6, 198.51.100.7"}, "198.51.100.7"},
		{"trusted hops are walked", "127.0.0.1,10.0.0.0/8", "127.0.0.1:1234", []string{"198.51.100.7, 10.0.0.2"}, "198.51.100.7"},
		{"multiple header lines", "127.0.0.1", "127.0.0.1:1234", []string{"6.6.6.6", "198.51.100.7"}, "198.51.100.7"},
		{"trusted peer without header", "127.0.0.1", "127.0.0.1:1234", nil, "127.0.0.1"},
		{"ipv6 peer", "::1", "[::1]:1234", []string{"2001:db8::1"}, "2001:db8::1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := ConfigureTrustedProxies(c.trusted); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = ConfigureTrustedProxies("") })
			r := httptest.NewRequest(http.MethodPost, "/", nil)
			r.RemoteAddr = c.remote
			for _, v := range c.xff {
				r.Header.Add("X-Forwarded-For", v)
			}
			if got := clientIP(r); got != c.want {
				t.Errorf("clientIP = %q, want %q", got, c.want)
			}
		})
	}
}

func TestConfigureTrustedProxiesRejectsGarbage(t *testing.T) {
	if err := ConfigureTrustedProxies("127.0.0.1, not-an-ip"); err == nil {
		t.Fatal("expected error")
	}
}

func TestLimiter(t *testing.T) {
	clock := time.Unix(0, 0)
	l := newLimiter(3, time.Minute)
	l.now = func() time.Time { return clock }

	for range 3 {
		if l.retryAfter("k") != 0 {
			t.Fatal("blocked too early")
		}
		l.record("k")
	}
	if l.retryAfter("k") != time.Minute {
		t.Fatalf("retryAfter = %v, want 1m", l.retryAfter("k"))
	}
	if l.retryAfter("other") != 0 {
		t.Fatal("keys must be independent")
	}

	clock = clock.Add(time.Minute + time.Second)
	if l.retryAfter("k") != 0 {
		t.Fatal("window did not expire")
	}

	for range 3 {
		l.record("k")
	}
	l.reset("k")
	if l.retryAfter("k") != 0 {
		t.Fatal("reset did not clear")
	}
}

func TestAllowAllWrites429(t *testing.T) {
	l := newLimiter(1, time.Minute)
	l.record("k")
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	if allowAll(w, r, "test", limitCheck{l, "k"}) {
		t.Fatal("expected refusal")
	}
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") == "" {
		t.Fatalf("got %d, Retry-After=%q", w.Code, w.Header().Get("Retry-After"))
	}
}
