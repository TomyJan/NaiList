package server

import (
	"errors"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/http/httpguts"
)

type rateRule struct {
	key    string
	limit  int
	window time.Duration
}
type rateBucket struct {
	count   int
	expires time.Time
}
type requestLimiter struct {
	mu          sync.Mutex
	buckets     map[string]rateBucket
	now         func() time.Time
	nextCleanup time.Time
}

const maxRateBuckets = 16384

func newRequestLimiter() *requestLimiter {
	return &requestLimiter{buckets: make(map[string]rateBucket), now: time.Now}
}

// Check all rules before spending any allowance. Rejected clients cannot consume
// the global allowance, and active buckets are never evicted to bypass a limit.
func (l *requestLimiter) allow(rules ...rateRule) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if !now.Before(l.nextCleanup) {
		for key, b := range l.buckets {
			if !now.Before(b.expires) {
				delete(l.buckets, key)
			}
		}
		l.nextCleanup = now.Add(time.Minute)
	}
	var retry time.Duration
	missing := 0
	for _, rule := range rules {
		b, ok := l.buckets[rule.key]
		if !ok {
			missing++
		}
		if now.Before(b.expires) && b.count >= rule.limit && b.expires.Sub(now) > retry {
			retry = b.expires.Sub(now)
		}
	}
	if retry > 0 {
		return retry
	}
	if len(l.buckets)+missing > maxRateBuckets {
		return time.Minute
	}
	for _, rule := range rules {
		b := l.buckets[rule.key]
		if !now.Before(b.expires) {
			b = rateBucket{expires: now.Add(rule.window)}
		}
		b.count++
		l.buckets[rule.key] = b
	}
	return 0
}

func ParseClientIPHeader(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value != "" && !httpguts.ValidHeaderFieldName(value) {
		return "", errors.New("CLIENT_IP_HEADER 必须为单个合法 HTTP 请求头名称")
	}
	return http.CanonicalHeaderKey(value), nil
}

func (s *Server) clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return "unknown"
	}
	peer = peer.Unmap()
	if s.cfg.ClientIPHeader == "" {
		return peer.String()
	}
	value := strings.Join(r.Header.Values(s.cfg.ClientIPHeader), ",")
	if value == "" || len(value) > 2048 {
		return peer.String()
	}
	if strings.EqualFold(s.cfg.ClientIPHeader, "X-Forwarded-For") {
		value, _, _ = strings.Cut(value, ",")
	}
	ip, err := netip.ParseAddr(strings.TrimSpace(value))
	if err != nil || ip.Zone() != "" {
		return peer.String()
	}
	return ip.Unmap().String()
}

func (s *Server) authRateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if !(r.Method == http.MethodGet && strings.HasPrefix(path, "/auth/")) && !(r.Method == http.MethodPost && strings.HasPrefix(path, "/api/auth/bind/")) {
			next.ServeHTTP(w, r)
			return
		}
		scope, perIP, global := "auth_start", 20, 120
		parts := strings.Split(strings.TrimPrefix(path, "/auth/"), "/")
		if r.Method == http.MethodGet && strings.HasPrefix(path, "/auth/") && len(parts) == 2 && parts[0] != "" && parts[1] == "callback" {
			scope, perIP, global = "auth_callback", 60, 240
		}
		retry := s.limiter.allow(rateRule{scope + ":ip:" + s.clientIP(r), perIP, time.Minute}, rateRule{scope + ":global", global, time.Minute})
		if retry == 0 {
			next.ServeHTTP(w, r)
			return
		}
		s.rateLimitResponse(w, r, scope, retry)
	})
}

func (s *Server) userWriteRateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := r.Context().Value(userKey).(User)
		key := strconv.FormatInt(user.ID, 10)
		retry := s.limiter.allow(rateRule{"write:minute:" + key, 30, time.Minute}, rateRule{"write:hour:" + key, 200, time.Hour})
		if retry == 0 {
			next.ServeHTTP(w, r)
			return
		}
		s.rateLimitResponse(w, r, "user_write", retry)
	})
}

func (s *Server) rateLimitResponse(w http.ResponseWriter, r *http.Request, scope string, retry time.Duration) {
	seconds := int((retry + time.Second - 1) / time.Second)
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	requestResult(r, "rejected", "rate_limited")
	requestLogger(r).WarnContext(r.Context(), "请求频率超过额度，已在业务处理前拒绝，请稍后重试", "component", "security", "event", "security.rate_limited", "outcome", "rejected", "reason", "rate_limited", "scope", scope, "retry_after_seconds", seconds)
	if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/auth/") {
		http.Redirect(w, r, "/?auth_error=rate_limited", http.StatusSeeOther)
		return
	}
	respond(w, r, http.StatusTooManyRequests, map[string]string{"error": "操作过于频繁，请稍后重试"})
}
