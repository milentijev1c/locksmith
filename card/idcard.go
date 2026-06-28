package card

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"log"
	"strings"

	"github.com/ebfe/scard"
)

// IDCardReader handles reading data from Serbian ID cards (Gemalto/Apollo cards)
type IDCardReader struct {
	logger *log.Logger
}

// NewIDCardReader creates a new ID card reader
func NewIDCardReader(logger *log.Logger) *IDCardReader {
	return &IDCardReader{
		logger: logger,
	}
}

// File locations on Serbian ID cards
var (
	ID_DOCUMENT_FILE_LOC  = []byte{0x0F, 0x02}
	ID_PERSONAL_FILE_LOC  = []byte{0x0F, 0x03}
	ID_RESIDENCE_FILE_LOC = []byte{0x0F, 0x04}
	ID_PHOTO_FILE_LOC     = []byte{0x0F, 0x06}
)

// AID (Application Identifiers) for Serbian ID cards
var (
	// Gemalto ID card AIDs
	GEMALTO_ID_AID = []byte{0xF3, 0x81, 0x00, 0x00, 0x02, 0x53, 0x45, 0x52, 0x49, 0x44, 0x01}
	GEMALTO_IF_AID = []byte{0xF3, 0x81, 0x00, 0x00, 0x02, 0x53, 0x45, 0x52, 0x49, 0x46, 0x01}
	GEMALTO_RP_AID = []byte{0xF3, 0x81, 0x00, 0x00, 0x02, 0x53, 0x45, 0x52, 0x52, 0x50, 0x01}
	// Apollo ID card AID
	APOLLO_ID_AID = []byte{0xA0, 0x00, 0x00, 0x00, 0x49, 0x44, 0x00, 0x00}
)

// ReadCard reads all data from a Serbian ID card and returns CardData
func (r *IDCardReader) ReadCard(card *scard.Card) (*CardData, error) {
	data := &CardData{}

	// Try to initialize and read Gemalto card first (newer cards)
	err := r.readGemaltoCard(card, data)
	if err != nil {
		// Try Apollo card
		err = r.readApolloCard(card, data)
		if err != nil {
			return nil, fmt.Errorf("failed to read card: %w", err)
		}
	}

	return data, nil
}

// readGemaltoCard reads data from Gemalto ID card
func (r *IDCardReader) readGemaltoCard(card *scard.Card, data *CardData) error {
	// Try each Gemalto AID
	aids := [][]byte{GEMALTO_ID_AID, GEMALTO_IF_AID, GEMALTO_RP_AID}

	for _, aid := range aids {
		if err := r.selectFileByAID(card, aid); err != nil {
			r.logger.Printf("Failed to select AID: %v", err)
			continue
		}

		// Successfully selected, now read files
		if err := r.readCardFiles(card, data); err != nil {
			return err
		}
		return nil
	}

	return fmt.Errorf("failed to initialize Gemalto card with any AID")
}

// readApolloCard reads data from Apollo ID card
func (r *IDCardReader) readApolloCard(card *scard.Card, data *CardData) error {
	if err := r.selectFileByAID(card, APOLLO_ID_AID); err != nil {
		return fmt.Errorf("failed to select Apollo AID: %w", err)
	}

	return r.readCardFiles(card, data)
}

// readCardFiles reads file data from the selected card
func (r *IDCardReader) readCardFiles(card *scard.Card, data *CardData) error {
	// Read personal data
	personalData, err := r.readFile(card, ID_PERSONAL_FILE_LOC)
	if err != nil {
		return fmt.Errorf("failed to read personal file: %w", err)
	}

	r.logger.Printf("Personal data length: %d bytes", len(personalData))
	r.logger.Printf("Personal data (hex): %s", formatHex(personalData[:min(len(personalData), 100)]))

	r.parsePersonalData(personalData, data)

	// Read document data
	documentData, err := r.readFile(card, ID_DOCUMENT_FILE_LOC)
	if err != nil {
		r.logger.Printf("Warning: failed to read document file: %v", err)
	} else {
		r.logger.Printf("Document data length: %d bytes", len(documentData))
		r.parseDocumentData(documentData, data)
	}

	// Read residence data
	residenceData, err := r.readFile(card, ID_RESIDENCE_FILE_LOC)
	if err != nil {
		r.logger.Printf("Warning: failed to read residence file: %v", err)
	} else {
		r.logger.Printf("Residence data length: %d bytes", len(residenceData))
		r.parseResidenceData(residenceData, data)
	}

	// Read photo
	photoData, err := r.readFile(card, ID_PHOTO_FILE_LOC)
	if err != nil {
		r.logger.Printf("Warning: failed to read photo file: %v", err)
	} else {
		// Trim first 4 bytes if present (header)
		if len(photoData) > 4 {
			photoData = photoData[4:]
		}
		data.PhotoBase64 = base64.StdEncoding.EncodeToString(photoData)
	}

	return nil
}

