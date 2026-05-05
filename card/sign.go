package card

import (
	"crypto/sha256"
	"fmt"
	"log"

	"github.com/miekg/pkcs11"
)

// SignService manages PKCS#11 signing with the card's private key
type SignService struct {
	logger       *log.Logger
	lib          *pkcs11.Ctx
	slot         uint
	pkcs11Module string
}

// NewSignService initializes PKCS#11 signing
func NewSignService(logger *log.Logger, pkcs11Module string) (*SignService, error) {
	if pkcs11Module == "" {
		return nil, fmt.Errorf("pkcs11_module not configured")
	}

	lib := pkcs11.New(pkcs11Module)
	if lib == nil {
		return nil, fmt.Errorf("failed to load PKCS#11 module: %s", pkcs11Module)
	}

	if err := lib.Initialize(); err != nil {
		return nil, fmt.Errorf("failed to initialize PKCS#11: %w", err)
	}

	slots, err := lib.GetSlotList(true)
	if err != nil {
		lib.Finalize()
		return nil, fmt.Errorf("failed to list slots: %w", err)
	}

	if len(slots) == 0 {
		lib.Finalize()
		return nil, fmt.Errorf("no card slots available")
	}

	return &SignService{
		logger:       logger,
		lib:          lib,
		slot:         slots[0],
		pkcs11Module: pkcs11Module,
	}, nil
}

// Sign signs a payload with the card's private key using PKCS#11
func (ss *SignService) Sign(payload []byte, pin string, algorithm string) ([]byte, error) {
	if pin == "" {
		return nil, fmt.Errorf("PIN required for signing")
	}

	session, err := ss.lib.OpenSession(ss.slot, pkcs11.CKF_SERIAL_SESSION|pkcs11.CKF_RW_SESSION)
	if err != nil {
		return nil, fmt.Errorf("failed to open PKCS#11 session: %w", err)
	}
	defer ss.lib.CloseSession(session)

	if err := ss.lib.Login(session, pkcs11.CKU_USER, pin); err != nil {
		return nil, fmt.Errorf("PKCS#11 login failed: %w", err)
	}
	defer ss.lib.Logout(session)

	template := []*pkcs11.Attribute{
		pkcs11.NewAttribute(pkcs11.CKA_CLASS, pkcs11.CKO_PRIVATE_KEY),
		pkcs11.NewAttribute(pkcs11.CKA_SIGN, true),
	}

	if err := ss.lib.FindObjectsInit(session, template); err != nil {
		return nil, fmt.Errorf("failed to init find objects: %w", err)
	}
	defer ss.lib.FindObjectsFinal(session)

	objects, _, err := ss.lib.FindObjects(session, 1)
	if err != nil {
		return nil, fmt.Errorf("failed to find signing key: %w", err)
	}

	if len(objects) == 0 {
		return nil, fmt.Errorf("no signing key found on card")
	}

	privKey := objects[0]

	hash := sha256.Sum256(payload)

	mechanism := []*pkcs11.Mechanism{pkcs11.NewMechanism(pkcs11.CKM_SHA256_RSA_PKCS, nil)}

	if err := ss.lib.SignInit(session, mechanism, privKey); err != nil {
		return nil, fmt.Errorf("sign init failed: %w", err)
	}

	signature, err := ss.lib.Sign(session, hash[:])
	if err != nil {
		return nil, fmt.Errorf("signing failed: %w", err)
	}

	return signature, nil
}

// Close cleans up PKCS#11 resources
func (ss *SignService) Close() error {
	if ss.lib != nil {
		ss.lib.Finalize()
	}
	return nil
}
