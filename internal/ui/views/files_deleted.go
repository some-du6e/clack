package views

// The Files sidebar's Deleted page: Slack's DeletedUnifiedFilesView
// (gantry-v2-async-client-deleted-unified-files-view, CDN build
// 4d0c78085232):
//
//   - DeletedFilesPage: the header (.header__N2UKb, 13px padding) holds
//     "Deleted" (18px, 900) and, while there are rows, "You can restore
//     deleted canvases or lists within 24 hours of deletion." (13px, 12px
//     after the title). No search, filters or templates.
//   - DeletedFilesList loads files.recentlyDeleted, keeps isDeletedFile's
//     canvases ("quip") and lists, the latest deletion first. Each
//     DeletedFileEntity is a c-pillow_file row: the trash icon in
//     c-file_entity__view_only_icon (32px, radius 4, foreground .04, the
//     20px icon at .7), the title and "Deleted {ago}" (toTimeAgo), and at
//     top 12 / right 12 the c-file__actions--canvas-ia4 button (always
//     shown): MessageActionButton, the 18px undo icon, its label "Restore
//     canvas" / "Restore list" as the tooltip.
//   - The button opens UndeleteFileDialog: "Restore canvas" / "Are you
//     sure you want to restore this canvas?", the row again (the canvas or
//     lists icon), Cancel and "Restore canvas" (focused). Its undeleteFile
//     is files.undelete {file_id}; on success the row goes and the toast
//     says "{title} restored." with View (the canvas in the main view, the
//     list); a failure toasts "Oops! We couldn’t restore that canvas".
//   - Nothing deleted: "Nothing to see here" / "You haven’t deleted any
//     canvases or lists recently."
//   - The Files sidebar loads files.recentlyDeleted when it mounts; its
//     Deleted item shows while that has rows, or always under
//     file_trash_updates "on", which also refetches the list on
//     file_deleted and drops rows on file_undeleted.

