package frontend

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	authclient "github.com/romaine-life/auth/client"
	"github.com/romaine-life/fzt/core"
)

// Machine authentication is shared with OpenProject, SSH and home-sync. Human
// credentials have a separate store and are never renewed by a machine key.
var readMachineToken = authclient.ReadMachineToken

func ReadAuthToken(configDir string) (string, error) { return readMachineToken(configDir) }

// SignedInEmail reads the machine's display identity without network access.
func SignedInEmail(configDir string) string {
	data, err := os.ReadFile(filepath.Join(configDir, authclient.MachineTokenFile))
	if err != nil {
		return ""
	}
	var cache authclient.Credentials
	if json.Unmarshal(bytes.TrimPrefix(data, []byte{239, 187, 191}), &cache) != nil {
		return ""
	}
	claims, err := authclient.ParseClaims(cache.Token)
	if err != nil || !authclient.IsMachine(claims) {
		return ""
	}
	return claims.Name
}

// SubFromToken selects the tree owner; machine attribution remains the token sub.
func SubFromToken(token string) (string, error) {
	claims, err := authclient.ParseClaims(token)
	if err != nil {
		return "", err
	}
	return authclient.TreeOwner(claims)
}
func EmailFromToken(token string) string {
	claims, err := authclient.ParseClaims(token)
	if err != nil {
		return ""
	}
	return claims.Email
}

func HandleValidate(s *core.State) {
	token, err := ReadAuthToken(s.ConfigDir)
	if err != nil {
		s.SetTitle(err.Error(), 2)
		return
	}
	c, _ := authclient.ParseClaims(token)
	left := time.Until(time.Unix(c.Exp, 0)).Round(time.Minute)
	s.SetTitle(fmt.Sprintf("%s · machine · token good for %s", c.Name, left), 1)
}
