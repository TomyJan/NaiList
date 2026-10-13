// Package traffic reads public Umami share statistics without exposing share credentials.
package traffic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"nailist/internal/logging"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

var beijing = time.FixedZone("Asia/Shanghai", 8*60*60)
var identifier = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,100}$`)

type Counts struct {
	Visitors  int64 `json:"visitors"`
	Pageviews int64 `json:"pageviews"`
}
type Stats struct {
	Today     Counts    `json:"today"`
	Total     Counts    `json:"total"`
	Online    int64     `json:"online"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type Client struct {
	base, slug string
	http       *http.Client
	now        func() time.Time
	mu         sync.Mutex
	flight     chan struct{}
	cached     Stats
	err        error
	until      time.Time
	day        string
}

func New(shareURL string) (*Client, error) {
	u, err := url.Parse(strings.TrimSpace(shareURL))
	invalid := errors.New("UMAMI_SHARE_URL 必须是有效的 HTTP(S) 网站分享链接，格式为 https://统计域名/share/分享码")
	if err != nil || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.Fragment != "" || u.RawPath != "" || strings.ContainsAny(u.Host, " \t\r\n;,'\"<>\\") {
		return nil, invalid
	}
	index := strings.LastIndex(u.Path, "/share/")
	if index < 0 {
		return nil, invalid
	}
	parts := strings.Split(strings.TrimRight(u.Path[index+7:], "/"), "/")
	if len(parts) > 2 || !identifier.MatchString(parts[0]) {
		return nil, invalid
	}
	for _, part := range strings.Split(u.Path[:index], "/") {
		if part == "." || part == ".." {
			return nil, invalid
		}
	}
	u.Path, u.RawQuery, u.ForceQuery = u.Path[:index], "", false
	return &Client{base: u.String(), slug: parts[0], now: time.Now, http: &http.Client{
		Timeout:       8 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

// The shared snapshot has its own bounded lifetime: one departing visitor cannot
// cancel the fetch for every other visitor. Waiting callers still cancel promptly.
func (c *Client) Stats(ctx context.Context) (Stats, error) {
	for {
		if err := ctx.Err(); err != nil {
			return Stats{}, err
		}
		c.mu.Lock()
		now := c.now()
		if now.Before(c.until) && c.day == now.In(beijing).Format("2006-01-02") {
			stats, err := c.cached, c.err
			c.mu.Unlock()
			logging.FromContext(ctx).DebugContext(ctx, "复用访问统计缓存，未请求 Umami", "component", "traffic", "event", "traffic.cache.hit", "failed", err != nil)
			return stats, err
		}
		if c.flight == nil {
			c.flight = make(chan struct{})
			go c.refresh(context.WithoutCancel(ctx), now)
		}
		flight := c.flight
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			return Stats{}, ctx.Err()
		case <-flight:
		}
	}
}

func (c *Client) refresh(parent context.Context, now time.Time) {
	ctx, cancel := context.WithTimeout(parent, 8*time.Second)
	defer cancel()
	started := time.Now()
	stats, err := c.load(ctx, now)
	ttl := 15 * time.Second
	if err != nil {
		ttl = 5 * time.Second
	}
	c.mu.Lock()
	c.cached, c.err, c.until, c.day = stats, err, c.now().Add(ttl), now.In(beijing).Format("2006-01-02")
	close(c.flight)
	c.flight = nil
	c.mu.Unlock()
	logger := logging.FromContext(ctx)
	if err != nil {
		logger.WarnContext(ctx, "Umami 访问统计获取失败，短暂退避后允许重试", append([]any{"component", "traffic", "event", "traffic.snapshot.failed", "retry_after_ms", ttl.Milliseconds(), "duration_ms", logging.DurationMS(started), "hint", "检查 Umami 可达性、分享链接有效性与流量概览权限"}, logging.ErrorAttrs(err)...)...)
	} else {
		logger.DebugContext(ctx, "Umami 访问统计已更新，今日按北京时间计算", "component", "traffic", "event", "traffic.snapshot.updated", "today_visitors", stats.Today.Visitors, "today_pageviews", stats.Today.Pageviews, "total_visitors", stats.Total.Visitors, "total_pageviews", stats.Total.Pageviews, "online", stats.Online, "cache_ms", ttl.Milliseconds(), "duration_ms", logging.DurationMS(started))
	}
}

func (c *Client) load(ctx context.Context, now time.Time) (Stats, error) {
	var share struct {
		WebsiteID string `json:"websiteId"`
		Token     string `json:"token"`
		ShareType *int   `json:"shareType"`
	}
	if err := c.get(ctx, "share", "/api/share/"+c.slug, "", &share); err != nil {
		return Stats{}, err
	}
	if !identifier.MatchString(share.WebsiteID) || share.Token == "" || len(share.Token) > 16384 || share.ShareType != nil && *share.ShareType != 1 {
		return Stats{}, errors.New("Umami share: invalid website share response")
	}
	day := now.In(beijing)
	start := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, beijing)
	stats := Stats{UpdatedAt: now}
	var wg sync.WaitGroup
	errs := make([]error, 3)
	wg.Add(3)
	go func() {
		defer wg.Done()
		stats.Today, errs[0] = c.counts(ctx, share.WebsiteID, share.Token, start.UnixMilli(), now.UnixMilli())
	}()
	go func() {
		defer wg.Done()
		stats.Total, errs[1] = c.counts(ctx, share.WebsiteID, share.Token, 0, now.UnixMilli())
	}()
	go func() {
		defer wg.Done()
		var active struct {
			Visitors *counter `json:"visitors"`
		}
		errs[2] = c.get(ctx, "active", "/api/websites/"+share.WebsiteID+"/active", share.Token, &active)
		if errs[2] == nil {
			if active.Visitors == nil {
				errs[2] = errors.New("Umami active: missing visitors")
			} else {
				stats.Online = int64(*active.Visitors)
			}
		}
	}()
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return Stats{}, err
		}
	}
	return stats, nil
}

