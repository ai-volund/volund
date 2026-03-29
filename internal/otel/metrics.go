package otel

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var meter = otel.Meter("volund")

// Gateway metrics.
var (
	DispatchTotal     metric.Int64Counter
	DispatchDuration  metric.Float64Histogram
	ClaimTotal        metric.Int64Counter
	ClaimDuration     metric.Float64Histogram
	ActiveInstances   metric.Int64UpDownCounter
	WarmPoolSize      metric.Int64UpDownCounter
	HTTPRequestTotal  metric.Int64Counter
	HTTPRequestDur    metric.Float64Histogram
)

func init() {
	var err error

	DispatchTotal, err = meter.Int64Counter("volund.dispatch.total",
		metric.WithDescription("Total tasks dispatched"))
	if err != nil {
		panic(err)
	}

	DispatchDuration, err = meter.Float64Histogram("volund.dispatch.duration_seconds",
		metric.WithDescription("Time to dispatch a task"),
		metric.WithUnit("s"))
	if err != nil {
		panic(err)
	}

	ClaimTotal, err = meter.Int64Counter("volund.claim.total",
		metric.WithDescription("Total pod claim attempts"))
	if err != nil {
		panic(err)
	}

	ClaimDuration, err = meter.Float64Histogram("volund.claim.duration_seconds",
		metric.WithDescription("Time to claim a warm pool pod"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.01, 0.025, 0.05, 0.1, 0.2, 0.3, 0.5, 0.75, 1.0, 2.0, 5.0))
	if err != nil {
		panic(err)
	}

	ActiveInstances, err = meter.Int64UpDownCounter("volund.active_instances",
		metric.WithDescription("Currently active agent instances"))
	if err != nil {
		panic(err)
	}

	WarmPoolSize, err = meter.Int64UpDownCounter("volund.warm_pool.size",
		metric.WithDescription("Current warm pool size (pods available for claiming)"))
	if err != nil {
		panic(err)
	}

	HTTPRequestTotal, err = meter.Int64Counter("volund.http.request.total",
		metric.WithDescription("Total HTTP requests"))
	if err != nil {
		panic(err)
	}

	HTTPRequestDur, err = meter.Float64Histogram("volund.http.request.duration_seconds",
		metric.WithDescription("HTTP request duration"),
		metric.WithUnit("s"))
	if err != nil {
		panic(err)
	}
}

// dispatchAttrs is the shared attribute set for dispatch metrics.
func dispatchAttrSet(profile, method string) attribute.Set {
	return attribute.NewSet(
		attribute.String("profile", profile),
		attribute.String("method", method),
	)
}

// DispatchAddAttrs returns AddOption for counters.
func DispatchAddAttrs(profile, method string) metric.AddOption {
	return metric.WithAttributeSet(dispatchAttrSet(profile, method))
}

// DispatchRecordAttrs returns RecordOption for histograms.
func DispatchRecordAttrs(profile, method string) metric.RecordOption {
	return metric.WithAttributeSet(dispatchAttrSet(profile, method))
}

// ClaimAttrs returns common metric attributes for claim operations (counters).
func ClaimAttrs(result string) metric.AddOption {
	return metric.WithAttributes(attribute.String("result", result))
}

// ClaimRecordAttrs returns common metric attributes for claim duration recording (histograms).
func ClaimRecordAttrs(result, path string) metric.RecordOption {
	return metric.WithAttributes(
		attribute.String("result", result),
		attribute.String("path", path), // "cache_hit", "db_claim", "unavailable"
	)
}

// WarmPoolAttrs returns common metric attributes for warm pool gauge.
func WarmPoolAttrs(profile string) metric.AddOption {
	return metric.WithAttributes(attribute.String("profile", profile))
}
