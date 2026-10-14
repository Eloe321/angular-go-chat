package chat

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func drain(client *Client) {
	for {
		select {
		case <-client.Events():
		default:
			return
		}
	}
}
func TestHubDropsSlowClientWithoutBlockingHealthyGuest(t *testing.T) {
	hub := NewHub(NewMemoryStore())
	defer hub.Close()
	slow, err := hub.Join("general", "Same name")
	if err != nil {
		t.Fatal(err)
	}
	fast, err := hub.Join("general", "Same name")
	if err != nil {
		t.Fatal(err)
	}

	drain(slow)
	drain(fast)
	for i := 0; i < OutboundCapacity+1; i++ {
		if err := hub.Send(fast, fmt.Sprint(i)); err != nil {
			t.Fatal(err)
		}
		found := false
		for !found {
			select {
			case event := <-fast.Events():
				found = event.Type == "message.created"
			default:
				t.Fatal("healthy guest did not receive broadcast")
			}
		}
		drain(fast)
	}
	if slow.ID == fast.ID {
		t.Fatal("duplicate names need distinct IDs")
	}
	select {
	case <-slow.Done():
	default:
		t.Fatal("slow guest was not dropped")
	}
	hub.Leave(slow)
	hub.Leave(slow)
	if err := hub.Send(fast, "still here"); err != nil {
		t.Fatal(err)
	}
}
func TestHubAtomicJoinHistoryAndLivePublication(t *testing.T) {
	hub := NewHub(NewMemoryStore())
	defer hub.Close()
	sender, err := hub.Join("general", "Sender")
	if err != nil {
		t.Fatal(err)
	}
	drain(sender)
	for n := 0; n < 25; n++ {
		content := fmt.Sprint(n)
		var joiner *Client
		var joinErr, sendErr error
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); joiner, joinErr = hub.Join("general", "Joiner") }()
		go func() { defer wg.Done(); sendErr = hub.Send(sender, content) }()
		wg.Wait()
		if joinErr != nil || sendErr != nil {
			t.Fatalf("join %v, send %v", joinErr, sendErr)
		}
		var first Event
		select {
		case first = <-joiner.Events():
		case <-time.After(100 * time.Millisecond):
			t.Fatal("joining guest received no session.ready")
		}
		if first.Type != "session.ready" || first.Messages == nil {
			t.Fatalf("first event: %+v", first)
		}
		seen := 0
		for _, m := range *first.Messages {
			if m.Content == content {
				seen++
			}
		}
		for {
			select {
			case event := <-joiner.Events():
				if event.Type == "message.created" && event.Message.Content == content {
					seen++
				}
			default:
				goto drained
			}
		}
	drained:
		if seen != 1 {
			t.Fatalf("message %s seen %d times across snapshot/live", content, seen)
		}
		hub.Leave(joiner)
		drain(sender)
	}
}
func TestHubCloseIsIdempotent(t *testing.T) {
	hub := NewHub(NewMemoryStore())
	client, err := hub.Join("general", "Guest")
	if err != nil {
		t.Fatal(err)
	}
	hub.Close()
	hub.Close()
	hub.Leave(client)
	select {
	case <-client.Done():
	default:
		t.Fatal("client remained connected")
	}
	if _, err := hub.Join("general", "New guest"); err == nil {
		t.Fatal("closed hub accepted guest")
	}
	if err := hub.Send(client, "after close"); err == nil {
		t.Fatal("closed hub accepted message")
	}
}
