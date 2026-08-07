package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"github.com/milentijev1c/locksmith/card"
	"github.com/milentijev1c/locksmith/config"
)

// Server represents the HTTP/WebSocket server
type Server struct {
	config      *config.Config
	cardService *card.CardService
	signService *card.SignService
	logger      *log.Logger
	version     string

	httpServer *http.Server
	hub        *WebSocketHub
}

// NewServer creates a new server instance
func NewServer(cfg *config.Config, cardService *card.CardService, signService *card.SignService, logger *log.Logger, version string) *Server {
	s := &Server{
		config:      cfg,
		cardService: cardService,
		signService: signService,
		logger:      logger,
		version:     version,
		hub:         NewWebSocketHub(logger),
	}

	// Setup routes
	mux := http.NewServeMux()

	// Add CORS middleware
	wrappedMux := s.corsMiddleware(mux)

	// Static web UI
	s.registerStaticUI(mux)

	// REST endpoints
	mux.HandleFunc("/status", s.handleStatus)
	mux.HandleFunc("/readers", s.handleReaders)
	mux.HandleFunc("/card/read", s.handleCardRead)
	mux.HandleFunc("/card/sign", s.handleCardSign)
	mux.HandleFunc("/card/sign-pdf", s.handleSignPDF)
	mux.HandleFunc("/card/photo", s.handleCardPhoto)
	mux.HandleFunc("/card/certificate", s.handleCardCertificate)

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

// isOriginAllowed checks if an origin is in the allowed list.
// It also allows any localhost/127.0.0.1 origin (self-origin for the web UI).
func isOriginAllowed(origin string, allowed []string) bool {
	if origin == "" {
		return true
	}

	for _, allowedOrigin := range allowed {
		if origin == allowedOrigin {
			return true
		}
	}

	// Allow any localhost/127.0.0.1 origin on any port (the web UI served by this daemon)
	if strings.HasPrefix(origin, "http://localhost") || strings.HasPrefix(origin, "http://127.0.0.1") {
		return true
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

// handleCardSign signs a document with the card's private key
func (s *Server) handleCardSign(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.signService == nil {
		http.Error(w, "signing not available", http.StatusServiceUnavailable)
		return
	}

	var req card.SignRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid request: %v", err), http.StatusBadRequest)
		return
	}

	if req.PayloadBase64 == "" || req.PIN == "" {
		http.Error(w, "missing payload_base64 or pin", http.StatusBadRequest)
		return
	}

	payload, err := base64.StdEncoding.DecodeString(req.PayloadBase64)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid payload encoding: %v", err), http.StatusBadRequest)
		return
	}

	signature, err := s.signService.Sign(payload, req.PIN, req.Algorithm)
	if err != nil {
		s.logger.Printf("Sign error: %v", err)
		http.Error(w, fmt.Sprintf("signing failed: %v", err), http.StatusInternalServerError)
		return
	}

	resp := card.SignResponse{
		SignatureBase64: base64.StdEncoding.EncodeToString(signature),
	}

	// Optionally include the signing certificate in the response
	certDER, err := s.signService.GetSigningCertificate()
	if err == nil && len(certDER) > 0 {
		resp.CertificateBase64 = base64.StdEncoding.EncodeToString(certDER)
	} else if err != nil {
		s.logger.Printf("Could not retrieve certificate: %v", err)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// handleCardCertificate returns the DER-encoded certificate from the card as base64
func (s *Server) handleCardCertificate(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.signService == nil {
		http.Error(w, "signing not available", http.StatusServiceUnavailable)
		return
	}

	certDER, err := s.signService.GetCertificate()
	if err != nil {
		s.logger.Printf("Certificate error: %v", err)
		http.Error(w, fmt.Sprintf("certificate retrieval failed: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"certificate_base64": base64.StdEncoding.EncodeToString(certDER),
	})
}

// handleSignPDF signs a PDF and returns the signed PDF bytes
func (s *Server) handleSignPDF(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.signService == nil {
		http.Error(w, "signing not available", http.StatusServiceUnavailable)
		return
	}

	var req card.SignRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid request: %v", err), http.StatusBadRequest)
		return
	}

	if req.PayloadBase64 == "" || req.PIN == "" {
		http.Error(w, "missing payload_base64 or pin", http.StatusBadRequest)
		return
	}

	pdfBytes, err := base64.StdEncoding.DecodeString(req.PayloadBase64)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid payload encoding: %v", err), http.StatusBadRequest)
		return
	}

	signedPDF, err := s.signService.SignPDF(pdfBytes, req.PIN, req.Algorithm)
	if err != nil {
		s.logger.Printf("PDF sign error: %v", err)
		http.Error(w, fmt.Sprintf("pdf signing failed: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", "attachment; filename=\"signed.pdf\"")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(signedPDF); err != nil {
		log.Printf("Failed to write signed PDF response: %v", err)
	}
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
			_ = conn.WriteJSON(message)
		case event := <-eventChan:
			_ = conn.WriteJSON(event)
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
