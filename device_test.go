package frontend

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// softSigner stands in for the TPM key with a software P-256 key.
type softSigner struct{ key *ecdsa.PrivateKey }

func (s softSigner) SignDigest(d []byte) ([]byte, error) {
	r, sv, err := ecdsa.Sign(rand.Reader, s.key, d)
	if err != nil {
		return nil, err
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	sv.FillBytes(sig[32:])
	return sig, nil
}
func (softSigner) Close() {}

func fakeJWT(claims map[string]any) string {
	p, _ := json.Marshal(claims)
	return "e30." + base64.RawURLEncoding.EncodeToString(p) + ".sig"
}

// deviceFixture points the seams at a temp registration, a software key and a fake
// token endpoint whose answer the test sets.
type deviceFixture struct {
	configDir string
	key       *ecdsa.PrivateKey
	hits      int
	status    int
	answer    string
	gotBody   map[string]string
}

func newDeviceFixture(t *testing.T, register bool) *deviceFixture {
	t.Helper()
	f := &deviceFixture{configDir: t.TempDir(), status: 200}
	f.key, _ = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.hits++
		json.NewDecoder(r.Body).Decode(&f.gotBody)
		w.WriteHeader(f.status)
		w.Write([]byte(f.answer))
	}))
	t.Cleanup(srv.Close)

	home := t.TempDir()
	regPath := filepath.Join(home, deviceRegistrationFile)
	if register {
		reg, _ := json.Marshal(deviceRegistration{DeviceID: "dev_test", Name: "testpc", TokenURL: srv.URL})
		os.WriteFile(regPath, append(append([]byte{}, utf8BOM...), reg...), 0o600)
	}
	oldPath, oldOpen := deviceRegistrationPath, openDeviceKey
	deviceRegistrationPath = func() (string, error) { return regPath, nil }
	openDeviceKey = func() (deviceSigner, error) { return softSigner{f.key}, nil }
	t.Cleanup(func() { deviceRegistrationPath, openDeviceKey = oldPath, oldOpen })
	return f
}

func (f *deviceFixture) cacheToken(t *testing.T, exp time.Time) string {
	t.Helper()
	tok := fakeJWT(map[string]any{"sub": "u1", "email": "n@romaine.life", "exp": exp.Unix()})
	if err := writeAuthToken(f.configDir, tok); err != nil {
		t.Fatal(err)
	}
	return tok
}

func TestAssertionIsWhatAuthVerifies(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	now := time.Unix(1_800_000_000, 0)
	reg := deviceRegistration{DeviceID: "dev_abc", TokenURL: "https://example.invalid"}
	jwt, err := signDeviceAssertion(reg, softSigner{key}, now)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(jwt, ".")
	if len(parts) != 3 {
		t.Fatalf("not a JWS: %q", jwt)
	}
	var header map[string]string
	var claims map[string]any
	hb, _ := base64.RawURLEncoding.DecodeString(parts[0])
	cb, _ := base64.RawURLEncoding.DecodeString(parts[1])
	json.Unmarshal(hb, &header)
	json.Unmarshal(cb, &claims)

	if header["alg"] != "ES256" || header["kid"] != "dev_abc" {
		t.Errorf("header = %v", header)
	}
	if claims["iss"] != "dev_abc" || claims["sub"] != "dev_abc" {
		t.Errorf("iss/sub = %v/%v", claims["iss"], claims["sub"])
	}
	if claims["aud"] != "https://auth.romaine.life/api/auth/device/token" {
		t.Errorf("aud = %v", claims["aud"])
	}
	if life := claims["exp"].(float64) - claims["iat"].(float64); life <= 0 || life > 300-30 {
		t.Errorf("lifetime %vs is outside auth.romaine.life's limit", life)
	}
	if jti, _ := claims["jti"].(string); len(jti) < 16 {
		t.Errorf("jti %q is not fresh enough", jti)
	}

	sig, _ := base64.RawURLEncoding.DecodeString(parts[2])
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	r, s := new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])
	if !ecdsa.Verify(&key.PublicKey, digest[:], r, s) {
		t.Error("signature does not verify under the device's public key")
	}

	again, _ := signDeviceAssertion(reg, softSigner{key}, now)
	if strings.Split(again, ".")[1] == parts[1] {
		t.Error("two assertions share a jti")
	}
}

func TestValidCachedTokenNeedsNoSignIn(t *testing.T) {
	f := newDeviceFixture(t, true)
	want := f.cacheToken(t, time.Now().Add(time.Hour))
	got, err := ReadAuthToken(f.configDir)
	if err != nil || got != want {
		t.Fatalf("got %q, %v", got, err)
	}
	if f.hits != 0 {
		t.Errorf("signed in %d times with a valid token cached", f.hits)
	}
}

