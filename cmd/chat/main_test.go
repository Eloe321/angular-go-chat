package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"reflect"
	"testing"
	"time"

	"chat-app/internal/transport"
)

func TestLoadConfig(t *testing.T) {
	defaults := loadConfig(func(string) string { return "" })
	if defaults.address != "127.0.0.1:8080" || !reflect.DeepEqual(defaults.origins, transport.DefaultOrigins()) {
		t.Fatalf("defaults: %+v", defaults)
	}
	values := map[string]string{"CHAT_ADDR": " 127.0.0.1:9191 ", "CHAT_ALLOWED_ORIGINS": " https://chat.example, http://localhost:4300 "}
	got := loadConfig(func(key string) string { return values[key] })
	if got.address != "127.0.0.1:9191" || !reflect.DeepEqual(got.origins, []string{"https://chat.example", "http://localhost:4300"}) {
		t.Fatalf("configured: %+v", got)
	}
}
func TestServeHealthAndGracefulCancellation(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	service := transport.New(transport.Config{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- serve(ctx, listener, service) }()
	client := http.Client{Timeout: 250 * time.Millisecond}
	endpoint := "http://" + listener.Addr().String() + "/healthz"
	deadline := time.Now().Add(2 * time.Second)
	for {
		resp, err := client.Get(endpoint)
		if err == nil {
			var body map[string]string
			decodeErr := json.NewDecoder(resp.Body).Decode(&body)
			_ = resp.Body.Close()
			if resp.StatusCode != 200 || decodeErr != nil || body["status"] != "ok" {
				t.Fatalf("health: %d, %v, %v", resp.StatusCode, body, decodeErr)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("service did not start: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("service did not shut down")
	}
	if resp, err := client.Get(endpoint); err == nil {
		_ = resp.Body.Close()
		t.Fatal("HTTP listener stayed open after shutdown")
	}
}
