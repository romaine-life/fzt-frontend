// Package frontend provides shared frontend behavior for fzt tools:
// command palette mechanics, frontend identity, and action routing.
//
// Every tool that wants to be an "fzt app" imports this package alongside
// the fzt engine (github.com/romaine-life/fzt/core).
package frontend

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/romaine-life/fzt/core"
)

// EngineVersion is the fzt engine version this build was compiled against.
// Set via ldflags: -X github.com/romaine-life/fzt-frontend.EngineVersion=v0.2.39
var EngineVersion = "dev"

// cmdAction creates an *ItemAction with type "command" for command palette items.
func cmdAction(target string) *core.ItemAction {
	if target == "" {
		return nil
	}
	return &core.ItemAction{Type: "command", Target: target}
}

// EditCommands returns the canonical `edit` submenu for frontends that edit
// the current tree in place (add-after, add-folder, edit-item, delete,
// save). Include it in your Config.FrontendCommands. Non-editors (e.g.
// fzt-showcase) should omit it.
//
// Action strings are stable identifiers handled by HandleCommandAction and
// tui key handlers. Renaming palette Names is safe; renaming Actions is not.
func EditCommands() core.CommandItem {
	return core.CommandItem{
		Name: "edit", Description: "Edit tree in place",
		Children: []core.CommandItem{
			{Name: "add-after", Description: "Add item after cursor", Action: "add-after"},
			{Name: "add-folder", Description: "Create folder at cursor", Action: "add-folder"},
			{Name: "edit-item", Description: "Edit item properties", Action: "rename"},
			{Name: "delete", Description: "Delete highlighted item", Action: "delete"},
			{Name: "save", Description: "Save changes to cloud", Action: "save"},
		},
	}
}

// InjectCommandFolder appends the `:` command folder and its children to the
// tree's AllItems. When a frontend is registered (FrontendName set), the first
// level holds frontend commands and a nested `:` subfolder holds core commands.
// When no frontend is registered, the first level holds core commands directly.
func buildVersionRegistry(s *core.State, coreVersion string) {
	coreVerStr := coreVersion
	if coreVerStr == "" || coreVerStr == "dev" {
		coreVerStr = "ERROR: use go run ./build"
	}
	s.VersionRegistry = nil
	if s.FrontendName != "" {
		feVer := s.FrontendVersion
		if feVer == "" || feVer == "UNSET" {
			feVer = "ERROR: use go run ./build"
		}
		s.VersionRegistry = append(s.VersionRegistry, s.FrontendName+" "+feVer)
		s.VersionRegistry = append(s.VersionRegistry, "fzt "+coreVerStr)
	} else {
		s.VersionRegistry = append(s.VersionRegistry, "fzt "+coreVerStr)
	}
}

func hasEnvTag(tags []string, tag string) bool {
	for _, t := range tags {
		if t == tag {
			return true
		}
	}
	return false
}

func InjectCommandFolder(s *core.State, coreVersion string) {
	ctx := s.TopCtx()

	// Strip any stale `:` palette carried in by loaded data. Older clients
	// serialized the client-injected palette back to the cloud menu on save
	// (before SerializeTree learned to skip Injected items). Re-inject fresh
	// below so the in-RAM palette always reflects current client code.
	stripClientPalette(ctx)

	hasFrontend := s.FrontendName != ""

	coreVerStr := coreVersion
	if coreVerStr == "" || coreVerStr == "dev" {
		coreVerStr = "ERROR: use go run ./build"
	}

	// Build version registry — each entry gets an index consumed by `version`
	// palette leaves. Identity is no longer appended here; `:whoami` emits it
	// as a one-shot status via SetTitle rather than as a persistent display.
	s.VersionRegistry = nil
	if hasFrontend {
		feLabel := s.FrontendName
		feVer := s.FrontendVersion
		if feVer == "" || feVer == "UNSET" {
			feVer = "ERROR: use go run ./build"
		}
		s.VersionRegistry = append(s.VersionRegistry, feLabel+" "+feVer) // index 0: frontend
		s.VersionRegistry = append(s.VersionRegistry, "fzt "+coreVerStr) // index 1: engine
	} else {
		s.VersionRegistry = append(s.VersionRegistry, "fzt "+coreVerStr) // index 0: engine
	}

	base := len(ctx.AllItems)
	ctlFolderIdx := base
	var items []core.Item

	if hasFrontend {
		items = buildTwoLevelCommandTree(s, ctlFolderIdx, 0, 1, s.EnvTags) // feIdx=0, coreIdx=1
	} else {
		items = buildCoreLevelCommandTree(s.VersionRegistry, ctlFolderIdx, 0, s.EnvTags) // coreIdx=0
	}

	// When the consumer opts into HidePalette, skip injection entirely.
	// The palette doesn't exist in this session — no `:` row at root AND
	// typing `:` won't reach any palette commands (since the items aren't
	// in AllItems for the search to find). VersionRegistry was still
	// built above in case some other path reads it; harmless.
	if s.HidePalette {
		return
	}
	// Tag every injected item so SerializeTree skips them on :save. The
	// cloud menu only ever receives user data; the palette is always
	// client-reconstructed.
	for i := range items {
		items[i].Injected = true
	}
	ctx.AllItems = append(ctx.AllItems, items...)
	ctx.Items = core.RootItemsOf(ctx.AllItems)
}

