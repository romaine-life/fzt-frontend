package frontend

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// A home PC signs in from its Windows login (ROMAINE-46): it holds a key in its TPM
// under the Windows user, registered once with auth.romaine.life, and trades a short
// assertion signed with that key for the same 24h user token a browser sign-in gives.
// The key, its name and the registration file are shared with home-sync (BAO-2), so the
// menu and OpenBao use one identity per PC.

// deviceKeyName is the CNG key's name in the Microsoft Platform Crypto Provider.
const deviceKeyName = "romaine-device"

// deviceRegistrationFile, in the Windows user's home, records the registration:
// {"device_id", "name", "token_url"}.
const deviceRegistrationFile = ".romaine-device.json"

// deviceTokenAudience is the assertion's required `aud`, fixed by auth.romaine.life
// whatever token_url the registration names.
const deviceTokenAudience = "https://auth.romaine.life/api/auth/device/token"

// assertionLifetime is well inside auth.romaine.life's 300 s limit, allowing its 30 s
// clock tolerance on either side.
const assertionLifetime = 2 * time.Minute

type deviceRegistration struct {
	DeviceID string `json:"device_id"`
	Name     string `json:"name"`
	TokenURL string `json:"token_url"`
}

// deviceSigner signs a SHA-256 digest with the device key, returning the 64-byte
// r||s form that JWS ES256 uses.
type deviceSigner interface {
	SignDigest(digest []byte) ([]byte, error)
	Close()
}

// Seams for tests; the defaults are the real registration file and TPM key.
var (
	deviceRegistrationPath = func() (string, error) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, deviceRegistrationFile), nil
	}
	openDeviceKey = openPlatformKey
	deviceClient  = &http.Client{Timeout: 10 * time.Second}
	deviceNow     = time.Now
)

// AuthErrorKind says why the menu has no usable token, so the screen can say which.
type AuthErrorKind int

const (
	// AuthNotRegistered: this PC has no registration, or auth.romaine.life doesn't know it.
	AuthNotRegistered AuthErrorKind = iota
	// AuthRevoked: the registration was revoked at auth.romaine.life/admin.
	AuthRevoked
	// AuthKeyUnavailable: the TPM key can't be opened or won't sign.
	AuthKeyUnavailable
	// AuthUnreachable: auth.romaine.life didn't answer, or answered with a server error.
	AuthUnreachable
	// AuthRefused: auth.romaine.life answered and refused for another reason.
	AuthRefused
)

// AuthError is a failed sign-in, worded for the person at the menu.
type AuthError struct {
	Kind   AuthErrorKind
	Reason string // auth.romaine.life's reason code, when it gave one
	Detail string
}

func (e *AuthError) Error() string {
	switch e.Kind {
	case AuthNotRegistered:
		return "signed out: this PC isn't registered with auth.romaine.life (" + e.Detail + ")"
	case AuthRevoked:
		return "signed out: this PC's registration was revoked at auth.romaine.life/admin"
	case AuthKeyUnavailable:
		return "signed out: this PC's sign-in key can't be used (" + e.Detail + ")"
	case AuthUnreachable:
		return "signed out: can't reach auth.romaine.life to renew sign-in (" + e.Detail + ")"
	default:
		return "signed out: auth.romaine.life refused this PC's sign-in (" + e.Detail + ")"
	}
}

func readDeviceRegistration() (deviceRegistration, error) {
	path, err := deviceRegistrationPath()
	if err != nil {
		return deviceRegistration{}, &AuthError{Kind: AuthNotRegistered, Detail: err.Error()}
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return deviceRegistration{}, &AuthError{Kind: AuthNotRegistered, Detail: "no " + path}
	}
	if err != nil {
		return deviceRegistration{}, &AuthError{Kind: AuthNotRegistered, Detail: err.Error()}
	}
	var reg deviceRegistration
	if err := json.Unmarshal(bytes.TrimPrefix(data, utf8BOM), &reg); err != nil || reg.DeviceID == "" || reg.TokenURL == "" {
		return deviceRegistration{}, &AuthError{Kind: AuthNotRegistered, Detail: path + " is malformed"}
	}
	return reg, nil
}

