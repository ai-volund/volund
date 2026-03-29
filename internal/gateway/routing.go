package gateway

import (
	"bufio"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

// RoutingTable is the interface for the conversation → instance routing cache.
// All gateway replicas share the same table via Redis so that any gateway can
// resolve which agent instance is handling a given conversation.
type RoutingTable interface {
	// Get returns the instance ID for a conversation, or "" if not set.
	Get(convID string) string
	// Set records which instance is handling a conversation.
	// The entry expires after ttl; zero means use the default TTL.
	Set(convID, instanceID string, ttl time.Duration)
	// Delete removes the routing entry.
	Delete(convID string)
	// Close releases any resources (connections, etc.).
	Close()
}

// ---------- Redis routing table ----------

const (
	routingKeyPrefix = "rt:" // Redis key prefix for routing entries
	defaultRouteTTL  = 35 * time.Minute
	redisDialTimeout = 3 * time.Second
	redisRWTimeout   = 2 * time.Second
)

// RedisRoutingTable stores conversation→instance routing in Redis so all
// gateway replicas share a consistent view.
type RedisRoutingTable struct {
	addr string
	mu   sync.Mutex
	conn net.Conn
	rd   *bufio.Reader
}

// NewRedisRoutingTable connects to Redis and returns a shared routing table.
// Returns an error if the initial connection fails.
func NewRedisRoutingTable(addr string) (*RedisRoutingTable, error) {
	rt := &RedisRoutingTable{addr: addr}
	if err := rt.connect(); err != nil {
		return nil, fmt.Errorf("redis routing table: %w", err)
	}
	slog.Info("redis routing table connected", "addr", addr)
	return rt, nil
}

func (rt *RedisRoutingTable) connect() error {
	conn, err := net.DialTimeout("tcp", rt.addr, redisDialTimeout)
	if err != nil {
		return err
	}
	rt.conn = conn
	rt.rd = bufio.NewReader(conn)
	return nil
}

// reconnect drops the current connection and establishes a new one.
func (rt *RedisRoutingTable) reconnect() error {
	if rt.conn != nil {
		rt.conn.Close()
	}
	return rt.connect()
}

func (rt *RedisRoutingTable) key(convID string) string {
	return routingKeyPrefix + convID
}

// do sends a RESP command and reads the reply line.
func (rt *RedisRoutingTable) do(args ...string) (string, error) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return rt.doLocked(args...)
}

func (rt *RedisRoutingTable) doLocked(args ...string) (string, error) {
	if rt.conn == nil {
		if err := rt.reconnect(); err != nil {
			return "", err
		}
	}

	// Build RESP array.
	var buf strings.Builder
	fmt.Fprintf(&buf, "*%d\r\n", len(args))
	for _, a := range args {
		fmt.Fprintf(&buf, "$%d\r\n%s\r\n", len(a), a)
	}

	_ = rt.conn.SetWriteDeadline(time.Now().Add(redisRWTimeout))
	if _, err := io.WriteString(rt.conn, buf.String()); err != nil {
		// Reconnect on write failure.
		_ = rt.reconnect()
		return "", err
	}

	_ = rt.conn.SetReadDeadline(time.Now().Add(redisRWTimeout))
	line, err := rt.rd.ReadString('\n')
	if err != nil {
		_ = rt.reconnect()
		return "", err
	}
	line = strings.TrimRight(line, "\r\n")

	if len(line) == 0 {
		return "", fmt.Errorf("empty redis response")
	}

	switch line[0] {
	case '+': // simple string
		return line[1:], nil
	case '-': // error
		return "", fmt.Errorf("redis: %s", line[1:])
	case ':': // integer
		return line[1:], nil
	case '$': // bulk string
		n, _ := strconv.Atoi(line[1:])
		if n < 0 {
			return "", nil // nil bulk string
		}
		data := make([]byte, n+2) // +2 for \r\n
		if _, err := io.ReadFull(rt.rd, data); err != nil {
			_ = rt.reconnect()
			return "", err
		}
		return string(data[:n]), nil
	default:
		return line, nil
	}
}

func (rt *RedisRoutingTable) Get(convID string) string {
	val, err := rt.do("GET", rt.key(convID))
	if err != nil {
		slog.Debug("routing table GET failed", "conv_id", convID, "error", err)
		return ""
	}
	return val
}

func (rt *RedisRoutingTable) Set(convID, instanceID string, ttl time.Duration) {
	if ttl == 0 {
		ttl = defaultRouteTTL
	}
	secs := strconv.Itoa(int(ttl.Seconds()))
	if _, err := rt.do("SET", rt.key(convID), instanceID, "EX", secs); err != nil {
		slog.Warn("routing table SET failed", "conv_id", convID, "error", err)
	}
}

func (rt *RedisRoutingTable) Delete(convID string) {
	if _, err := rt.do("DEL", rt.key(convID)); err != nil {
		slog.Warn("routing table DEL failed", "conv_id", convID, "error", err)
	}
}

func (rt *RedisRoutingTable) Close() {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.conn != nil {
		rt.conn.Close()
		rt.conn = nil
	}
}

// ---------- In-memory routing table (fallback) ----------

// MemoryRoutingTable is a process-local routing table used when Redis is not
// available. Only works for single-gateway deployments.
type MemoryRoutingTable struct {
	mu    sync.RWMutex
	table map[string]string
}

// NewMemoryRoutingTable creates an in-memory routing table.
func NewMemoryRoutingTable() *MemoryRoutingTable {
	return &MemoryRoutingTable{table: make(map[string]string)}
}

func (m *MemoryRoutingTable) Get(convID string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.table[convID]
}

func (m *MemoryRoutingTable) Set(convID, instanceID string, _ time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.table[convID] = instanceID
}

func (m *MemoryRoutingTable) Delete(convID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.table, convID)
}

func (m *MemoryRoutingTable) Close() {}
