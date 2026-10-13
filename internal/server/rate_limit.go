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

func ParseTrustedProxyCIDRs(value string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	if strings.TrimSpace(value) == "" {
		return out, nil
	}
	for _, entry := range strings.Split(value, ",") {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(entry))
		if err != nil || prefix.Addr().Is4In6() {
			return nil, errors.New("TRUSTED_PROXY_CIDRS 必须为逗号分隔的 IPv4 / IPv6 CIDR")
		}
		out = append(out, prefix.Masked())
	}
	return out, nil
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
	trusted := func(ip netip.Addr) bool {
		for _, p := range s.cfg.TrustedProxyCIDRs {
			if p.Contains(ip) {
				return true
			}
		}
		return false
	}
	if !trusted(peer) {
		return peer.String()
	}
	forwarded := r.Header.Values("X-Forwarded-For")
	chain := strings.Join(forwarded, ",")
	if chain == "" || len(chain) > 2048 {
		return peer.String()
	}
	parts := strings.Split(chain, ",")
	if len(parts) > 32 {
		return peer.String()
	}
	ips := make([]netip.Addr, len(parts))
	for i, part := range parts {
		ip, err := netip.ParseAddr(strings.TrimSpace(part))
		if err != nil || ip.Zone() != "" {
			return peer.String()
		}
		ips[i] = ip.Unmap()
	}
	for i := len(ips) - 1; i >= 0; i-- {
		peer = ips[i]
		if !trusted(peer) {
			break
		}
	}
	return peer.String()
}

func (s *Server) authRateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if !(r.Method == http.MethodGet && strings.HasPrefix(path, "/auth/")) && !(r.Method == http.MethodPost && strings.HasPrefix(path, "/api/auth/bind/")) {
			next.ServeHTTP(w, r)
			return
		}
		scope, perIP, global := "auth_start", 20, 120
		if strings.HasSuffix(path, "/callback") {
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
