package frontend

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/romaine-life/fzt/core"
)

// utf8BOM is the UTF-8 byte order mark. Token files written by the
// `authromaine` PowerShell helper (Set-Content -Encoding UTF8 on Windows
// PowerShell 5.1) are prefixed with it; encoding/json rejects the BOM, so
// strip it before unmarshalling.
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// authTokenFile holds the cached auth.romaine.life bearer token, written by
// the login flow (the `authromaine` device-flow helper). fzt-frontend only
// consumes it — it never mints tokens. Replaces the legacy HS256
// `api-jwt-signing-secret` keyring path retired in the auth.romaine.life
// migration.
const authTokenFile = "auth-token.json"

// tokenClaims is the subset of the auth.romaine.life JWT payload we read.
type tokenClaims struct {
	Sub   string `json:"sub"`
	Email string `json:"email"`
	Exp   int64  `json:"exp"`
}

// parseTokenClaims base64url-decodes the JWT payload (no signature check —
// fzt-frontend.romaine.life is the verifier; the CLI only needs the sub/exp).
func parseTokenClaims(token string) (tokenClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return tokenClaims{}, fmt.Errorf("not a JWT")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		if payload, err = base64.URLEncoding.DecodeString(parts[1]); err != nil {
			return tokenClaims{}, err
		}
	}
	var c tokenClaims
	if err := json.Unmarshal(payload, &c); err != nil {
		return tokenClaims{}, err
	}
	return c, nil
}

// ReadAuthToken returns the cached, unexpired auth.romaine.life bearer token
// from <configDir>/auth-token.json, or a user-facing error pointing at the
// login flow.
func ReadAuthToken(configDir string) (string, error) {
	path := filepath.Join(configDir, authTokenFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("not signed in — run `authromaine` to get an auth.romaine.life token")
	}
	data = bytes.TrimPrefix(data, utf8BOM)
	var doc struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(data, &doc); err != nil || doc.Token == "" {
		return "", fmt.Errorf("auth token file is malformed — run `authromaine` to refresh")
	}
	c, err := parseTokenClaims(doc.Token)
	if err != nil {
		return "", fmt.Errorf("auth token is unreadable — run `authromaine` to refresh")
	}
	if c.Exp != 0 && time.Now().Unix() >= c.Exp {
		return "", fmt.Errorf("auth token expired — run `authromaine` to refresh")
	}
	return doc.Token, nil
}

// SubFromToken extracts the `sub` claim — the opaque auth.romaine.life user id
// that keys the caller's trees (<sub>-menu, <sub>-bookmarks).
func SubFromToken(token string) (string, error) {
	c, err := parseTokenClaims(token)
	if err != nil {
		return "", err
	}
	if c.Sub == "" {
		return "", fmt.Errorf("token has no sub claim")
	}
	return c.Sub, nil
}

// EmailFromToken extracts the `email` claim for the whoami display.
func EmailFromToken(token string) string {
	c, err := parseTokenClaims(token)
	if err != nil {
		return ""
	}
	return c.Email
}

// HandleValidate checks that a usable auth.romaine.life token is present and
// posts the result to the title bar.
func HandleValidate(s *core.State) {
	if _, err := ReadAuthToken(s.ConfigDir); err != nil {
		s.SetTitle(err.Error(), 2)
		return
	}
	s.SetTitle("auth token OK", 1)
}
