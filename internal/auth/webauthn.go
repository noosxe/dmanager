package auth

import (
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
}

func (u *WebAuthnUser) WebAuthnID() []byte {
	b := make([]byte, 8)
	if u.ID > 0 {
		binary.BigEndian.PutUint64(b, uint64(u.ID))
	}
	return b
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
			UserVerified:   true,
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
		CreatedAt:          timestamppb.New(c.CreatedAt),
		LastUsedAt:         lastUsed,
	}
}
