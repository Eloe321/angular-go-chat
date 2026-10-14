package bdd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"chat-app/internal/transport"
	"github.com/cucumber/godog"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

type message struct {
	ID         string `json:"id"`
	RoomID     string `json:"roomId"`
	SenderID   string `json:"senderId"`
	SenderName string `json:"senderName"`
	Content    string `json:"content"`
	CreatedAt  string `json:"createdAt"`
}
type participant struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type event struct {
	Type      string `json:"type"`
	SessionID string `json:"sessionId"`
	Room      struct {
		ID string `json:"id"`
	} `json:"room"`
	Messages     []message       `json:"messages"`
	Participants []participant   `json:"participants"`
	Message      json.RawMessage `json:"message"`
	Code         string          `json:"code"`
}
type guest struct {
	conn  *websocket.Conn
	ready event
	room  string
}
type scenario struct {
	service *transport.Server
	server  *httptest.Server
	rooms   []struct {
		ID string `json:"id"`
	}
	guests map[string]*guest
	status int
	code   string
}

func (s *scenario) start() error {
	gin.SetMode(gin.TestMode)
	s.service = transport.New(transport.Config{})
	s.server = httptest.NewServer(s.service)
	s.server.Client().Timeout = 2 * time.Second
	s.guests = make(map[string]*guest)
	return nil
}
func (s *scenario) cleanup() error {
	for _, g := range s.guests {
		_ = g.conn.Close()
	}
	var err error
	if s.service != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		err = s.service.Shutdown(ctx)
	}
	if s.server != nil {
		s.server.Close()
	}
	return err
}
func (s *scenario) roomList() error {
	resp, err := s.server.Client().Get(s.server.URL + "/api/rooms")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("room list status: got %d, want 200", resp.StatusCode)
	}
	var body struct {
		Rooms []struct {
			ID string `json:"id"`
		} `json:"rooms"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return err
	}
	s.rooms = body.Rooms
	return nil
}
func (s *scenario) roomIDs(want string) error {
	var ids []string
	for _, room := range s.rooms {
		ids = append(ids, room.ID)
	}
	if got := strings.Join(ids, ","); got != want {
		return fmt.Errorf("room IDs: got %q, want %q", got, want)
	}
	return nil
}
func (s *scenario) socketURL(room, name string) string {
	return "ws" + strings.TrimPrefix(s.server.URL, "http") + "/ws?" + url.Values{"room": {room}, "name": {name}}.Encode()
}
func (s *scenario) connect(name, room, alias string) error {
	dialer := websocket.Dialer{HandshakeTimeout: 2 * time.Second}
	conn, resp, err := dialer.Dial(s.socketURL(room, name), nil)
	if err != nil {
		if resp != nil {
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("connect: %w (status %d: %s)", err, resp.StatusCode, body)
		}
		return err
	}
	g := &guest{conn: conn, room: room}
	s.guests[alias] = g
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if err := conn.ReadJSON(&g.ready); err != nil {
		return err
	}
	if g.ready.Type != "session.ready" || g.ready.SessionID == "" || g.ready.Room.ID != room || g.ready.Messages == nil || len(g.ready.Participants) == 0 {
		return fmt.Errorf("invalid first event: %+v", g.ready)
	}
	return nil
}
func (s *scenario) send(alias, content string) error {
	g := s.guests[alias]
	_ = g.conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	return g.conn.WriteJSON(map[string]string{"type": "message.send", "content": content, "senderId": "forged", "senderName": "forged", "id": "forged", "createdAt": "forged"})
}
func (s *scenario) until(alias string, match func(event) bool) (event, error) {
	g := s.guests[alias]
	deadline := time.Now().Add(2 * time.Second)
	for {
		_ = g.conn.SetReadDeadline(deadline)
		var e event
		if err := g.conn.ReadJSON(&e); err != nil {
			return e, err
		}
		if match(e) {
			return e, nil
		}
	}
}
func (s *scenario) receives(alias, content, sender string) error {
	e, err := s.until(alias, func(e event) bool { return e.Type == "message.created" })
	if err != nil {
		return err
	}
	var m *message
	if err := json.Unmarshal(e.Message, &m); err != nil {
		return err
	}
	if m == nil || m.Content != content || m.SenderName != sender || m.RoomID != s.guests[alias].room || m.ID == "" || m.ID == "forged" || m.SenderID == "" || m.SenderID == "forged" {
		return fmt.Errorf("unexpected message: %+v", m)
	}
	timestamp, err := time.Parse(time.RFC3339, m.CreatedAt)
	if err != nil || timestamp.Location() != time.UTC {
		return fmt.Errorf("invalid UTC timestamp %q", m.CreatedAt)
	}
	for _, g := range s.guests {
		if g.ready.SessionID == m.SenderID && g.ready.Participants != nil {
			return nil
		}
	}
	return fmt.Errorf("sender %q is not a server-issued session", m.SenderID)
}
func (s *scenario) noMessage(alias string) error {
	g := s.guests[alias]
	deadline := time.Now().Add(150 * time.Millisecond)
	for {
		_ = g.conn.SetReadDeadline(deadline)
		var e event
		if err := g.conn.ReadJSON(&e); err != nil {
			if strings.Contains(err.Error(), "i/o timeout") {
				return nil
			}
			return err
		}
		if e.Type == "message.created" {
			return fmt.Errorf("message crossed room boundary: %+v", e.Message)
		}
	}
}
func compareHistory(messages []message, want string) error {
	var contents []string
	for _, m := range messages {
		contents = append(contents, m.Content)
	}
	if got := strings.Join(contents, "|"); got != want {
		return fmt.Errorf("history got %q, want %q", got, want)
	}
	return nil
}
func (s *scenario) history(alias, want string) error {
	return compareHistory(s.guests[alias].ready.Messages, want)
}
func (s *scenario) storedHistory(room, want string) error {
	resp, err := s.server.Client().Get(s.server.URL + "/api/rooms/" + room + "/messages")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("history status %d", resp.StatusCode)
	}
	var body struct {
		Messages []message `json:"messages"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return err
	}
	return compareHistory(body.Messages, want)
}
func (s *scenario) receivesError(alias, code string) error {
	e, err := s.until(alias, func(e event) bool { return e.Type == "error" })
	if err != nil {
		return err
	}
	if e.Code != code {
		return fmt.Errorf("error code got %q, want %q", e.Code, code)
	}
	return nil
}
func (s *scenario) attempt(room, name, origin string) error {
	headers := http.Header{}
	if origin != "" {
		headers.Set("Origin", origin)
	}
	dialer := websocket.Dialer{HandshakeTimeout: 2 * time.Second}
	conn, resp, err := dialer.Dial(s.socketURL(room, name), headers)
	if conn != nil {
		_ = conn.Close()
		return fmt.Errorf("invalid handshake was accepted")
	}
	if err == nil || resp == nil {
		return fmt.Errorf("no rejection response: %v", err)
	}
	defer resp.Body.Close()
	s.status = resp.StatusCode
	var body struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return err
	}
	if body.Message == "" {
		return fmt.Errorf("empty error message")
	}
	s.code = body.Code
	return nil
}
func (s *scenario) handshakeStatus(status int, code string) error {
	if s.status != status || s.code != code {
		return fmt.Errorf("handshake got %d/%s, want %d/%s", s.status, s.code, status, code)
	}
	return nil
}
func (s *scenario) disconnect(alias string) error { return s.guests[alias].conn.Close() }
func (s *scenario) participants(alias, want string) error {
	_, err := s.until(alias, func(e event) bool {
		if e.Type != "presence.updated" {
			return false
		}
		var names []string
		for _, p := range e.Participants {
			if p.ID == "" {
				return false
			}
			names = append(names, p.Name)
		}
		return strings.Join(names, ",") == want
	})
	return err
}
func TestFeatures(t *testing.T) {
	suite := godog.TestSuite{Name: "chat", ScenarioInitializer: func(sc *godog.ScenarioContext) {
		s := &scenario{}
		sc.After(func(ctx context.Context, _ *godog.Scenario, err error) (context.Context, error) {
			return ctx, s.cleanup()
		})
		sc.Step(`^an isolated chat service$`, s.start)
		sc.Step(`^I request the room list$`, s.roomList)
		sc.Step(`^the available room IDs are "([^"]*)"$`, s.roomIDs)
		sc.Step(`^"([^"]*)" connects to "([^"]*)" as "([^"]*)"$`, s.connect)
		sc.Step(`^"([^"]*)" sends "([^"]*)"$`, s.send)
		sc.Step(`^"([^"]*)" receives "([^"]*)" from "([^"]*)"$`, s.receives)
		sc.Step(`^"([^"]*)" receives no message$`, s.noMessage)
		sc.Step(`^"([^"]*)" has history "([^"]*)"$`, s.history)
		sc.Step(`^"([^"]*)" has stored history "([^"]*)"$`, s.storedHistory)
		sc.Step(`^"([^"]*)" receives error "([^"]*)"$`, s.receivesError)
		sc.Step(`^I attempt room "([^"]*)" with name "([^"]*)" and origin "([^"]*)"$`, s.attempt)
		sc.Step(`^the handshake status is (\d+) with code "([^"]*)"$`, s.handshakeStatus)
		sc.Step(`^"([^"]*)" disconnects$`, s.disconnect)
		sc.Step(`^"([^"]*)" sees participants "([^"]*)"$`, s.participants)
	}, Options: &godog.Options{Format: "pretty", Paths: []string{"../features"}, Strict: true, TestingT: t}}
	if suite.Run() != 0 {
		t.Fatal("chat behavior scenarios failed")
	}
}
