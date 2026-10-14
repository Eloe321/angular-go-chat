package chat

import (
	"crypto/rand"
	"sort"
	"sync"
	"time"
)

const OutboundCapacity = 64

type Client struct {
	ID     string
	Name   string
	RoomID string
	events chan Event
	done   chan struct{}
}

func (c *Client) Events() <-chan Event  { return c.events }
func (c *Client) Done() <-chan struct{} { return c.done }

// Hub serializes registration, history snapshots, and publication. A message is
// therefore delivered in either a joining guest's snapshot or its live queue.
type Hub struct {
	mu      sync.Mutex
	store   Repository
	members map[string]map[string]*Client
	closed  bool
}

func NewHub(store Repository) *Hub {
	return &Hub{store: store, members: make(map[string]map[string]*Client)}
}

func (h *Hub) Join(roomID, name string) (*Client, error) {
	room, ok := FindRoom(roomID)
	if !ok {
		return nil, &Error{Code: "unknown_room", Message: "Room does not exist."}
	}
	name, err := NormalizeName(name)
	if err != nil {
		return nil, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil, &Error{Code: "service_unavailable", Message: "Chat service is shutting down."}
	}
	history, err := h.store.History(roomID)
	if err != nil {
		return nil, err
	}
	client := &Client{ID: rand.Text(), Name: name, RoomID: roomID, events: make(chan Event, OutboundCapacity), done: make(chan struct{})}
	if h.members[roomID] == nil {
		h.members[roomID] = make(map[string]*Client)
	}
	h.members[roomID][client.ID] = client
	participants := h.participantsLocked(roomID)
	client.events <- Event{Type: "session.ready", SessionID: client.ID, Room: &room, Messages: &history, Participants: &participants}
	h.presenceLocked(roomID)
	return client, nil
}

func (h *Hub) Send(client *Client, content string) error {
	content, err := NormalizeContent(content)
	if err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed || h.members[client.RoomID][client.ID] != client {
		return &Error{Code: "session_closed", Message: "Chat session is no longer connected."}
	}
	message := Message{ID: rand.Text(), RoomID: client.RoomID, SenderID: client.ID, SenderName: client.Name, Content: content, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if err := h.store.Append(message); err != nil {
		return err
	}
	if h.broadcastLocked(client.RoomID, Event{Type: "message.created", Message: &message}) {
		h.presenceLocked(client.RoomID)
	}
	return nil
}

func (h *Hub) SendError(client *Client, protocolErr *Error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.members[client.RoomID][client.ID] != client {
		return
	}
	select {
	case client.events <- Event{Type: "error", Code: protocolErr.Code, ErrorMessage: protocolErr.Message}:
	default:
		h.removeLocked(client)
		h.presenceLocked(client.RoomID)
	}
}
func (h *Hub) Leave(client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.removeLocked(client) {
		h.presenceLocked(client.RoomID)
	}
}
func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	h.closed = true
	for _, members := range h.members {
		for _, client := range members {
			close(client.done)
		}
	}
	h.members = make(map[string]map[string]*Client)
}
func (h *Hub) removeLocked(client *Client) bool {
	if h.members[client.RoomID][client.ID] != client {
		return false
	}
	delete(h.members[client.RoomID], client.ID)
	close(client.done)
	return true
}
func (h *Hub) participantsLocked(roomID string) []Participant {
	participants := make([]Participant, 0, len(h.members[roomID]))
	for _, client := range h.members[roomID] {
		participants = append(participants, Participant{ID: client.ID, Name: client.Name})
	}
	sort.Slice(participants, func(i, j int) bool {
		if participants[i].Name == participants[j].Name {
			return participants[i].ID < participants[j].ID
		}
		return participants[i].Name < participants[j].Name
	})
	return participants
}

// A full queue disconnects only that guest. No publication waits on a reader.
func (h *Hub) broadcastLocked(roomID string, event Event) bool {
	dropped := false
	for _, client := range h.members[roomID] {
		select {
		case client.events <- event:
		default:
			h.removeLocked(client)
			dropped = true
		}
	}
	return dropped
}
func (h *Hub) presenceLocked(roomID string) {
	for {
		participants := h.participantsLocked(roomID)
		if !h.broadcastLocked(roomID, Event{Type: "presence.updated", Participants: &participants}) {
			return
		}
	}
}
