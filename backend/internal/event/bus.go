package event

import (
	"encoding/json"
	"sync"
)

type Event struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

type Handler func(Event)

type Bus struct {
	mu       sync.RWMutex
	handlers map[string][]Handler
}

func NewBus() *Bus {
	return &Bus{handlers: make(map[string][]Handler)}
}

func (b *Bus) Subscribe(eventType string, h Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers[eventType] = append(b.handlers[eventType], h)
}

func (b *Bus) SubscribeAll(h Handler) {
	b.Subscribe("*", h)
}

func (b *Bus) Publish(e Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for _, h := range b.handlers[e.Type] {
		go h(e)
	}
	for _, h := range b.handlers["*"] {
		go h(e)
	}
}

func (b *Bus) PublishJSON(eventType string, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	b.Publish(Event{Type: eventType, Payload: data})
}
