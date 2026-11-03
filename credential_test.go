package frontend

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	authclient "github.com/romaine-life/auth/client"
)

func machineJWT(claims map[string]any) string {
	b, _ := json.Marshal(claims)
	return "e30." + base64.RawURLEncoding.EncodeToString(b) + ".sig"
}
func TestMachineUsesGrantedMenuAndKeepsOwnIdentity(t *testing.T) {
	token := machineJWT(map[string]any{"sub": "device:owner/mac", "name": "Machine: mac", "email": "mac@service.machine.romaine.life", "role": "service", "purpose": "machine", "device": "mac", "apps": map[string]any{"fzt": map[string]string{"owner_sub": "owner"}}})
	owner, err := SubFromToken(token)
	if err != nil || owner != "owner" {
		t.Fatalf("owner=%q error=%v", owner, err)
	}
	if EmailFromToken(token) != "mac@service.machine.romaine.life" {
		t.Fatal("machine attribution changed")
	}
	dir := t.TempDir()
	authclient.Save(filepath.Join(dir, authclient.MachineTokenFile), authclient.Credentials{Token: token})
	old := readMachineToken
	readMachineToken = func(string) (string, error) { t.Fatal("identity display tried to sign in"); return "", nil }
	defer func() { readMachineToken = old }()
	if SignedInEmail(dir) != "Machine: mac" {
		t.Fatal("machine identity is not displayed")
	}
}
func TestMachineWithoutOwnerGrantCannotSelectPersonalMenu(t *testing.T) {
	token := machineJWT(map[string]any{"sub": "device:owner/mac", "role": "service", "purpose": "machine", "device": "mac"})
	if _, err := SubFromToken(token); err == nil {
		t.Fatal("missing tree grant accepted")
	}
}
func TestLegacyHumanDeviceTokenIsNotShownAsMachine(t *testing.T) {
	dir := t.TempDir()
	token := machineJWT(map[string]any{"sub": "owner", "role": "admin", "device": "mac"})
	data, _ := json.Marshal(map[string]string{"token": token})
	os.WriteFile(filepath.Join(dir, "auth-token.json"), data, 0600)
	if SignedInEmail(dir) != "" {
		t.Fatal("legacy human identity reused")
	}
}
func TestStaleCheckReportsMachineRevocation(t *testing.T) {
	old := readMachineToken
	readMachineToken = func(string) (string, error) {
		return "", &authclient.AuthError{Kind: "revoked", Reason: "device_revoked", Detail: "this machine is revoked"}
	}
	defer func() { readMachineToken = old }()
	stale, err := CheckMenuStale(t.TempDir())
	var ae *authclient.AuthError
	if stale || !errors.As(err, &ae) || ae.Kind != "revoked" {
		t.Fatalf("stale=%v err=%v", stale, err)
	}
	if strings.Contains(err.Error(), "http") {
		t.Fatal("message repeats a transport URL")
	}
}
