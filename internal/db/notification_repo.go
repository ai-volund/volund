package db

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Notification represents a user notification.
type Notification struct {
	ID        string          `json:"id"`
	TenantID  string          `json:"tenant_id"`
	UserID    string          `json:"user_id"`
	Type      string          `json:"type"`
	Title     string          `json:"title"`
	Body      string          `json:"body"`
	Read      bool            `json:"read"`
	Metadata  json.RawMessage `json:"metadata"`
	CreatedAt time.Time       `json:"created_at"`
}

// NotificationRepo handles the notifications table.
type NotificationRepo struct{ pool *Pool }

func NewNotificationRepo(pool *Pool) *NotificationRepo {
	return &NotificationRepo{pool: pool}
}

// Create inserts a notification.
func (r *NotificationRepo) Create(ctx context.Context, n *Notification) error {
	if n.Metadata == nil {
		n.Metadata = json.RawMessage("{}")
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO notifications (tenant_id, user_id, type, title, body, metadata)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		n.TenantID, n.UserID, n.Type, n.Title, n.Body, n.Metadata)
	return err
}

// List returns notifications for a user, unread first.
func (r *NotificationRepo) List(ctx context.Context, userID string, limit int) ([]*Notification, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, user_id, type, title, body, read, metadata, created_at
		FROM notifications WHERE user_id = $1
		ORDER BY read ASC, created_at DESC LIMIT $2`, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("list notifications: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (*Notification, error) {
		var n Notification
		err := row.Scan(&n.ID, &n.TenantID, &n.UserID, &n.Type, &n.Title, &n.Body, &n.Read, &n.Metadata, &n.CreatedAt)
		return &n, err
	})
}

// MarkRead marks a notification as read.
func (r *NotificationRepo) MarkRead(ctx context.Context, id, userID string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE notifications SET read = true WHERE id = $1 AND user_id = $2`, id, userID)
	return err
}

// MarkAllRead marks all notifications for a user as read.
func (r *NotificationRepo) MarkAllRead(ctx context.Context, userID string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE notifications SET read = true WHERE user_id = $1 AND read = false`, userID)
	return err
}

// CountUnread returns the count of unread notifications for a user.
func (r *NotificationRepo) CountUnread(ctx context.Context, userID string) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM notifications WHERE user_id = $1 AND read = false`, userID).Scan(&count)
	return count, err
}
