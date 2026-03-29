package gateway

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------- MemoryRoutingTable tests ----------

func TestMemoryRoutingTable_GetSetDelete(t *testing.T) {
	rt := NewMemoryRoutingTable()

	// Get non-existent key returns "".
	if got := rt.Get("conv-1"); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}

	// Set and get.
	rt.Set("conv-1", "inst-abc", 0)
	if got := rt.Get("conv-1"); got != "inst-abc" {
		t.Fatalf("expected inst-abc, got %q", got)
	}

	// Overwrite.
	rt.Set("conv-1", "inst-xyz", 0)
	if got := rt.Get("conv-1"); got != "inst-xyz" {
		t.Fatalf("expected inst-xyz, got %q", got)
	}

	// Delete.
	rt.Delete("conv-1")
	if got := rt.Get("conv-1"); got != "" {
		t.Fatalf("expected empty after delete, got %q", got)
	}

	// Delete non-existent is a no-op.
	rt.Delete("conv-nonexistent")
}

func TestMemoryRoutingTable_ConcurrentAccess(t *testing.T) {
	rt := NewMemoryRoutingTable()
	var wg sync.WaitGroup

	// Concurrent writers.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			key := fmt.Sprintf("conv-%d", n)
			rt.Set(key, fmt.Sprintf("inst-%d", n), 0)
			rt.Get(key)
			rt.Delete(key)
		}(i)
	}
	wg.Wait()
}

func TestMemoryRoutingTable_Close(t *testing.T) {
	rt := NewMemoryRoutingTable()
	rt.Set("conv-1", "inst-1", 0)
	rt.Close() // should not panic
}

// ---------- RedisRoutingTable tests ----------

// mockRedisServer is a minimal Redis server for testing the RESP protocol client.
type mockRedisServer struct {
	listener net.Listener
	store    map[string]string
	mu       sync.Mutex
	wg       sync.WaitGroup
}

func newMockRedisServer(t *testing.T) *mockRedisServer {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s := &mockRedisServer{
		listener: lis,
		store:    make(map[string]string),
	}
	s.wg.Add(1)
	go s.serve(t)
	return s
}

func (s *mockRedisServer) Addr() string { return s.listener.Addr().String() }

func (s *mockRedisServer) Close() {
	s.listener.Close()
	s.wg.Wait()
}

func (s *mockRedisServer) serve(t *testing.T) {
	defer s.wg.Done()
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		s.wg.Add(1)
		go s.handleConn(t, conn)
	}
}

func (s *mockRedisServer) handleConn(t *testing.T, conn net.Conn) {
	defer s.wg.Done()
	defer conn.Close()
	rd := bufio.NewReader(conn)

	for {
		args, err := readRESPArray(rd)
		if err != nil {
			return
		}
		if len(args) == 0 {
			continue
		}

		cmd := strings.ToUpper(args[0])
		s.mu.Lock()
		switch cmd {
		case "GET":
			if len(args) < 2 {
				io.WriteString(conn, "-ERR wrong number of arguments\r\n")
			} else {
				val, ok := s.store[args[1]]
				if !ok {
					io.WriteString(conn, "$-1\r\n")
				} else {
					fmt.Fprintf(conn, "$%d\r\n%s\r\n", len(val), val)
				}
			}
		case "SET":
			if len(args) < 3 {
				io.WriteString(conn, "-ERR wrong number of arguments\r\n")
			} else {
				s.store[args[1]] = args[2]
				io.WriteString(conn, "+OK\r\n")
			}
		case "DEL":
			if len(args) < 2 {
				io.WriteString(conn, "-ERR wrong number of arguments\r\n")
			} else {
				n := 0
				for _, key := range args[1:] {
					if _, ok := s.store[key]; ok {
						delete(s.store, key)
						n++
					}
				}
				fmt.Fprintf(conn, ":%d\r\n", n)
			}
		default:
			io.WriteString(conn, "+OK\r\n") // catch-all
		}
		s.mu.Unlock()
	}
}

