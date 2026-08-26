package auth

import (
	"bufio"
	//nolint:gosec // HIBP API requires SHA-1 hash for k-anonymity lookups
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	connect "connectrpc.com/connect"
)

const (
	MinPasswordLength = 12
	hibpRangeAPIURL   = "https://api.pwnedpasswords.com/range/"
)

type PasswordValidator struct {
	checkBreached bool
	httpClient    *http.Client
	logger        *slog.Logger
	apiURL        string
}

func NewPasswordValidator(checkBreached bool, logger *slog.Logger) *PasswordValidator {
	return &PasswordValidator{
		checkBreached: checkBreached,
		httpClient: &http.Client{
			Timeout: 3 * time.Second,
		},
		logger: logger,
		apiURL: hibpRangeAPIURL,
	}
}

// Validate verifies that the password meets minimum length policy (>=12) and optionally checks for breaches via HIBP.
func (v *PasswordValidator) Validate(password string) error {
	if len(password) < MinPasswordLength {
		return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("password must be at least %d characters", MinPasswordLength))
	}

	if !v.checkBreached {
		return nil
	}

	return v.checkHIBP(password)
}

func (v *PasswordValidator) checkHIBP(password string) error {
	//nolint:gosec // HIBP API requires SHA-1 for k-anonymity
	h := sha1.New()
	h.Write([]byte(password))
	sha1Hex := strings.ToUpper(hex.EncodeToString(h.Sum(nil)))

	if len(sha1Hex) != 40 {
		return nil
	}

	prefix := sha1Hex[:5]
	suffix := sha1Hex[5:]

	url := v.apiURL + prefix
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		if v.logger != nil {
			v.logger.Warn("Failed to create HIBP request, allowing password", "error", err)
		}
		return nil
	}
	req.Header.Set("User-Agent", "dmanager-auth-validator")

	resp, err := v.httpClient.Do(req)
	if err != nil {
		if v.logger != nil {
			v.logger.Warn("HIBP API request failed, failing open", "error", err)
		}
		return nil
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		if v.logger != nil {
			v.logger.Warn("HIBP API returned non-200 status, failing open", "status", resp.StatusCode)
		}
		return nil
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parts := strings.Split(line, ":")
		if len(parts) >= 1 && strings.EqualFold(parts[0], suffix) {
			return connect.NewError(connect.CodeInvalidArgument, errors.New("the chosen password appears in a known data breach; please choose a different password"))
		}
	}

	if err := scanner.Err(); err != nil && v.logger != nil {
		v.logger.Warn("Error reading HIBP API response, failing open", "error", err)
	}

	return nil
}
