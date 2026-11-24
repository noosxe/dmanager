package auth

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"strings"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"google.golang.org/protobuf/types/known/timestamppb"

	"dmanager/internal/db"
	v1 "dmanager/internal/gen/proto/dmanager/v1"
)

// WebAuthnUser implements the webauthn.User interface for go-webauthn.
type WebAuthnUser struct {
	ID          int64
	Username    string
	DisplayName string
	Credentials []webauthn.Credential
	// Handle is the stored per-user WebAuthn handle (users.webauthn_user_id).
	// When empty, WebAuthnID falls back to the legacy handle.
	Handle []byte
}

// WebAuthnHandleLen is the length of a randomly generated user handle.
const WebAuthnHandleLen = 32

// legacyWebAuthnHandle is the handle used before per-user random handles: the
// 8-byte big-endian row ID. Users who registered passkeys back then keep it,
// because their authenticators stored it.
func legacyWebAuthnHandle(id int64) []byte {
	b := make([]byte, 8)
	if id > 0 {
		binary.BigEndian.PutUint64(b, uint64(id))
	}
	return b
}

// WebAuthnID returns the stored handle, or the legacy row-ID handle when none
// is stored.
func (u *WebAuthnUser) WebAuthnID() []byte {
	if len(u.Handle) > 0 {
		return u.Handle
	}
	return legacyWebAuthnHandle(u.ID)
}

func (u *WebAuthnUser) WebAuthnName() string {
	return u.Username
}

func (u *WebAuthnUser) WebAuthnDisplayName() string {
	if u.DisplayName != "" {
		return u.DisplayName
	}
	return u.Username
}

func (u *WebAuthnUser) WebAuthnIcon() string {
	return ""
}

func (u *WebAuthnUser) WebAuthnCredentials() []webauthn.Credential {
	return u.Credentials
}

func dbCredToWebAuthn(c db.WebauthnCredential) webauthn.Credential {
	var transports []protocol.AuthenticatorTransport
	if c.Transport != "" {
		for _, t := range strings.Split(c.Transport, ",") {
			t = strings.TrimSpace(t)
			if t != "" {
				transports = append(transports, protocol.AuthenticatorTransport(t))
			}
		}
	}

	var signCount uint32
	if c.SignCount > 0 && c.SignCount <= math.MaxUint32 {
		signCount = uint32(c.SignCount)
	}

	return webauthn.Credential{
		ID:              c.CredentialID,
		PublicKey:       c.PublicKey,
		AttestationType: c.AttestationType,
		Transport:       transports,
		Flags: webauthn.CredentialFlags{
			UserPresent:    true,
			UserVerified:   c.UserVerified == 1,
			BackupEligible: c.BackupEligible == 1,
			BackupState:    c.BackupState == 1,
		},
		Authenticator: webauthn.Authenticator{
			AAGUID:    c.Aaguid,
			SignCount: signCount,
		},
	}
}

func aaguidToDeviceName(aaguid []byte) string {
	if len(aaguid) != 16 {
		return "Passkey / Security Key"
	}
	aaguidStr := fmt.Sprintf("%x-%x-%x-%x-%x", aaguid[0:4], aaguid[4:6], aaguid[6:8], aaguid[8:10], aaguid[10:16])
	switch strings.ToLower(aaguidStr) {
	case "adce0002-35bc-c60a-648b-0b25f1f05503":
		return "Chrome on Mac"
	case "08987058-cadc-4b81-b6e1-30de50dcbe96":
		return "Windows Hello"
	case "6028b012-b1a8-444a-89a1-7787cf3d3b76":
		return "iCloud Keychain"
	case "fbfc3007-154e-4ecc-8c0b-6e020557d7b1":
		return "YubiKey 5 Series"
	case "cb69481e-8ff7-4039-93ec-0a2729a1d67b":
		return "YubiKey 5 NFC"
	case "fa2b99dc-9e39-4257-8f92-4a30d23c4118":
		return "YubiKey 5Ci"
	case "ee882879-721c-4916-be20-320099538356":
		return "Google Password Manager"
	case "ea9b8d66-4d01-1d21-3ce4-b6b48cb575d4":
		return "1Password"
	case "53b27b38-d652-4467-96a4-c0ec5f97ec9e":
		return "Bitwarden"
	default:
		return "Passkey / Security Key"
	}
}

func formatPasskeyProto(c db.WebauthnCredential) *v1.Passkey {
	idHex := hex.EncodeToString(c.CredentialID)
	aaguidHex := hex.EncodeToString(c.Aaguid)
	friendlyName := aaguidToDeviceName(c.Aaguid)

	name := c.Name
	if name == "" {
		name = friendlyName
	}

	var lastUsed *timestamppb.Timestamp
	if c.LastUsedAt.Valid {
		lastUsed = timestamppb.New(c.LastUsedAt.Time)
	}

	var signCount32 int32
	if c.SignCount > 0 && c.SignCount <= math.MaxInt32 {
		signCount32 = int32(c.SignCount)
	}

	return &v1.Passkey{
		Id:                 idHex,
		Name:               name,
		Aaguid:             aaguidHex,
		FriendlyDeviceName: friendlyName,
		BackupEligible:     c.BackupEligible == 1,
		BackupState:        c.BackupState == 1,
		SignCount:          signCount32,
		CloneWarning:       c.CloneWarning == 1,
		UserVerified:       c.UserVerified == 1,
		CreatedAt:          timestamppb.New(c.CreatedAt),
		LastUsedAt:         lastUsed,
	}
}

// ensureWebAuthnHandle returns the user's WebAuthn handle, choosing and storing
// one the first time it is needed. A user who already has passkeys gets the
// legacy row-ID handle (their authenticators hold it); anyone else gets 32
// random bytes. Whichever is chosen is persisted, so it never changes later.
func (s *Service) ensureWebAuthnHandle(ctx context.Context, user db.User) ([]byte, error) {
	if len(user.WebauthnUserID) > 0 {
		return user.WebauthnUserID, nil
	}

	count, err := s.Queries.CountWebAuthnCredentialsByUser(ctx, user.ID)
	if err != nil {
		return nil, fmt.Errorf("count passkeys: %w", err)
	}
	var handle []byte
	if count > 0 {
		handle = legacyWebAuthnHandle(user.ID)
	} else {
		handle = make([]byte, WebAuthnHandleLen)
		if _, rerr := rand.Read(handle); rerr != nil {
			return nil, fmt.Errorf("generate user handle: %w", rerr)
		}
	}

	if _, serr := s.Queries.SetUserWebAuthnID(ctx, db.SetUserWebAuthnIDParams{WebauthnUserID: handle, ID: user.ID}); serr != nil {
		return nil, fmt.Errorf("store user handle: %w", serr)
	}
	// Re-read: a concurrent request may have stored a handle first.
	stored, err := s.Queries.GetUser(ctx, user.ID)
	if err != nil {
		return nil, fmt.Errorf("read user handle: %w", err)
	}
	return stored.WebauthnUserID, nil
}

// newWebAuthnUser builds the go-webauthn user for a database user, with the
// stored handle (created on first use).
func (s *Service) newWebAuthnUser(ctx context.Context, user db.User, creds []webauthn.Credential) (*WebAuthnUser, error) {
	handle, err := s.ensureWebAuthnHandle(ctx, user)
	if err != nil {
		return nil, err
	}
	return &WebAuthnUser{
		ID:          user.ID,
		Username:    user.Username,
		DisplayName: user.Username,
		Credentials: creds,
		Handle:      handle,
	}, nil
}
