package hub

import (
	"sync"
	"time"
)

// LogEntry represents a log message from a node.
type LogEntry struct {
	NodeID    string    `json:"node_id"`
	NodeName  string    `json:"node_name"`
	Level     string    `json:"level"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"created_at"`
}

// logBroker fans out log entries to SSE subscribers.
type logBroker struct {
	mu   sync.RWMutex
	subs map[chan LogEntry]struct{}
}

func newLogBroker() *logBroker {
	return &logBroker{
		subs: make(map[chan LogEntry]struct{}),
	}
}

func (b *logBroker) subscribe() chan LogEntry {
	ch := make(chan LogEntry, 64)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

func (b *logBroker) unsubscribe(ch chan LogEntry) {
	b.mu.Lock()
	delete(b.subs, ch)
	b.mu.Unlock()
	close(ch)
}

func (b *logBroker) publish(entry LogEntry) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for ch := range b.subs {
		select {
		case ch <- entry:
		default:
			// drop if subscriber can't keep up
		}
	}
}