// selectFileByAID selects a file by AID (Application ID)
func (r *IDCardReader) selectFileByAID(card *scard.Card, aid []byte) error {
	// CLA=0x00, INS=0xA4 (SELECT), P1=0x04 (by AID), P2=0x00
	apdu := buildAPDU(0x00, 0xA4, 0x04, 0x00, aid, 0)

	resp, err := card.Transmit(apdu)
	if err != nil {
		return fmt.Errorf("transmit error: %w", err)
	}

	if !responseOK(resp) {
		return fmt.Errorf("select AID failed: %s", formatResponse(resp))
	}

	return nil
}

// readFile reads a file from the card by file ID using Gemalto method
func (r *IDCardReader) readFile(card *scard.Card, fileID []byte) ([]byte, error) {
	// Select file: CLA=0x00, INS=0xA4, P1=0x08 (by file ID), P2=0x00, Ne=4
	apdu := buildAPDU(0x00, 0xA4, 0x08, 0x00, fileID, 4)

	resp, err := card.Transmit(apdu)
	if err != nil {
		return nil, fmt.Errorf("select file error: %w", err)
	}

	if !responseOK(resp) {
		return nil, fmt.Errorf("select file failed: %s", formatResponse(resp))
	}

	// Read file header (4 bytes)
	output := make([]byte, 0, 256)

	header, err := r.readBinary(card, 0, 4)
	if err != nil {
		return nil, fmt.Errorf("read header error: %w", err)
	}

	if len(header) < 3 {
		return nil, fmt.Errorf("file too short")
	}

	// File length is at offset 2-3 (little endian)
	length := int(binary.LittleEndian.Uint16(header[2:4]))

	// Read rest of file
	data, err := r.readBinary(card, 4, length)
	if err != nil {
		return nil, fmt.Errorf("read data error: %w", err)
	}

	output = append(output, data...)
	return output, nil
}

// readBinary reads binary data from card at specified offset and length
func (r *IDCardReader) readBinary(card *scard.Card, offset uint, length int) ([]byte, error) {
	output := make([]byte, 0, length)
	remainingLength := length

	for remainingLength > 0 {
		// Read up to 256 bytes at a time
		readSize := remainingLength
		if readSize > 0xFF {
			readSize = 0xFF
		}

		apdu := buildAPDU(0x00, 0xB0, byte((offset>>8)&0xFF), byte(offset&0xFF), nil, uint(readSize))

		resp, err := card.Transmit(apdu)
		if err != nil {
			return nil, fmt.Errorf("read binary error: %w", err)
		}

		if len(resp) < 2 {
			return nil, fmt.Errorf("read binary: bad status code")
		}

		data := resp[:len(resp)-2]
		output = append(output, data...)

		// Check status word
		sw := binary.BigEndian.Uint16(resp[len(resp)-2:])
		if sw == 0x9000 {
			break
		}
		if sw&0xFF00 == 0x6100 {
			// More data available
			offset += uint(len(data))
			remainingLength -= len(data)
			continue
		}

		break
	}

	return output, nil
}

// buildAPDU constructs an ISO 7816-4 APDU command
func buildAPDU(cla, ins, p1, p2 byte, data []byte, ne uint) []byte {
	length := len(data)

	apdu := make([]byte, 4)
	apdu[0] = cla
	apdu[1] = ins
	apdu[2] = p1
	apdu[3] = p2

	if length == 0 {
		if ne != 0 {
			if ne <= 256 {
				l := byte(0x00)
				if ne != 256 {
					l = byte(ne)
				}
				apdu = append(apdu, l)
			} else {
				var l1, l2 byte
				if ne == 65536 {
					l1 = 0
					l2 = 0
				} else {
					l1 = byte(ne >> 8)
					l2 = byte(ne)
				}
				apdu = append(apdu, []byte{l1, l2}...)
			}
		}
	} else {
		if ne == 0 {
			if length <= 255 {
				apdu = append(apdu, byte(length))
				apdu = append(apdu, data...)
			} else {
				l := []byte{0x0, byte(length >> 8), byte(length)}
				apdu = append(apdu, l...)
				apdu = append(apdu, data...)
			}
		} else {
			if length <= 255 && ne <= 256 {
				apdu = append(apdu, byte(length))
				apdu = append(apdu, data...)
				if ne != 256 {
					apdu = append(apdu, byte(ne))
				} else {
					apdu = append(apdu, 0x00)
				}
			} else {
				l := []byte{0x00, byte(length >> 8), byte(length)}
				apdu = append(apdu, l...)
				apdu = append(apdu, data...)
				if ne != 65536 {
					neB := []byte{byte(ne >> 8), byte(ne)}
					apdu = append(apdu, neB...)
				}
			}
		}
	}

	return apdu
}

// responseOK checks if response has 0x9000 status code
func responseOK(resp []byte) bool {
	if len(resp) < 2 {
		return false
	}
	return resp[len(resp)-2] == 0x90 && resp[len(resp)-1] == 0x00
}

// formatResponse formats card response for debugging
func formatResponse(resp []byte) string {
	if len(resp) < 2 {
		return fmt.Sprintf("%02X", resp)
	}
	sw := binary.BigEndian.Uint16(resp[len(resp)-2:])
	return fmt.Sprintf("0x%04X", sw)
}