// stripClientPalette removes any top-level hidden `:` folder (and its whole
// subtree) from the loaded tree, rebuilding the index space so ParentIdx and
// Children arrays stay consistent. Called from InjectCommandFolder before
// fresh injection.
func stripClientPalette(ctx *core.TreeContext) {
	var removed map[int]bool
	for i, item := range ctx.AllItems {
		if item.Depth == 0 && item.Hidden && len(item.Fields) > 0 && item.Fields[0] == ":" {
			if removed == nil {
				removed = make(map[int]bool)
			}
			collectSubtree(ctx.AllItems, i, removed)
		}
	}
	if len(removed) == 0 {
		return
	}

	oldToNew := make([]int, len(ctx.AllItems))
	kept := make([]core.Item, 0, len(ctx.AllItems)-len(removed))
	for i, item := range ctx.AllItems {
		if removed[i] {
			oldToNew[i] = -1
			continue
		}
		oldToNew[i] = len(kept)
		kept = append(kept, item)
	}
	for i := range kept {
		if kept[i].ParentIdx >= 0 {
			kept[i].ParentIdx = oldToNew[kept[i].ParentIdx]
		}
		var newChildren []int
		for _, c := range kept[i].Children {
			if nc := oldToNew[c]; nc >= 0 {
				newChildren = append(newChildren, nc)
			}
		}
		kept[i].Children = newChildren
	}
	ctx.AllItems = kept
	ctx.Items = core.RootItemsOf(kept)
}

func collectSubtree(items []core.Item, idx int, out map[int]bool) {
	out[idx] = true
	for _, c := range items[idx].Children {
		collectSubtree(items, c, out)
	}
}

// helpEntries returns the static key → description table for the `:help/`
// palette subfolder. Each entry becomes a tree item with the key symbol as
// its name and the long-form explanation as its description. Consumed by
// the `?` help-mode lookup in core, and by users who manually scope into
// `:help` to browse.
func helpEntries() [][2]string {
	return [][2]string{
		{"`", "enter normal mode (cursor on tree)"},
		{"/", "return to search mode; query preserved"},
		{"h", "normal mode: navigate left"},
		{"j", "normal mode: navigate down"},
		{"k", "normal mode: navigate up"},
		{"l", "normal mode: navigate right"},
		{"enter", "select leaf / push scope into folder / commit edit"},
		{"shift+enter", "universal confirm-select; in edit modes, confirms the edit"},
		{"backspace", "delete last query char; in normal mode chops query + returns to search"},
		{"shift+backspace", "reset navigation to home (pop all scope)"},
		{"esc", "clear query → pop scope → quit (cascade)"},
		{"tab", "autocomplete top match; on folder, push scope"},
		{"home", "move query cursor to start"},
		{"end", "move query cursor to end"},
		{"space", "on folder, push scope (same as enter on folder)"},
		{"?", "arm help mode — next keypress jumps to that key's help entry"},
		{":", "scope into the commands palette"},
	}
}

// buildHelpSubfolder appends the help subfolder + its children to items.
// Returns the updated items slice and the next available index.
// helpFolderIdx is the index the help folder will occupy in ctx.AllItems.
// parentIdx is the index of its parent (the `:` folder). depth is the tree
// depth for the help folder itself (children are depth+1).
func buildHelpSubfolder(items []core.Item, helpFolderIdx, parentIdx, depth int) ([]core.Item, []int) {
	entries := helpEntries()
	childIdxs := make([]int, len(entries))
	for i := range entries {
		childIdxs[i] = helpFolderIdx + 1 + i
	}
	items = append(items, core.Item{
		Fields: []string{"help", "lookup what each key does"}, Depth: depth,
		HasChildren: true, ParentIdx: parentIdx, Children: childIdxs, PropertyOf: -1,
	})
	for _, e := range entries {
		items = append(items, core.Item{
			Fields: []string{e[0], e[1]}, Depth: depth + 1,
			ParentIdx: helpFolderIdx,
			Action:    cmdAction("help-entry"),
			PropertyOf: -1,
		})
	}
	return items, childIdxs
}

