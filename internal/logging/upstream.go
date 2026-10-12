package logging

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptrace"
	"sync"
	"time"
)

type upstreamFailureKey struct{}
type upstreamFailure struct {
	mu  sync.Mutex
	err error
}

// TrackUpstreamErrors retains typed transport/body causes when provider SDKs
// flatten them into strings. Reuse the same tracker across discovery and JWKS.
func TrackUpstreamErrors(ctx context.Context) context.Context {
	if _, ok := ctx.Value(upstreamFailureKey{}).(*upstreamFailure); ok {
		return ctx
	}
	return context.WithValue(ctx, upstreamFailureKey{}, &upstreamFailure{})
}

func UpstreamFailure(ctx context.Context) error {
	if failure, ok := ctx.Value(upstreamFailureKey{}).(*upstreamFailure); ok {
		failure.mu.Lock()
		defer failure.mu.Unlock()
		return failure.err
	}
	return nil
}

func recordUpstreamFailure(ctx context.Context, err error) error {
	if err != nil {
		// Client.Timeout can cancel a transport with context.Canceled while its
		// own deadline has expired. Preserve the real timeout classification.
		if deadline, ok := ctx.Deadline(); ok && !time.Now().Before(deadline) {
			err = context.DeadlineExceeded
		} else if ctx.Err() != nil {
			err = ctx.Err()
		}
	}
	if failure, ok := ctx.Value(upstreamFailureKey{}).(*upstreamFailure); ok {
		failure.mu.Lock()
		failure.err = err
		failure.mu.Unlock()
	}
	return err
}

// Clone preserves test/custom transports, timeouts, cookies and redirect policy.
func TraceClient(client *http.Client) *http.Client {
	copy := *client
	transport := copy.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	if _, ok := transport.(upstreamTransport); !ok {
		copy.Transport = upstreamTransport{transport}
	}
	return &copy
}

type upstreamTransport struct{ base http.RoundTripper }

func (t upstreamTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	ctx := r.Context()
	logger := FromContext(ctx).With("component", "upstream", "upstream_host", r.URL.Host, "upstream_path", r.URL.Path, "upstream_method", r.Method)
	started := time.Now()
	recordUpstreamFailure(ctx, nil)
	logger.DebugContext(ctx, "向登录服务发送 HTTP 请求", "event", "upstream.request.started")
	var mu sync.Mutex
	var dnsStart, connectStart, tlsStart time.Time
	var dnsMS, connectMS, tlsMS, firstByteMS float64
	var reused bool
	trace := &httptrace.ClientTrace{
		DNSStart: func(httptrace.DNSStartInfo) { mu.Lock(); dnsStart = time.Now(); mu.Unlock() },
		DNSDone: func(httptrace.DNSDoneInfo) {
			mu.Lock()
			if !dnsStart.IsZero() {
				dnsMS = DurationMS(dnsStart)
			}
			mu.Unlock()
		},
		ConnectStart: func(string, string) { mu.Lock(); connectStart = time.Now(); mu.Unlock() },
		ConnectDone: func(string, string, error) {
			mu.Lock()
			if !connectStart.IsZero() {
				connectMS = DurationMS(connectStart)
			}
			mu.Unlock()
		},
		GotConn:              func(info httptrace.GotConnInfo) { mu.Lock(); reused = info.Reused; mu.Unlock() },
		GotFirstResponseByte: func() { mu.Lock(); firstByteMS = DurationMS(started); mu.Unlock() },
	}
	// TLS timing is recorded separately from TCP and first-byte latency.
	trace.TLSHandshakeStart = func() { mu.Lock(); tlsStart = time.Now(); mu.Unlock() }
	trace.TLSHandshakeDone = func(_ tls.ConnectionState, _ error) {
		mu.Lock()
		if !tlsStart.IsZero() {
			tlsMS = DurationMS(tlsStart)
		}
		mu.Unlock()
	}
	response, err := t.base.RoundTrip(r.WithContext(httptrace.WithClientTrace(ctx, trace)))
	loggedError := recordUpstreamFailure(ctx, err)
	status := 0
	if response != nil {
		status = response.StatusCode
	}
	mu.Lock()
	args := []any{"event", "upstream.request.completed", "upstream_status", status, "duration_ms", DurationMS(started), "dns_ms", dnsMS, "connect_ms", connectMS, "tls_ms", tlsMS, "first_byte_ms", firstByteMS, "connection_reused", reused, "outcome", "success"}
	mu.Unlock()
	if err != nil || status >= 400 {
		args[len(args)-1] = "failed"
		args = append(args, ErrorAttrs(loggedError)...)
	}
	logger.DebugContext(ctx, "登录服务 HTTP 请求结束（耗时截至响应头）", args...)
	if time.Since(started) >= 3*time.Second && ctx.Err() != context.Canceled {
		logger.WarnContext(ctx, "登录服务响应超过 3 秒，检查上游服务及网络", "event", "upstream.request.slow", "duration_ms", DurationMS(started), "upstream_status", status, "threshold_ms", 3000)
	}
	if response != nil && response.Body != nil {
		response.Body = &upstreamBody{ReadCloser: response.Body, ctx: ctx, logger: logger, started: started, status: status}
	}
	return response, err
}

type upstreamBody struct {
	io.ReadCloser
	ctx      context.Context
	logger   *slog.Logger
	started  time.Time
	status   int
	mu       sync.Mutex
	bytes    int64
	finished bool
}

func (b *upstreamBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.mu.Lock()
	b.bytes += int64(n)
	b.mu.Unlock()
	if err != nil {
		b.finish(err)
	}
	return n, err
}

func (b *upstreamBody) Close() error {
	err := b.ReadCloser.Close()
	b.finish(err)
	return err
}

func (b *upstreamBody) finish(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.finished {
		return
	}
	b.finished = true
	outcome, reason := "success", "body_closed"
	if errors.Is(err, io.EOF) {
		err, reason = nil, "body_read_completed"
	}
	if err != nil {
		outcome, reason = "failed", "body_read_failed"
		err = recordUpstreamFailure(b.ctx, err)
	}
	args := []any{"event", "upstream.response.completed", "upstream_status", b.status, "duration_ms", DurationMS(b.started), "response_bytes", b.bytes, "outcome", outcome, "reason", reason}
	args = append(args, ErrorAttrs(err)...)
	b.logger.DebugContext(b.ctx, "登录服务响应读取结束（包含正文读取耗时）", args...)
}
