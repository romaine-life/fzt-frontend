package frontend

import (
	"encoding/json"
	"testing"

	"github.com/romaine-life/fzt/core"
)

// The cache MenuToYAML writes must load back through fzt's LoadYAML with every
// value intact, whatever characters the menu holds.
func TestMenuToYAMLRoundTrip(t *testing.T) {
	menu := `[
		{"name": "open", "description": "Open a program", "children": [
			{"name": "rustdesk", "action": "launch 'RustDesk' '{6D809377-6AF0-444B-8957-A3773F02200E}\\RustDesk\\rustdesk.exe'"},
			{"name": "say \"hi\"", "description": "colon: hash # and a\ttab", "action": "echo \"$x\""},
			{"name": "Google", "description": "Google suite — mail, drive", "url": "https://mail.google.com", "hidden": true}
		]},
		{"name": "windows", "action": "folder:openwindows", "children": []}
	]`
	var items []interface{}
	if err := json.Unmarshal([]byte(menu), &items); err != nil {
		t.Fatal(err)
	}

	data, err := MenuToYAML(items)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := core.LoadYAMLFromString(string(data))
	if err != nil {
		t.Fatalf("cache doesn't load: %v\n%s", err, data)
	}

	byName := map[string]core.Item{}
	for _, it := range loaded {
		byName[it.Fields[0]] = it
	}
	want := []struct{ name, desc, target string }{
		{"rustdesk", "", "launch 'RustDesk' '{6D809377-6AF0-444B-8957-A3773F02200E}\\RustDesk\\rustdesk.exe'"},
		{`say "hi"`, "colon: hash # and a\ttab", `echo "$x"`},
		{"Google", "Google suite — mail, drive", "https://mail.google.com"},
		{"windows", "", "folder:openwindows"},
	}
	for _, w := range want {
		it, ok := byName[w.name]
		if !ok {
			t.Errorf("%q missing from the loaded cache:\n%s", w.name, data)
			continue
		}
		desc := ""
		if len(it.Fields) > 1 {
			desc = it.Fields[1]
		}
		if desc != w.desc {
			t.Errorf("%q description = %q, want %q", w.name, desc, w.desc)
		}
		if it.Action == nil || it.Action.Target != w.target {
			t.Errorf("%q action = %+v, want %q", w.name, it.Action, w.target)
		}
	}
	if byName["windows"].HasChildren {
		t.Error("an empty children list should leave windows a leaf, so its folder: action marks it")
	}
}
