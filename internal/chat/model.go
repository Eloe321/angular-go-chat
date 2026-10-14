package chat

import (
	"bytes"
	"encoding/json"
	"strings"
	"unicode/utf8"
)

type Room struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// Rooms returns the fixed public rooms in navigation order.
func Rooms() []Room {
	return []Room{
		{ID: "general", Name: "General", Description: "A place for everyone to chat."},
		{ID: "angular", Name: "Angular", Description: "Discuss Angular and frontend development."},
		{ID: "go", Name: "Go", Description: "Discuss Go and backend development."},
	}
}
func FindRoom(id string) (Room, bool) {
	for _, room := range Rooms() {
		if room.ID == id {
			return room, true
		}
	}
	return Room{}, false
}

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return e.Message }

func NormalizeName(value string) (string, error) {
	return normalize(value, 32, "invalid_name", "Display name must contain 1 to 32 valid Unicode characters.")
}
func NormalizeContent(value string) (string, error) {
	return normalize(value, 1000, "invalid_content", "Message must contain 1 to 1000 valid Unicode characters.")
}
func normalize(value string, limit int, code, message string) (string, error) {
	if !utf8.ValidString(value) {
		return "", &Error{Code: code, Message: message}
	}
	value = strings.TrimSpace(value)
	count := utf8.RuneCountInString(value)
	if count < 1 || count > limit {
		return "", &Error{Code: code, Message: message}
	}
	return value, nil
}

type Message struct {
	ID         string `json:"id"`
	RoomID     string `json:"roomId"`
	SenderID   string `json:"senderId"`
	SenderName string `json:"senderName"`
	Content    string `json:"content"`
	CreatedAt  string `json:"createdAt"`
}
type Participant struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type Command struct {
	Type    string `json:"type"`
	Content string `json:"content"`
}

func DecodeCommand(data []byte) (Command, error) {
	var command Command
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '{' || !utf8.Valid(data) || json.Unmarshal(data, &command) != nil {
		return Command{}, &Error{Code: "invalid_command", Message: "Send a JSON object with string type and content fields."}
	}
	if command.Type != "message.send" {
		return Command{}, &Error{Code: "unsupported_type", Message: "Only message.send commands are supported."}
	}
	content, err := NormalizeContent(command.Content)
	if err != nil {
		return Command{}, err
	}
	command.Content = content
	return command, nil
}

// Event uses pointers for optional slices so an empty snapshot is encoded as []
// while events without a snapshot omit those fields.
type Event struct {
	Type         string         `json:"type"`
	SessionID    string         `json:"sessionId,omitempty"`
	Room         *Room          `json:"room,omitempty"`
	Messages     *[]Message     `json:"messages,omitempty"`
	Participants *[]Participant `json:"participants,omitempty"`
	Message      *Message       `json:"message,omitempty"`
	Code         string         `json:"code,omitempty"`
	ErrorMessage string         `json:"-"`
}

func (e Event) MarshalJSON() ([]byte, error) {
	if e.Type == "error" {
		return json.Marshal(struct {
			Type    string `json:"type"`
			Code    string `json:"code"`
			Message string `json:"message"`
		}{e.Type, e.Code, e.ErrorMessage})
	}
	type wireEvent Event
	return json.Marshal(wireEvent(e))
}
