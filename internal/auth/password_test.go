package auth

import (
	//nolint:gosec // HIBP test checks SHA-1 k-anonymity
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	connect "connectrpc.com/connect"
)

func TestPasswordLengthBoundaries(t *testing.T) {
	validator := NewPasswordValidator(false, slog.Default())

	tests := []struct {
		name     string
		password string
		wantErr  bool
	}{
		{name: "empty password", password: "", wantErr: true},
		{name: "11 characters", password: "1234567890a", wantErr: true},
		{name: "12 characters", password: "1234567890ab", wantErr: false},
		{name: "long passphrase", password: "correct horse battery staple passphrase 2026", wantErr: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validator.Validate(tc.password)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for password %q, got nil", tc.password)
				}
				if connect.CodeOf(err) != connect.CodeInvalidArgument {
					t.Errorf("expected CodeInvalidArgument, got %v", err)
				}
			} else {
				if err != nil {
					t.Fatalf("expected valid password, got err: %v", err)
				}
			}
		})
	}
}

func TestHIBPBreachedPasswordVerification(t *testing.T) {
	testPassword := "compromised-passphrase-test"
	//nolint:gosec // HIBP test uses SHA-1
	h := sha1.New()
	h.Write([]byte(testPassword))
	fullHash := strings.ToUpper(hex.EncodeToString(h.Sum(nil)))
	expectedPrefix := fullHash[:5]
	expectedSuffix := fullHash[5:]

	var receivedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		if strings.HasSuffix(r.URL.Path, expectedPrefix) {
			// Return matching suffix with breach count
			_, _ = fmt.Fprintf(w, "%s:42\nOTHER12345:10\n", expectedSuffix)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintln(w, "UNRELATEDSUFFIX:5")
	}))
	defer server.Close()

	validator := NewPasswordValidator(true, slog.Default())
	validator.apiURL = server.URL + "/"

	// 1. Breached password hit
	err := validator.Validate(testPassword)
	if err == nil {
		t.Fatalf("expected breached password to be rejected, got nil")
	}
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("expected CodeInvalidArgument, got %v", err)
	}
	if !strings.HasSuffix(receivedPath, expectedPrefix) {
		t.Errorf("expected request path to only contain 5-char prefix %s, got %s", expectedPrefix, receivedPath)
	}

	// 2. Clean password miss
	//nolint:gosec // Test input for clean passphrase
	cleanPassphrase := "very-clean-unique-passphrase-not-breached"
	err = validator.Validate(cleanPassphrase)
	if err != nil {
		t.Errorf("expected clean password to pass, got err: %v", err)
	}

	// 3. Network / server outage (fail-open)
	failingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer failingServer.Close()

	failingValidator := NewPasswordValidator(true, slog.Default())
	failingValidator.apiURL = failingServer.URL + "/"

	err = failingValidator.Validate(testPassword)
	if err != nil {
		t.Errorf("expected fail-open on server error, got err: %v", err)
	}
}
