package server

import (
	"context"
	"errors"
	"nailist/internal/logging"
	"net/http"
)

func (s *Server) trafficStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if s.traffic == nil {
		requestResult(r, "rejected", "traffic_disabled")
		respond(w, r, 404, map[string]string{"error": "访问统计未启用"})
		return
	}
	stats, err := s.traffic.Stats(r.Context())
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			requestResult(r, "canceled", "request_canceled")
		} else {
			// The shared fetch records one semantic warning per failed attempt;
			// cached failures must not flood logs at public visitor request rates.
			requestResult(r, "failed", "umami_unavailable")
			requestErrorLogged(r)
		}
		requestLogger(r).DebugContext(r.Context(), "页脚统计请求未获取数据，可稍后刷新重试", append([]any{"component", "traffic", "event", "traffic.request.failed"}, logging.ErrorAttrs(err)...)...)
		respond(w, r, 503, map[string]string{"error": "访问统计暂不可用，请稍后刷新"})
		return
	}
	requestResult(r, "success", "")
	respond(w, r, 200, stats)
}