import (
	"context"
	"encoding/json"
	"image"
	"strconv"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// filesDeleted is the Deleted page's state.
type filesDeleted struct {
	data    async[[]core.DeletedFile]
	gone    map[string]bool // restored this load
	restore map[string]*widget.Clickable
	tips    map[string]*popups.TipState
	sub     bool // following file_deleted / file_undeleted
}

// trashEventIDs is getFileIdsFromMsg: file_ids, else [file_id].
func trashEventIDs(raw json.RawMessage) []string {
	var e struct {
		FileID  string   `json:"file_id"`
		FileIDs []string `json:"file_ids"`
	}
	_ = json.Unmarshal(raw, &e)
	if e.FileIDs != nil {
		return e.FileIDs
	}
	if e.FileID != "" {
		return []string{e.FileID}
	}
	return nil
}

// followTrash subscribes to core's file_trash events (once).
func (f *Files) followTrash() {
	if f.del.sub || f.h.Core() == nil {
		return
	}
	f.del.sub = true
	ch, _ := f.h.Core().Subscribe()
	go func() {
		for ev := range ch {
			if ev.Type != "file_trash" {
				continue
			}
			name, ids := ev.Name, trashEventIDs(ev.Raw)
			f.post(func() { f.onTrashEvent(name, ids) })
		}
	}()
}

// onTrashEvent is filesDeleted / filesUndeleted under file_trash_updates
// (boot-render zuQH): a deletion invalidates the deleted list
// (invalidateDeletedFiles: DeletedFilesList fetches it again), a restore
// drops the rows (removeRecentlyDeletedFilesByIds). Without the
// experiment the list waits for the next load.
func (f *Files) onTrashEvent(name string, ids []string) {
	if c := f.h.Core(); c == nil || c.Experiment("file_trash_updates") != "on" || len(ids) == 0 {
		return
	}
	switch name {
	case "file_deleted":
		f.startDeleted()
	case "file_undeleted":
		for _, id := range ids {
			f.del.gone[id] = true
		}
	}
}

func (d *filesDeleted) btn(id string) (*widget.Clickable, *popups.TipState) {
	if d.restore == nil {
		d.restore, d.tips = map[string]*widget.Clickable{}, map[string]*popups.TipState{}
	}
	c := d.restore[id]
	if c == nil {
		c, d.tips[id] = new(widget.Clickable), new(popups.TipState)
		d.restore[id] = c
	}
	return c, d.tips[id]
}

// rows are the loaded rows not restored since.
func (d *filesDeleted) rows() ([]core.DeletedFile, bool, error) {
	all, ok, err := d.data.get()
	out := make([]core.DeletedFile, 0, len(all))
	for _, f := range all {
		if !d.gone[f.ID] {
			out = append(out, f)
		}
	}
	return out, ok, err
}

// deletedNavShown is the sidebar's Deleted item (and the divider above
// it): visible while recentlyDeletedFiles has rows, or always under
// file_trash_updates (client-unified-files-sidebar-view w695).
func (f *Files) deletedNavShown() bool {
	if c := f.h.Core(); c != nil && c.Experiment("file_trash_updates") == "on" {
		return true
	}
	rows, _, _ := f.del.rows()
	return len(rows) > 0
}

// startDeleted loads files.recentlyDeleted (pageFileType none: both).
func (f *Files) startDeleted() {
	a := f.h.Core()
	f.followTrash()
	f.del.gone = map[string]bool{}
	f.del.data.load(f.h, false, func(ctx context.Context) ([]core.DeletedFile, error) { return a.RecentlyDeleted(ctx, "") })
}

// deletedNoun is {fileType, select, quip {canvas} other {list}}.
func deletedNoun(fileType string) string {
	if fileType == "quip" {
		return "canvas"
	}
	return "list"
}

// deletedAgo is toTimeAgo's en-US past ("< 1 minute ago", "1 minute
// ago", "N minutes ago", "1 hour ago", "N hours ago", "1 day ago", "N
// days ago").
func deletedAgo(t, now time.Time) string {
	s := now.Sub(t).Seconds()
	if s < 0 {
		s = -s
	}
	m, h, d := s/60, s/3600, s/86400
	round := func(v float64) string { return strconv.Itoa(int(v + 0.5)) }
	switch {
	case s < 45:
		return "< 1 minute ago"
	case s < 90:
		return "1 minute ago"
	case m < 45:
		return round(m) + " minutes ago"
	case m < 90:
		return "1 hour ago"
	case h < 24:
		return round(h) + " hours ago"
	case d < 2:
		return "1 day ago"
	}
	return round(d) + " days ago"
}

// deletedUpdate handles the rows' Restore buttons.
func (f *Files) deletedUpdate(gtx C) {
	rows, _, _ := f.del.rows()
	for _, r := range rows {
		if c, _ := f.del.btn(r.ID); c.Clicked(gtx) {
			f.confirmRestore(r)
		}
	}
}

// confirmRestore is UndeleteFileDialog.
func (f *Files) confirmRestore(r core.DeletedFile) {
	ph, ok := f.h.(popups.Host)
	if !ok {
		return
	}
	noun := deletedNoun(r.FileType)
	popups.Confirm(ph, "Restore "+noun, "Are you sure you want to restore this "+noun+"?", "Restore "+noun, false,
		func(gtx C) D {
			return layout.Inset{Top: 4}.Layout(gtx, func(gtx C) D { return f.deletedEntity(gtx, r, true, true, nil, nil) })
		}, func() { f.restoreFile(r) })
}

// restoreFile is undeleteFile, then onSuccessfulUndelete.
func (f *Files) restoreFile(r core.DeletedFile) {
	f.write(func(ctx context.Context, a *core.App) error { return a.UndeleteFile(ctx, r.ID) }, func(err error) {
		if err != nil {
			f.h.Toast("Oops! We couldn’t restore that " + deletedNoun(r.FileType))
			return
		}
		f.del.gone[r.ID] = true
		msg := r.Title + " restored."
		view := func() { f.viewRestored(r) }
		if t, ok := f.h.(interface {
			ToastAction(text, label string, do func())
		}); ok {
			t.ToastAction(msg, "View", view)
		} else {
			f.h.Toast(msg)
		}
	})
}

// viewRestored is the toast's View: the canvas in the main view, the list.
func (f *Files) viewRestored(r core.DeletedFile) {
	ft := "list"
	if r.FileType == "quip" {
		ft = "quip"
	}
	openFile(f.h, fileItem{core.File{ID: r.ID, Title: r.Title, Filetype: ft, Mode: ft}})
}

// deletedHeader is DeletedFilesPage's header row.
func (f *Files) deletedHeader(gtx C) D {
	rows, _, _ := f.del.rows()
	g := gtx
	g.Constraints.Min.Y = gtx.Dp(56)
	return layout.Inset{Top: 13, Bottom: 13, Left: 24, Right: 16}.Layout(g, func(gtx C) D {
		return row(gtx, layout.Middle,
			layout.Rigid(func(gtx C) D { return f.label(gtx, 18, wBlack, f.p.Text, "Deleted") }),
			hgap(12),
			layout.Flexed(1, func(gtx C) D {
				if len(rows) == 0 {
					return D{}
				}
				return f.label(gtx, 13, wReg, f.p.Text, "You can restore deleted canvases or lists within 24 hours of deletion.")
			}),
		)
	})
}

// deletedBody is DeletedFilesList.
func (f *Files) deletedBody(gtx C) D {
	rows, loaded, err := f.del.rows()
	switch {
	case !loaded:
		return layout.Inset{Left: 35, Right: 35}.Layout(gtx, func(gtx C) D { return f.skeleton(gtx, 6, 36) })
	case err != nil && len(rows) == 0:
		g := gtx
		g.Constraints.Min.Y = gtx.Dp(300)
		return f.errorState(g, err, &f.retry)
	case len(rows) == 0:
		g := gtx
		g.Constraints.Min.Y = gtx.Dp(300)
		return f.emptyState(g, "", "Nothing to see here", "You haven’t deleted any canvases or lists recently.", nil, "")
	}
	return f.scrollList(gtx, &f.list, len(rows), func(gtx C, i int) D {
		bottom := unit.Dp(0)
		if i == len(rows)-1 {
			bottom = 8
		}
		c, tip := f.del.btn(rows[i].ID)
		return layout.Inset{Left: 35, Right: 35, Bottom: bottom}.Layout(gtx, func(gtx C) D {
			return f.deletedEntity(gtx, rows[i], i == 0, i == len(rows)-1, c, tip)
		})
	})
}

// deletedEntity is one DeletedFileEntity row (the dialog's copy without
// the Restore button: c nil).
func (f *Files) deletedEntity(gtx C, r core.DeletedFile, first, last bool, c *widget.Clickable, tip *popups.TipState) D {
	bw := max(1, gtx.Dp(1))
	h := gtx.Dp(65)
	if last {
		h += bw
	}
	w := min(gtx.Constraints.Max.X, gtx.Dp(1000))
	rr := image.Rectangle{Max: image.Pt(w, h)}
	rad := gtx.Dp(12)
	var tr, br int
	if first {
		tr = rad
	}
	if last {
		br = rad
	}
	in := image.Rect(bw, bw, w-bw, h)
	if last {
		in.Max.Y -= bw
	}
	func() {
		defer clip.RRect{Rect: rr, NW: tr, NE: tr, SW: br, SE: br}.Push(gtx.Ops).Pop()
		fill(gtx, rr, theme.Over(f.p.CardLine, f.p.Surface))
	}()
	func() {
		defer clip.RRect{Rect: in, NW: max(tr-bw, 0), NE: max(tr-bw, 0), SW: max(br-bw, 0), SE: max(br-bw, 0)}.Push(gtx.Ops).Pop()
		fill(gtx, in, f.p.Surface)
	}()
	icon := "trash"
	if c == nil { // UndeleteFileDialog: the file's own icon
		icon = "lists"
		if r.FileType == "quip" {
			icon = "canvas"
		}
	}
	actW := 0
	if c != nil {
		actW = gtx.Dp(32 + 12)
	}
	func() {
		defer op.Offset(image.Pt(gtx.Dp(12), bw)).Push(gtx.Ops).Pop()
		g := gtx
		g.Constraints = layout.Exact(image.Pt(w-gtx.Dp(24)-actW, gtx.Dp(64)))
		row(g, layout.Middle,
			layout.Rigid(func(gtx C) D {
				// c-file_entity__view_only_icon
				s := gtx.Dp(32)
				rrect(gtx, image.Rectangle{Max: image.Pt(s, s)}, gtx.Dp(4), theme.WithAlpha(f.p.Text, 0x0a))
				defer op.Offset(image.Pt((s-gtx.Dp(20))/2, (s-gtx.Dp(20))/2)).Push(gtx.Ops).Pop()
				f.icon(gtx, icon, 20, theme.WithAlpha(f.p.Text, 0xb3))
				return D{Size: image.Pt(s, s)}
			}),
			hgap(8),
			layout.Flexed(1, func(gtx C) D {
				return col(gtx,
					layout.Rigid(func(gtx C) D { return f.label(gtx, 15, wBold, f.p.Text, r.Title) }),
					layout.Rigid(func(gtx C) D {
						return layout.Inset{Top: 2}.Layout(gtx, func(gtx C) D {
							return f.label(gtx, 13, wReg, f.p.Meta, "Deleted "+deletedAgo(time.Unix(r.DateDelete, 0), time.Now()))
						})
					}),
				)
			}),
		)
	}()
	if c != nil {
		// c-file__actions: top 12, right 12; MessageActionButton (32px,
		// content-sec at .75, hover foreground .04 and the primary colour)
		s := gtx.Dp(32)
		func() {
			defer op.Offset(image.Pt(w-gtx.Dp(12)-s, gtx.Dp(12))).Push(gtx.Ops).Pop()
			d := c.Layout(gtx, func(gtx C) D {
				pointerCursor(gtx)
				col := theme.WithAlpha(f.p.Meta, 0xbf)
				if c.Hovered() {
					rrect(gtx, image.Rectangle{Max: image.Pt(s, s)}, gtx.Dp(8), theme.WithAlpha(f.p.Text, 0x0a))
					col = f.p.Text
				}
				defer op.Offset(image.Pt((s-gtx.Dp(18))/2, (s-gtx.Dp(18))/2)).Push(gtx.Ops).Pop()
				f.icon(gtx, "undo", 18, col)
				return D{Size: image.Pt(s, s)}
			})
			popups.Tooltip(gtx, f.th, tip, c.Hovered(), d.Size, popups.Above, "Restore "+deletedNoun(r.FileType), "")
		}()
	}
	return D{Size: rr.Size()}
}
