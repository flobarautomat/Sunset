package pubsub

import (
	"encoding/json"
	"sync"
)

type MsgType string

const (
	SessionCreated MsgType = "session_created"
	EventsRecorded MsgType = "events_recorded"
	SessionIdle    MsgType = "session_idle"
)

type Message struct {
	Type      MsgType         `json:"type"`
	SessionID string          `json:"session_id"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

type Hub struct {
	mu   sync.RWMutex
	subs map[chan Message]struct{}
}

func NewHub() *Hub {
	return &Hub{
		subs: make(map[chan Message]struct{}),
	}
}

// Publish sends a message to all subscribers. Non-blocking: if a subscriber's
// buffer is full, the message is dropped for that subscriber.
func (h *Hub) Publish(msg Message) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for ch := range h.subs {
		select {
		case ch <- msg:
		default:
		}
	}
}

// Subscribe returns a channel that receives published messages and an
// unsubscribe function. The caller must call unsubscribe when done.
func (h *Hub) Subscribe() (<-chan Message, func()) {
	ch := make(chan Message, 64)

	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()

	unsub := func() {
		h.mu.Lock()
		delete(h.subs, ch)
		h.mu.Unlock()
	}

	return ch, unsub
}
