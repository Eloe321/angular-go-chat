package transport

import (
	"errors"
	"time"

	"chat-app/internal/chat"
	"github.com/gorilla/websocket"
)

const (
	readLimit    = 8 * 1024
	writeTimeout = 10 * time.Second
	pongTimeout  = 60 * time.Second
	pingInterval = 25 * time.Second
)

func (s *Server) serveSocket(conn *websocket.Conn, client *chat.Client) {
	writerDone := make(chan struct{})
	go func() { defer close(writerDone); s.writeSocket(conn, client) }()
	defer func() { s.hub.Leave(client); _ = conn.Close(); <-writerDone }()
	conn.SetReadLimit(readLimit)
	_ = conn.SetReadDeadline(time.Now().Add(pongTimeout))
	conn.SetPongHandler(func(string) error { return conn.SetReadDeadline(time.Now().Add(pongTimeout)) })
	for {
		messageType, payload, err := conn.ReadMessage()
		if err != nil {
			return
		}
		if messageType != websocket.TextMessage {
			s.hub.SendError(client, &chat.Error{Code: "invalid_command", Message: "Commands must use text JSON frames."})
			continue
		}
		command, err := chat.DecodeCommand(payload)
		if err == nil {
			err = s.hub.Send(client, command.Content)
		}
		if err != nil {
			var protocolErr *chat.Error
			if !errors.As(err, &protocolErr) {
				protocolErr = &chat.Error{Code: "storage_unavailable", Message: "Message could not be saved."}
			}
			s.hub.SendError(client, protocolErr)
		}
	}
}
func (s *Server) writeSocket(conn *websocket.Conn, client *chat.Client) {
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()
	defer conn.Close()
	for {
		select {
		case <-client.Done():
			return
		case event := <-client.Events():
			_ = conn.SetWriteDeadline(time.Now().Add(writeTimeout))
			if err := conn.WriteJSON(event); err != nil {
				return
			}
		case <-ticker.C:
			_ = conn.SetWriteDeadline(time.Now().Add(writeTimeout))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
