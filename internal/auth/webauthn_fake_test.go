package auth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"testing"
)

// A minimal synthetic WebAuthn authenticator for ceremony-level tests. It
// speaks the real wire format — CBOR attestation objects, COSE ES256 keys,
// signed assertions — so go-webauthn's full verification paths run against
// dmanager's service exactly as they would against a browser. No external
// CBOR dependency: the encoder below covers only the shapes WebAuthn needs
// (uints, negative ints, byte strings, text strings, maps), deterministic
// and canonical.

const (
	flagUP byte = 0x01 // user present
	flagUV byte = 0x04 // user verified
	flagBE byte = 0x08 // backup eligible
	flagBS byte = 0x10 // backup state
	flagAT byte = 0x40 // attested credential data included
	flagED byte = 0x80 // extension data included
)

func cborHead(major byte, n uint64) []byte {
	switch {
	case n < 24:
		return []byte{major<<5 | byte(n)}
	case n <= 0xff:
		return []byte{major<<5 | 24, byte(n)}
	case n <= 0xffff:
		return []byte{major<<5 | 25, byte(n >> 8), byte(n)}
	default:
		return []byte{major<<5 | 26, byte(n >> 24), byte(n >> 16), byte(n >> 8), byte(n)}
	}
}

func cborUint(v uint64) []byte  { return cborHead(0, v) }
func cborNeg(v int64) []byte    { return cborHead(1, uint64(-1-v)) } // v < 0
func cborText(s string) []byte  { return append(cborHead(3, uint64(len(s))), s...) }
func cborBytes(b []byte) []byte { return append(cborHead(2, uint64(len(b))), b...) }
func cborMap(pairs ...[]byte) []byte {
	out := cborHead(5, uint64(len(pairs)/2)) // pairs are key+value argument chunks
	for _, p := range pairs {
		out = append(out, p...)
	}
	return out
}

func b64url(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

type fakeAuthenticator struct {
	t      *testing.T
	rpID   string
	origin string
	key    *ecdsa.PrivateKey
	credID []byte
	aaguid [16]byte
}

func newFakeAuthenticator(t *testing.T, rpID, origin string) *fakeAuthenticator {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("fake authenticator keygen: %v", err)
	}
	credID := make([]byte, 32)
	if _, err := rand.Read(credID); err != nil {
		t.Fatalf("fake authenticator credential id: %v", err)
	}
	return &fakeAuthenticator{t: t, rpID: rpID, origin: origin, key: key, credID: credID}
}

// shadowOf returns a copy bound to different RP parameters while reusing the
// same credential key material — for rpIdHash / origin mismatch red-team
// assertions where the client presents a real signature over wrong context.
func (f *fakeAuthenticator) shadowOf(rpID, origin string) *fakeAuthenticator {
	cpy := *f
	cpy.rpID = rpID
	cpy.origin = origin
	return &cpy
}

func (f *fakeAuthenticator) cosePublicKey() []byte {
	x := make([]byte, 32)
	y := make([]byte, 32)
	f.key.PublicKey.X.FillBytes(x)
	f.key.PublicKey.Y.FillBytes(y)
	return cborMap(
		cborUint(1), cborUint(2), // kty: EC2
		cborUint(3), cborNeg(-7), // alg: ES256
		cborNeg(-1), cborUint(1), // crv: P-256
		cborNeg(-2), cborBytes(x), // x coordinate
		cborNeg(-3), cborBytes(y), // y coordinate
	)
}

func (f *fakeAuthenticator) rpIdHash() []byte {
	sum := sha256.Sum256([]byte(f.rpID))
	return sum[:]
}

func (f *fakeAuthenticator) authData(flags byte, counter uint32, withCredential bool) []byte {
	buf := f.rpIdHash()
	buf = append(buf, flags)
	buf = binary.BigEndian.AppendUint32(buf, counter)
	if withCredential {
		buf = append(buf, f.aaguid[:]...)
		buf = binary.BigEndian.AppendUint16(buf, uint16(len(f.credID)))
		buf = append(buf, f.credID...)
		buf = append(buf, f.cosePublicKey()...)
	}
	return buf
}

func (f *fakeAuthenticator) attestationObject(flags byte, counter uint32) []byte {
	return cborMap(
		cborText("fmt"), cborText("none"),
		cborText("attStmt"), cborMap(),
		cborText("authData"), cborBytes(f.authData(flags, counter, true)),
	)
}

func (f *fakeAuthenticator) clientData(typ, challenge, origin string) []byte {
	f.t.Helper()
	b, err := json.Marshal(map[string]any{
		"type":        typ,
		"challenge":   challenge,
		"origin":      origin,
		"crossOrigin": false,
	})
	if err != nil {
		f.t.Fatalf("fake authenticator clientDataJSON: %v", err)
	}
	return b
}

// RegistrationResponseJSON builds the browser's JSON registration reply.
// flags control UP/UV/BE/BS as presented by the client; counter seeds the
// authenticator's signature counter in the attestation object.
func (f *fakeAuthenticator) RegistrationResponseJSON(challenge string, clientExt map[string]any, flags byte, counter uint32) string {
	f.t.Helper()
	if clientExt == nil {
		clientExt = map[string]any{}
	}
	b, err := json.Marshal(map[string]any{
		"id":    b64url(f.credID),
		"rawId": b64url(f.credID),
		"type":  "public-key",
		"response": map[string]any{
			"attestationObject": b64url(f.attestationObject(flags, counter)),
			"clientDataJSON":    b64url(f.clientData("webauthn.create", challenge, f.origin)),
			"transports":        []string{"internal"},
		},
		"clientExtensionResults": clientExt,
	})
	if err != nil {
		f.t.Fatalf("fake authenticator registration response: %v", err)
	}
	return string(b)
}

// AssertionResponseJSON builds the browser's JSON assertion reply: signs
// authenticatorData || SHA-256(clientDataJSON) with the credential key, the
// real WebAuthn assertion signature scheme. origin overrides the origin in
// clientDataJSON (defaulting to the authenticator's own), enabling mismatch
// red-team cases while keeping the signature internally consistent.
func (f *fakeAuthenticator) AssertionResponseJSON(challenge string, userHandle []byte, clientExt map[string]any, flags byte, counter uint32, origin string) string {
	f.t.Helper()
	if origin == "" {
		origin = f.origin
	}
	if clientExt == nil {
		clientExt = map[string]any{}
	}
	ad := f.authData(flags, counter, false)
	cd := f.clientData("webauthn.get", challenge, origin)
	cdHash := sha256.Sum256(cd)
	sigInput := append(append([]byte{}, ad...), cdHash[:]...)
	digest := sha256.Sum256(sigInput)
	sig, err := ecdsa.SignASN1(rand.Reader, f.key, digest[:])
	if err != nil {
		f.t.Fatalf("fake authenticator assertion signature: %v", err)
	}
	b, err := json.Marshal(map[string]any{
		"id":    b64url(f.credID),
		"rawId": b64url(f.credID),
		"type":  "public-key",
		"response": map[string]any{
			"authenticatorData": b64url(ad),
			"clientDataJSON":    b64url(cd),
			"signature":         b64url(sig),
			"userHandle":        b64url(userHandle),
		},
		"clientExtensionResults": clientExt,
	})
	if err != nil {
		f.t.Fatalf("fake authenticator assertion response: %v", err)
	}
	return string(b)
}
