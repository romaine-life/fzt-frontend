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

// utf8BOM is the UTF-8 byte order mark. Token and registration files written by
// Windows PowerShell 5.1 (Set-Content -Encoding UTF8) start with it; encoding/json
// rejects the BOM, so strip it before unmarshalling.
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// authTokenFile holds the cached auth.romaine.life bearer token, {"token": "<jwt>"}.
// On a registered home PC this package renews it from the device key (device.go);
// a browser sign-in (`devctl auth`) also writes it.
const authTokenFile = "auth-token.json"

// renewMargin renews a token this close to expiry, so a request never carries one
// that lapses on the way.
const renewMargin = 5 * time.Minute

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

// readTokenFile returns the cached token and its claims, expired or not.
func readTokenFile(configDir string) (string, tokenClaims, error) {
	data, err := os.ReadFile(filepath.Join(configDir, authTokenFile))
	if err != nil {
		return "", tokenClaims{}, err
	}
	var doc struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(bytes.TrimPrefix(data, utf8BOM), &doc); err != nil || doc.Token == "" {
		return "", tokenClaims{}, fmt.Errorf("auth token file is malformed")
	}
	c, err := parseTokenClaims(doc.Token)
	if err != nil {
		return "", tokenClaims{}, fmt.Errorf("auth token is unreadable")
	}
	return doc.Token, c, nil
}

// ReadAuthToken returns a usable auth.romaine.life bearer token. The cached one is
// used while it has more than renewMargin left; otherwise the PC signs in again with
// its device key. A failure is an *AuthError saying why.
func ReadAuthToken(configDir string) (string, error) {
	token, c, err := readTokenFile(configDir)
	if err == nil && (c.Exp == 0 || deviceNow().Add(renewMargin).Unix() < c.Exp) {
		return token, nil
	}
	return renewAuthToken(configDir)
}

// SignedInEmail is the cached token's email claim, read without renewing or touching
// the network, for showing who the menu belongs to. Empty when there is no token.
func SignedInEmail(configDir string) string {
	_, c, err := readTokenFile(configDir)
	if err != nil {
		return ""
	}
	return c.Email
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

// HandleValidate signs in (renewing from the device key if needed) and posts the
// result to the title bar.
func HandleValidate(s *core.State) {
	token, err := ReadAuthToken(s.ConfigDir)
	if err != nil {
		s.SetTitle(err.Error(), 2)
		return
	}
	c, _ := parseTokenClaims(token)
	left := time.Until(time.Unix(c.Exp, 0)).Round(time.Minute)
	s.SetTitle(fmt.Sprintf("signed in as %s, token good for %s", c.Email, left), 1)
}
