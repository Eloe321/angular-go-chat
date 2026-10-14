package transport

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"chat-app/internal/chat"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

func newTestService(t *testing.T, config Config) (*Server, *httptest.Server) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	service := New(config)
	server := httptest.NewServer(service)
	server.Client().Timeout = 2 * time.Second
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := service.Shutdown(ctx); err != nil {
			t.Error(err)
		}
		server.Close()
	})
	return service, server
}
func dialGuest(t *testing.T, server *httptest.Server, room, name string, headers http.Header) *websocket.Conn {
	t.Helper()
	dialer := websocket.Dialer{HandshakeTimeout: 2 * time.Second}
	endpoint := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws?" + url.Values{"room": {room}, "name": {name}}.Encode()
	conn, resp, err := dialer.Dial(endpoint, headers)
	if err != nil {
		if resp != nil {
			defer resp.Body.Close()
		}
		t.Fatalf("dial guest: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	first := nextEvent(t, conn)
	if first["type"] != "session.ready" {
		t.Fatalf("first event: %v", first)
	}
	return conn
}
func nextEvent(t *testing.T, conn *websocket.Conn) map[string]any {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var event map[string]any
	if err := conn.ReadJSON(&event); err != nil {
		t.Fatal(err)
	}
	return event
}
func nextType(t *testing.T, conn *websocket.Conn, want string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		_ = conn.SetReadDeadline(deadline)
		var event map[string]any
		if err := conn.ReadJSON(&event); err != nil {
			t.Fatal(err)
		}
		if event["type"] == want {
			return event
		}
	}
}
func TestHealthAndHistoryRoutes(t *testing.T) {
	store := chat.NewMemoryStore()
	_ = store.Append(chat.Message{ID: "one", RoomID: "general", Content: "hello"})
	_, server := newTestService(t, Config{Store: store})
	for _, tc := range []struct {
		path   string
		status int
		field  string
	}{
		{"/healthz", 200, "status"},
		{"/api/rooms/general/messages", 200, "messages"},
		{"/api/rooms/missing/messages", 404, "code"},
		{"/missing", 404, "code"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			resp, err := server.Client().Get(server.URL + tc.path)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tc.status {
				t.Fatalf("status %d, want %d", resp.StatusCode, tc.status)
			}
			var body map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body[tc.field] == nil {
				t.Fatalf("missing %s in %v", tc.field, body)
			}
		})
	}
}
func TestSocketInvalidCommandsKeepConnectionUsable(t *testing.T) {
	_, server := newTestService(t, Config{})
	conn := dialGuest(t, server, "general", "Alice", nil)
	for _, tc := range []struct{ payload, code string }{
		{`{`, "invalid_command"},
		{`{"type":"bogus","content":"hello"}`, "unsupported_type"},
		{`{"type":7,"content":"hello"}`, "invalid_command"},
		{`{"type":"message.send","content":7}`, "invalid_command"},
	} {
		if err := conn.WriteMessage(websocket.TextMessage, []byte(tc.payload)); err != nil {
			t.Fatal(err)
		}
		event := nextType(t, conn, "error")
		if event["code"] != tc.code || event["message"] == "" {
			t.Fatalf("event %v, want %s", event, tc.code)
		}
	}
	if err := conn.WriteJSON(map[string]string{"type": "message.send", "content": "valid afterwards"}); err != nil {
		t.Fatal(err)
	}
	event := nextType(t, conn, "message.created")
	if event["message"] == nil {
		t.Fatal("missing accepted message")
	}
}
func TestOversizedFrameDisconnectsOnlySender(t *testing.T) {
	_, server := newTestService(t, Config{})
	bad := dialGuest(t, server, "general", "Oversized", nil)
	good := dialGuest(t, server, "general", "Healthy", nil)
	_ = bad.SetWriteDeadline(time.Now().Add(2 * time.Second))
	if err := bad.WriteMessage(websocket.TextMessage, []byte(strings.Repeat("x", 8193))); err != nil {
		t.Fatal(err)
	}
	_ = bad.SetReadDeadline(time.Now().Add(2 * time.Second))
	for {
		_, _, err := bad.ReadMessage()
		if err != nil {
			if !websocket.IsCloseError(err, websocket.CloseMessageTooBig) {
				t.Fatalf("close error %v", err)
			}
			break
		}
	}
	if err := good.WriteJSON(map[string]string{"type": "message.send", "content": "still working"}); err != nil {
		t.Fatal(err)
	}
	_ = nextType(t, good, "message.created")
	resp, err := server.Client().Get(server.URL + "/api/rooms/general/messages")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body struct {
		Messages []chat.Message `json:"messages"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Messages) != 1 {
		t.Fatalf("history %+v", body.Messages)
	}
}
func TestExplicitOriginsAndOriginlessClients(t *testing.T) {
	_, server := newTestService(t, Config{AllowedOrigins: []string{"https://chat.example"}})
	for _, origin := range []string{"", "https://chat.example"} {
		var headers http.Header
		if origin != "" {
			headers = http.Header{"Origin": {origin}}
		}
		_ = dialGuest(t, server, "go", "Guest", headers)
	}
	dialer := websocket.Dialer{HandshakeTimeout: 2 * time.Second}
	for _, headers := range []http.Header{{"Origin": {"http://localhost:4200"}}, {"Origin": {""}}, {"Origin": {"https://chat.example", "https://evil.example"}}} {
		conn, resp, err := dialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ws?room=go&name=Guest", headers)
		if conn != nil {
			_ = conn.Close()
			t.Fatal("disallowed origin accepted")
		}
		if err == nil || resp == nil {
			t.Fatalf("missing rejection: %v", err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != 403 {
			t.Fatalf("status %d, want 403", resp.StatusCode)
		}
	}
}
func TestShutdownClosesSocketsAndRejectsNewGuests(t *testing.T) {
	service, server := newTestService(t, Config{})
	conn := dialGuest(t, server, "general", "Guest", nil)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := service.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if err := service.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			if strings.Contains(err.Error(), "i/o timeout") {
				t.Fatal("shutdown left socket open")
			}
			break
		}
	}
	resp, err := server.Client().Get(server.URL + "/ws?room=general&name=New")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 503 {
		t.Fatalf("new guest status %d, want 503", resp.StatusCode)
	}
}
