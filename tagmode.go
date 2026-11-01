package frontend

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/romaine-life/fzt/core"
)

// The tag-mode setting is one per user, shared by every fzt app on the
// machine: the tmux palette starts a new fzt each time it opens, so the
// setting has to outlive the process to be something you can live with.

// tagModePath is where the setting lives: <user config dir>/fzt/tag-mode,
// holding "on" or "off".
func tagModePath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "fzt", "tag-mode"), nil
}

// LoadTagMode reports whether tag search is on. A missing file is off, the
// default. Any other failure to read it is returned with off, so the caller
// can say the saved setting wasn't read.
func LoadTagMode() (bool, error) {
	path, err := tagModePath()
	if err != nil {
		return false, err
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(b)) == "on", nil
}

// SaveTagMode records the tag search setting for every fzt app.
func SaveTagMode(on bool) error {
	path, err := tagModePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	v := "off"
	if on {
		v = "on"
	}
	return os.WriteFile(path, []byte(v+"\n"), 0o644)
}

// tagModeDesc is the `tags` palette row's description: the current setting.
func tagModeDesc(on bool) string {
	if on {
		return "Search by tag: on"
	}
	return "Search by tag: off"
}

// toggleTagMode flips tag search, saves it, and refreshes the palette row
// that shows it. The session keeps the new setting even if saving fails; the
// status says it won't survive a restart.
func toggleTagMode(s *core.State) {
	s.TagMode = !s.TagMode
	for _, ctx := range s.Contexts {
		for i := range ctx.AllItems {
			if a := ctx.AllItems[i].Action; a != nil && a.Target == "tags" && len(ctx.AllItems[i].Fields) > 1 {
				ctx.AllItems[i].Fields[1] = tagModeDesc(s.TagMode)
			}
		}
	}
	if err := SaveTagMode(s.TagMode); err != nil {
		s.SetTitle(tagModeDesc(s.TagMode)+" (not saved: "+err.Error()+")", 2)
		return
	}
	s.SetTitle(tagModeDesc(s.TagMode), 1)
}
