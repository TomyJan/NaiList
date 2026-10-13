package traffic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestShareURL(t *testing.T) {
	for _, raw := range []string{"javascript:alert(1)", "https://user:pass@stats.test/share/secret", "https://stats.test/not-share/secret", "https://stats.test/share/../evil", "https://stats.test/share/a%2Fb", "https://stats.test/share/"} {
		if _, err := New(raw); err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatalf("invalid URL must be rejected without disclosing it: %q, %v", raw, err)
		}
	}
	client, err := New("https://stats.test/umami/share/Secret123/site.example?date=30day")
	if err != nil || client.base != "https://stats.test/umami" || client.slug != "Secret123" {
		t.Fatalf("share URL with base path and display suffix: %v", err)
	}
}

func TestStatsProxyCacheAndMidnight(t *testing.T) {
	var requests atomic.Int64
	now := time.Date(2026, 10, 13, 15, 59, 58, 0, time.UTC)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/umami/api/share/Secret123" {
			fmt.Fprint(w, `{"websiteId":"website-1","shareType":1,"token":"private-token"}`)
			return
		}
		if r.Header.Get("x-umami-share-token") != "private-token" || r.Header.Get("x-umami-share-context") == "" || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Error("share credentials must be server-generated and isolated")
		}
		switch r.URL.Path {
		case "/umami/api/websites/website-1/active":
			fmt.Fprint(w, `{"visitors":66}`)
		case "/umami/api/websites/website-1/stats":
			q := r.URL.Query()
			if q.Get("endAt") != strconv.FormatInt(now.UnixMilli(), 10) || q.Get("timezone") != "Asia/Shanghai" {
				t.Error("date range must end at snapshot time and use Beijing time")
			}
			if q.Get("startAt") == "0" {
				fmt.Fprint(w, `{"visitors":999,"pageviews":1234}`)
			} else {
				day := now.In(beijing)
				start := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, beijing)
				if q.Get("startAt") != strconv.FormatInt(start.UnixMilli(), 10) {
					t.Error("today must begin at Beijing midnight")
				}
				fmt.Fprint(w, `{"visitors":{"value":123,"change":1},"pageviews":{"value":456}}`)
			}
		default:
			t.Errorf("unexpected upstream path: %s", r.URL.Path)
		}
	}))
	defer upstream.Close()
	client, _ := New(upstream.URL + "/umami/share/Secret123")
	client.now = func() time.Time { return now }
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			stats, err := client.Stats(context.Background())
			if err != nil || stats.Today.Visitors != 123 || stats.Today.Pageviews != 456 || stats.Total.Visitors != 999 || stats.Total.Pageviews != 1234 || stats.Online != 66 {
				t.Errorf("unexpected snapshot: %+v, %v", stats, err)
			}
			body, _ := json.Marshal(stats)
			if strings.Contains(string(body), "private-token") || strings.Contains(string(body), "website-1") {
				t.Error("credentials must never leave the proxy")
			}
		}()
	}
	wg.Wait()
	if requests.Load() != 4 {
		t.Fatalf("concurrent calls must share one snapshot: %d", requests.Load())
	}
	now = now.Add(3 * time.Second)
	if _, err := client.Stats(context.Background()); err != nil || requests.Load() != 8 {
		t.Fatalf("midnight must invalidate cache: %v, %d", err, requests.Load())
	}
	now = now.Add(16 * time.Second)
	if _, err := client.Stats(context.Background()); err != nil || requests.Load() != 12 {
		t.Fatalf("expired cache must reload: %v, %d", err, requests.Load())
	}
}

func TestUpstreamFailuresAndRecovery(t *testing.T) {
	for _, response := range []string{`{}`, `{"visitors":-1,"pageviews":1}`, `{"visitors":null,"pageviews":1}`, `{"visitors":1.5,"pageviews":1}`, `{"visitors":1,"pageviews":1} {}`, strings.Repeat("x", 65537)} {
		t.Run(response[:min(len(response), 40)], func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.URL.Path, "/share/") {
					fmt.Fprint(w, `{"websiteId":"website-1","token":"private-token"}`)
					return
				}
				if strings.HasSuffix(r.URL.Path, "/active") {
					fmt.Fprint(w, `{"visitors":1}`)
					return
				}
				fmt.Fprint(w, response)
			}))
			defer upstream.Close()
			client, _ := New(upstream.URL + "/share/Secret123")
			if _, err := client.Stats(context.Background()); err == nil || strings.Contains(err.Error(), response) {
				t.Fatalf("invalid response must fail without body disclosure: %v", err)
			}
		})
	}
	var calls atomic.Int64
	var fail atomic.Bool
	fail.Store(true)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if fail.Load() {
			http.Error(w, "private-token", 403)
			return
		}
		if strings.Contains(r.URL.Path, "/share/") {
			fmt.Fprint(w, `{"websiteId":"website-1","token":"private-token"}`)
			return
		}
		fmt.Fprint(w, `{"visitors":0,"pageviews":0}`)
	}))
	defer upstream.Close()
	client, _ := New(upstream.URL + "/share/Secret123")
	now := time.Now()
	client.now = func() time.Time { return now }
	for range 10 {
		if _, err := client.Stats(context.Background()); err == nil || strings.Contains(err.Error(), "private-token") {
			t.Fatalf("expected safe upstream error: %v", err)
		}
	}
	if calls.Load() != 1 {
		t.Fatal("failures must back off")
	}
	fail.Store(false)
	now = now.Add(6 * time.Second)
	if _, err := client.Stats(context.Background()); err != nil {
		t.Fatalf("must recover after failure: %v", err)
	}
}

func TestRedirectAndCanceledWaiter(t *testing.T) {
	var leaked atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked.Store(true) }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 302) }))
	defer redirect.Close()
	client, _ := New(redirect.URL + "/share/Secret123")
	if _, err := client.Stats(context.Background()); err == nil || leaked.Load() {
		t.Fatal("must never follow redirects")
	}
	entered, release := make(chan struct{}), make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/share/") {
			close(entered)
			<-release
			fmt.Fprint(w, `{"websiteId":"website-1","token":"private-token"}`)
			return
		}
		fmt.Fprint(w, `{"visitors":0,"pageviews":0}`)
	}))
	defer upstream.Close()
	client, _ = New(upstream.URL + "/share/Secret123")
	done := make(chan error, 1)
	go func() { _, err := client.Stats(context.Background()); done <- err }()
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := client.Stats(ctx)
	close(release)
	if err != context.DeadlineExceeded {
		t.Fatalf("waiting callers must cancel promptly: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("one canceled waiter must not cancel the shared fetch: %v", err)
	}
}

func TestUpstreamTimeoutDoesNotDiscloseShareURL(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer upstream.Close()
	client, _ := New(upstream.URL + "/share/Secret123")
	client.http.Timeout = 20 * time.Millisecond
	_, err := client.Stats(context.Background())
	if !errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "Secret123") {
		t.Fatalf("timeout must remain classifiable without private URL: %v", err)
	}
}
