package otel

import (
	"context"

	"go.opentelemetry.io/otel"
)

// NATSCarrier adapts a string map for OTel context propagation through NATS.
// The carrier is serialized alongside the task JSON payload.
type NATSCarrier map[string]string

func (c NATSCarrier) Get(key string) string    { return c[key] }
func (c NATSCarrier) Set(key, value string)     { c[key] = value }
func (c NATSCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	return keys
}

// InjectContext serializes the trace context from ctx into a carrier map.
func InjectContext(ctx context.Context) NATSCarrier {
	carrier := make(NATSCarrier)
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	return carrier
}

// ExtractContext deserializes trace context from a carrier into a new context.
func ExtractContext(ctx context.Context, carrier NATSCarrier) context.Context {
	return otel.GetTextMapPropagator().Extract(ctx, carrier)
}