// readRESPArray reads one RESP array from a buffered reader.
func readRESPArray(rd *bufio.Reader) ([]string, error) {
	line, err := rd.ReadString('\n')
	if err != nil {
		return nil, err
	}
	line = strings.TrimRight(line, "\r\n")
	if len(line) == 0 || line[0] != '*' {
		return nil, fmt.Errorf("expected array, got %q", line)
	}
	var count int
	fmt.Sscanf(line[1:], "%d", &count)
	args := make([]string, 0, count)
	for i := 0; i < count; i++ {
		line, err := rd.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if len(line) == 0 || line[0] != '$' {
			return nil, fmt.Errorf("expected bulk string, got %q", line)
		}
		var n int
		fmt.Sscanf(line[1:], "%d", &n)
		buf := make([]byte, n+2) // +2 for \r\n
		if _, err := io.ReadFull(rd, buf); err != nil {
			return nil, err
		}
		args = append(args, string(buf[:n]))
	}
	return args, nil
}

func TestRedisRoutingTable_GetSetDelete(t *testing.T) {
	srv := newMockRedisServer(t)
	defer srv.Close()

	rt, err := NewRedisRoutingTable(srv.Addr())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer rt.Close()

	// Get non-existent.
	if got := rt.Get("conv-1"); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}

	// Set and get.
	rt.Set("conv-1", "inst-abc", 30*time.Minute)
	if got := rt.Get("conv-1"); got != "inst-abc" {
		t.Fatalf("expected inst-abc, got %q", got)
	}

	// Overwrite.
	rt.Set("conv-1", "inst-xyz", 30*time.Minute)
	if got := rt.Get("conv-1"); got != "inst-xyz" {
		t.Fatalf("expected inst-xyz, got %q", got)
	}

	// Delete.
	rt.Delete("conv-1")
	if got := rt.Get("conv-1"); got != "" {
		t.Fatalf("expected empty after delete, got %q", got)
	}
}

func TestRedisRoutingTable_KeyPrefix(t *testing.T) {
	srv := newMockRedisServer(t)
	defer srv.Close()

	rt, err := NewRedisRoutingTable(srv.Addr())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer rt.Close()

	rt.Set("conv-abc", "inst-1", 0)

	// Verify the key in Redis has the prefix.
	srv.mu.Lock()
	_, ok := srv.store["rt:conv-abc"]
	srv.mu.Unlock()
	if !ok {
		t.Fatal("expected key with rt: prefix in Redis store")
	}
}

func TestRedisRoutingTable_DefaultTTL(t *testing.T) {
	// The SET command should include EX with the default TTL.
	// Our mock server accepts it but doesn't enforce TTL — we just verify the command
	// goes through without error.
	srv := newMockRedisServer(t)
	defer srv.Close()

	rt, err := NewRedisRoutingTable(srv.Addr())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer rt.Close()

	rt.Set("conv-ttl", "inst-1", 0) // 0 means default TTL
	if got := rt.Get("conv-ttl"); got != "inst-1" {
		t.Fatalf("expected inst-1, got %q", got)
	}
}

func TestRedisRoutingTable_ConnectionFailure(t *testing.T) {
	_, err := NewRedisRoutingTable("127.0.0.1:1") // nothing listening
	if err == nil {
		t.Fatal("expected error connecting to invalid address")
	}
}

func TestRedisRoutingTable_MultipleKeys(t *testing.T) {
	srv := newMockRedisServer(t)
	defer srv.Close()

	rt, err := NewRedisRoutingTable(srv.Addr())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer rt.Close()

	rt.Set("conv-1", "inst-a", 0)
	rt.Set("conv-2", "inst-b", 0)
	rt.Set("conv-3", "inst-c", 0)

	if got := rt.Get("conv-1"); got != "inst-a" {
		t.Fatalf("expected inst-a, got %q", got)
	}
	if got := rt.Get("conv-2"); got != "inst-b" {
		t.Fatalf("expected inst-b, got %q", got)
	}
	if got := rt.Get("conv-3"); got != "inst-c" {
		t.Fatalf("expected inst-c, got %q", got)
	}

	// Delete one, others persist.
	rt.Delete("conv-2")
	if got := rt.Get("conv-2"); got != "" {
		t.Fatalf("expected empty after delete, got %q", got)
	}
	if got := rt.Get("conv-1"); got != "inst-a" {
		t.Fatalf("conv-1 should still be inst-a, got %q", got)
	}
}

// ---------- Interface compliance ----------

var _ RoutingTable = (*RedisRoutingTable)(nil)
var _ RoutingTable = (*MemoryRoutingTable)(nil)
