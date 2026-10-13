package server

import (
	"fmt"
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
