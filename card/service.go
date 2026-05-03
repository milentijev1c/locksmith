package card

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/ebfe/scard"
)

// CardService manages card reading operations and event notifications
type CardService struct {
	ReaderManager   *ReaderManager
	cardReader      *IDCardReader
	logger          *log.Logger

	// State
	mu              sync.RWMutex
	readerConnected bool
	cardPresent     bool
	lastCardData    *CardData
	activeReader    string

	// Event broadcasting
	subscribers map[chan CardEvent]bool
	subMu       sync.RWMutex

	// Polling
	pollInterval time.Duration
	stopChan     chan struct{}
}

// NewCardService initializes the card service
func NewCardService(logger *log.Logger) (*CardService, error) {
	readerMgr, err := NewReaderManager(logger)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize reader manager: %w", err)
	}

	return &CardService{
		ReaderManager: readerMgr,
		cardReader:    NewIDCardReader(logger),
		logger:        logger,
		subscribers:   make(map[chan CardEvent]bool),
		pollInterval:  500 * time.Millisecond,
		stopChan:      make(chan struct{}),
	}, nil
}

// Start begins polling for card events
func (cs *CardService) Start(ctx context.Context) {
	ticker := time.NewTicker(cs.pollInterval)
	defer ticker.Stop()

	var lastCardPresent bool
	var lastReaderConnected bool

	for {
		select {
		case <-cs.stopChan:
			return
		case <-ticker.C:
			// Check reader availability
			readers, _ := cs.ReaderManager.ListReaders()
			readerConnected := len(readers) > 0

			if readerConnected != lastReaderConnected {
				lastReaderConnected = readerConnected
				
				cs.mu.Lock()
				cs.readerConnected = readerConnected
				cs.mu.Unlock()

				if readerConnected {
					cs.broadcastEvent(CardEvent{
						Type:      "reader.connected",
						Reader:    readers[0],
						Timestamp: time.Now(),
					})
					cs.activeReader = readers[0]
				} else {
					cs.broadcastEvent(CardEvent{
						Type:      "reader.disconnected",
						Timestamp: time.Now(),
					})
					cs.activeReader = ""
				}
			}

			// Check card presence
			if cs.activeReader != "" {
				cardPresent, _ := cs.ReaderManager.IsCardPresent(cs.activeReader)

				if cardPresent != lastCardPresent {
					lastCardPresent = cardPresent
					
					cs.mu.Lock()
					cs.cardPresent = cardPresent
					cs.mu.Unlock()

					if cardPresent {
						cs.broadcastEvent(CardEvent{
							Type:      "card.inserted",
							Reader:    cs.activeReader,
							Timestamp: time.Now(),
						})
					} else {
						cs.broadcastEvent(CardEvent{
							Type:      "card.removed",
							Reader:    cs.activeReader,
							Timestamp: time.Now(),
						})
					}
				}
			}
		}
	}
}

// ReadCard reads data from the currently inserted card
func (cs *CardService) ReadCard() (*CardData, error) {
	cs.mu.RLock()
	if !cs.cardPresent || cs.activeReader == "" {
		cs.mu.RUnlock()
		return nil, fmt.Errorf("no card present")
	}
	reader := cs.activeReader
	cs.mu.RUnlock()

	// Connect to card
	card, err := cs.ReaderManager.ConnectCard(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to card: %w", err)
	}
	defer card.Disconnect(scard.LeaveCard)

	// Read card data
	cardData, err := cs.cardReader.ReadCard(card)
	if err != nil {
		return nil, fmt.Errorf("failed to read card data: %w", err)
	}

	// Cache the data
	cs.mu.Lock()
	cs.lastCardData = cardData
	cs.mu.Unlock()

	return cardData, nil
}

// GetStatus returns the current status
func (cs *CardService) GetStatus() map[string]interface{} {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	return map[string]interface{}{
		"reader_connected": cs.readerConnected,
		"card_present":     cs.cardPresent,
		"active_reader":    cs.activeReader,
	}
}

// Subscribe creates a new event subscription
func (cs *CardService) Subscribe() chan CardEvent {
	ch := make(chan CardEvent, 10)
	
	cs.subMu.Lock()
	cs.subscribers[ch] = true
	cs.subMu.Unlock()

	return ch
}

// Unsubscribe removes an event subscription
func (cs *CardService) Unsubscribe(ch chan CardEvent) {
	cs.subMu.Lock()
	delete(cs.subscribers, ch)
	cs.subMu.Unlock()
	close(ch)
}

// broadcastEvent sends an event to all subscribers
func (cs *CardService) broadcastEvent(event CardEvent) {
	cs.subMu.RLock()
	defer cs.subMu.RUnlock()

	for ch := range cs.subscribers {
		select {
		case ch <- event:
		default:
			// Don't block on slow subscribers
		}
	}
}

// Close closes the card service
func (cs *CardService) Close() error {
	close(cs.stopChan)
	
	cs.subMu.Lock()
	for ch := range cs.subscribers {
		close(ch)
	}
	cs.subscribers = make(map[chan CardEvent]bool)
	cs.subMu.Unlock()

		return cs.ReaderManager.Close()
}