func (c *Client) counts(ctx context.Context, id, token string, start, end int64) (Counts, error) {
	q := url.Values{"startAt": {strconv.FormatInt(start, 10)}, "endAt": {strconv.FormatInt(end, 10)}, "timezone": {"Asia/Shanghai"}}
	var data struct {
		Visitors  *counter `json:"visitors"`
		Pageviews *counter `json:"pageviews"`
	}
	err := c.get(ctx, "stats", "/api/websites/"+id+"/stats?"+q.Encode(), token, &data)
	if err != nil {
		return Counts{}, err
	}
	if data.Visitors == nil || data.Pageviews == nil {
		return Counts{}, errors.New("Umami stats: missing counts")
	}
	return Counts{Visitors: int64(*data.Visitors), Pageviews: int64(*data.Pageviews)}, nil
}

// Both current numeric responses and older {value, change} counts are accepted.
type counter int64

func (n *counter) UnmarshalJSON(body []byte) error {
	if len(body) > 0 && body[0] == '{' {
		var value struct {
			Value *json.RawMessage `json:"value"`
		}
		if json.Unmarshal(body, &value) != nil || value.Value == nil {
			return errors.New("invalid count")
		}
		body = *value.Value
	}
	v, err := strconv.ParseInt(string(body), 10, 64)
	if err != nil || v < 0 || v > 9007199254740991 {
		return errors.New("invalid count")
	}
	*n = counter(v)
	return nil
}

func (c *Client) get(ctx context.Context, operation, path, token string, out any) error {
	started := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return fmt.Errorf("Umami %s: invalid request", operation)
	}
	req.Header.Set("Accept", "application/json")
	if token != "" {
		req.Header.Set("x-umami-share-token", token)
		req.Header.Set("x-umami-share-context", "1")
	}
	response, err := c.http.Do(req)
	if err != nil {
		// net/url.Error contains the private share URL; retain only its cause.
		var urlError *url.Error
		if errors.As(err, &urlError) {
			err = urlError.Err
		}
		return fmt.Errorf("Umami %s: %w", operation, err)
	}
	defer response.Body.Close()
	logging.FromContext(ctx).DebugContext(ctx, "Umami 汇总接口已响应", "component", "traffic", "event", "traffic.upstream.response", "operation", operation, "upstream_status", response.StatusCode, "duration_ms", logging.DurationMS(started))
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("Umami %s: HTTP %d", operation, response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 65537))
	if err != nil {
		return fmt.Errorf("Umami %s: response read failed", operation)
	}
	if len(body) > 65536 || json.Unmarshal(body, out) != nil {
		return fmt.Errorf("Umami %s: invalid response", operation)
	}
	return nil
}
