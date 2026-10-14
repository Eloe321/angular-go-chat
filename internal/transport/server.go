package transport

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"

	"chat-app/internal/chat"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

type Config struct {
	Store          chat.Repository
	AllowedOrigins []string
}

func DefaultOrigins() []string { return []string{"http://localhost:4200", "http://127.0.0.1:4200"} }

type Server struct {
	router   *gin.Engine
	store    chat.Repository
	hub      *chat.Hub
	origins  map[string]bool
	upgrader websocket.Upgrader
	mu       sync.Mutex
	closing  bool
	sockets  map[net.Conn]struct{}
	workers  sync.WaitGroup
	stopOnce sync.Once
	stopped  chan struct{}
}

func New(config Config) *Server {
	store := config.Store
	if store == nil {
		store = chat.NewMemoryStore()
	}
	origins := config.AllowedOrigins
	if origins == nil {
		origins = DefaultOrigins()
	}
	s := &Server{store: store, hub: chat.NewHub(store), origins: make(map[string]bool), sockets: make(map[net.Conn]struct{}), stopped: make(chan struct{})}
	for _, origin := range origins {
		s.origins[origin] = true
	}
	s.upgrader = websocket.Upgrader{HandshakeTimeout: 5 * time.Second, ReadBufferSize: 1024, WriteBufferSize: 1024, CheckOrigin: s.originAllowed, Error: func(w http.ResponseWriter, r *http.Request, status int, reason error) {
		writeHTTPError(w, status, "invalid_handshake", "A valid WebSocket upgrade request is required.")
	}}
	router := gin.New()
	router.Use(gin.Recovery())
	_ = router.SetTrustedProxies(nil)
	router.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	router.GET("/api/rooms", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"rooms": chat.Rooms()}) })
	router.GET("/api/rooms/:roomId/messages", s.history)
	router.GET("/ws", s.upgrade)
	router.NoRoute(func(c *gin.Context) {
		c.JSON(http.StatusNotFound, chat.Error{Code: "not_found", Message: "Route does not exist."})
	})
	s.router = router
	return s
}
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.router.ServeHTTP(w, r) }
func (s *Server) history(c *gin.Context) {
	roomID := c.Param("roomId")
	if _, ok := chat.FindRoom(roomID); !ok {
		c.JSON(http.StatusNotFound, chat.Error{Code: "unknown_room", Message: "Room does not exist."})
		return
	}
	messages, err := s.store.History(roomID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, chat.Error{Code: "storage_unavailable", Message: "Room history is unavailable."})
		return
	}
	c.JSON(http.StatusOK, gin.H{"messages": messages})
}
func (s *Server) originAllowed(r *http.Request) bool {
	origins := r.Header.Values("Origin")
	if len(origins) == 0 {
		return true
	}
	return len(origins) == 1 && s.origins[origins[0]]
}
func (s *Server) upgrade(c *gin.Context) {
	roomID := c.Query("room")
	if roomID == "" {
		c.JSON(http.StatusBadRequest, chat.Error{Code: "invalid_room", Message: "A room parameter is required."})
		return
	}
	name, err := chat.NormalizeName(c.Query("name"))
	if err != nil {
		c.JSON(http.StatusBadRequest, err)
		return
	}
	if _, ok := chat.FindRoom(roomID); !ok {
		c.JSON(http.StatusNotFound, chat.Error{Code: "unknown_room", Message: "Room does not exist."})
		return
	}
	if !s.originAllowed(c.Request) {
		c.JSON(http.StatusForbidden, chat.Error{Code: "origin_not_allowed", Message: "Browser origin is not allowed."})
		return
	}
	// Count pending handshakes before releasing the lifecycle lock. Shutdown
	// cannot finish while an upgrade may still create a hijacked connection.
	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()
		c.JSON(http.StatusServiceUnavailable, chat.Error{Code: "service_unavailable", Message: "Chat service is shutting down."})
		return
	}
	s.workers.Add(1)
	s.mu.Unlock()
	writer := &upgradeWriter{ResponseWriter: c.Writer, server: s}
	defer func() {
		if writer.conn != nil {
			_ = writer.conn.Close()
			s.mu.Lock()
			delete(s.sockets, writer.conn)
			s.mu.Unlock()
		}
		s.workers.Done()
	}()
	conn, err := s.upgrader.Upgrade(writer, c.Request, nil)
	if err != nil {
		return
	}
	s.mu.Lock()
	closing := s.closing
	s.mu.Unlock()
	if closing {
		return
	}
	client, err := s.hub.Join(roomID, name)
	if err != nil {
		return
	}
	defer s.hub.Leave(client)
	s.serveSocket(conn, client)
}

// upgradeWriter tracks the raw connection immediately after hijacking, before
// Gorilla writes its handshake response. The same connection remains tracked
// through the reader/writer lifecycle, so shutdown covers pending and live peers.
type upgradeWriter struct {
	http.ResponseWriter
	server *Server
	conn   net.Conn
}

func (w *upgradeWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, buffered, err := w.ResponseWriter.(http.Hijacker).Hijack()
	if err != nil {
		return nil, nil, err
	}
	w.conn = conn
	w.server.mu.Lock()
	closing := w.server.closing
	if !closing {
		w.server.sockets[conn] = struct{}{}
	}
	w.server.mu.Unlock()
	if closing {
		_ = conn.Close()
		return nil, nil, errors.New("chat service is shutting down")
	}
	return conn, buffered, nil
}
func writeHTTPError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(chat.Error{Code: code, Message: message})
}

// Shutdown stops upgrades, closes hijacked sockets, stops the hub, and waits for
// its connection workers. The executable separately shuts down the HTTP server.
func (s *Server) Shutdown(ctx context.Context) error {
	s.stopOnce.Do(func() {
		s.mu.Lock()
		s.closing = true
		connections := make([]net.Conn, 0, len(s.sockets))
		for conn := range s.sockets {
			connections = append(connections, conn)
		}
		s.mu.Unlock()
		go func() {
			for _, conn := range connections {
				_ = conn.Close()
			}
			s.hub.Close()
			s.workers.Wait()
			close(s.stopped)
		}()
	})
	select {
	case <-s.stopped:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