func TestExpiredOrExpiringTokenRenews(t *testing.T) {
	for name, exp := range map[string]time.Time{
		"expired":  time.Now().Add(-7 * time.Hour),
		"expiring": time.Now().Add(renewMargin / 2),
	} {
		t.Run(name, func(t *testing.T) {
			f := newDeviceFixture(t, true)
			f.cacheToken(t, exp)
			fresh := fakeJWT(map[string]any{"sub": "u1", "email": "n@romaine.life", "exp": time.Now().Add(24 * time.Hour).Unix()})
			f.answer = `{"token":"` + fresh + `","expires_at":1,"expires_in_hours":24}`

			got, err := ReadAuthToken(f.configDir)
			if err != nil || got != fresh {
				t.Fatalf("got %q, %v", got, err)
			}
			if f.gotBody["assertion"] == "" || f.gotBody["audience"] != "" {
				t.Errorf("request body = %v; want an assertion and no audience", f.gotBody)
			}
			cached, _, err := readTokenFile(f.configDir)
			if err != nil || cached != fresh {
				t.Errorf("token file holds %q, %v", cached, err)
			}
		})
	}
}

func TestNoTokenFileSignsIn(t *testing.T) {
	f := newDeviceFixture(t, true)
	f.answer = `{"token":"` + fakeJWT(map[string]any{"sub": "u1", "exp": time.Now().Add(time.Hour).Unix()}) + `"}`
	if _, err := ReadAuthToken(f.configDir); err != nil {
		t.Fatal(err)
	}
}

func TestSignInFailuresSayWhy(t *testing.T) {
	cases := []struct {
		name   string
		status int
		answer string
		want   AuthErrorKind
	}{
		{"unknown key", 401, `{"error":"this device isn't registered","reason":"device_not_registered"}`, AuthNotRegistered},
		{"revoked", 401, `{"error":"revoked","reason":"device_revoked"}`, AuthRevoked},
		{"replayed", 401, `{"error":"seen","reason":"assertion_replayed"}`, AuthRefused},
		{"not approved", 403, `{"error":"not approved","reason":"account_not_approved"}`, AuthRefused},
		{"server error", 500, `{"error":"boom","reason":"internal"}`, AuthUnreachable},
		{"proxy page", 502, `<html>bad gateway</html>`, AuthUnreachable},
		{"no token", 200, `{}`, AuthUnreachable},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newDeviceFixture(t, true)
			f.status, f.answer = c.status, c.answer
			_, err := ReadAuthToken(f.configDir)
			var ae *AuthError
			if !errors.As(err, &ae) || ae.Kind != c.want {
				t.Fatalf("err = %v; want kind %d", err, c.want)
			}
			if !strings.HasPrefix(err.Error(), "signed out: ") {
				t.Errorf("message %q doesn't lead with the state", err)
			}
		})
	}
}

func TestUnregisteredPC(t *testing.T) {
	f := newDeviceFixture(t, false)
	f.cacheToken(t, time.Now().Add(-time.Minute))
	_, err := ReadAuthToken(f.configDir)
	var ae *AuthError
	if !errors.As(err, &ae) || ae.Kind != AuthNotRegistered {
		t.Fatalf("err = %v", err)
	}
	if f.hits != 0 {
		t.Error("called auth.romaine.life with no registration")
	}
}

func TestUnusableKey(t *testing.T) {
	f := newDeviceFixture(t, true)
	openDeviceKey = func() (deviceSigner, error) { return nil, errors.New("NTE_BAD_KEYSET") }
	_, err := ReadAuthToken(f.configDir)
	var ae *AuthError
	if !errors.As(err, &ae) || ae.Kind != AuthKeyUnavailable {
		t.Fatalf("err = %v", err)
	}
}

func TestUnreachable(t *testing.T) {
	f := newDeviceFixture(t, true)
	path, _ := deviceRegistrationPath()
	reg, _ := json.Marshal(deviceRegistration{DeviceID: "dev_test", TokenURL: "http://127.0.0.1:1/token"})
	os.WriteFile(path, reg, 0o600)
	_, err := ReadAuthToken(f.configDir)
	var ae *AuthError
	if !errors.As(err, &ae) || ae.Kind != AuthUnreachable {
		t.Fatalf("err = %v", err)
	}
}

func TestStaleCheckReportsSignInFailure(t *testing.T) {
	f := newDeviceFixture(t, true)
	f.status, f.answer = 401, `{"reason":"device_revoked"}`
	stale, err := CheckMenuStale(f.configDir)
	var ae *AuthError
	if stale || !errors.As(err, &ae) || ae.Kind != AuthRevoked {
		t.Fatalf("stale=%v err=%v; want the revocation reported", stale, err)
	}
}

func TestSignedInEmailNeverSignsIn(t *testing.T) {
	f := newDeviceFixture(t, true)
	f.cacheToken(t, time.Now().Add(-time.Hour))
	if got := SignedInEmail(f.configDir); got != "n@romaine.life" {
		t.Errorf("email = %q", got)
	}
	if f.hits != 0 {
		t.Error("SignedInEmail touched the network")
	}
}
