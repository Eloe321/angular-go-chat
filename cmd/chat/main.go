package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"chat-app/internal/transport"
)

type config struct {
	address string
	origins []string
}

func loadConfig(getenv func(string) string) config {
	cfg := config{address: "127.0.0.1:8080", origins: transport.DefaultOrigins()}
	if address := strings.TrimSpace(getenv("CHAT_ADDR")); address != "" {
		cfg.address = address
	}
	if origins := getenv("CHAT_ALLOWED_ORIGINS"); origins != "" {
		cfg.origins = make([]string, 0)
		for _, origin := range strings.Split(origins, ",") {
			if origin = strings.TrimSpace(origin); origin != "" {
				cfg.origins = append(cfg.origins, origin)
			}
		}
	}
	return cfg
}
func serve(ctx context.Context, listener net.Listener, service *transport.Server) error {
	server := &http.Server{Handler: service, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
	result := make(chan error, 1)
	go func() { result <- server.Serve(listener) }()
	var serveErr error
	select {
	case serveErr = <-result:
		if errors.Is(serveErr, http.ErrServerClosed) {
			serveErr = nil
		}
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	socketErr := service.Shutdown(shutdownCtx)
	httpErr := server.Shutdown(shutdownCtx)
	if httpErr != nil {
		_ = server.Close()
	}
	return errors.Join(serveErr, socketErr, httpErr)
}
func run(ctx context.Context, cfg config) error {
	listener, err := net.Listen("tcp", cfg.address)
	if err != nil {
		return err
	}
	service := transport.New(transport.Config{AllowedOrigins: cfg.origins})
	log.Printf("Chat service listening on %s", listener.Addr())
	return serve(ctx, listener, service)
}
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, loadConfig(os.Getenv)); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}
