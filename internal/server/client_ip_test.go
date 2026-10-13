package server

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseClientIPHeader(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"", ""}, {"  ", ""}, {"x-forwarded-for", "X-Forwarded-For"},
		{" CF-Connecting-IP ", "Cf-Connecting-Ip"}, {"X-Real-IP", "X-Real-Ip"},
	} {
		got, err := ParseClientIPHeader(tc.input)
		if err != nil || got != tc.want {
			t.Fatalf("input %q: got %q, %v", tc.input, got, err)
		}
	}
	for _, bad := range []string{"bad header", "X-Real-IP,X-Forwarded-For", "X-IP: value", "X-IP\r\nInjected: yes", "中文"} {
		if _, err := ParseClientIPHeader(bad); err == nil {
			t.Fatalf("invalid header accepted: %q", bad)
		}
	}
}

func TestConfiguredClientIPHeader(t *testing.T) {
	for _, tc := range []struct{ name, header, value, peer, want string }{
		{"default ignores forwarding", "", "198.51.100.1", "192.0.2.1:80", "192.0.2.1"},
		{"xff first address", "X-Forwarded-For", "198.51.100.1, 10.0.0.1", "192.0.2.1:80", "198.51.100.1"},
		{"case insensitive", "x-forwarded-for", "198.51.100.1", "192.0.2.1:80", "198.51.100.1"},
		{"cloudflare", "CF-Connecting-IP", "198.51.100.2", "192.0.2.1:80", "198.51.100.2"},
		{"real ip", "X-Real-IP", " 198.51.100.3 ", "192.0.2.1:80", "198.51.100.3"},
		{"custom header", "Client-IP", "2001:db8::2", "[2001:db8::1]:80", "2001:db8::2"},
		{"mapped ipv4", "X-Real-IP", "::ffff:198.51.100.1", "192.0.2.1:80", "198.51.100.1"},
		{"missing", "X-Real-IP", "", "192.0.2.1:80", "192.0.2.1"},
		{"malformed", "X-Real-IP", "garbage", "192.0.2.1:80", "192.0.2.1"},
		{"port", "X-Real-IP", "198.51.100.1:80", "192.0.2.1:80", "192.0.2.1"},
		{"zone", "X-Real-IP", "fe80::1%eth0", "192.0.2.1:80", "192.0.2.1"},
		{"single header rejects list", "X-Real-IP", "198.51.100.1, 10.0.0.1", "192.0.2.1:80", "192.0.2.1"},
		{"invalid first xff", "X-Forwarded-For", "garbage, 198.51.100.1", "192.0.2.1:80", "192.0.2.1"},
		{"long header", "X-Forwarded-For", strings.Repeat(" ", 2049) + "198.51.100.1", "192.0.2.1:80", "192.0.2.1"},
		{"peer mapped", "", "", "[::ffff:192.0.2.1]:80", "192.0.2.1"},
		{"unparseable peer", "", "", "invalid", "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &Server{cfg: Config{ClientIPHeader: tc.header}}
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = tc.peer
			r.Header.Set("X-Forwarded-For", "203.0.113.1")
			r.Header.Set("CF-Connecting-IP", "203.0.113.2")
			if tc.header != "" {
				r.Header.Set(tc.header, tc.value)
			}
			if got := s.clientIP(r); got != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
}

func TestClientIPHeaderControlsLoginRateLimit(t *testing.T) {
	s := New(Config{PublicURL: "https://groups.example", QQAppID: "test", QQAppKey: "test", ClientIPHeader: "CF-Connecting-IP"}, nil, &memoryGroups{})
	h := s.Routes()
	request := func(ip string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/auth/qq", nil)
		r.RemoteAddr = "192.0.2.1:80"
		r.Header.Set("CF-Connecting-IP", ip)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	for i := 0; i < 20; i++ {
		if w := request("198.51.100.1"); w.Code != 302 {
			t.Fatalf("start %d: %d", i, w.Code)
		}
	}
	if w := request("198.51.100.1"); w.Header().Get("Retry-After") == "" {
		t.Fatal("missing client IP limit")
	}
	if w := request("198.51.100.2"); w.Code != 302 {
		t.Fatal("shared CDN peer exhausted another visitor's allowance")
	}
}
