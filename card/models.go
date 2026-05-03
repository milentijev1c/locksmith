package card

import (
	"time"
)

// CardData contains all readable data from a Serbian ID card
type CardData struct {
	FirstName             string `json:"first_name"`
	LastName              string `json:"last_name"`
	JMBG                  string `json:"jmbg"`
	BirthDate             string `json:"birth_date"`
	Sex                   string `json:"sex"`
	PlaceOfBirth          string `json:"place_of_birth"`
	MunicipalityOfBirth   string `json:"municipality_of_birth"`
	StateOfBirth          string `json:"state_of_birth"`
	State                 string `json:"state"`
	Community             string `json:"community"`
	Place                 string `json:"place"`
	Street                string `json:"street"`
	HouseNumber           string `json:"house_number"`
	AddressStreet         string `json:"address_street"`
	AddressNumber         string `json:"address_number"`
	AddressMunicipality   string `json:"address_municipality"`
	AddressPlace          string `json:"address_place"`
	DocumentNumber        string `json:"document_number"`
	DocumentType          string `json:"document_type"`
	DocumentExpiry        string `json:"document_expiry"`
	PhotoBase64           string `json:"photo_base64"`
}

// CardEvent represents card-related events
type CardEvent struct {
	Type      string    `json:"type"` // "card.inserted", "card.removed", "reader.connected", "reader.disconnected"
	Reader    string    `json:"reader,omitempty"`
	Timestamp time.Time `json:"timestamp"`
	Payload   any       `json:"payload,omitempty"`
}

// SignRequest is the request payload for signing
type SignRequest struct {
	PayloadBase64 string `json:"payload_base64"`
	PIN           string `json:"pin"`
	Algorithm     string `json:"algorithm"` // "SHA256withRSA", etc.
}

// SignResponse is the response from signing
type SignResponse struct {
	SignatureBase64 string `json:"signature_base64"`
	CertificateBase64 string `json:"certificate_base64,omitempty"`
}
