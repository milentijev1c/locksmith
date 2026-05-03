package card

import (
	"fmt"
	"log"
)

// SignService manages signing operations (stub)
type SignService struct {
	logger *log.Logger
}

// NewSignService creates a new signing service
func NewSignService(logger *log.Logger) *SignService {
	return &SignService{
		logger: logger,
	}
}

// Sign attempts to sign payload with card (not implemented)
func (ss *SignService) Sign(payload []byte, pin string, algorithm string) ([]byte, error) {
	return nil, fmt.Errorf("signing not implemented")
}
