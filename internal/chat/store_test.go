package chat

import (
	"fmt"
	"sync"
	"testing"
)

func TestMemoryHistoryLatest100AndCopies(t *testing.T) {
	store := NewMemoryStore()
	for i := 0; i < 105; i++ {
		if err := store.Append(Message{ID: fmt.Sprint(i), RoomID: "general", Content: fmt.Sprint(i)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Append(Message{ID: "other", RoomID: "go"}); err != nil {
		t.Fatal(err)
	}
	history, err := store.History("general")
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 100 {
		t.Fatalf("history length: got %d, want 100", len(history))
	}
	for i, m := range history {
		if m.ID != fmt.Sprint(i+5) {
			t.Fatalf("history[%d]=%s", i, m.ID)
		}
	}
	history[0].Content = "mutated"
	again, _ := store.History("general")
	if again[0].Content != "5" {
		t.Fatal("history exposes stored data")
	}
	other, _ := store.History("go")
	if len(other) != 1 || other[0].ID != "other" {
		t.Fatalf("room isolation: %+v", other)
	}
	empty, _ := store.History("angular")
	if empty == nil || len(empty) != 0 {
		t.Fatalf("empty history: %#v", empty)
	}
	fresh, _ := NewMemoryStore().History("general")
	if len(fresh) != 0 {
		t.Fatal("a new store must start empty")
	}
}
func TestMemoryStoreConcurrentAccess(t *testing.T) {
	store := NewMemoryStore()
	var wg sync.WaitGroup
	for n := 0; n < 10; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				_ = store.Append(Message{ID: fmt.Sprintf("%d-%d", n, i), RoomID: "general"})
				_, _ = store.History("general")
			}
		}(n)
	}
	wg.Wait()
	history, _ := store.History("general")
	if len(history) != 100 {
		t.Fatalf("history length %d", len(history))
	}
}
