package card

import (
	"crypto/sha256"
	"crypto/sha512"
	"fmt"
	"hash"
	"log"

	"github.com/miekg/pkcs11"
)

// Algorithm constants supported by Serbian ID cards
const (
	AlgSHA256WithRSA = "SHA256withRSA"
	AlgSHA384WithRSA = "SHA384withRSA"
	AlgSHA512WithRSA = "SHA512withRSA"
)

// DigestInfo DER prefixes for RSA PKCS#1 v1.5 signatures
var digestInfoPrefix = map[string]struct {
	oid  []byte
	hash func() hash.Hash
	size int
}{
	AlgSHA256WithRSA: {
		oid:  []byte{0x06, 0x09, 0x60, 0x86, 0x48, 0x01, 0x65, 0x03, 0x04, 0x02, 0x01},
		hash: sha256.New,
		size: sha256.Size,
	},
	AlgSHA384WithRSA: {
		oid:  []byte{0x06, 0x09, 0x60, 0x86, 0x48, 0x01, 0x65, 0x03, 0x04, 0x02, 0x02},
		hash: sha512.New384,
		size: sha512.Size384,
	},
	AlgSHA512WithRSA: {
		oid:  []byte{0x06, 0x09, 0x60, 0x86, 0x48, 0x01, 0x65, 0x03, 0x04, 0x02, 0x03},
		hash: sha512.New,
		size: sha512.Size,
	},
}

// SignService manages signing with the Serbian ID card's private key using PKCS#11.
// It supports lazy initialization — the PKCS#11 module is loaded on the first
// request (Init), so the middleware can start without a card inserted.
type SignService struct {
	modulePath  string
	logger      *log.Logger
	ctx         *pkcs11.Ctx
	slotID      uint
	initialized bool
}

// NewSignService creates a new lazy-initialized signing service.
// The PKCS#11 module is NOT loaded at this point; it will be loaded on the
// first call to Init(). This allows the middleware to start without a card
// inserted.
func NewSignService(logger *log.Logger, modulePath string) (*SignService, error) {
	if modulePath == "" {
		return nil, fmt.Errorf("PKCS#11 module path not configured (set pkcs11_module in config)")
	}

	logger.Printf("Sign service created (lazy init, module: %s)", modulePath)

	return &SignService{
		modulePath: modulePath,
		logger:     logger,
	}, nil
}

// Init initializes the PKCS#11 module, opens a session, and selects the
// first available token. It is safe to call multiple times — subsequent calls
// are no-ops if already initialized. Returns an error if initialization fails.
func (ss *SignService) Init() error {
	if ss.initialized {
		return nil
	}

	ss.logger.Printf("Initializing PKCS#11 module: %s", ss.modulePath)

	p := pkcs11.New(ss.modulePath)
	if p == nil {
		return fmt.Errorf("failed to load PKCS#11 module: %s", ss.modulePath)
	}

	err := p.Initialize()
	if err != nil {
		p.Destroy()
		return fmt.Errorf("failed to initialize PKCS#11 module: %w", err)
	}

	slots, err := p.GetSlotList(true)
	if err != nil || len(slots) == 0 {
		p.Finalize()
		p.Destroy()
		return fmt.Errorf("no card readers with tokens available")
	}

	ss.ctx = p
	ss.slotID = slots[0]
	ss.initialized = true

	ss.logger.Printf("Sign service initialized — %d slot(s) with tokens", len(slots))
	return nil
}

// ensureInitialized ensures the PKCS#11 module is loaded and ready.
// Returns an error if initialization fails.
func (ss *SignService) ensureInitialized() error {
	if ss.initialized {
		return nil
	}
	return ss.Init()
}

// Reinitialize tears down and re-initializes the PKCS#11 session.
// Useful for hot-swapping cards.
func (ss *SignService) Reinitialize() error {
	ss.Close()
	return ss.Init()
}

// IsInitialized returns whether the PKCS#11 module has been successfully initialized.
func (ss *SignService) IsInitialized() bool {
	return ss.initialized
}

