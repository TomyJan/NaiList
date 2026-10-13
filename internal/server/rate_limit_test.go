package server

import (
	"fmt"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRateLimiterAtomicGlobalAllowanceAndExpiry(t *testing.T) {
	l := newRequestLimiter()
	now := time.Unix(1000, 0)
	l.now = func() time.Time { return now }
	for i := 0; i < 20; i++ {
		if l.allow(rateRule{"ip:a", 20, time.Minute}, rateRule{"global", 21, time.Minute}) != 0 {
			t.Fatal("early rejection")
		}
	}
	if l.allow(rateRule{"ip:a", 20, time.Minute}, rateRule{"global", 21, time.Minute}) != time.Minute {
		t.Fatal("missing IP limit")
	}
	if l.allow(rateRule{"ip:b", 20, time.Minute}, rateRule{"global", 21, time.Minute}) != 0 {
		t.Fatal("rejected IP spent global allowance")
	}
	if l.allow(rateRule{"ip:c", 20, time.Minute}, rateRule{"global", 21, time.Minute}) == 0 {
		t.Fatal("missing global limit")
	}
	now = now.Add(time.Minute)
	if l.allow(rateRule{"ip:a", 20, time.Minute}, rateRule{"global", 21, time.Minute}) != 0 {
		t.Fatal("expired buckets not reset")
	}
}

func TestRateLimiterCapacityAndConcurrentRequests(t *testing.T) {
	l := newRequestLimiter()
	var accepted atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if l.allow(rateRule{"account", 30, time.Minute}) == 0 {
				accepted.Add(1)
			}
		}()
	}
	wg.Wait()
	if accepted.Load() != 30 {
		t.Fatalf("concurrent limit: %d", accepted.Load())
	}
	for i := 1; i < maxRateBuckets; i++ {
		l.allow(rateRule{fmt.Sprint(i), 1, time.Hour})
	}
	if l.allow(rateRule{"overflow", 1, time.Minute}) == 0 || len(l.buckets) != maxRateBuckets {
		t.Fatal("limiter map not bounded")
	}
	if l.allow(rateRule{"account", 30, time.Minute}) == 0 {
		t.Fatal("active bucket evicted")
	}
}

func TestRateLimiterSustainedWrites(t *testing.T) {
	l := newRequestLimiter()
	now := time.Unix(1000, 0)
	l.now = func() time.Time { return now }
	for i := 0; i < 200; i++ {
		if i%30 == 0 {
			now = now.Add(time.Minute)
		}
		if l.allow(rateRule{"minute", 30, time.Minute}, rateRule{"hour", 200, time.Hour}) != 0 {
			t.Fatalf("early rejection at %d", i)
		}
	}
	now = now.Add(time.Minute)
	if l.allow(rateRule{"minute", 30, time.Minute}, rateRule{"hour", 200, time.Hour}) == 0 {
		t.Fatal("hourly limit bypassed")
	}
	now = now.Add(time.Hour)
	if l.allow(rateRule{"minute", 30, time.Minute}, rateRule{"hour", 200, time.Hour}) != 0 {
		t.Fatal("hourly limit did not recover")
	}
}

func TestTrustedProxyClientIP(t *testing.T) {
	trusted, err := ParseTrustedProxyCIDRs("127.0.0.1/32,10.1.2.3/32,2001:db8::1/128")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{cfg: Config{TrustedProxyCIDRs: trusted}}
	for _, tc := range []struct{ peer, forwarded, want string }{
		{"192.0.2.1:80", "198.51.100.1", "192.0.2.1"},
		{"127.0.0.1:80", "198.51.100.1", "198.51.100.1"},
		{"127.0.0.1:80", "198.51.100.2, 10.1.2.3", "198.51.100.2"},
		{"127.0.0.1:80", "198.51.100.2, 10.1.2.4", "10.1.2.4"},
		{"127.0.0.1:80", "garbage, 10.1.2.3", "127.0.0.1"},
		{"127.0.0.1:80", "198.51.100.1:90", "127.0.0.1"},
		{"[2001:db8::1]:80", "2001:db8::2", "2001:db8::2"},
		{"[::ffff:192.0.2.1]:80", "198.51.100.1", "192.0.2.1"},
		{"127.0.0.1:80", "", "127.0.0.1"},
	} {
		t.Run(tc.peer+tc.forwarded, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = tc.peer
			r.Header.Set("X-Forwarded-For", tc.forwarded)
			r.Header.Set("X-Real-IP", "203.0.113.1")
			if got := s.clientIP(r); got != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
	for _, bad := range []string{"127.0.0.1", "0.0.0.0/no", "127.0.0.1/32,", "::ffff:127.0.0.1/128"} {
		if _, err := ParseTrustedProxyCIDRs(bad); err == nil {
			t.Fatalf("invalid CIDR accepted: %s", bad)
		}
	}
}