// parseTLV parses TLV with 2-byte tag and 2-byte little-endian length
func (r *IDCardReader) parseTLV(data []byte) map[int][]byte {
	result := make(map[int][]byte)

	for i := 0; i < len(data); {
		// Read 2-byte tag (big endian)
		if i+4 > len(data) {
			break
		}

		tag := int(binary.BigEndian.Uint16(data[i : i+2]))
		// Read 2-byte length (little endian)
		length := int(binary.LittleEndian.Uint16(data[i+2 : i+4]))
		i += 4

		if i+length > len(data) {
			r.logger.Printf("TLV: truncated data for tag %04X (need %d bytes, have %d)", tag, length, len(data)-i)
			break
		}

		value := data[i : i+length]
		result[tag] = value

		r.logger.Printf("TLV: tag=%04X, length=%d, value=%s", tag, length, formatValue(value))

		i += length
	}

	return result
}

// formatValue formats a TLV value for debugging
func formatValue(data []byte) string {
	if len(data) > 50 {
		return fmt.Sprintf("%s... (truncated, %d bytes)", string(data[:50]), len(data))
	}
	return string(data)
}

// parseDocumentData parses document TLV data
func (r *IDCardReader) parseDocumentData(data []byte, cardData *CardData) {
	fields := r.parseTLV(data)

	// TLV tags for document data
	if docRegNo, ok := fields[1546]; ok {
		cardData.DocumentNumber = strings.TrimRight(string(docRegNo), "\x00")
	}
	if docType, ok := fields[1547]; ok {
		cardData.DocumentType = strings.TrimRight(string(docType), "\x00")
	}
	if expiryDate, ok := fields[1550]; ok {
		cardData.DocumentExpiry = r.formatDate(expiryDate)
	}
}

// parsePersonalData parses personal TLV data
func (r *IDCardReader) parsePersonalData(data []byte, cardData *CardData) {
	fields := r.parseTLV(data)

	// TLV tags for personal data (based on actual card output)
	// 0x1606 = JMBG, 0x1706 = Surname, 0x1806 = GivenName, 0x1906 = Father's name, 0x1A06 = Sex, 0x1B06 = Place of birth

	if jmbg, ok := fields[0x1606]; ok {
		cardData.JMBG = strings.TrimRight(string(jmbg), "\x00")
		r.logger.Printf("Parsed JMBG: %s", cardData.JMBG)
	}
	if surname, ok := fields[0x1706]; ok {
		cardData.LastName = strings.TrimRight(string(surname), "\x00")
		r.logger.Printf("Parsed LastName: %s", cardData.LastName)
	}
	if givenName, ok := fields[0x1806]; ok {
		cardData.FirstName = strings.TrimRight(string(givenName), "\x00")
		r.logger.Printf("Parsed FirstName: %s", cardData.FirstName)
	}
	if fatherName, ok := fields[0x1906]; ok {
		r.logger.Printf("Father name: %s", string(fatherName))
	}
	if sex, ok := fields[0x1A06]; ok {
		cardData.Sex = strings.TrimRight(string(sex), "\x00")
		r.logger.Printf("Parsed Sex: %s", cardData.Sex)
	}
	if birthPlace, ok := fields[0x1B06]; ok {
		cardData.PlaceOfBirth = strings.TrimRight(string(birthPlace), "\x00")
		r.logger.Printf("Parsed PlaceOfBirth: %s", cardData.PlaceOfBirth)
	}

	// Try old-style tags as fallback
	if jmbg, ok := fields[1558]; ok && cardData.JMBG == "" {
		cardData.JMBG = strings.TrimRight(string(jmbg), "\x00")
	}
	if surname, ok := fields[1559]; ok && cardData.LastName == "" {
		cardData.LastName = strings.TrimRight(string(surname), "\x00")
	}
	if givenName, ok := fields[1560]; ok && cardData.FirstName == "" {
		cardData.FirstName = strings.TrimRight(string(givenName), "\x00")
	}
}

// parseResidenceData parses residence TLV data
func (r *IDCardReader) parseResidenceData(data []byte, cardData *CardData) {
	fields := r.parseTLV(data)

	// TLV tags for residence data
	if state, ok := fields[1568]; ok {
		cardData.State = strings.TrimRight(string(state), "\x00")
	}
	if community, ok := fields[1569]; ok {
		cardData.Community = strings.TrimRight(string(community), "\x00")
	}
	if place, ok := fields[1570]; ok {
		cardData.Place = strings.TrimRight(string(place), "\x00")
	}
	if street, ok := fields[1571]; ok {
		cardData.Street = strings.TrimRight(string(street), "\x00")
	}
	if houseNumber, ok := fields[1572]; ok {
		cardData.HouseNumber = strings.TrimRight(string(houseNumber), "\x00")
	}
}

// formatDate formats date bytes (usually DD/MM/YYYY)
func (r *IDCardReader) formatDate(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	return strings.TrimRight(string(data), "\x00")
}

// formatHex formats bytes as hex string
func formatHex(data []byte) string {
	result := ""
	for i, b := range data {
		if i > 0 && i%16 == 0 {
			result += "\n"
		}
		result += fmt.Sprintf("%02X ", b)
	}
	return result
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