// Sign signs a payload with the card's private key using PKCS#11.
// The algorithm parameter selects the hash algorithm (e.g. "SHA256withRSA").
// If empty, defaults to SHA256withRSA.
func (ss *SignService) Sign(payload []byte, pin string, algorithm string) ([]byte, error) {
	if err := ss.ensureInitialized(); err != nil {
		return nil, fmt.Errorf("signing unavailable: %w", err)
	}

	if pin == "" {
		return nil, fmt.Errorf("PIN required for signing")
	}

	// Default to SHA256
	if algorithm == "" {
		algorithm = AlgSHA256WithRSA
	}

	// Validate algorithm
	prefix, ok := digestInfoPrefix[algorithm]
	if !ok {
		return nil, fmt.Errorf("unsupported algorithm: %s (supported: SHA256withRSA, SHA384withRSA, SHA512withRSA)", algorithm)
	}

	// Open session to the card
	session, err := ss.ctx.OpenSession(ss.slotID, pkcs11.CKF_SERIAL_SESSION|pkcs11.CKF_RW_SESSION)
	if err != nil {
		return nil, fmt.Errorf("failed to open session: %w", err)
	}
	defer ss.ctx.CloseSession(session)

	// Log in with PIN
	err = ss.ctx.Login(session, pkcs11.CKU_USER, pin)
	if err != nil {
		return nil, fmt.Errorf("PIN authentication failed: %w", err)
	}
	defer ss.ctx.Logout(session)

	// Find signing private key (CKA_SIGN=true, CKA_CLASS=CKO_PRIVATE_KEY)
	privateKey, err := ss.findSigningKey(session)
	if err != nil {
		return nil, err
	}

	// Compute hash using the selected algorithm
	h := prefix.hash()
	h.Write(payload)
	hashSum := h.Sum(nil)

	// Build DigestInfo and sign
	signature, err := ss.signWithPKCS11(session, privateKey, prefix.oid, hashSum)
	if err != nil {
		return nil, err
	}

	return signature, nil
}

