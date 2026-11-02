package frontend

import (
	"testing"

	"github.com/romaine-life/fzt/core"
)

// Every row of the `:` palette is marked as the palette's (a host routes it to
// HandleCommandAction) and as injected (never saved); a menu row is neither.
func TestInjectCommandFolderMarksPaletteRows(t *testing.T) {
	s, _ := core.NewState([]core.Item{{Fields: []string{"menu row"}, ParentIdx: -1, PropertyOf: -1}}, core.Config{Tiered: true})
	InjectCommandFolder(s, "v0.0.0")

	all := s.TopCtx().AllItems
	if len(all) < 2 {
		t.Fatalf("no palette rows injected: %d rows", len(all))
	}
	for i, item := range all {
		menu := len(item.Fields) > 0 && item.Fields[0] == "menu row"
		if menu && (item.Palette || item.Injected) {
			t.Errorf("menu row %d marked: Palette=%v Injected=%v", i, item.Palette, item.Injected)
		}
		if !menu && (!item.Palette || !item.Injected) {
			t.Errorf("palette row %d %q not marked: Palette=%v Injected=%v", i, item.Fields, item.Palette, item.Injected)
		}
	}
}
