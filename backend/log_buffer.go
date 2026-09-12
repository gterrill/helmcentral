package main

import (
	"io"
	"log"
	"os"
	"sync"
	"time"
)

const defaultLogBufferCapacity = 2000

type logEntry struct {
	ID        int64     `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Message   string    `json:"message"`
}

type logBuffer struct {
	mu          sync.RWMutex
	capacity    int
	entriesList []logEntry
	nextID      int64
	subscribers map[chan logEntry]struct{}
}

func newLogBuffer(capacity int) *logBuffer {
	if capacity <= 0 {
		capacity = defaultLogBufferCapacity
	}
	return &logBuffer{
		capacity:    capacity,
		entriesList: make([]logEntry, 0, capacity),
		nextID:      1,
		subscribers: make(map[chan logEntry]struct{}),
	}
}

func (b *logBuffer) write(msg string) logEntry {
	b.mu.Lock()
	entry := logEntry{
		ID:        b.nextID,
		Timestamp: time.Now().UTC(),
		Message:   msg,
	}
	b.nextID++

	if len(b.entriesList) >= b.capacity {
		b.entriesList = b.entriesList[1:]
	}
	b.entriesList = append(b.entriesList, entry)

	// Broadcast to subscribers
	for ch := range b.subscribers {
		select {
		case ch <- entry:
		default:
			// drop if full to avoid blocking writer
		}
	}
	b.mu.Unlock()

	return entry
}

func (b *logBuffer) entries() []logEntry {
	b.mu.RLock()
	defer b.mu.RUnlock()
	res := make([]logEntry, len(b.entriesList))
	copy(res, b.entriesList)
	return res
}

func (b *logBuffer) entriesSince(sinceID int64) []logEntry {
	b.mu.RLock()
	defer b.mu.RUnlock()
	var res []logEntry
	for _, e := range b.entriesList {
		if e.ID > sinceID {
			res = append(res, e)
		}
	}
	if res == nil {
		res = []logEntry{}
	}
	return res
}

func (b *logBuffer) subscribe() (<-chan logEntry, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	ch := make(chan logEntry, 100)
	b.subscribers[ch] = struct{}{}

	var once sync.Once
	unsubscribe := func() {
		once.Do(func() {
			b.mu.Lock()
			delete(b.subscribers, ch)
			close(ch)
			b.mu.Unlock()
		})
	}

	return ch, unsubscribe
}

// logBufferWriter wraps an io.Writer and captures lines to logBuffer
type logBufferWriter struct {
	underlying io.Writer
	buf        *logBuffer
	lineBuf    []byte
	mu         sync.Mutex
}

func newLogBufferWriter(underlying io.Writer, buf *logBuffer) *logBufferWriter {
	return &logBufferWriter{
		underlying: underlying,
		buf:        buf,
	}
}

func (w *logBufferWriter) Write(p []byte) (n int, err error) {
	if w.underlying != nil {
		_, _ = w.underlying.Write(p)
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	w.lineBuf = append(w.lineBuf, p...)
	for {
		idx := -1
		for i, b := range w.lineBuf {
			if b == '\n' {
				idx = i
				break
			}
		}
		if idx == -1 {
			break
		}
		line := string(w.lineBuf[:idx])
		w.lineBuf = w.lineBuf[idx+1:]
		if len(line) > 0 && line[len(line)-1] == '\r' {
			line = line[:len(line)-1]
		}
		if line != "" {
			w.buf.write(line)
		}
	}

	return len(p), nil
}

var globalLogBuffer = newLogBuffer(defaultLogBufferCapacity)

func initLogCapture() io.Writer {
	writer := newLogBufferWriter(os.Stderr, globalLogBuffer)
	log.SetOutput(writer)
	return writer
}
