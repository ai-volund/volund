package dispatch

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	votel "github.com/ai-volund/volund/internal/otel"
)

var tracer = otel.Tracer("volund/dispatch")

// MaxDelegationDepth is the maximum allowed nesting for agent-to-agent delegation.
const MaxDelegationDepth = 5

// Dispatcher publishes tasks to the NATS agent pool and steering messages
// to specific agent instances.
type Dispatcher struct {
	conn *nats.Conn
	noop bool
}

// Connect creates a Dispatcher connected to the given NATS server.
// If natsURL is empty a no-op dispatcher is returned (useful when NATS
// is not configured; tasks will be silently dropped).
func Connect(natsURL string) (*Dispatcher, error) {
	if natsURL == "" {
		slog.Warn("no NATS URL configured, task dispatch is no-op")
		return &Dispatcher{noop: true}, nil
	}
	conn, err := nats.Connect(natsURL)
	if err != nil {
		return nil, fmt.Errorf("dispatch: connect to NATS at %s: %w", natsURL, err)
	}
	slog.Info("dispatch: connected to NATS", "url", natsURL)
	return &Dispatcher{conn: conn}, nil
}

// Dispatch publishes a Task to the agent pool for the given profile.
// NATS queue group semantics ensure exactly one warm pod receives it.
func (d *Dispatcher) Dispatch(ctx context.Context, task *Task) error {
	if task.DelegationDepth > MaxDelegationDepth {
		return fmt.Errorf("dispatch: delegation depth %d exceeds max %d (possible cycle)", task.DelegationDepth, MaxDelegationDepth)
	}

	ctx, span := tracer.Start(ctx, "dispatch.pool",
		trace.WithAttributes(
			attribute.String("task_id", task.TaskID),
			attribute.String("conv_id", task.ConversationID),
			attribute.String("profile", task.ProfileName),
			attribute.Int("delegation_depth", task.DelegationDepth),
		))
	defer span.End()

	if d.noop {
		slog.WarnContext(ctx, "dispatch no-op: NATS not configured", "task_id", task.TaskID, "conv_id", task.ConversationID)
		return nil
	}

	// Inject trace context into task for cross-service propagation.
	task.TraceContext = votel.InjectContext(ctx)

	start := time.Now()
	data, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("dispatch: marshal task: %w", err)
	}
	subject := PoolSubject(task.ProfileName)
	if err := d.conn.Publish(subject, data); err != nil {
		return fmt.Errorf("dispatch: publish to %s: %w", subject, err)
	}

	votel.DispatchTotal.Add(ctx, 1, votel.DispatchAddAttrs(task.ProfileName, "pool"))
	votel.DispatchDuration.Record(ctx, time.Since(start).Seconds(), votel.DispatchRecordAttrs(task.ProfileName, "pool"))

	slog.InfoContext(ctx, "task dispatched",
		"task_id", task.TaskID,
		"conv_id", task.ConversationID,
		"profile", task.ProfileName,
		"subject", subject,
	)
	return nil
}

// DispatchToInstance sends a task directly to a specific agent instance,
// bypassing the pool queue group. Used when the gateway has claimed a warm
// pod for the conversation.
func (d *Dispatcher) DispatchToInstance(ctx context.Context, task *Task, instanceID string) error {
	ctx, span := tracer.Start(ctx, "dispatch.instance",
		trace.WithAttributes(
			attribute.String("task_id", task.TaskID),
			attribute.String("conv_id", task.ConversationID),
			attribute.String("instance_id", instanceID),
		))
	defer span.End()

	if d.noop {
		slog.WarnContext(ctx, "dispatch no-op: NATS not configured", "task_id", task.TaskID, "instance_id", instanceID)
		return nil
	}
	task.InstanceID = instanceID

	// Inject trace context into task for cross-service propagation.
	task.TraceContext = votel.InjectContext(ctx)

	start := time.Now()
	data, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("dispatch: marshal task: %w", err)
	}
	subject := InstanceSubject(instanceID)
	if err := d.conn.Publish(subject, data); err != nil {
		return fmt.Errorf("dispatch: publish to %s: %w", subject, err)
	}

	votel.DispatchTotal.Add(ctx, 1, votel.DispatchAddAttrs(task.ProfileName, "instance"))
	votel.DispatchDuration.Record(ctx, time.Since(start).Seconds(), votel.DispatchRecordAttrs(task.ProfileName, "instance"))

	slog.InfoContext(ctx, "task dispatched to instance",
		"task_id", task.TaskID,
		"conv_id", task.ConversationID,
		"instance_id", instanceID,
		"subject", subject,
	)
	return nil
}

// Steer publishes a mid-run correction to a specific agent instance.
func (d *Dispatcher) Steer(_ context.Context, instanceID, content string) error {
	if d.noop {
		return nil
	}
	data, err := json.Marshal(SteerMessage{Content: content})
	if err != nil {
		return fmt.Errorf("dispatch: marshal steer: %w", err)
	}
	subject := SteerSubject(instanceID)
	if err := d.conn.Publish(subject, data); err != nil {
		return fmt.Errorf("dispatch: publish steer to %s: %w", subject, err)
	}
	return nil
}

// Close drains and closes the NATS connection.
func (d *Dispatcher) Close() {
	if d.conn != nil {
		d.conn.Drain() //nolint:errcheck
	}
}

// NewTaskID generates a random task ID.
func NewTaskID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("task-%x", b)
}
