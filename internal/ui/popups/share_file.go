package popups

import (
	"context"
	"image"
	"strconv"
	"strings"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The share dialog for a file (FileActions onShare → openShareDialog
// {fileId}: the share-dialog chunk zUYy, CDN build 0e695dd18302): the
// forward dialog's picker, note and footer (actions_forward.go) with the
// file's own parts:
//
//   - the title e3: "Share this file";
//   - one destination (selectedChannelId): a pick replaces the chip;
//   - the preview (ShareDialogPreview with the file) over
//     renderFileAccessMetadata: .p-share_dialog__file_access_metadata
//     (surf-pry, 12px padding, the bottom corners 8) with a spinner until
//     files.getOverview answers (getFileOverviewAndUpdateFile on mount),
//     then "{accessInformation} has / have access to this file.": "{n}
//     user(s)" (at least 1), "{n} channel(s)", "{n} file(s)", each
//     "{count}+ …" (99) when max-counted;
//   - the footer: ShareDialogCopyLinkButton with renderAsLink (the link
//     icon 4px before "Copy Link", tip "You can also copy a link to this
//     file. To share, paste it in any channel or conversation.", "Copied!"
//     once clicked; no toast for a file) on the left; Share (Forward with
//     private_message_forwarding) on the right. The schedule caret is the
//     message forward's (renderForwardActions) and Save Draft
//     (maybeRenderDraftButton) would attach the file to a draft, which
//     Clack's drafts don't carry: neither is drawn here.
//   - Share is shareMessageOrFile: files.share (core.ShareFileTo) to the
//     destination, then navigate to it at the answer's ts or file_msg_ts.

// ShareFileOpts is the file the dialog shares.
type ShareFileOpts struct {
	ID, Title, Permalink string
	// Preview draws ShareDialogPreview's file entity (the caller's file
	// tile and title), at the dialog's width.
	Preview layout.Widget
	// Shared runs after files.share answers, with the destination and
	// the message ts; the caller navigates there.
	Shared func(ch, ts string)
}

var nsShareDlg = i18n.NS("share-dialog")

// NewShareFileModal builds the share dialog for a file. Show it with
// h.ShowOverlay.
func NewShareFileModal(h Host, o ShareFileOpts) *Modal { return newShareFile(h, o).modal }

func newShareFile(h Host, o ShareFileOpts) *actForward {
	f := &actForward{h: h, focusTo: true, hov: -1, file: &o}
	f.to.SingleLine = true
	f.to.Submit = true
	f.sugList.Axis = layout.Vertical
	f.link = o.Permalink
	f.all = actFwdTargets(h)
	v2 := func() bool { return expOn(h, ExpSlackKitV2Button) }
	label := nsShare.T("Share")
	if privateForwarding(h.Core()) {
		label = nsShare.T("Forward")
	}
	f.fwd = &ModalButton{Label: label, Kind: BtnPrimary, Enabled: f.canGo, V2: v2, Do: func() bool {
		f.shareFile()
		return true
	}}
	f.copy = &ModalButton{Label: nsShare.T("Copy Link"), Kind: BtnOutline, Left: true, Draw: f.drawCopyLink, Do: func() bool {
		f.copyLink = true
		return false
	}, Tip: f.copyTip, TipHidden: func() { f.copied = false }}
	f.modal = NewModal(h, ModalOpts{
		TitleFunc: f.title,
		Body:      f.layout,
		Buttons:   []*ModalButton{f.copy, f.fwd},
		FooterPad: 24,
	})
	id := o.ID
	if a := h.Core(); a != nil && id != "" {
		run(h, func(ctx context.Context) (core.FileOverview, error) { return a.FileOverview(ctx, id) },
			func(ov core.FileOverview, err error) {
				f.overview, f.overviewDone = ov, true // the .finally: loaded either way
				h.Invalidate()
			})
	} else {
		f.overviewDone = true
	}
	return f
}

// drawCopyLink is ShareDialogCopyLinkButton with renderAsLink: a link
// button (c-link--button: link colour, underlined on hover) with the
// link icon 4px before the label.
func (f *actForward) drawCopyLink(gtx layout.Context, th *theme.Theme, c *widget.Clickable, kind BtnKind, ring bool, grp grpSide) layout.Dimensions {
	cl := colorsOf(th)
	label := nsShare.T("Copy Link")
	tw := textW(gtx, th, 15, theme.Regular, label)
	is := dp(gtx, 18)
	h := dp(gtx, 36)
	sz := image.Pt(is+dp(gtx, 4)+tw, h)
	return c.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		at(gtx, image.Pt(0, (h-is)/2), func(gtx layout.Context) layout.Dimensions { return icon(gtx, "link", 18, cl.Link) })
		at(gtx, image.Pt(is+dp(gtx, 4), 0), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 15, theme.Regular, cl.Link, label)
			})
		})
		if c.Hovered() {
			uy := (h+dp(gtx, 15))/2 + dp(gtx, 1)
			fill(gtx, image.Rect(is+dp(gtx, 4), uy, sz.X, uy+border1(gtx)), cl.Link)
		}
		if ring {
			focusRing(gtx, cl, image.Rectangle{Max: sz}, dp(gtx, 4))
		}
		pointerCursor(gtx, sz)
		return layout.Dimensions{Size: sz}
	})
}

