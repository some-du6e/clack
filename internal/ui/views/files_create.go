package views

// The Files tab's create menus, ported from Slack's web client (CDN build
// 4d0c78085232, fetched without a cookie):
//
//   - the sidebar's "+" (UnifiedFilesSidebarView eK, cNNe; tooltip
//     "Create new", a Popover at bottom-left): Canvas ("Create and share
//     content"), List ("Track and manage projects") and, with
//     custom_file_sections, Sidebar section ("Group and organize your
//     files"), each with its 20px icon;
//   - the gear before it ("More options", unified-files-sidebar-settings-
//     menu): rendered only while its menu has items, and its one item,
//     "Bulk manage files", needs custom_file_sections off a free plan;
//   - the primary view's "New" (UnifiedFilesCreateMenu, explorer helpers
//     n+Lz: a primary ViewHeaderPrimaryDropdownButton with plus-filled,
//     its menu 250 wide at bottom-right): All files New canvas, New list,
//     then Templates (Canvas templates, List templates) and Import CSV as
//     a list; Canvases New canvas and Start from a template; Lists New
//     list, Start from a template and Import CSV.
//
// Clack draws the items it can do: New canvas (createAndOpenSlackDoc,
// Clack's CreateCanvas) and New list (createAndOpenList: lists.create
// {template_id: blank, todo_mode with lists_grid_todos}). The templates
// gallery (openCanvasTemplateGalleryModal), ListTemplateBrowseModal and
// the CSV import modal aren't ported, so those rows aren't drawn
// (docs/FILES-AUDIT.md).

import (
	"context"
	"image"
	"image/color"

	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/popups"
)

// plusMenuItems is the "+" menu (eK).
func (f *Files) plusMenuItems() []popups.MenuItem {
	a := f.h.Core()
	items := []popups.MenuItem{
		{Key: "sidebar_create_canvas", Label: "Canvas", Subtitle: "Create and share content", Icon: "add-channel-canvas", Do: f.newCanvas},
		{Key: "sidebar_create_list", Label: "List", Subtitle: "Track and manage projects", Icon: "lists", Do: f.newList},
	}
	// U && D: custom_file_sections and the FileCollectionsCreate
	// permission (Clack's accounts have it)
	if a != nil && a.FileSectionsOn() {
		items = append(items, popups.MenuItem{Key: "sidebar_create_section", Label: "Sidebar section", Subtitle: "Group and organize your files",
			Icon: "section", Do: func() { f.sectionModal("") }})
	}
	return items
}

// gearMenuItems is the gear's menu (e5): Bulk manage files with
// custom_file_sections, not on a free plan.
func (f *Files) gearMenuItems() []popups.MenuItem {
	a := f.h.Core()
	if a == nil || !a.FileSectionsOn() || a.OnFreePlan() {
		return nil
	}
	return []popups.MenuItem{{Key: "bulk-manage-files", Label: "Bulk manage files", Do: f.startOrganize}}
}

// newMenuItems is UnifiedFilesCreateMenu's template for the section, the
// rows Clack can do.
func (f *Files) newMenuItems() []popups.MenuItem {
	canvas := popups.MenuItem{Key: "new_canvas", Label: "New canvas", Do: f.newCanvas}
	list := popups.MenuItem{Key: "new_list", Label: "New list", Do: f.newList}
	switch f.kind() {
	case "canvases":
		return []popups.MenuItem{canvas}
	case "lists":
		return []popups.MenuItem{list}
	}
	return []popups.MenuItem{canvas, list}
}

// newCanvas is createAndOpenSlackDoc from the Files tab: a standalone
// canvas, opened.
func (f *Files) newCanvas() {
	if cc, ok := f.h.(interface{ CreateCanvas() }); ok {
		cc.CreateCanvas()
	}
}

// newList is createAndOpenList: lists.create from the blank template (in
// to-do mode under lists_grid_todos), then the list opens.
func (f *Files) newList() {
	var id string
	f.write(func(ctx context.Context, a *core.App) error {
		var err error
		id, err = a.CreateListWith(ctx, core.CreateListOpts{TemplateID: "blank", TodoMode: a.Experiment("lists_grid_todos") == "on"})
		return err
	}, func(err error) {
		if err != nil {
			f.h.Toast("Couldn't create the list: " + friendlyErr(err))
			return
		}
		if lo, ok := f.h.(interface{ OpenListFull(id string) }); ok {
			lo.OpenListFull(id)
		}
	})
}

// openMenuAt opens items at a Popover position next to btn.
func (f *Files) openMenuAt(items []popups.MenuItem, btn image.Rectangle, pos string, width float32) {
	ph, ok := f.h.(popups.Host)
	if !ok || len(items) == 0 {
		return
	}
	ph.ShowOverlay(popups.NewMenuOpts(ph, items, popups.MenuOpts{Anchor: btn, Width: width, Popover: pos}))
}

// headerButton is the sidebar header's sidebar-header-button: 28px,
// radius 8 (viewHeaderActionButton), the 20px icon in
// theme-content-inv-sec, theme-content-inv-pry on theme-surf-inv-ter
// under the pointer.
func (f *Files) headerButton(gtx C, c *widget.Clickable, tip *popups.TipState, icon, label string, sc sbColors) D {
	return f.sidebarIconButton(gtx, c, tip, icon, label, 20, sc.text, sc)
}

// sidebarIconButton is a 28px themed IconButton with an iconSize icon in
// fg (theme-content-inv-pry under the pointer) and its tooltip below.
func (f *Files) sidebarIconButton(gtx C, c *widget.Clickable, tip *popups.TipState, icon, label string, iconSize unit.Dp, fg0 color.NRGBA, sc sbColors) D {
	d := c.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		s := gtx.Dp(28)
		fg := fg0
		if c.Hovered() {
			rrect(gtx, image.Rect(0, 0, s, s), gtx.Dp(8), sc.hover)
			fg = sc.pry
		}
		g := gtx
		g.Constraints = layout.Exact(image.Pt(s, s))
		layout.Center.Layout(g, func(gtx C) D { return f.icon(gtx, icon, iconSize, fg) })
		return D{Size: image.Pt(s, s)}
	})
	if label != "" {
		popups.Tooltip(gtx, f.th, tip, c.Hovered(), d.Size, popups.Below, label, "")
	}
	return d
}
