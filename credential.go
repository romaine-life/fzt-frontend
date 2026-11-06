package frontend

import (
	"context"
	"fmt"
	"time"

	authclient "github.com/romaine-life/auth/client"
	"github.com/romaine-life/fzt/core"
)

// A home machine signs in to the tree store with a token OpenBao signs for its device
// key. Human credentials have a separate store and are never renewed by a machine key.
var readHomeToken = func() (string, error) {
	credentials, err := authclient.HomeToken(context.Background(), "fzt-frontend")
	return credentials.Token, err
}

var readRegistration = authclient.ReadRegistration

func ReadAuthToken() (string, error) { return readHomeToken() }

// SignedInEmail names the enrolled machine without network access.
func SignedInEmail(string) string {
	registration, err := readRegistration()
	if err != nil || registration.Name == "" {
		return ""
	}
	return "Machine: " + registration.Name
}

// SubFromToken selects the tree owner; machine attribution remains the token sub.
func SubFromToken(token string) (string, error) {
	claims, err := authclient.ParseClaims(token)
	if err != nil {
		return "", err
	}
	return authclient.TreeOwner(claims)
}

func HandleValidate(s *core.State) {
	token, err := ReadAuthToken()
	if err != nil {
		s.SetTitle(err.Error(), 2)
		return
	}
	c, _ := authclient.ParseClaims(token)
	left := time.Until(time.Unix(c.Exp, 0)).Round(time.Minute)
	s.SetTitle(fmt.Sprintf("%s · machine · token good for %s", c.Device, left), 1)
}
