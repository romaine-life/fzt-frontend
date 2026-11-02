package frontend

import "github.com/romaine-life/fzt/core"

// tagModeDesc is the `tags` palette row's description: the current setting.
func tagModeDesc(on bool) string {
	if on {
		return "Search by tag: on"
	}
	return "Search by tag: off"
}

// toggleTagMode flips tag search for this menu and refreshes the palette row
// that shows it. It is not saved: every menu opens with tag search off.
func toggleTagMode(s *core.State) {
	s.TagMode = !s.TagMode
	for _, ctx := range s.Contexts {
		for i := range ctx.AllItems {
			if a := ctx.AllItems[i].Action; a != nil && a.Target == "tags" && len(ctx.AllItems[i].Fields) > 1 {
				ctx.AllItems[i].Fields[1] = tagModeDesc(s.TagMode)
			}
		}
	}
	s.SetTitle(tagModeDesc(s.TagMode), 1)
}