// buildCoreLevelCommandTree builds `:` → core commands directly (no frontend layer).
// versionIdx is the index into State.VersionRegistry for this level's version string.
func buildCoreLevelCommandTree(registry []string, ctlFolderIdx int, versionIdx int, envTags []string) []core.Item {
	idx := ctlFolderIdx + 1
	var ctlChildren []int
	var items []core.Item

	versionDesc := ""
	if versionIdx >= 0 && versionIdx < len(registry) {
		versionDesc = registry[versionIdx]
	}

	// version — always shown
	versionItemIdx := idx
	ctlChildren = append(ctlChildren, versionItemIdx)
	idx++
	items = append(items, core.Item{
		Fields: []string{"version", versionDesc}, Depth: 1,
		ParentIdx: ctlFolderIdx, Action: cmdAction("version"), PropertyOf: -1,
	})

	// Conditional core commands
	type coreCmd struct {
		fields    []string
		action    string
		condition string
	}
	coreCmds := []coreCmd{
		{[]string{"updatetimer", "Show time to next sync check"}, "updatetimer", ""},
		{[]string{"validate", "Validate auth token"}, "validate", ""},
	}
	for _, cmd := range coreCmds {
		if cmd.condition != "" && !hasEnvTag(envTags, cmd.condition) {
			continue
		}
		ctlChildren = append(ctlChildren, idx)
		idx++
		items = append(items, core.Item{
			Fields: cmd.fields, Depth: 1, ParentIdx: ctlFolderIdx,
			Action: cmdAction(cmd.action), DisplayCondition: cmd.condition, PropertyOf: -1,
		})
	}

	// Help subfolder — key → description lookup. Child of the `:` folder.
	helpFolderIdx := idx
	ctlChildren = append(ctlChildren, helpFolderIdx)
	items, _ = buildHelpSubfolder(items, helpFolderIdx, ctlFolderIdx, 1)
	idx = helpFolderIdx + 1 + len(helpEntries())

	// Prepend the : folder itself. Visible in the root tree — discoverability
	// over stealth (users including future-you can find the palette without
	// tribal knowledge of the `:` gesture).
	ctlFolder := core.Item{
		Fields: []string{":", "commands"}, Depth: 0, HasChildren: true,
		ParentIdx: -1, Children: ctlChildren, PropertyOf: -1,
	}
	return append([]core.Item{ctlFolder}, items...)
}