// GetCertificate retrieves the X.509 certificate (DER-encoded) from the card.
func (ss *SignService) GetCertificate() ([]byte, error) {
	if err := ss.ensureInitialized(); err != nil {
		return nil, fmt.Errorf("certificate retrieval unavailable: %w", err)
	}

	// Open session (no PIN required for reading certificates)
	session, err := ss.ctx.OpenSession(ss.slotID, pkcs11.CKF_SERIAL_SESSION)
	if err != nil {
		return nil, fmt.Errorf("failed to open session: %w", err)
	}
	defer ss.ctx.CloseSession(session)

	// Find certificate objects (CKA_CLASS=CKO_CERTIFICATE)
	err = ss.ctx.FindObjectsInit(session, []*pkcs11.Attribute{
		pkcs11.NewAttribute(pkcs11.CKA_CLASS, pkcs11.CKO_CERTIFICATE),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize certificate search: %w", err)
	}

	objects, _, err := ss.ctx.FindObjects(session, 1)
	ss.ctx.FindObjectsFinal(session)
	if err != nil {
		return nil, fmt.Errorf("failed to find certificates: %w", err)
	}
	if len(objects) == 0 {
		return nil, fmt.Errorf("no certificate found on card")
	}

	// Read the DER-encoded certificate value
	attrs, err := ss.ctx.GetAttributeValue(session, objects[0], []*pkcs11.Attribute{
		pkcs11.NewAttribute(pkcs11.CKA_VALUE, nil),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to read certificate: %w", err)
	}

	if len(attrs) == 0 || len(attrs[0].Value) == 0 {
		return nil, fmt.Errorf("certificate value is empty")
	}

	return attrs[0].Value, nil
}

// GetSigningCertificate retrieves the signing certificate from the card.
// For Serbian ID cards there is typically one certificate — this returns it
// directly without trying to locate the private key (which requires PIN login).
func (ss *SignService) GetSigningCertificate() ([]byte, error) {
	return ss.GetCertificate()
}

// findSigningKey locates the private key used for signing (CKA_SIGN=true).
func (ss *SignService) findSigningKey(session pkcs11.SessionHandle) (pkcs11.ObjectHandle, error) {
	err := ss.ctx.FindObjectsInit(session, []*pkcs11.Attribute{
		pkcs11.NewAttribute(pkcs11.CKA_CLASS, pkcs11.CKO_PRIVATE_KEY),
		pkcs11.NewAttribute(pkcs11.CKA_SIGN, true),
	})
	if err != nil {
		return 0, fmt.Errorf("failed to initialize signing key search: %w", err)
	}

	objects, _, err := ss.ctx.FindObjects(session, 1)
	ss.ctx.FindObjectsFinal(session)
	if err != nil {
		return 0, fmt.Errorf("failed to find signing keys: %w", err)
	}
	if len(objects) == 0 {
		return ss.findAnyPrivateKey(session)
	}

	ss.logger.Printf("Found %d signing key(s) on card", len(objects))
	return objects[0], nil
}

// findAnyPrivateKey falls back to finding any private key on the card.
func (ss *SignService) findAnyPrivateKey(session pkcs11.SessionHandle) (pkcs11.ObjectHandle, error) {
	err := ss.ctx.FindObjectsInit(session, []*pkcs11.Attribute{
		pkcs11.NewAttribute(pkcs11.CKA_CLASS, pkcs11.CKO_PRIVATE_KEY),
	})
	if err != nil {
		return 0, fmt.Errorf("failed to initialize private key search: %w", err)
	}

	objects, _, err := ss.ctx.FindObjects(session, 1)
	ss.ctx.FindObjectsFinal(session)
	if err != nil {
		return 0, fmt.Errorf("failed to find private keys: %w", err)
	}
	if len(objects) == 0 {
		return 0, fmt.Errorf("no private key found on card")
	}

	for i, obj := range objects {
		attrs, err := ss.ctx.GetAttributeValue(session, obj, []*pkcs11.Attribute{
			pkcs11.NewAttribute(pkcs11.CKA_LABEL, nil),
			pkcs11.NewAttribute(pkcs11.CKA_ID, nil),
			pkcs11.NewAttribute(pkcs11.CKA_KEY_TYPE, nil),
		})
		if err == nil && len(attrs) >= 3 {
			ss.logger.Printf("Private key %d: label=%q, id=%x, type=%d", i, attrs[0].Value, attrs[1].Value, attrs[2].Value)
		}
	}

	ss.logger.Printf("Using first available private key (%d found)", len(objects))
	return objects[0], nil
}

// signWithPKCS11 performs signing with DigestInfo wrapping using CKM_RSA_PKCS mechanism.
// It builds a PKCS#1 v1.5 DigestInfo structure containing the hash and algorithm OID,
// then passes it to the card which signs using CKM_RSA_PKCS.
func (ss *SignService) signWithPKCS11(session pkcs11.SessionHandle, privateKey pkcs11.ObjectHandle, oid []byte, hash []byte) ([]byte, error) {
	hashLen := byte(len(hash))
	innerLen := byte(len(oid) + 2)
	outerLen := innerLen + 2 + 2 + hashLen

	digestInfo := []byte{
		0x30, outerLen,
		0x30, innerLen,
	}
	digestInfo = append(digestInfo, oid...)
	digestInfo = append(digestInfo, 0x05, 0x00)
	digestInfo = append(digestInfo, 0x04, hashLen)
	digestInfo = append(digestInfo, hash...)

	ss.logger.Printf("Signing %d bytes with CKM_RSA_PKCS, DigestInfo: %d bytes", len(digestInfo), len(digestInfo))

	mechanism := []*pkcs11.Mechanism{
		pkcs11.NewMechanism(pkcs11.CKM_RSA_PKCS, nil),
	}

	err := ss.ctx.SignInit(session, mechanism, privateKey)
	if err != nil {
		return nil, fmt.Errorf("sign initialization failed: %w", err)
	}

	signature, err := ss.ctx.Sign(session, digestInfo)
	if err != nil {
		return nil, fmt.Errorf("signing operation failed: %w", err)
	}

	ss.logger.Printf("Signature produced: %d bytes", len(signature))
	return signature, nil
}

// Close releases resources. Safe to call even if Init() was never called.
func (ss *SignService) Close() {
	if ss.ctx != nil {
		ss.ctx.Finalize()
		ss.ctx.Destroy()
		ss.ctx = nil
	}
	ss.initialized = false
}
