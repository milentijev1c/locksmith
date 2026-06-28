package server

import (
	"log"

	"github.com/gorilla/websocket"
)

// WebSocketHub manages WebSocket connections
type WebSocketHub struct {
	clients    map[*WebSocketClient]bool
	broadcast  chan interface{}
	register   chan *WebSocketClient
	unregister chan *WebSocketClient
	logger     *log.Logger
	stop       chan struct{}
}

// WebSocketClient represents a WebSocket client connection
type WebSocketClient struct {
	conn *websocket.Conn
	send chan interface{}
}

// NewWebSocketHub creates a new WebSocket hub
func NewWebSocketHub(logger *log.Logger) *WebSocketHub {
	return &WebSocketHub{
		broadcast:  make(chan interface{}, 256),
		register:   make(chan *WebSocketClient),
		unregister: make(chan *WebSocketClient),
		clients:    make(map[*WebSocketClient]bool),
		logger:     logger,
		stop:       make(chan struct{}),
	}
}

// Run starts the hub event loop
func (h *WebSocketHub) Run() {
	for {
		select {
		case <-h.stop:
			return
		case client := <-h.register:
			h.clients[client] = true
			h.logger.Printf("WebSocket client registered, total: %d", len(h.clients))

		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
				h.logger.Printf("WebSocket client unregistered, total: %d", len(h.clients))
			}

		case message := <-h.broadcast:
			for client := range h.clients {
				select {
				case client.send <- message:
				default:
					// Client's send channel is full, skip
					go func(c *WebSocketClient) {
						h.unregister <- c
					}(client)
				}
			}
		}
	}
}

// Register registers a new client
func (h *WebSocketHub) Register(client *WebSocketClient) {
	h.register <- client
}

// Unregister removes a client
func (h *WebSocketHub) Unregister(client *WebSocketClient) {
	h.unregister <- client
}

// Broadcast sends a message to all clients
func (h *WebSocketHub) Broadcast(message interface{}) {
	h.broadcast <- message
}

// Stop stops the hub
func (h *WebSocketHub) Stop() {
	close(h.stop)
}

// readPump reads messages from the WebSocket client
func (c *WebSocketClient) readPump() {
	defer func() {
		c.conn.Close()
	}()

	for {
		var msg map[string]interface{}
		err := c.conn.ReadJSON(&msg)
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("Unexpected WebSocket close error: %v", err)
			}
			break
		}
		// Handle client messages if needed
	}
}
