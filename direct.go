package frontend

// A machine with no OpenBao, such as a work laptop, can't sign in to the tree API, so it
// reads and saves its menu straight from the tree store the API keeps (romaine-life/at on
// Forgejo, trees/<id>.json), with a Forgejo token of its own in forgejo-token in its config
// folder. Its tree is <identity>-menu, its identity in .identity (the laptop's nelson-ea).
// Saving writes what the API writes: the next version, refused when the base is stale.

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// treeStore is the tree store's folder of trees, through Forgejo's contents API; a
// variable so tests can point it elsewhere.
var treeStore = "https://forgejo.romaine.life/api/v1/repos/romaine-life/at/contents/trees"

// directStore gives the Forgejo token and menu tree of a machine that reads the store
// directly; ok is false for one that signs in to the API.
func directStore(configDir string) (token, treeID string, ok bool) {
	t, err := os.ReadFile(filepath.Join(configDir, "forgejo-token"))
	if err != nil {
		return "", "", false
	}
	id, err := os.ReadFile(filepath.Join(configDir, ".identity"))
	if err != nil || strings.TrimSpace(string(id)) == "" {
		return "", "", false
	}
	return strings.TrimSpace(string(t)), MenuTreeID(strings.TrimSpace(string(id))), true
}

type treeDoc struct {
	Version   int           `json:"version"`
	UpdatedAt string        `json:"updatedAt"`
	Tree      []interface{} `json:"tree"`
}

// fetchDirect reads trees/<id>.json and the blob it is, which a save names as its base.
func fetchDirect(token, treeID string) (treeDoc, string, error) {
	var doc treeDoc
	body, status, err := storeCall("GET", token, treeID, nil)
	if err != nil {
		return doc, "", err
	}
	if status == http.StatusNotFound {
		return doc, "", fmt.Errorf("the tree store has no %s", treeID)
	}
	if status != http.StatusOK {
		return doc, "", fmt.Errorf("the tree store answered %d: %s", status, strings.TrimSpace(string(body)))
	}
	var file struct {
		Content string `json:"content"`
		SHA     string `json:"sha"`
	}
	if err := json.Unmarshal(body, &file); err != nil {
		return doc, "", err
	}
	raw, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(file.Content, "\n", ""))
	if err != nil {
		return doc, "", err
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return doc, "", fmt.Errorf("%s isn't a tree: %w", treeID, err)
	}
	return doc, file.SHA, nil
}

// errStale is a save whose base isn't the store's version.
var errStale = errors.New("conflict — sync first")

// saveDirect writes tree as the next version of treeID; baseVersion must be the
// store's version, as the API requires.
func saveDirect(token, treeID string, tree []interface{}, baseVersion int) (int, error) {
	current, sha, err := fetchDirect(token, treeID)
	if err != nil {
		return 0, err
	}
	if current.Version != baseVersion {
		return 0, errStale
	}
	doc := treeDoc{Version: current.Version + 1, UpdatedAt: time.Now().UTC().Format("2006-01-02T15:04:05.000Z"), Tree: tree}
	var text bytes.Buffer
	enc := json.NewEncoder(&text)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return 0, err
	}
	req, _ := json.Marshal(map[string]string{
		"content": base64.StdEncoding.EncodeToString(text.Bytes()),
		"sha":     sha,
		"message": fmt.Sprintf("Update %s to v%d", treeID, doc.Version),
	})
	body, status, err := storeCall("PUT", token, treeID, req)
	if err != nil {
		return 0, err
	}
	if status == http.StatusConflict || status == http.StatusUnprocessableEntity {
		return 0, errStale
	}
	if status != http.StatusOK && status != http.StatusCreated {
		return 0, fmt.Errorf("the tree store answered %d: %s", status, strings.TrimSpace(string(body)))
	}
	return doc.Version, nil
}

func storeCall(method, token, treeID string, body []byte) ([]byte, int, error) {
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, treeStore+"/"+treeID+".json", r)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "token "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	return out, resp.StatusCode, err
}
