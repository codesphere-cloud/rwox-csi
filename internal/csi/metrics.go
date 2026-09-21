package csi

import (
	"context"
	"log/slog"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

var (
	operations = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "rwox_csi_operations_total", Help: "CSI operations by method and status."}, []string{"method", "status"})
	duration   = prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "rwox_csi_operation_duration_seconds", Help: "CSI operation latency."}, []string{"method"})
)

func init() { prometheus.MustRegister(operations, duration) }
func UnaryMetrics(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	start := time.Now()
	resp, err := handler(ctx, req)
	elapsed := time.Since(start)
	code := status.Code(err).String()
	operations.WithLabelValues(info.FullMethod, code).Inc()
	duration.WithLabelValues(info.FullMethod).Observe(elapsed.Seconds())
	slog.Info("CSI operation", "method", info.FullMethod, "status", code, "duration", elapsed.String())
	return resp, err
}