// fileAccessText is renderFileAccessMetadata's sentence.
func fileAccessText(ov core.FileOverview) string {
	users := max(ov.Users, 1) // share_metadata.users.count || 1
	var parts []string
	count := func(n int, maxed bool, one, other, plus string) {
		if n == 0 {
			return
		}
		if maxed {
			parts = append(parts, nsShareDlg.T(plus, "count", "99"))
			return
		}
		s := other
		if n == 1 {
			s = one
		}
		parts = append(parts, nsShareDlg.T(s, "count", strconv.Itoa(n)))
	}
	count(users, ov.MaxUsers, "{count} user", "{count} users", "{count}+ users")
	count(ov.Channels, ov.MaxChannels, "{count} channel", "{count} channels", "{count}+ channels")
	count(ov.Files, ov.MaxFiles, "{count} file", "{count} files", "{count}+ files")
	info := ""
	switch len(parts) {
	case 1:
		info = parts[0]
	case 2:
		info = parts[0] + " and " + parts[1]
	default:
		info = strings.Join(parts[:len(parts)-1], ", ") + " and " + parts[len(parts)-1]
	}
	n := users + ov.Channels + ov.Files
	if n == 1 {
		return nsShareDlg.T("{accessInformation} has access to this file.", "accessInformation", info)
	}
	return nsShareDlg.T("{accessInformation} have access to this file.", "accessInformation", info)
}

// filePreview is ShareDialogPreview's file over the access line.
func (f *actForward) filePreview(gtx layout.Context, th *theme.Theme, cl *Colors) layout.Dimensions {
	w := gtx.Constraints.Max.X
	bw := border1(gtx)
	rad := dp(gtx, 8)
	call, d := record(loose(gtx, image.Pt(w-2*bw, 1<<20)), func(gtx layout.Context) layout.Dimensions {
		if f.file.Preview == nil {
			return layout.Dimensions{}
		}
		return f.file.Preview(gtx)
	})
	line := dp(gtx, 20)
	pad := dp(gtx, 12)
	stripH := 2*pad + line
	h := bw + d.Size.Y + stripH + bw
	r := image.Rect(0, 0, w, h)
	rrect(gtx, r, rad, cl.Ring)
	rrect(gtx, r.Inset(bw), rad-bw, cl.Bg)
	macroAt(gtx, call, image.Pt(bw, bw))
	sr := image.Rect(bw, bw+d.Size.Y, w-bw, h-bw)
	func() {
		defer clip.RRect{Rect: sr, SW: rad - bw, SE: rad - bw}.Push(gtx.Ops).Pop()
		fill(gtx, sr, woTok(th, "surf-pry"))
	}()
	at(gtx, image.Pt(sr.Min.X+pad, sr.Min.Y+pad), func(gtx layout.Context) layout.Dimensions {
		if !f.overviewDone {
			return abSpinner(gtx, dp(gtx, 20), cl.Fg3) // Spinner size medium
		}
		gtx.Constraints = layout.Constraints{Max: image.Pt(sr.Dx()-2*pad, line)}
		return vcenter(gtx, line, func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, 15, theme.Regular, cl.Fg, fileAccessText(f.overview))
		})
	})
	return layout.Dimensions{Size: image.Pt(w, h)}
}

// shareFile is onGo for a file: files.share to the destination, then
// navigate there (shareMessageOrFile's then).
func (f *actForward) shareFile() {
	if len(f.chips) == 0 {
		return
	}
	ch, id := f.chips[0].t.id, f.file.ID
	note := strings.TrimSpace(f.note.Text())
	a := f.h.Core()
	if a == nil {
		f.h.Toast("Not connected yet")
		return
	}
	done := f.file.Shared
	run(f.h, func(ctx context.Context) (string, error) { return a.ShareFileTo(ctx, id, ch, note) },
		func(ts string, err error) {
			if err != nil {
				f.h.Toast("Couldn't do that: " + errText(err)) // Clack's write error (do)
				return
			}
			if done != nil {
				done(ch, ts)
			}
		})
}
