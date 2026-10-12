package server

import (
	"io"
	"nailist/internal/logging"
	"net/http"

	"github.com/go-chi/chi/v5/middleware"
)

// Keep optional HTTP interfaces while observing every body write, including
// FileServer's io.Copy fast path. Never buffer or inspect the response body.
type observedResponse struct {
	middleware.WrapResponseWriter
	request *http.Request
}

func (w *observedResponse) Write(data []byte) (int, error) {
	n, err := w.WrapResponseWriter.Write(data)
	if err == nil && n < len(data) {
		err = io.ErrShortWrite
	}
	if err != nil {
		responseFailure(w.request, w.Status(), err)
	}
	return n, err
}

func (w *observedResponse) ReadFrom(r io.Reader) (int64, error) {
	// Suppress ReaderFrom recursion; all copied bytes pass through Write.
	return io.Copy(struct{ io.Writer }{w}, r)
}

func (w *observedResponse) Flush() {
	if w.Status() == 0 {
		w.WriteHeader(http.StatusOK)
	}
	if err := http.NewResponseController(w.WrapResponseWriter).Flush(); err != nil {
		responseFailure(w.request, w.Status(), err)
	}
}

func observeResponse(w middleware.WrapResponseWriter, r *http.Request) middleware.WrapResponseWriter {
	o := &observedResponse{WrapResponseWriter: w, request: r}
	f, fl := w.(http.Flusher)
	h, hj := w.(http.Hijacker)
	p, ps := w.(http.Pusher)
	// Embed the observer's method set through an interface that excludes Flush;
	// advertise optional interfaces only when supported by the original writer.
	type bodyWriter interface {
		middleware.WrapResponseWriter
		io.ReaderFrom
	}
	var body bodyWriter = o
	if fl {
		f = o
	}
	switch {
	case fl && hj && ps:
		return struct {
			bodyWriter
			http.Flusher
			http.Hijacker
			http.Pusher
		}{body, f, h, p}
	case fl && hj:
		return struct {
			bodyWriter
			http.Flusher
			http.Hijacker
		}{body, f, h}
	case fl && ps:
		return struct {
			bodyWriter
			http.Flusher
			http.Pusher
		}{body, f, p}
	case hj && ps:
		return struct {
			bodyWriter
			http.Hijacker
			http.Pusher
		}{body, h, p}
	case fl:
		return struct {
			bodyWriter
			http.Flusher
		}{body, f}
	case hj:
		return struct {
			bodyWriter
			http.Hijacker
		}{body, h}
	case ps:
		return struct {
			bodyWriter
			http.Pusher
		}{body, p}
	default:
		return struct{ bodyWriter }{body}
	}
}

func responseFailure(r *http.Request, status int, err error) {
	if state, ok := r.Context().Value(requestStateKey{}).(*requestState); ok {
		state.mu.Lock()
		alreadyLogged := state.writeError
		state.writeError = true
		if state.outcome != "failed" {
			state.outcome, state.reason = "failed", "response_write_failed"
		}
		state.mu.Unlock()
		if alreadyLogged {
			return
		}
	}
	requestLogger(r).WarnContext(r.Context(), "响应写入未完成，客户端可能断开连接", append([]any{"component", "http", "event", "http.response.write_failed", "status", status, "reason", "response_write_failed"}, logging.ErrorAttrs(err)...)...)
}
