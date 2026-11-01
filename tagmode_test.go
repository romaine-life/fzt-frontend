package frontend

import (
	"testing"

	"github.com/romaine-life/fzt/core"
)

// withConfigDir points os.UserConfigDir at a temp dir on every OS.
func withConfigDir(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("APPDATA", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
}

func TestTagModeDefaultsOff(t *testing.T) {
	withConfigDir(t)
	on, err := LoadTagMode()
	if err != nil || on {
		t.Fatalf("LoadTagMode = %v, %v; want off, nil", on, err)
	}
}

func TestTagsToggleSavesAndUpdatesRow(t *testing.T) {
	withConfigDir(t)
	s, _ := core.NewState(nil, core.Config{Tiered: true})
	InjectCommandFolder(s, "v0.0.0")

	row := func() core.Item {
		for _, item := range s.TopCtx().AllItems {
			if item.Action != nil && item.Action.Target == "tags" {
				return item
			}
		}
		t.Fatal("no tags row in the palette")
		return core.Item{}
	}
	if got := row().Fields[1]; got != "Search by tag: off" {
		t.Fatalf("row = %q before toggle", got)
	}

	HandleCommandAction(s, row())
	if !s.TagMode {
		t.Fatal("TagMode still off after toggle")
	}
	if got := row().Fields[1]; got != "Search by tag: on" {
		t.Fatalf("row = %q after toggle", got)
	}
	if on, err := LoadTagMode(); err != nil || !on {
		t.Fatalf("saved setting = %v, %v; want on", on, err)
	}

	HandleCommandAction(s, row())
	if on, _ := LoadTagMode(); on || s.TagMode {
		t.Fatal("second toggle didn't turn tag mode off")
	}
}
