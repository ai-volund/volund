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
	ActiveInstances   metric.Int64UpDownCounter
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

	ActiveInstances, err = meter.Int64UpDownCounter("volund.active_instances",
		metric.WithDescription("Currently active agent instances"))
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

// ClaimAttrs returns common metric attributes for claim operations.
func ClaimAttrs(result string) metric.AddOption {
	return metric.WithAttributes(attribute.String("result", result))
}
