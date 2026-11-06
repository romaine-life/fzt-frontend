package frontend

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	authclient "github.com/romaine-life/auth/client"
	"github.com/romaine-life/fzt/core"
)

func machineJWT(claims map[string]any) string {
	b, _ := json.Marshal(claims)
	return "e30." + base64.RawURLEncoding.EncodeToString(b) + ".sig"
}
func homeJWT(claims map[string]any) string {
	claims["iss"] = authclient.HomeIssuer
	claims["aud"] = "fzt-frontend"
	claims["sub"] = "2f1d58de-entity"
	return machineJWT(claims)
}
func TestHomeTokenSelectsTheGrantedMenu(t *testing.T) {
	token := homeJWT(map[string]any{"device": "mac", "apps": map[string]any{"fzt": map[string]string{"owner_sub": "owner"}}})
	owner, err := SubFromToken(token)
	if err != nil || owner != "owner" {
		t.Fatalf("owner=%q error=%v", owner, err)
	}
}
func TestSignInAsksOpenBaoForThisServicesToken(t *testing.T) {
	old := readHomeToken
	readHomeToken = func() (string, error) { return "home-token", nil }
	defer func() { readHomeToken = old }()
	if token, err := ReadAuthToken(); err != nil || token != "home-token" {
		t.Fatalf("token=%q error=%v", token, err)
	}
}
func TestIdentityDisplayNamesTheEnrolledMachineWithoutSigningIn(t *testing.T) {
	oldRead, oldRegistration := readHomeToken, readRegistration
	defer func() { readHomeToken, readRegistration = oldRead, oldRegistration }()
	readHomeToken = func() (string, error) { t.Fatal("identity display tried to sign in"); return "", nil }
	readRegistration = func() (authclient.Registration, error) { return authclient.Registration{Name: "mac"}, nil }
	if got := SignedInEmail(t.TempDir()); got != "Machine: mac" {
		t.Fatalf("display=%q", got)
	}
	readRegistration = func() (authclient.Registration, error) {
		return authclient.Registration{}, &authclient.AuthError{Kind: "unregistered", Detail: "this machine is not enrolled"}
	}
	if got := SignedInEmail(t.TempDir()); got != "" {
		t.Fatalf("an unenrolled machine displayed %q", got)
	}
}
func TestValidateReportsTheHomeTokensDevice(t *testing.T) {
	old := readHomeToken
	exp := time.Now().Add(5 * time.Minute).Unix()
	readHomeToken = func() (string, error) { return homeJWT(map[string]any{"device": "mac", "exp": exp}), nil }
	defer func() { readHomeToken = old }()
	s := &core.State{}
	HandleValidate(s)
	if !strings.HasPrefix(s.TitleOverride, "mac · machine · token good for") {
		t.Fatalf("title=%q", s.TitleOverride)
	}
}
func TestHomeTokenWithoutOwnerGrantCannotSelectPersonalMenu(t *testing.T) {
	for _, token := range []string{
		homeJWT(map[string]any{"device": "mac"}),
		homeJWT(map[string]any{"device": "mac", "apps": map[string]any{"fzt": map[string]string{"owner_sub": ""}}}),
	} {
		if _, err := SubFromToken(token); err == nil {
			t.Fatal("missing tree grant accepted")
		}
	}
}
func TestStaleCheckReportsMachineRevocation(t *testing.T) {
	old := readHomeToken
	readHomeToken = func() (string, error) {
		return "", &authclient.AuthError{Kind: "refused", Detail: "OpenBao refused this machine (HTTP 403, permission denied)"}
	}
	defer func() { readHomeToken = old }()
	stale, err := CheckMenuStale(t.TempDir())
	var ae *authclient.AuthError
	if stale || !errors.As(err, &ae) || ae.Kind != "refused" {
		t.Fatalf("stale=%v err=%v", stale, err)
	}
	if strings.Contains(err.Error(), "http") {
		t.Fatal("message repeats a transport URL")
	}
}
