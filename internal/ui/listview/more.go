package listview

// The list header's More actions (⋮, "More list actions"): Slack's
// MoreActionsMenu (client-boot-shared 57SI), with the items Clack can
// carry out, in Slack's order and under Slack's labels:
//
//	Add to starred / Remove from starred ─
//	Save for later / Remove from Later (Add to saved / Remove from Saved
//	under later_todos) ─
//	Copy link to list · Make a copy · Download (lists_export_enrichment:
//	the download modal) or Download CSV ▸ (Download without / with
//	archived items) ─
//	Open in split view (the full view) ─
//	Delete list (the list's creator)
//
// Clack's header has no star or share buttons, so the star row shows in
// every mode (Slack's shouldShowMinWidthActions); Share list, Details,
// Workflows, version history, the archive, templates and Report to Slack
// have no screen in Clack and are left out, as is Add to-do (to-do state
// isn't loaded for files).

import (
	"context"
	"errors"
	"strings"
	"time"

	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

func (v *View) openMore() {
	if v.act.Menu == nil {
		return
	}
	v.act.Menu(v.moreItems())
}

// moreItems is the menu for the list as loaded.
func (v *View) moreItems() []MenuItem {
	d := v.Doc()
	a := v.h.Core()
	var items []MenuItem
	sep := func() {
		if n := len(items); n > 0 && !items[n-1].Divider {
			items = append(items, MenuItem{Divider: true})
		}
	}
	if d == nil || a == nil {
		if v.mode == Full && v.act.SideView != nil {
			items = append(items, MenuItem{Label: nsLists.T("Open in split view"), Icon: "panel-right", Do: v.act.SideView})
		}
		return items
	}
	id := v.id
	// star_list
	starred := a.FileStarred(id)
	star := nsLists.T("Add to starred")
	if starred {
		star = nsLists.T("Remove from starred")
	}
	items = append(items, MenuItem{Label: star, Do: func() {
		v.write(func(ctx context.Context, a *core.App) error { return a.StarFile(ctx, id, !starred) })
	}})
	sep()
	// save_for_later: addCanvasToSaved / removeFromSaved
	saved := a.FileSaved(id)
	todos := a.Experiment("later_todos") == "on"
	label, icon := nsLists.T("Save for later"), "bookmark"
	switch {
	case todos && saved:
		label, icon = nsLists.T("Remove from Saved"), "bookmark-filled"
	case todos:
		label = nsLists.T("Add to saved")
	case saved:
		label, icon = nsLists.T("Remove from Later"), "bookmark-filled"
	}
	items = append(items, MenuItem{Label: label, Icon: icon, Do: func() {
		v.write(func(ctx context.Context, a *core.App) error {
			if saved {
				return a.UnsaveFile(ctx, id)
			}
			return a.SaveFile(ctx, id)
		})
	}})
	sep()
	if link := d.Info.Permalink; link != "" {
		items = append(items, MenuItem{Label: nsLists.T("Copy link to list"), Do: func() {
			if v.act.Copy != nil {
				v.act.Copy(link)
			}
		}})
	}
	if !d.Info.Template {
		items = append(items, MenuItem{Label: nsLists.T("Make a copy"), Do: func() { v.makeCopy(d) }})
		// MoreActionsMenu: !isTemplate → Download, or Download CSV ▸
		if v.exp("lists_export_enrichment") {
			items = append(items, MenuItem{Label: nsLists.T("Download"), Do: func() { v.openDownload(d) }})
		} else {
			items = append(items, MenuItem{Label: nsLists.T("Download CSV"), Sub: []MenuItem{
				{Label: nsLists.T("Download without archived items"), Do: func() { v.download(d, core.ListDownload{}) }},
				{Label: nsLists.T("Download with archived items"), Do: func() { v.download(d, core.ListDownload{Archived: true}) }},
			}})
		}
	}
	if v.mode == Full && v.act.SideView != nil {
		sep()
		items = append(items, MenuItem{Label: nsLists.T("Open in split view"), Icon: "panel-right", Do: v.act.SideView})
	}
	// delete_list: the list's creator (canDeleteFile)
	if self := a.Self().ID; self != "" && d.Info.Creator == self {
		sep()
		items = append(items, MenuItem{Label: nsLists.T("Delete list"), Danger: true, Do: func() { v.confirmDelete(d) }})
	}
	return items
}

// write runs f off the UI goroutine and redraws; a failure is a toast.
func (v *View) write(f func(ctx context.Context, a *core.App) error) {
	a := v.h.Core()
	if a == nil {
		return
	}
	go func() {
		ctx, cancel := ctxTimeout(30 * time.Second)
		defer cancel()
		if err := f(ctx, a); err != nil {
			v.h.Toast("Something went wrong: " + err.Error())
		}
		v.h.Invalidate()
	}()
}

// makeCopy is Make a copy: createList {name: "Copy of {title}",
// descriptionBlocks, icon, copyFromListId, reason:
// "create-list-from-copy-list-menu"}; the copy opens.
func (v *View) makeCopy(d *core.ListDoc) {
	name := nsLists.T("Copy of Untitled")
	if d.Info.Title != "" {
		name = nsLists.T("Copy of {title}", "title", d.Info.Title)
	}
	o := core.CreateListOpts{Name: name, CopyFromListID: d.Info.ID, Icon: d.Meta.Icon, DescriptionBlocks: d.Meta.DescriptionBlocks,
		Reason: "create-list-from-copy-list-menu"}
	v.write(func(ctx context.Context, a *core.App) error {
		id, err := a.CreateListWith(ctx, o)
		if err != nil {
			return err
		}
		v.post(func() { v.h.Navigate("lists/" + id) })
		return nil
	})
}

// download is downloadFile for the list: the file comes from
// ListDownloadURL into the Downloads folder, then Slack's toast.
func (v *View) download(d *core.ListDoc, o core.ListDownload) {
	a := v.h.Core()
	ph, ok := v.h.(popups.Host)
	if a == nil || !ok {
		return
	}
	src := a.ListDownloadURL(d.Info, o)
	kind := "CSV"
	ext := ".csv"
	if o.JSON {
		kind, ext = "JSON", ".json"
	}
	if src == "" {
		v.h.Toast(nsLists.T(kind + " download failed")) // DownloadModal's "CSV download failed" / "JSON download failed"
		return
	}
	name := strings.TrimSuffix(firstNonEmptyStr(d.Info.Name, d.Info.Title, "list"), ext) + ext
	popups.SaveDownload(ph, src, name, func(_ string, err error) {
		if errors.Is(err, popups.ErrDownloadCanceled) {
			return // the save dialog was closed
		}
		if err != nil {
			v.h.Toast(nsLists.T(kind + " download failed"))
			return
		}
		v.h.Toast(nsLists.T(kind + " downloaded successfully"))
	})
}

func firstNonEmptyStr(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

// downloadModal is Slack's DownloadModal (lists_export_enrichment):
// "Download {title}", three switches (Include archived items, Include
// message threads, Include item attachments), the hint "Threads and
// attachments require JSON download", and Cancel, Download JSON and
// Download CSV (disabled while threads or attachments are on).
type downloadModal struct {
	archived, threads, attachments bool
	sw                             [3]widget.Clickable
}

func (v *View) openDownload(d *core.ListDoc) {
	ph, ok := v.h.(popups.Host)
	if !ok {
		return
	}
	m, buttons := v.downloadModal(d)
	mod := popups.NewModal(ph, popups.ModalOpts{
		Title: nsLists.T("Download {title}", "title", d.Info.Title), // DownloadModal
		Body: func(gtx layout.Context) layout.Dimensions {
			return m.layout(gtx, v.h.Theme())
		},
		Buttons: buttons,
	})
	ph.ShowOverlay(mod)
}

// downloadModal is the modal's state and its footer: Cancel, Download
// JSON (with every switch), Download CSV (archived items only; disabled
// while threads or attachments are on).
func (v *View) downloadModal(d *core.ListDoc) (*downloadModal, []*popups.ModalButton) {
	m := &downloadModal{}
	jsonBtn := &popups.ModalButton{Label: nsLists.T("Download JSON"), Kind: popups.BtnPrimary, Do: func() bool {
		v.download(d, core.ListDownload{JSON: true, Archived: m.archived, Threads: m.threads, Attachments: m.attachments})
		return true
	}}
	csvBtn := &popups.ModalButton{Label: nsLists.T("Download CSV"), Kind: popups.BtnPrimary,
		Enabled: func() bool { return !m.threads && !m.attachments },
		Do: func() bool {
			v.download(d, core.ListDownload{Archived: m.archived})
			return true
		}}
	return m, []*popups.ModalButton{{Label: nsLists.T("Cancel"), Kind: popups.BtnOutline}, jsonBtn, csvBtn}
}

// layout draws the switches (a 28x16 switch, 8px, the label) 12px apart
// and the hint.
func (m *downloadModal) layout(gtx layout.Context, th *theme.Theme) layout.Dimensions {
	rows := []struct {
		on    *bool
		label string
	}{{&m.archived, nsLists.T("Include archived items")}, {&m.threads, nsLists.T("Include message threads")}, {&m.attachments, nsLists.T("Include item attachments")}}
	var ch []layout.FlexChild
	for i, r := range rows {
		i, r := i, r
		if i > 0 {
			ch = append(ch, layout.Rigid(layout.Spacer{Height: unit.Dp(12)}.Layout))
		}
		ch = append(ch, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			clk := &m.sw[i]
			if clk.Clicked(gtx) {
				*r.on = !*r.on
			}
			return clk.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return popups.Toggle(gtx, th, *r.on, false) }),
					layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return th.Label(gtx, 15, theme.Regular, th.C.Fg, r.label)
					}),
				)
			})
		}))
	}
	ch = append(ch, layout.Rigid(layout.Spacer{Height: unit.Dp(12)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return th.Text(gtx, theme.LabelOpts{Font: theme.Font(theme.Regular), Size: 13, Color: th.C.FgMuted},
				nsLists.T("Threads and attachments require JSON download"))
		}))
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, ch...)
}

// confirmDelete is DeleteFileDialog for the list: "Delete list" / "Are
// you sure you want to delete this list?" / "Delete list"; then
// files.delete, and the view leaves the list.
func (v *View) confirmDelete(d *core.ListDoc) {
	ph, ok := v.h.(popups.Host)
	if !ok {
		return
	}
	id := d.Info.ID
	popups.Confirm(ph, nsLists.T("Delete list"), nsLists.T("Are you sure you want to delete this list?"), nsLists.T("Delete list"), true, nil, func() {
		v.write(func(ctx context.Context, a *core.App) error {
			if err := a.DeleteFile(ctx, id); err != nil {
				v.h.Toast(nsLists.T("Oops! We couldn’t delete that file"))
				return nil
			}
			v.post(func() {
				if v.act.Close != nil {
					v.act.Close()
				}
			})
			return nil
		})
	}).OldDialog()
}
