package main

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestLogBufferStoresAndRetrievesLines(t *testing.T) {
	buf := newLogBuffer(10)
	for i := 1; i <= 5; i++ {
		buf.write(fmt.Sprintf("line %d", i))
	}

	entries := buf.entries()
	if len(entries) != 5 {
		t.Fatalf("expected 5 entries, got %d", len(entries))
	}
	if entries[0].Message != "line 1" || entries[4].Message != "line 5" {
		t.Fatalf("unexpected entries: %+v", entries)
	}
	if entries[0].ID != 1 || entries[4].ID != 5 {
		t.Fatalf("unexpected IDs: first=%d last=%d", entries[0].ID, entries[4].ID)
	}
}

func TestLogBufferEvictsOldestWhenFull(t *testing.T) {
	buf := newLogBuffer(3)
	buf.write("line 1")
	buf.write("line 2")
	buf.write("line 3")
	buf.write("line 4")

	entries := buf.entries()
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	if entries[0].Message != "line 2" || entries[1].Message != "line 3" || entries[2].Message != "line 4" {
		t.Fatalf("unexpected entries: %+v", entries)
	}
	if entries[0].ID != 2 || entries[2].ID != 4 {
		t.Fatalf("unexpected IDs: first=%d last=%d", entries[0].ID, entries[2].ID)
	}
}

func TestLogBufferEntriesSince(t *testing.T) {
	buf := newLogBuffer(5)
	for i := 1; i <= 5; i++ {
		buf.write(fmt.Sprintf("msg %d", i))
	}

	since2 := buf.entriesSince(2)
	if len(since2) != 3 {
		t.Fatalf("expected 3 entries since ID 2, got %d", len(since2))
	}
	if since2[0].ID != 3 || since2[2].ID != 5 {
		t.Fatalf("unexpected entries: %+v", since2)
	}

	since5 := buf.entriesSince(5)
	if len(since5) != 0 {
		t.Fatalf("expected 0 entries since ID 5, got %d", len(since5))
	}

	since0 := buf.entriesSince(0)
	if len(since0) != 5 {
		t.Fatalf("expected 5 entries since ID 0, got %d", len(since0))
	}
}

func TestLogBufferSubscribeAndBroadcast(t *testing.T) {
	buf := newLogBuffer(10)
	ch, unsubscribe := buf.subscribe()
	defer unsubscribe()

	buf.write("broadcast test")

	select {
	case entry := <-ch:
		if entry.Message != "broadcast test" {
			t.Fatalf("expected message 'broadcast test', got %q", entry.Message)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for broadcast message")
	}

	unsubscribe()
	buf.write("after unsubscribe")

	select {
	case entry, ok := <-ch:
		if ok {
			t.Fatalf("received unexpected entry after unsubscribe: %+v", entry)
		}
	default:
		// success: channel is closed or empty
	}
}

func TestLogBufferConcurrentWrites(t *testing.T) {
	buf := newLogBuffer(100)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				buf.write(fmt.Sprintf("writer %d msg %d", id, j))
			}
		}(i)
	}
	wg.Wait()

	entries := buf.entries()
	if len(entries) != 100 {
		t.Fatalf("expected 100 entries, got %d", len(entries))
	}
}
