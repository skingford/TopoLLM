package observability

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Prometheus 指标。通过 promauto 注册到默认 Registry，由 /metrics 暴露。
var (
	// HTTPRequests 统计对外 HTTP 请求总数。
	HTTPRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "topollm_http_requests_total",
		Help: "对外 HTTP 请求总数。",
	}, []string{"method", "path", "status"})

	// HTTPDuration 统计对外 HTTP 请求耗时。
	HTTPDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "topollm_http_request_duration_seconds",
		Help:    "对外 HTTP 请求耗时（秒）。",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "path"})

	// RelayRequests 统计转发到上游渠道的请求结果。
	RelayRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "topollm_relay_requests_total",
		Help: "转发到上游渠道的请求总数（按渠道/模型/结果）。",
	}, []string{"channel", "model", "result"})

	// RelayTokens 统计经网关计量的 token 用量。
	RelayTokens = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "topollm_relay_tokens_total",
		Help: "经网关计量的 token 用量（kind=prompt|completion）。",
	}, []string{"channel", "model", "kind"})
)
