package logging

import "sync"

// EventType describes the kind of console-log event broadcast to SSE clients.
type EventType string

const (
	// EventLine is broadcast when a new JSON log line is written to the log file.
	EventLine EventType = "line"
	// EventClear is broadcast when the log file is cleared.
	EventClear EventType = "clear"
)

// BroadcastEvent is a single event delivered to SSE subscribers.
type BroadcastEvent struct {
	Type EventType
	// Line is the raw JSON log line when Type is EventLine.
	Line string
}

// Broadcaster fans out log-file events to multiple SSE subscribers.
type Broadcaster struct {
	mu      sync.RWMutex
	clients map[chan BroadcastEvent]struct{}
}

// NewBroadcaster creates an empty Broadcaster.
func NewBroadcaster() *Broadcaster {
	return &Broadcaster{
		clients: make(map[chan BroadcastEvent]struct{}),
	}
}

// Subscribe registers a new subscriber channel. The caller must call
// Unsubscribe when the SSE connection closes.
func (b *Broadcaster) Subscribe() chan BroadcastEvent {
	b.mu.Lock()
	defer b.mu.Unlock()
	ch := make(chan BroadcastEvent, 16)
	b.clients[ch] = struct{}{}
	return ch
}

// Unsubscribe removes a previously subscribed channel.
func (b *Broadcaster) Unsubscribe(ch chan BroadcastEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.clients[ch]; ok {
		delete(b.clients, ch)
		close(ch)
	}
}

// BroadcastLine notifies all subscribers that a new JSON log line was written.
func (b *Broadcaster) BroadcastLine(line string) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for ch := range b.clients {
		select {
		case ch <- BroadcastEvent{Type: EventLine, Line: line}:
		default:
			// Subscriber is slow; drop the event to avoid blocking writers.
		}
	}
}

// BroadcastClear notifies all subscribers that the log file was cleared.
func (b *Broadcaster) BroadcastClear() {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for ch := range b.clients {
		select {
		case ch <- BroadcastEvent{Type: EventClear}:
		default:
			// Subscriber is slow; drop the event to avoid blocking writers.
		}
	}
}
