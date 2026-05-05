package server

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"github.com/milentijev1c/locksmith/card"
	"github.com/milentijev1c/locksmith/config"
)

// Server represents the HTTP/WebSocket server
type Server struct {
	config      *config.Config
	cardService *card.CardService
	logger      *log.Logger
	version     string

	httpServer *http.Server
	hub        *WebSocketHub
}

// NewServer creates a new server instance
func NewServer(cfg *config.Config, cardService *card.CardService, logger *log.Logger, version string) *Server {
	s := &Server{
		config:      cfg,
		cardService: cardService,
		logger:      logger,
		version:     version,
		hub:         NewWebSocketHub(logger),
	}

	// Setup routes
	mux := http.NewServeMux()
	
	// Add CORS middleware
	wrappedMux := s.corsMiddleware(mux)

	// REST endpoints
	mux.HandleFunc("/status", s.handleStatus)
	mux.HandleFunc("/readers", s.handleReaders)
	mux.HandleFunc("/card/read", s.handleCardRead)
	mux.HandleFunc("/card/sign", s.handleCardSign)
	mux.HandleFunc("/card/photo", s.handleCardPhoto)

	// WebSocket endpoint
	mux.HandleFunc("/ws", s.handleWebSocket)

	s.httpServer = &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.BindAddress, cfg.Port),
		Handler:      wrappedMux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start WebSocket hub
	go s.hub.Run()

	return s
}

// ListenAndServe starts the HTTP server
func (s *Server) ListenAndServe(addr string) error {
	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown(ctx context.Context) error {
	s.hub.Stop()
	return s.httpServer.Shutdown(ctx)
}

// corsMiddleware adds CORS validation
func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		
		// Allow localhost and configured origins
		if isOriginAllowed(origin, s.config.AllowedOrigins) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		}

		// Handle preflight
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// isOriginAllowed checks if an origin is in the allowed list
func isOriginAllowed(origin string, allowed []string) bool {
	if origin == "" {
		return true
	}

	for _, allowedOrigin := range allowed {
		if origin == allowedOrigin || origin == "http://localhost:3000" || origin == "http://127.0.0.1:3000" {
			return true
		}
	}
	return false
}

// handleStatus returns server status
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	status := s.cardService.GetStatus()

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"status":"ok","version":"%s","reader_connected":%v,"card_present":%v}`,
		s.version, status["reader_connected"], status["card_present"])
}

// handleReaders returns list of available readers
func (s *Server) handleReaders(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	readers, err := s.cardService.ReaderManager.ListReaders()
	if err != nil {
		http.Error(w, fmt.Sprintf("Error: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"readers":%v}`, readers)
}

// handleCardRead reads card data
func (s *Server) handleCardRead(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	cardData, err := s.cardService.ReadCard()
	if err != nil {
		http.Error(w, fmt.Sprintf("Error: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	// Return as JSON (implementation of JSON marshaling would go here)
	fmt.Fprintf(w, `{"first_name":"%s","last_name":"%s","jmbg":"%s"}`,
		cardData.FirstName, cardData.LastName, cardData.JMBG)
}

// handleCardSign signs a document (not implemented)
func (s *Server) handleCardSign(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	http.Error(w, "signing not implemented", http.StatusNotImplemented)
}

// handleCardPhoto returns the card's photo
func (s *Server) handleCardPhoto(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	cardData, err := s.cardService.ReadCard()
	if err != nil {
		http.Error(w, fmt.Sprintf("Error: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"photo_base64":"%s"}`, cardData.PhotoBase64)
}

// handleWebSocket handles WebSocket connections
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return isOriginAllowed(r.Header.Get("Origin"), s.config.AllowedOrigins)
		},
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Printf("WebSocket upgrade error: %v", err)
		return
	}

	client := &WebSocketClient{
		conn: conn,
		send: make(chan interface{}, 256),
	}

	s.hub.Register(client)
	defer s.hub.Unregister(client)

	status := s.cardService.GetStatus()
	for _, message := range webSocketStartupMessages(status, s.version) {
		client.send <- message
	}

	// Subscribe to card events
	eventChan := s.cardService.Subscribe()
	defer s.cardService.Unsubscribe(eventChan)

	// Read messages
	go client.readPump()

	// Write messages and card events
	for {
		select {
		case message := <-client.send:
			conn.WriteJSON(message)
		case event := <-eventChan:
			conn.WriteJSON(event)
		}
	}
}

func webSocketStartupMessages(status map[string]interface{}, version string) []interface{} {
	messages := []interface{}{
		map[string]interface{}{
			"type": "ws.connected",
			"payload": map[string]string{
				"version": version,
			},
		},
	}

	readerConnected, _ := status["reader_connected"].(bool)
	readerName, _ := status["active_reader"].(string)

	if readerConnected {
		messages = append(messages, card.CardEvent{
			Type:      "reader.connected",
			Reader:    readerName,
			Timestamp: time.Now(),
		})
		return messages
	}

	messages = append(messages, card.CardEvent{
		Type:      "reader.disconnected",
		Timestamp: time.Now(),
	})

	return messages
}