// buildTwoLevelCommandTree builds `:` → frontend commands + `::` → core commands.
// feIdx and coreIdx are indices into State.VersionRegistry.
//
// Index allocation: the function pre-allocates contiguous index ranges for all items
// before building the slice. Starting from ctlFolderIdx+1, it reserves indices for:
// version (1), whoami leaf (1), each FrontendCommand + its Children,
// and the core subfolder. Items must be appended in the same order as indices were reserved.
func buildTwoLevelCommandTree(s *core.State, ctlFolderIdx int, feIdx int, coreIdx int, envTags []string) []core.Item {
	idx := ctlFolderIdx + 1
	var ctlChildren []int

	// frontend version — single toggle leaf
	feVersionIdx := idx
	ctlChildren = append(ctlChildren, feVersionIdx)
	idx++

	// whoami leaf — emits identity as one-shot status (no persistent display)
	whoamiIdx := idx
	ctlChildren = append(ctlChildren, whoamiIdx)
	idx++

	for _, cmd := range s.FrontendCommands {
		ctlChildren = append(ctlChildren, idx)
		idx++
		idx += len(cmd.Children) // reserve indices for children
	}

	coreSubfolderIdx := idx
	ctlChildren = append(ctlChildren, coreSubfolderIdx)
	idx++

	// core version — always shown
	coreVersionIdx := idx
	coreSubChildren := []int{coreVersionIdx}
	idx++

	// Conditional core commands
	type coreCmd struct {
		fields    []string
		action    string
		condition string
	}
	coreCmds := []coreCmd{
		{[]string{"updatetimer", "Show time to next sync check"}, "updatetimer", ""},
		{[]string{"validate", "Validate auth token"}, "validate", ""},
	}
	for _, cmd := range coreCmds {
		if cmd.condition != "" && !hasEnvTag(envTags, cmd.condition) {
			continue
		}
		coreSubChildren = append(coreSubChildren, idx)
		idx++
	}

	// Help subfolder — reserve indices for folder + one entry per helpEntries().
	helpFolderIdx := idx
	ctlChildren = append(ctlChildren, helpFolderIdx)
	idx++
	idx += len(helpEntries())

	var items []core.Item

	items = append(items, core.Item{
		Fields: []string{":", "commands"}, Depth: 0, HasChildren: true,
		ParentIdx: -1, Children: ctlChildren, PropertyOf: -1,
	})

	// Frontend version toggle
	feVersionDesc := ""
	if feIdx >= 0 && feIdx < len(s.VersionRegistry) {
		feVersionDesc = s.VersionRegistry[feIdx]
	}
	items = append(items, core.Item{
		Fields: []string{"version", feVersionDesc}, Depth: 1,
		ParentIdx: ctlFolderIdx, Action: cmdAction("version"), PropertyOf: -1,
	})

	items = append(items, core.Item{
		Fields: []string{"whoami", "Emit loaded identity as status"}, Depth: 1,
		ParentIdx: ctlFolderIdx, Action: cmdAction("whoami"), PropertyOf: -1,
	})

	for _, cmd := range s.FrontendCommands {
		cmdIdx := ctlFolderIdx + len(items)
		hasChildren := len(cmd.Children) > 0
		cmdItem := core.Item{
			Fields: []string{cmd.Name, cmd.Description}, Depth: 1,
			ParentIdx: ctlFolderIdx, HasChildren: hasChildren,
			Action: cmdAction(cmd.Action), PropertyOf: -1,
		}
		if hasChildren {
			for i := range cmd.Children {
				cmdItem.Children = append(cmdItem.Children, cmdIdx+1+i)
			}
		}
		items = append(items, cmdItem)
		for _, child := range cmd.Children {
			items = append(items, core.Item{
				Fields: []string{child.Name, child.Description}, Depth: 2, ParentIdx: cmdIdx,
				Action: cmdAction(child.Action), PropertyOf: -1,
			})
		}
	}

	items = append(items, core.Item{
		Fields: []string{":", "fzt core"}, Depth: 1,
		HasChildren: true, ParentIdx: ctlFolderIdx, Children: coreSubChildren, PropertyOf: -1,
	})

	// Core version toggle
	coreVersionDesc := ""
	if coreIdx >= 0 && coreIdx < len(s.VersionRegistry) {
		coreVersionDesc = s.VersionRegistry[coreIdx]
	}
	items = append(items, core.Item{
		Fields: []string{"version", coreVersionDesc}, Depth: 2,
		ParentIdx: coreSubfolderIdx, Action: cmdAction("version"), PropertyOf: -1,
	})

	for _, cmd := range coreCmds {
		if cmd.condition != "" && !hasEnvTag(envTags, cmd.condition) {
			continue
		}
		items = append(items, core.Item{
			Fields: cmd.fields, Depth: 2, ParentIdx: coreSubfolderIdx,
			Action: cmdAction(cmd.action), DisplayCondition: cmd.condition, PropertyOf: -1,
		})
	}

	// Help subfolder + its children — appended in the index range reserved
	// above (helpFolderIdx..helpFolderIdx+len(helpEntries())).
	items, _ = buildHelpSubfolder(items, helpFolderIdx, ctlFolderIdx, 1)

	return items
}

