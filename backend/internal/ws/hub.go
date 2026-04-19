package ws

import (
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/SoldierOfGod1/command-centre/internal/event"
)

type Envelope struct {
	Type      string          `json:"type"`
	Timestamp string          `json:"timestamp"`
	Payload   json.RawMessage `json:"payload"`
}

type Hub struct {
	clients    map[*Client]bool
	broadcast  chan []byte
	register   chan *Client
	unregister chan *Client
	mu         sync.RWMutex
	log        *slog.Logger
}

func NewHub(log *slog.Logger, bus *event.Bus) *Hub {
	h := &Hub{
		clients:    make(map[*Client]bool),
		broadcast:  make(chan []byte, 256),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		log:        log,
	}

	bus.SubscribeAll(func(e event.Event) {
		env := Envelope{
			Type:      e.Type,
			Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
			Payload:   e.Payload,
		}
		data, err := json.Marshal(env)
		if err != nil {
			return
		}
		h.Broadcast(data)
	})

	return h
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
			h.log.Info("websocket client connected", "clients", h.ClientCount())

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			h.mu.Unlock()
			h.log.Info("websocket client disconnected", "clients", h.ClientCount())

		case message := <-h.broadcast:
			h.mu.RLock()
			for client := range h.clients {
				select {
				case client.send <- message:
				default:
					close(client.send)
					delete(h.clients, client)
				}
			}
			h.mu.RUnlock()
		}
	}
}

func (h *Hub) Broadcast(data []byte) {
	select {
	case h.broadcast <- data:
	default:
	}
}

func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

func (h *Hub) Register(c *Client)   { h.register <- c }
func (h *Hub) Unregister(c *Client) { h.unregister <- c }