// signDeviceAssertion builds the ES256 JWT auth.romaine.life accepts from a device:
// kid = iss = sub = the device id, aud fixed, a short lifetime and a fresh jti.
func signDeviceAssertion(reg deviceRegistration, signer deviceSigner, now time.Time) (string, error) {
	jti := make([]byte, 16)
	if _, err := rand.Read(jti); err != nil {
		return "", err
	}
	header, _ := json.Marshal(map[string]string{"alg": "ES256", "typ": "JWT", "kid": reg.DeviceID})
	claims, _ := json.Marshal(map[string]any{
		"iss": reg.DeviceID,
		"sub": reg.DeviceID,
		"aud": deviceTokenAudience,
		"iat": now.Unix(),
		"exp": now.Add(assertionLifetime).Unix(),
		"jti": base64.RawURLEncoding.EncodeToString(jti),
	})
	input := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(claims)
	digest := sha256.Sum256([]byte(input))
	sig, err := signer.SignDigest(digest[:])
	if err != nil {
		return "", err
	}
	if len(sig) != 64 {
		return "", fmt.Errorf("signature is %d bytes, want 64", len(sig))
	}
	return input + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

// renewAuthToken signs in with the device key and writes the new token to
// <configDir>/auth-token.json.
func renewAuthToken(configDir string) (string, error) {
	reg, err := readDeviceRegistration()
	if err != nil {
		return "", err
	}
	signer, err := openDeviceKey()
	if err != nil {
		return "", &AuthError{Kind: AuthKeyUnavailable, Detail: err.Error()}
	}
	defer signer.Close()
	assertion, err := signDeviceAssertion(reg, signer, deviceNow())
	if err != nil {
		return "", &AuthError{Kind: AuthKeyUnavailable, Detail: err.Error()}
	}

	body, _ := json.Marshal(map[string]string{"assertion": assertion})
	resp, err := deviceClient.Post(reg.TokenURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return "", &AuthError{Kind: AuthUnreachable, Detail: err.Error()}
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode != http.StatusOK {
		var refusal struct {
			Error  string `json:"error"`
			Reason string `json:"reason"`
		}
		json.Unmarshal(data, &refusal)
		return "", refusalError(resp.StatusCode, refusal.Reason, refusal.Error)
	}
	var ok struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(data, &ok); err != nil || ok.Token == "" {
		return "", &AuthError{Kind: AuthUnreachable, Detail: "auth.romaine.life's answer had no token"}
	}
	if err := writeAuthToken(configDir, ok.Token); err != nil {
		return "", err
	}
	return ok.Token, nil
}

func refusalError(status int, reason, message string) *AuthError {
	detail := message
	if detail == "" {
		detail = fmt.Sprintf("HTTP %d", status)
	}
	switch {
	case reason == "device_not_registered":
		return &AuthError{Kind: AuthNotRegistered, Reason: reason, Detail: "auth.romaine.life doesn't know this PC's key"}
	case reason == "device_revoked":
		return &AuthError{Kind: AuthRevoked, Reason: reason, Detail: detail}
	case status >= 500:
		return &AuthError{Kind: AuthUnreachable, Reason: reason, Detail: detail}
	default:
		if reason != "" {
			detail = reason + ": " + detail
		}
		return &AuthError{Kind: AuthRefused, Reason: reason, Detail: detail}
	}
}

// writeAuthToken replaces the token file in one rename, so a reader never sees half a
// file.
func writeAuthToken(configDir, token string) error {
	data, _ := json.Marshal(map[string]string{"token": token})
	tmp, err := os.CreateTemp(configDir, authTokenFile+".*")
	if err != nil {
		return fmt.Errorf("saving the auth token: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return fmt.Errorf("saving the auth token: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return fmt.Errorf("saving the auth token: %w", err)
	}
	if err := os.Rename(tmp.Name(), filepath.Join(configDir, authTokenFile)); err != nil {
		os.Remove(tmp.Name())
		return fmt.Errorf("saving the auth token: %w", err)
	}
	return nil
}
