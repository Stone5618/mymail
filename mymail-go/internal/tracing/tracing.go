// Package tracing 提供 OpenTelemetry 分布式追踪初始化。
// 采样后的追踪数据通过 OTLP 协议导出到配置的 Collector。
package tracing

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
)

// Tracer 封装追踪器，提供 Shutdown 能力。
type Tracer struct {
	tp *trace.TracerProvider
}

// Init 初始化 OpenTelemetry 追踪。
// endpoint: OTLP Collector 地址（如 "localhost:4318"）
// serviceName: 服务名称
// sampleRate: 采样率（0.0-1.0）
func Init(ctx context.Context, endpoint, serviceName string, sampleRate float64) (*Tracer, error) {
	exporter, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpoint(endpoint),
		otlptracehttp.WithInsecure(),
	)
	if err != nil {
		return nil, fmt.Errorf("创建追踪 exporter 失败: %w", err)
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(serviceName),
			semconv.ServiceVersion("1.0.0"),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("创建追踪资源失败: %w", err)
	}

	tp := trace.NewTracerProvider(
		trace.WithBatcher(exporter),
		trace.WithResource(res),
		trace.WithSampler(trace.TraceIDRatioBased(sampleRate)),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	return &Tracer{tp: tp}, nil
}

// Shutdown 优雅关闭追踪 exporter，刷新未发送的 span。
func (t *Tracer) Shutdown(ctx context.Context) error {
	if t.tp == nil {
		return nil
	}
	return t.tp.Shutdown(ctx)
}