// HandleCommandAction processes a selected leaf item in the command tree.
// Returns an action string for the frontend, or "" if handled internally.
func HandleCommandAction(s *core.State, item core.Item) string {
	if len(item.Fields) == 0 {
		return ""
	}
	// Route by stable Action field, fall back to display name
	action := ""
	if item.Action != nil {
		action = item.Action.Target
	}
	if action == "" {
		action = item.Fields[0]
	}

	switch action {
	case "version":
		// Emit the version as a status. Repeats pulse (SetTitle detects repeat).
		// Previously toggled between show/hide — removed: selecting the item
		// should just re-emit the status, not hide it.
		if len(item.Fields) >= 2 && item.Fields[1] != "" {
			s.SetTitle(item.Fields[1], 1)
		}
		return ""
	case "whoami":
		// Emit the loaded identity as a one-shot status — same pattern as
		// `version`. No persistent display; gets wiped by the next status.
		label := s.IdentityLabel
		if label == "" {
			label = "(not signed in — run authromaine)"
		}
		s.SetTitle(label, 1)
		return ""
	case "updatetimer":
		if s.SyncTimerShown {
			s.ClearTitle()
		} else {
			s.SyncTimerShown = true
		}
		return ""
	case "toggle-states":
		s.StatesBannerOn = !s.StatesBannerOn
		if s.StatesBannerOn {
			s.SetTitle("states inspector on — actions suppressed", 1)
		} else {
			s.LastActionPreview = ""
			s.SetTitle("states inspector off", 0)
		}
		return ""
	case "validate":
		HandleValidate(s)
		return ""
	case "unload":
		if s.ConfigDir != "" {
			os.Remove(filepath.Join(s.ConfigDir, "menu-cache.yaml"))
		}
		return "unloaded"
	case "sync":
		if s.ConfigDir == "" {
			s.SetTitle("no config directory set", 2)
			return ""
		}
		count, ver, err := SyncMenu(s.ConfigDir)
		if err != nil {
			s.SetTitle(err.Error(), 2)
			return ""
		}
		s.MenuVersion = ver
		s.SetTitle(fmt.Sprintf("synced %d items @ v%d", count, ver), 1)
		return "synced"
	case "add-after":
		s.EditMode = "add-after"
		s.SetTitle("add after: navigate, Shift+Enter to place", 0)
		return ""
	case "add-folder":
		s.EditMode = "add-folder"
		s.SetTitle("add folder: navigate, Shift+Enter to place", 0)
		return ""
	case "rename":
		s.EditMode = "inspect"
		s.SetTitle("edit: navigate to item, Shift+Enter", 0)
		return ""
	case "delete":
		s.EditMode = "delete"
		s.SetTitle("delete: navigate to item, Shift+Enter", 0)
		return ""
	case "inspect":
		s.EditMode = "inspect"
		s.SetTitle("inspect: navigate to item, Shift+Enter", 0)
		return ""
	case "save":
		if s.ConfigDir == "" {
			s.SetTitle("no config directory set", 2)
			return ""
		}
		if !s.Dirty {
			s.SetTitle("no unsaved changes", 0)
			return ""
		}
		ctx := s.TopCtx()
		menu := core.SerializeTree(ctx)
		version, err := SaveMenu(s.ConfigDir, menu, s.MenuVersion)
		if err != nil {
			s.SetTitle(err.Error(), 2)
			return ""
		}
		s.Dirty = false
		s.MenuVersion = version
		s.SetTitle(fmt.Sprintf("saved v%d", version), 1)
		return ""
	case "update":
		return "update"
	case "help-entry":
		// Help entries carry their long-form description in Fields[1]. On
		// select, echo it to the title bar so the user gets a visible
		// confirmation that Enter was recognized (the description is also
		// always visible in the row's description column).
		if len(item.Fields) >= 2 && item.Fields[1] != "" {
			s.SetTitle(item.Fields[1], 3)
		}
		return ""
	}

	for _, cmd := range s.FrontendCommands {
		if cmd.Name == action || cmd.Action == action {
			return cmd.Action
		}
		for _, child := range cmd.Children {
			if child.Name == action || child.Action == action {
				return child.Action
			}
		}
	}

	return ""
}

// ApplyConfig sets frontend identity and commands from Config onto State.
// Call before InjectCommandFolder.
func ApplyConfig(s *core.State, cfg core.Config) {
	if cfg.FrontendName != "" {
		s.FrontendName = cfg.FrontendName
	}
	if cfg.FrontendVersion != "" {
		s.FrontendVersion = cfg.FrontendVersion
	}
	if len(cfg.FrontendCommands) > 0 {
		s.FrontendCommands = cfg.FrontendCommands
	}
	if cfg.InitialDisplay != "" {
		s.IdentityLabel = cfg.InitialDisplay
	}
	if cfg.UpdateRepo != "" {
		s.UpdateRepo = cfg.UpdateRepo
	}
	if cfg.UpdateAssetPrefix != "" {
		s.UpdateAssetPrefix = cfg.UpdateAssetPrefix
	}
	if cfg.UpdateBinaryName != "" {
		s.UpdateBinaryName = cfg.UpdateBinaryName
	}
	s.HidePalette = cfg.HidePalette
}
