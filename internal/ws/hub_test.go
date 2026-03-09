package ws

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

// newTestWSPair 创建一对内存 WebSocket 连接（服务端 + 客户端）
func newTestWSPair(t *testing.T) (serverConn *websocket.Conn, clientConn *websocket.Conn) {
	t.Helper()
	upgrader := websocket.Upgrader{}
	connCh := make(chan *websocket.Conn, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade failed: %v", err)
			return
		}
		connCh <- c
	}))
	t.Cleanup(srv.Close)

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	cc, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	t.Cleanup(func() { _ = cc.Close() })

	sc := <-connCh
	t.Cleanup(func() { _ = sc.Close() })

	return sc, cc
}

// newTestClient 创建一个用于 Hub 测试的最小 Client（带真实 WS conn）
func newTestClient(t *testing.T, userID int64, hub *Hub, logger *zap.Logger) *Client {
	t.Helper()
	serverConn, _ := newTestWSPair(t)
	return NewClient(userID, serverConn, hub, nil, &noopRedisRepo{}, nil, "localhost:0", ClientConfig{}, logger)
}

func testLogger() *zap.Logger {
	l, _ := zap.NewDevelopment()
	return l
}

func TestHub_RegisterAndGetClient(t *testing.T) {
	hub := NewHub(testLogger())
	go hub.Run()
	defer hub.Stop()

	client := newTestClient(t, 100, hub, testLogger())
	hub.Register(client)

	// 等待 Hub 处理 register 事件
	time.Sleep(50 * time.Millisecond)

	got, ok := hub.GetClient(100)
	if !ok {
		t.Fatal("expected client to be registered")
	}
	if got != client {
		t.Error("GetClient returned wrong client pointer")
	}
}

func TestHub_OnlineCount(t *testing.T) {
	hub := NewHub(testLogger())
	go hub.Run()
	defer hub.Stop()

	if hub.OnlineCount() != 0 {
		t.Errorf("expected 0 online, got %d", hub.OnlineCount())
	}

	c1 := newTestClient(t, 1, hub, testLogger())
	c2 := newTestClient(t, 2, hub, testLogger())
	hub.Register(c1)
	hub.Register(c2)
	time.Sleep(50 * time.Millisecond)

	if hub.OnlineCount() != 2 {
		t.Errorf("expected 2 online, got %d", hub.OnlineCount())
	}
}

func TestHub_Unregister(t *testing.T) {
	hub := NewHub(testLogger())
	go hub.Run()
	defer hub.Stop()

	client := newTestClient(t, 200, hub, testLogger())
	hub.Register(client)
	time.Sleep(50 * time.Millisecond)

	hub.Unregister(client)
	time.Sleep(50 * time.Millisecond)

	_, ok := hub.GetClient(200)
	if ok {
		t.Error("expected client to be unregistered")
	}
	if hub.OnlineCount() != 0 {
		t.Errorf("expected 0 online, got %d", hub.OnlineCount())
	}
}

func TestHub_Unregister_OnlyCurrentConnection(t *testing.T) {
	hub := NewHub(testLogger())
	go hub.Run()
	defer hub.Stop()

	old := newTestClient(t, 300, hub, testLogger())
	new := newTestClient(t, 300, hub, testLogger())

	hub.Register(old)
	time.Sleep(50 * time.Millisecond)
	hub.Register(new) // 替换旧连接
	time.Sleep(50 * time.Millisecond)

	// 尝试注销旧连接（指针不匹配，不应删除）
	hub.Unregister(old)
	time.Sleep(50 * time.Millisecond)

	got, ok := hub.GetClient(300)
	if !ok {
		t.Fatal("expected new client to still be registered")
	}
	if got != new {
		t.Error("expected new client, got different pointer")
	}
}

func TestHub_ReplaceOldConnection(t *testing.T) {
	hub := NewHub(testLogger())
	go hub.Run()
	defer hub.Stop()

	old := newTestClient(t, 400, hub, testLogger())
	hub.Register(old)
	time.Sleep(50 * time.Millisecond)

	new := newTestClient(t, 400, hub, testLogger())
	hub.Register(new)
	time.Sleep(50 * time.Millisecond)

	got, ok := hub.GetClient(400)
	if !ok {
		t.Fatal("expected client to be registered")
	}
	if got != new {
		t.Error("expected new connection to replace old one")
	}
	if hub.OnlineCount() != 1 {
		t.Errorf("expected 1 online after replacement, got %d", hub.OnlineCount())
	}
}

func TestHub_Stop(t *testing.T) {
	hub := NewHub(testLogger())
	done := make(chan struct{})
	go func() {
		hub.Run()
		close(done)
	}()

	c1 := newTestClient(t, 500, hub, testLogger())
	c2 := newTestClient(t, 501, hub, testLogger())
	hub.Register(c1)
	hub.Register(c2)
	time.Sleep(50 * time.Millisecond)

	hub.Stop()

	select {
	case <-done:
		// Hub.Run() exited
	case <-time.After(2 * time.Second):
		t.Fatal("hub.Run() did not exit after Stop()")
	}

	if hub.OnlineCount() != 0 {
		t.Errorf("expected 0 online after stop, got %d", hub.OnlineCount())
	}
}
