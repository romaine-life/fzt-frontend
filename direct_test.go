package frontend

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeStore is Forgejo's contents API for one tree file.
type fakeStore struct {
	file  []byte
	sha   string
	auth  string
	saves int
}

func (f *fakeStore) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.auth = r.Header.Get("Authorization")
	if !strings.HasSuffix(r.URL.Path, "/nelson-ea-menu.json") {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case "GET":
		json.NewEncoder(w).Encode(map[string]string{"content": base64.StdEncoding.EncodeToString(f.file), "sha": f.sha})
	case "PUT":
		var req map[string]string
		json.NewDecoder(r.Body).Decode(&req)
		if req["sha"] != f.sha {
			w.WriteHeader(http.StatusConflict)
			return
		}
		f.file, _ = base64.StdEncoding.DecodeString(req["content"])
		f.sha += "x"
		f.saves++
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("{}"))
	}
}

func directSetup(t *testing.T) (string, *fakeStore) {
	t.Helper()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "forgejo-token"), []byte("tok\n"), 0o600)
	os.WriteFile(filepath.Join(dir, ".identity"), []byte("nelson-ea"), 0o644)
	f := &fakeStore{file: []byte(`{"version":4,"updatedAt":"2026-04-23T00:33:10.061Z","tree":[{"name":"git"}]}`), sha: "a"}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	old := treeStore
	treeStore = srv.URL + "/trees"
	t.Cleanup(func() { treeStore = old })
	return dir, f
}

func TestAMachineWithAForgejoTokenReadsItsMenuFromTheStore(t *testing.T) {
	dir, f := directSetup(t)
	n, version, err := SyncMenu(dir)
	if err != nil || n != 1 || version != 4 {
		t.Fatalf("n %d version %d err %v", n, version, err)
	}
	if f.auth != "token tok" {
		t.Fatalf("auth %q", f.auth)
	}
	cache, _ := os.ReadFile(filepath.Join(dir, "menu-cache.yaml"))
	if !strings.Contains(string(cache), "git") {
		t.Fatal(string(cache))
	}
}

func TestAMachineWithAForgejoTokenSavesTheNextVersionOrIsRefused(t *testing.T) {
	dir, f := directSetup(t)
	if _, err := SaveMenu(dir, []interface{}{map[string]interface{}{"name": "eks"}}, 3); err == nil || f.saves != 0 {
		t.Fatalf("a stale base saved: %v", err)
	}
	version, err := SaveMenu(dir, []interface{}{map[string]interface{}{"name": "eks"}}, 4)
	if err != nil || version != 5 || f.saves != 1 {
		t.Fatalf("version %d err %v saves %d", version, err, f.saves)
	}
	var doc treeDoc
	if err := json.Unmarshal(f.file, &doc); err != nil || doc.Version != 5 || len(doc.Tree) != 1 {
		t.Fatalf("%s", f.file)
	}
	if !strings.HasPrefix(string(f.file), "{\n  \"version\": 5,\n  \"updatedAt\"") {
		t.Fatalf("not the API's layout: %s", f.file)
	}
}
