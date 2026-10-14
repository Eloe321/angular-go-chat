package transport

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// net.Pipe makes the handshake response block until its peer reads it. The
// optional gate also permits a successful response write to finish after shutdown.
type handshakeConn struct {
	net.Conn
	started    chan struct{}
	completed  chan struct{}
	release    <-chan struct{}
	firstWrite sync.Once
}

func (c *handshakeConn) Write(data []byte) (int, error) {
	first := false
	c.firstWrite.Do(func() { first = true; close(c.started) })
	n, err := c.Conn.Write(data)
	if first && err == nil && c.release != nil {
		close(c.completed)
		<-c.release
	}
	return n, err
}

type pipeResponseWriter struct {
	*httptest.ResponseRecorder
	conn net.Conn
}

func (w *pipeResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.conn, bufio.NewReadWriter(bufio.NewReader(w.conn), bufio.NewWriter(w.conn)), nil
}
func startPipeHandshake(t *testing.T, service *Server, release <-chan struct{}) (*handshakeConn, net.Conn, <-chan struct{}, *http.Request) {
	t.Helper()
	server, peer := net.Pipe()
	conn := &handshakeConn{Conn: server, started: make(chan struct{}), completed: make(chan struct{}), release: release}
	request := httptest.NewRequest(http.MethodGet, "http://chat.example/ws?room=general&name=Handshake", nil)
	request.Header.Set("Connection", "Upgrade")
	request.Header.Set("Upgrade", "websocket")
	request.Header.Set("Sec-WebSocket-Version", "13")
	request.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	done := make(chan struct{})
	go func() {
		defer close(done)
		service.ServeHTTP(&pipeResponseWriter{ResponseRecorder: httptest.NewRecorder(), conn: conn}, request)
	}()
	t.Cleanup(func() { _ = peer.Close(); _ = server.Close() })
	select {
	case <-conn.started:
	case <-time.After(time.Second):
		t.Fatal("handshake response write did not start")
	}
	return conn, peer, done, request
}
func waitForHandler(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("handshake handler did not stop")
	}
}
func boundedShutdown(t *testing.T, service *Server) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- service.Shutdown(ctx) }()
	select {
	case err := <-result:
		return err
	case <-time.After(200 * time.Millisecond):
		t.Fatal("shutdown remained blocked beyond its 20ms deadline during a handshake write")
		return nil
	}
}
func TestShutdownDeadlineDuringBlockedHandshakeWrite(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := New(Config{})
	_, peer, handlerDone, _ := startPipeHandshake(t, service, nil)
	defer func() {
		_ = peer.Close()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = service.Shutdown(ctx)
	}()
	err := boundedShutdown(t, service)
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	waitForHandler(t, handlerDone)
}
func TestCompletingHandshakeCannotEscapeShutdown(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := New(Config{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseWrite := func() { releaseOnce.Do(func() { close(release) }) }
	conn, peer, handlerDone, request := startPipeHandshake(t, service, release)
	defer func() {
		releaseWrite()
		_ = peer.Close()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = service.Shutdown(ctx)
	}()
	reader := bufio.NewReader(peer)
	_ = peer.SetReadDeadline(time.Now().Add(time.Second))
	response, err := http.ReadResponse(reader, request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status %d", response.StatusCode)
	}
	select {
	case <-conn.completed:
	case <-time.After(time.Second):
		t.Fatal("handshake response was not written")
	}
	if err := boundedShutdown(t, service); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("shutdown must wait for the pending handler: %v", err)
	}
	_ = peer.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	if _, err := reader.ReadByte(); !errors.Is(err, io.EOF) {
		t.Fatalf("pending connection escaped shutdown: %v", err)
	}
	releaseWrite()
	waitForHandler(t, handlerDone)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := service.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
}
func TestStalledHandshakeDoesNotBlockOtherGuests(t *testing.T) {
	service, server := newTestService(t, Config{})
	_, peer, handlerDone, _ := startPipeHandshake(t, service, nil)
	defer func() { _ = peer.Close(); waitForHandler(t, handlerDone) }()
	type result struct {
		conn *websocket.Conn
		err  error
	}
	ready := make(chan result, 1)
	go func() {
		dialer := websocket.Dialer{HandshakeTimeout: time.Second}
		conn, response, err := dialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ws?room=general&name=Healthy", nil)
		if err != nil {
			if response != nil {
				_ = response.Body.Close()
			}
			ready <- result{err: err}
			return
		}
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		var event map[string]any
		err = conn.ReadJSON(&event)
		if err == nil && event["type"] != "session.ready" {
			err = fmt.Errorf("first event: %v", event)
		}
		ready <- result{conn: conn, err: err}
	}()
	select {
	case r := <-ready:
		if r.conn != nil {
			defer r.conn.Close()
		}
		if r.err != nil {
			t.Fatal(r.err)
		}
	case <-time.After(200 * time.Millisecond):
		_ = peer.Close()
		r := <-ready
		if r.conn != nil {
			_ = r.conn.Close()
		}
		t.Fatal("a stalled handshake blocked an unrelated guest")
	}
}

func TestHandshakeResponseWriteHasBoundedTimeout(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := New(Config{})
	if timeout := service.upgrader.HandshakeTimeout; timeout <= 0 || timeout > 5*time.Second {
		t.Fatalf("unbounded handshake timeout: %s", timeout)
	}
	// Exercise the actual Gorilla/net.Pipe write deadline without waiting 5s.
	service.upgrader.HandshakeTimeout = 25 * time.Millisecond
	_, peer, handlerDone, _ := startPipeHandshake(t, service, nil)
	defer func() {
		_ = peer.Close()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = service.Shutdown(ctx)
	}()
	waitForHandler(t, handlerDone)
	_ = peer.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	if _, err := peer.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Fatalf("timed out handshake connection remained open: %v", err)
	}
}
