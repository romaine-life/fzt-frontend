package frontend

import (
	"testing"

	"github.com/romaine-life/fzt/core"
)

func TestTagsToggleFlipsAndUpdatesRow(t *testing.T) {
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
	if s.TagMode || row().Fields[1] != "Search by tag: off" {
		t.Fatalf("a new menu must start with tag search off: %v %q", s.TagMode, row().Fields[1])
	}
	HandleCommandAction(s, row())
	if !s.TagMode || row().Fields[1] != "Search by tag: on" {
		t.Fatalf("after toggle: %v %q", s.TagMode, row().Fields[1])
	}
	HandleCommandAction(s, row())
	if s.TagMode || row().Fields[1] != "Search by tag: off" {
		t.Fatalf("second toggle: %v %q", s.TagMode, row().Fields[1])
	}
}
