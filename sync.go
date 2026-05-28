package frontend

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const APIBase = "https://fzt-frontend.romaine.life/fzt"

// StripMetadata removes keys starting with "_" from bookmark objects recursively.
func StripMetadata(items []interface{}) []interface{} {
	var out []interface{}
	for _, item := range items {
		m, ok := item.(map[string]interface{})
		if !ok {
			out = append(out, item)
			continue
		}
		clean := make(map[string]interface{})
		for k, v := range m {
			if strings.HasPrefix(k, "_") {
				continue
			}
			if k == "children" {
				if children, ok := v.([]interface{}); ok {
					clean[k] = StripMetadata(children)
					continue
				}
			}
			clean[k] = v
		}
		out = append(out, clean)
	}
	return out
}

// FetchTree GETs a tree from the /fzt/tree/:id endpoint.
// Returns (tree, version, updatedAt, error).
func FetchTree(token, treeID string) ([]interface{}, int, string, error) {
	url := fmt.Sprintf("%s/tree/%s", APIBase, treeID)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, 0, "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, 0, "", fmt.Errorf("API returned %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, "", err
	}

	var result struct {
		Tree      []interface{} `json:"tree"`
		Version   int           `json:"version"`
		UpdatedAt *string       `json:"updatedAt"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, 0, "", err
	}

	updatedAt := ""
	if result.UpdatedAt != nil {
		updatedAt = *result.UpdatedAt
	}
	return result.Tree, result.Version, updatedAt, nil
}

// SaveTree PUTs a tree to the /fzt/tree/:id endpoint.
// Returns the new version number, or an error.
func SaveTree(token, treeID string, tree []interface{}, baseVersion int) (int, error) {
	url := fmt.Sprintf("%s/tree/%s", APIBase, treeID)
	body := map[string]interface{}{
		"tree":        tree,
		"baseVersion": baseVersion,
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return 0, err
	}

	req, err := http.NewRequest("PUT", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}

	if resp.StatusCode == 409 {
		return 0, fmt.Errorf("conflict — sync first")
	}
	if resp.StatusCode != 200 {
		return 0, fmt.Errorf("API returned %d", resp.StatusCode)
	}

	var result struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return 0, err
	}
	return result.Version, nil
}

// MenuTreeID returns the tree id for the caller's menu: "<sub>-menu".
// Personal trees use the "<identity>-<kind>" convention; shared trees
// are flat names (e.g. "google").
func MenuTreeID(sub string) string {
	return sub + "-menu"
}

// SyncMenu fetches the caller's menu tree from the API and writes the cache
// file as YAML. The tree id is keyed by the auth.romaine.life token's `sub`.
// Returns (item count, version number, error).
func SyncMenu(configDir string) (int, int, error) {
	token, err := ReadAuthToken(configDir)
	if err != nil {
		return 0, 0, err
	}
	sub, err := SubFromToken(token)
	if err != nil {
		return 0, 0, err
	}

	menu, version, _, err := FetchTree(token, MenuTreeID(sub))
	if err != nil {
		return 0, 0, fmt.Errorf("API error: %w", err)
	}

	data, err := MenuToYAML(menu)
	if err != nil {
		return 0, 0, err
	}

	cacheFile := filepath.Join(configDir, "menu-cache.yaml")
	if err := os.WriteFile(cacheFile, data, 0644); err != nil {
		return 0, 0, fmt.Errorf("failed to write cache: %w", err)
	}

	versionFile := filepath.Join(configDir, ".menu-version")
	os.WriteFile(versionFile, []byte(fmt.Sprintf("%d", version)), 0644)

	return len(menu), version, nil
}

// SaveMenu PUTs the caller's menu tree and updates the local cache. The tree
// id is keyed by the auth.romaine.life token's `sub`. Returns the new version.
func SaveMenu(configDir string, menu []interface{}, baseVersion int) (int, error) {
	token, err := ReadAuthToken(configDir)
	if err != nil {
		return 0, err
	}
	sub, err := SubFromToken(token)
	if err != nil {
		return 0, err
	}

	version, err := SaveTree(token, MenuTreeID(sub), menu, baseVersion)
	if err != nil {
		return 0, err
	}

	data, err := MenuToYAML(menu)
	if err != nil {
		return version, nil // saved to API but cache write failed — non-fatal
	}
	cacheFile := filepath.Join(configDir, "menu-cache.yaml")
	os.WriteFile(cacheFile, data, 0644)

	versionFile := filepath.Join(configDir, ".menu-version")
	os.WriteFile(versionFile, []byte(fmt.Sprintf("%d", version)), 0644)

	return version, nil
}

// MenuToYAML converts the API menu response (JSON objects) to YAML format
// compatible with fzt's LoadYAML.
func MenuToYAML(items []interface{}) ([]byte, error) {
	return marshalYAMLItems(items, 0), nil
}

func marshalYAMLItems(items []interface{}, indent int) []byte {
	var buf []byte
	prefix := strings.Repeat("  ", indent)
	for _, item := range items {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := m["name"].(string)
		desc, _ := m["description"].(string)
		url, _ := m["url"].(string)
		action, _ := m["action"].(string)
		hidden, _ := m["hidden"].(bool)
		children, hasChildren := m["children"].([]interface{})

		buf = append(buf, []byte(prefix+"- name: \""+name+"\"\n")...)
		if desc != "" {
			buf = append(buf, []byte(prefix+"  description: \""+desc+"\"\n")...)
		}
		if url != "" {
			buf = append(buf, []byte(prefix+"  url: \""+url+"\"\n")...)
		}
		if action != "" {
			buf = append(buf, []byte(prefix+"  action: \""+action+"\"\n")...)
		}
		if hidden {
			buf = append(buf, []byte(prefix+"  hidden: true\n")...)
		}
		if hasChildren && len(children) > 0 {
			buf = append(buf, []byte(prefix+"  children:\n")...)
			buf = append(buf, marshalYAMLItems(children, indent+2)...)
		}
	}
	return buf
}

// CheckBookmarkStaleness checks if the local menu cache is older than what the
// API has. Returns true if the cache is missing or the server has a newer
// version. Used by the tui's background sync ticker. Does not modify state.
//
// Extracted from fzt-terminal/tui/sync.go during the fzt-frontend split so the
// "fetch and compare" logic lives alongside the other API-calling code rather
// than inside the renderer.
func CheckBookmarkStaleness(configDir string) bool {
	token, err := ReadAuthToken(configDir)
	if err != nil {
		return false
	}
	sub, err := SubFromToken(token)
	if err != nil {
		return false
	}

	_, _, updatedAt, err := FetchTree(token, MenuTreeID(sub))
	if err != nil {
		return false
	}

	if updatedAt == "" {
		return false
	}

	serverTime, err := time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return false
	}

	cacheFile := filepath.Join(configDir, "menu-cache.yaml")
	info, err := os.Stat(cacheFile)
	if err != nil {
		return true // no cache = stale
	}

	return serverTime.After(info.ModTime())
}
