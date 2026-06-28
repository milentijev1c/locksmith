package card

import (
	"fmt"
	"log"
	"time"

	"github.com/ebfe/scard"
)

// ReaderManager handles PC/SC reader detection and connection
type ReaderManager struct {
	ctx    *scard.Context
	logger *log.Logger
}

// NewReaderManager initializes PC/SC context
func NewReaderManager(logger *log.Logger) (*ReaderManager, error) {
	ctx, err := scard.EstablishContext()
	if err != nil {
		return nil, fmt.Errorf("failed to establish PC/SC context: %w", err)
	}

	return &ReaderManager{
		ctx:    ctx,
		logger: logger,
	}, nil
}

// ListReaders returns all available card readers
func (rm *ReaderManager) ListReaders() ([]string, error) {
	readers, err := rm.ctx.ListReaders()
	if err != nil {
		return nil, fmt.Errorf("failed to list readers: %w", err)
	}
	return readers, nil
}

// MonitorReaders continuously monitors all readers for card presence changes
func (rm *ReaderManager) MonitorReaders(stateChan chan<- *ReaderState) error {
	readerStates := make([]scard.ReaderState, 0)
	knownReaders := make(map[string]bool)

	for {
		// Update list of available readers
		readers, err := rm.ListReaders()
		if err != nil {
			rm.logger.Printf("Failed to list readers: %v", err)
			time.Sleep(1 * time.Second)
			continue
		}

		// Build reader state array for monitoring
		readerStates = readerStates[:0]

		// Add new readers to monitoring list
		for _, reader := range readers {
			if !knownReaders[reader] {
				knownReaders[reader] = true
				rm.logger.Printf("New reader detected: %s", reader)
			}

			readerStates = append(readerStates, scard.ReaderState{
				Reader:       reader,
				CurrentState: scard.StateUnaware,
			})
		}

		// Wait for status change with 500ms timeout
		err = rm.ctx.GetStatusChange(readerStates, 500*time.Millisecond)
		if err != nil && err != scard.ErrTimeout {
			rm.logger.Printf("GetStatusChange error: %v", err)
			time.Sleep(1 * time.Second)
			continue
		}

		// Check for card presence changes
		for i, state := range readerStates {
			reader := readers[i]

			// Check if card is present
			cardPresent := (state.EventState & scard.StatePresent) != 0

			// Send state update
			stateChan <- &ReaderState{
				Reader:      reader,
				CardPresent: cardPresent,
			}
		}
	}
}

// ReaderState represents the state of a reader
type ReaderState struct {
	Reader      string
	CardPresent bool
}

// IsCardPresent checks if a card is present in a specific reader
func (rm *ReaderManager) IsCardPresent(reader string) (bool, error) {
	states := []scard.ReaderState{
		{
			Reader:       reader,
			CurrentState: scard.StateUnaware,
		},
	}

	// Non-blocking check with very short timeout
	err := rm.ctx.GetStatusChange(states, 10*time.Millisecond)
	if err != nil && err != scard.ErrTimeout {
		return false, err
	}

	// Check if card is present in the current event state
	cardPresent := (states[0].EventState & scard.StatePresent) != 0
	return cardPresent, nil
}

// ConnectCard establishes a connection to the card in a reader
func (rm *ReaderManager) ConnectCard(reader string) (*scard.Card, error) {
	card, err := rm.ctx.Connect(reader, scard.ShareShared, scard.ProtocolAny)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to card: %w", err)
	}
	return card, nil
}

// Close releases the PC/SC context
func (rm *ReaderManager) Close() error {
	if rm.ctx != nil {
		return rm.ctx.Release()
	}
	return nil
}
