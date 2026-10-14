package chat

import "sync"

const HistoryLimit = 100

// Repository keeps storage separate from room membership and the transport.
type Repository interface {
	History(roomID string) ([]Message, error)
	Append(Message) error
}

type MemoryStore struct {
	mu       sync.RWMutex
	messages map[string][]Message
}

func NewMemoryStore() *MemoryStore { return &MemoryStore{messages: make(map[string][]Message)} }
func (s *MemoryStore) History(roomID string) ([]Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]Message, len(s.messages[roomID]))
	copy(result, s.messages[roomID])
	return result, nil
}
func (s *MemoryStore) Append(message Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	messages := append(s.messages[message.RoomID], message)
	if len(messages) > HistoryLimit {
		copy(messages, messages[len(messages)-HistoryLimit:])
		messages = messages[:HistoryLimit]
	}
	s.messages[message.RoomID] = messages
	return nil
}
