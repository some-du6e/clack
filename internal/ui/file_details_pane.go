package ui

import (
	"context"
	"image"
	"image/color"
	"strings"
	"time"

	"gioui.org/layout"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/messages"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/views"
)

// A file's FileDetails (boot-shared eg5B FileDetailsWithSubscription,
// build d4845cd3e2ea), for any file but an email (email_pane.go): the
// media viewer's Details tab draws it with its header hidden (bDav's
// flexpaneHeaderProps {hideHeader: true}: FileDetailsPane), and the
// secondary view shows it with the header (toFileDetails:
// OpenFileDetails), the gate's fallback for "View file details".
//
//   - The flexpane (VWbZ, p-file_flexpane--detail): the header is the
//     title "File" (eg5B render's titleContent for a file) and the close
//     button, with no rule under it (hideBottomBorder); hidden, there's
//     neither. The body scrolls; the footer follows it.
//   - Its data: files.info as fetchFileById sends it for jruc
//     withFileSubscription (core.FileDetails), on creation (the viewer asks
//     for a new pane when its file changes). Until it answers the pane
//     shows the opener's copy, the store's file in Slack; one it never had
//     draws nothing while it loads (isFileFetching), and FileDetailsError
//     if the read fails ("Couldn’t load file", "If the problem persists,
//     please drop us a line.", Try Again reading it again). A deleted
//     file is FileDetailsTombstone (trash, "This file was deleted."), a
//     missing one FileDetailsNotFound (emoji-missing-filled, "This file
//     was not found."), one the viewer may not see (access_denied, a
//     blocked Slack Connect canvas) its preview alone.
//   - .p-file_details__container (16px in, none under): the name (maaT
//     Name: 22px Black, one line with an ellipsis and the whole name in a
//     tooltip when cut; "Video clip" / "Audio clip" for a clip, the
//     title, else the name, else "Untitled"), 8px under it
//     renderReferencesMetadata in content-sec, both rows Bold 15px/21:
//     "Owned by {ownerName}" (G+uc getFileOwner: the bot's or app's name,
//     else the member's with "(you)" for the viewer, else file.username;
//     a KHV7 ButtonLink in content-hgl-1, underlined under the pointer,
//     opening the profile) and "Created today" / "Created yesterday" /
//     "Created on November 3rd" (toCalendarDateOrNamedDay with
//     uncapitalizedRelativeDay, the year by shouldExcludeYear), 8px under
//     them; then renderPreview: MessageFile with forceExpanded and
//     largeImage (richview's FileDetails: an image at thumb_480's size,
//     else thumb_360's, fitted to the width and never upscaled; a video
//     the message's inline player; any other file its card), and
//     FileDetailsFigure's caption under it when the file has alt text
//     (R3SR getFileAltText, "{alt_text} (AI generated)" when the AI wrote
//     it: 13px/18 sk_foreground_max_solid, 8px over and under).
//   - Under the container FileShareContext (2TKv: views.FileShares,
//     files.getShares).
//   - The footer (.p-flexpane__footer: a 1px rgba(foreground_low,.13)
//     rule, 16px over and under, 20px at the sides, 8px between) is
//     renderReferencesFooter, v1+y FileActions with isFileDetails:
//     "Share File…" as an outline button filling the row (renderShare(true),
//     the share dialog: openShareDialog), Download as a 36px button with
//     the otl-sec border (p-file_details__references_actions:
//     downloadFile) and More actions, ellipsis-vertical-filled at 20 in a
//     36px button with no border (the file's menu, right-bottom). Share
//     and Download need shouldShowFile (a file, not a stub), Download a
//     hosted file (not external, remote or a post).
//
// Not ported: K8Ba showDownloadAndShareActions (enable_new_channel_
// settings' download settings and the IP check) is taken as true, so
// neither teams.authorized.ip.range.checkIp nor files.channelSettings
// AllowDownloads is called; the owner's member hover card (XFqE
// renderProfileHoverCard) and the title's emoji (FormatTextWithHighlight
// Words); a canvas, list or surface's rows (Viewed by, Generated).

var fileDetailsNS = i18n.NS("file-details")

// fileDetailsTitle is FileDetails' titleContent for a file.
func fileDetailsTitle() string { return fileDetailsNS.T("File") }

var _ popups.FileDetailsHost = (*App)(nil)

// FileDetailsPane is popups.FileDetailsHost's: FileDetails for f with its
// header hidden, for the media viewer's Details tab.
func (a *App) FileDetailsPane(f *richtext.File, channel string) popups.FileDetailsPane {
	if f == nil || f.ID == "" {
		return nil
	}
	return a.newFileDetailsPane(f, channel, false)
}

// OpenFileDetails is popups.FileDetailsHost's: the secondary view
// navigates to FileDetails for f (toFileDetails(f.ID, {channelId})). An
// email file is the email FileDetails (new_email_rendering:
// EmailFileDetails, email_pane.go).
func (a *App) OpenFileDetails(f *richtext.File, channel string) {
	if f == nil || f.ID == "" {
		return
	}
	if c := a.Core(); f.Email != nil && c != nil && c.NewEmailRendering() {
		a.OpenEmailFile(f, channel, "")
		return
	}
	if p, ok := a.secondary.view.(*fileDetailsPane); ok && a.secondary.kind == secFileDetails && a.secondary.open() && p.id == f.ID {
		return
	}
	p := a.newFileDetailsPane(f, channel, true)
	a.secondary = secondaryState{kind: secFileDetails, channel: channel, width: a.secondary.width}
	a.secondary.view = p
	a.Invalidate()
}

func (a *App) newFileDetailsPane(f *richtext.File, channel string, header bool) *fileDetailsPane {
	p := &fileDetailsPane{a: a, id: f.ID, channel: channel, header: header, msg: f}
	p.list.Axis = layout.Vertical
	p.fetch()
	return p
}

// fdState is what renderFileDetails draws.
type fdState uint8

const (
	fdNothing     fdState = iota // an unknown file loading
	fdError                      // FileDetailsError
	fdTombstone                  // FileDetailsTombstone
	fdNotFound                   // FileDetailsNotFound
	fdPreviewOnly                // external access denied, a blocked canvas
	fdFile                       // the container and the shares
)

// fileDetailsPane is FileDetails for one file.
type fileDetailsPane struct {
	a       *App
	id      string
	channel string
	header  bool           // the secondary view's (the tab's hides it)
	msg     *richtext.File // the opener's copy

	info     core.FileDetailsInfo // files.info's, once got
	got      bool
	full     *richtext.File // info's file
	fetching bool
	seq      int // the fetch going

	close, nameHov, owner widget.Clickable
	retry, help           widget.Clickable
	share, download, more widget.Clickable
	nameTip, dlTip        popups.TipState
	moreTip               popups.TipState

	list     layout.List
	view     *richview.View
	viewOf   *richtext.File // the file view shows
	shares   *views.FileShares
	sharesOf string // the file shares reads

	org                  originTracker   // the pane's window origin
	foot                 image.Point     // the footer in the pane
	shareR, downloadR    image.Rectangle // in the window
	moreR                image.Rectangle
	drawn                fdDrawn
	ownerName, ownerUser string
}

// fdDrawn is what the pane drew last frame (the tests' reads).
type fdDrawn struct {
	state                   fdState
	name, owner, created    string
	caption                 string
	header                  string
	share, download, more   bool
	previewW, previewH      int
	contentW                int // the container's width inside its 16px padding
	sharesShown, footerDraw bool
}

// FileID is popups.FileDetailsPane's: the file the pane shows.
func (p *fileDetailsPane) FileID() string { return p.id }

// fetch is withFileSubscription's fetchFileInfo (alwaysFetchOnMount) and
// reloadFile: one read at a time.
func (p *fileDetailsPane) fetch() {
	a := p.a
	c := a.Core()
	if c == nil || p.fetching {
		return
	}
	p.fetching = true
	p.seq++
	seq, id := p.seq, p.id
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		info, err := c.FileDetails(ctx, id)
		a.Post(func() {
			if p.seq != seq {
				return
			}
			p.fetching = false
			if err == nil {
				p.info, p.got = info, true
				p.full = info.RichFile(id)
			}
			a.Invalidate()
		})
	}()
}

// known reports whether f is a file the client has (not isUnknown: more
// than an id).
func fdKnown(f *richtext.File) bool {
	return f != nil && (f.Name != "" || f.Title != "" || f.URL != "" || f.Mode != "" || f.Thumb != nil || f.Permalink != "")
}

// file is the file shown: files.info's once it answered, else the
// opener's copy when it is one (nil: none).
func (p *fileDetailsPane) file() *richtext.File {
	if p.full != nil {
		return p.full
	}
	if fdKnown(p.msg) {
		return p.msg
	}
	return nil
}

// state is renderFileDetails' choice.
func (p *fileDetailsPane) state() fdState {
	f := p.file()
	if f == nil {
		if p.fetching {
			return fdNothing
		}
		return fdError
	}
	switch f.Mode {
	case "tombstone":
		return fdTombstone
	case "not_found":
		return fdNotFound
	case "access_denied", "canvas_slack_connect_blocked":
		return fdPreviewOnly
	}
	return fdFile
}

// fdShowFile is shouldShowFile (vITP ZG): a file, not one of the stubs.
func fdShowFile(f *richtext.File) bool {
	if f == nil {
		return false
	}
	switch f.Mode {
	case "tombstone", "not_found", "access_denied", "not_visible", "hidden_by_limit",
		"canvas_slack_connect_blocked", "lists_slack_connect_blocked":
		return false
	}
	return true
}

func (p *fileDetailsPane) Layout(gtx layout.Context) layout.Dimensions {
	a := p.a
	th := a.th
	c := &th.C
	sz := gtx.Constraints.Max
	p.drawn = fdDrawn{state: p.state()}
	hh := 0
	if p.header {
		hh = gtx.Dp(theme.HeaderH)
		bs := gtx.Dp(28)
		cx := sz.X - gtx.Dp(8) - bs
		tx := gtx.Dp(20)
		title := fileDetailsTitle()
		p.drawn.header = title
		tw := min(th.Measure(gtx, 18, theme.Black, title), cx-gtx.Dp(8)-tx)
		at(gtx, image.Pt(tx, 0), image.Pt(tw+1, hh), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, hh, func(gtx layout.Context) layout.Dimensions {
				return th.Label(gtx, 18, theme.Black, c.Fg, title)
			})
		})
		at(gtx, image.Pt(cx, (hh-bs)/2), image.Pt(bs, bs), func(gtx layout.Context) layout.Dimensions {
			return iconButton(gtx, &p.close, "close", 28, 18, c.FgMuted, c.BgHover, 8)
		})
		if clicked(&p.close) {
			a.CloseSecondary()
			return layout.Dimensions{Size: sz}
		}
	}
	f := p.file()
	fh := 0
	if f != nil { // renderReferencesFooter: FileActions for a file the store has
		fh = 2*gtx.Dp(16) + gtx.Dp(36) + max(1, gtx.Dp(1))
	}
	bh := max(sz.Y-hh-fh, 0)
	atExact(gtx, image.Pt(0, hh), image.Pt(sz.X, bh), func(gtx layout.Context) layout.Dimensions { return p.body(gtx, f) })
	if f != nil {
		p.foot = image.Pt(0, hh+bh)
		at(gtx, p.foot, image.Pt(sz.X, fh), func(gtx layout.Context) layout.Dimensions { return p.footer(gtx, sz, f) })
	}
	p.org.track(gtx, a) // last: over everything (pass-through), for the menus' anchors
	return layout.Dimensions{Size: sz}
}

// winOrigin is the pane's top-left in the window (it sits at the window's
// right until the pointer has crossed it).
func (p *fileDetailsPane) winOrigin(pane image.Point) image.Point {
	return p.org.get(image.Pt(p.a.winSize.X-pane.X, p.a.winSize.Y-pane.Y))
}

// body is the flexpane's scrolling body: renderFileDetails.
func (p *fileDetailsPane) body(gtx layout.Context, f *richtext.File) layout.Dimensions {
	a := p.a
	switch p.drawn.state {
	case fdNothing:
		return layout.Dimensions{Size: gtx.Constraints.Max}
	case fdError:
		return p.errorState(gtx)
	case fdTombstone:
		return p.tombstone(gtx, "trash", fileDetailsNS.T("This file was deleted."))
	case fdNotFound:
		return p.tombstone(gtx, "emoji-missing-filled", fileDetailsNS.T("This file was not found."))
	case fdPreviewOnly:
		return p.list.Layout(gtx, 1, func(gtx layout.Context, _ int) layout.Dimensions {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return p.preview(gtx, f, gtx.Constraints.Max.X)
		})
	}
	if p.shares == nil || p.sharesOf != f.ID {
		p.shares, p.sharesOf = views.NewFileShares(a, f.ID), f.ID
	}
	p.drawn.sharesShown = true
	return p.list.Layout(gtx, 2, func(gtx layout.Context, i int) layout.Dimensions {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		if i == 1 {
			return p.shares.Layout(gtx, a.winSize.Y)
		}
		return p.container(gtx, f)
	})
}

// container is .p-file_details__container: the name, the owner and date
// rows, the preview and its caption.
func (p *fileDetailsPane) container(gtx layout.Context, f *richtext.File) layout.Dimensions {
	a := p.a
	th := a.th
	w := gtx.Constraints.Max.X
	pad := gtx.Dp(16)
	cw := max(w-2*pad, 1)
	p.drawn.contentW = cw
	y := pad
	// maaT Name: 22px Black at the inherited 1.4 (30.8px), one line
	name := fileDetailsName(f)
	p.drawn.name = name
	lh := gtx.Dp(31)
	cut := th.Measure(gtx, 22, theme.Black, name) > cw
	at(gtx, image.Pt(pad, y), image.Pt(cw, lh), func(gtx layout.Context) layout.Dimensions {
		d := p.nameHov.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
				return th.Layout(gtx, theme.TextStyle{Size: 22, Weight: theme.Black, Color: th.C.Fg, MaxLines: 1}, name)
			})
		})
		if cut { // the whole name, only when the text is cut
			popups.Tooltip(gtx, th, &p.nameTip, p.nameHov.Hovered(), d.Size, popups.Above, name, "")
		}
		return d
	})
	y += lh + gtx.Dp(8)
	y += p.meta(gtx, f, pad, y, cw)
	y += p.preview2(gtx, f, pad, y, cw)
	return layout.Dimensions{Size: image.Pt(w, y)}
}

// meta is renderReferencesMetadata at (x, y): "Owned by {ownerName}" and
// the date, Bold 15px/21 in content-sec, 8px under them.
func (p *fileDetailsPane) meta(gtx layout.Context, f *richtext.File, x, y, w int) int {
	a := p.a
	th := a.th
	col := theme.ContentSec(th)
	rh := gtx.Dp(21)
	y0 := y
	name, user := p.owner0(f)
	p.ownerName, p.ownerUser = name, user
	parts := fileDetailsNS.RT("Owned by {ownerName}", "ownerName", name)
	p.drawn.owner = i18n.Plain(parts)
	at(gtx, image.Pt(x, y), image.Pt(w, rh), func(gtx layout.Context) layout.Dimensions {
		cx := 0
		for _, pt := range parts {
			if pt.Text == "" || cx >= w {
				continue
			}
			tw := min(th.Measure(gtx, 15, theme.Bold, pt.Text), w-cx)
			if pt.Arg != "ownerName" {
				at(gtx, image.Pt(cx, 0), image.Pt(tw+1, rh), func(gtx layout.Context) layout.Dimensions {
					return vcenter(gtx, rh, func(gtx layout.Context) layout.Dimensions { return th.Label(gtx, 15, theme.Bold, col, pt.Text) })
				})
				cx += tw
				continue
			}
			// KHV7 ButtonLink (c-link--button): content-hgl-1, underlined
			// under the pointer; navigateToProfile({memberId})
			link := theme.ContentHgl1(th)
			at(gtx, image.Pt(cx, 0), image.Pt(tw+1, rh), func(gtx layout.Context) layout.Dimensions {
				return clickArea(gtx, &p.owner, nil, func(gtx layout.Context) layout.Dimensions {
					d := vcenter(gtx, rh, func(gtx layout.Context) layout.Dimensions { return th.Label(gtx, 15, theme.Bold, link, pt.Text) })
					if p.owner.Hovered() || p.owner.Pressed() {
						uy := (rh+gtx.Sp(15))/2 + max(1, gtx.Dp(1))
						theme.Fill(gtx, image.Rect(0, uy, d.Size.X, uy+max(1, gtx.Dp(1))), link)
					}
					return d
				})
			})
			cx += tw
		}
		return layout.Dimensions{Size: image.Pt(w, rh)}
	})
	if clicked(&p.owner) && user != "" {
		a.OpenProfile(image.Point{}, user)
	}
	y += rh
	if ts := p.timestamp(f); ts > 0 {
		created := fileCreatedText(i18n.Locale(), time.Unix(ts, 0), time.Now())
		p.drawn.created = created
		at(gtx, image.Pt(x, y), image.Pt(w, rh), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, rh, func(gtx layout.Context) layout.Dimensions { return th.Label(gtx, 15, theme.Bold, col, created) })
		})
		y += rh
	}
	return y - y0 + gtx.Dp(8)
}

// timestamp is file.timestamp (files.info's, else the file's created).
func (p *fileDetailsPane) timestamp(f *richtext.File) int64 {
	if sf := p.info.File; sf != nil && sf.Timestamp > 0 {
		return sf.Timestamp
	}
	return f.Created
}

// fileCreatedText is renderReferencesMetadata's date row: "Created
// {date}" for today and yesterday (toCalendarDateIfYesterdayOrToday),
// else "Created on {date}", the date toCalendarDateOrNamedDay's with
// uncapitalizedRelativeDay.
func fileCreatedText(loc string, t, now time.Time) string {
	d := i18n.CalendarDateOrNamedDay(loc, t, now, i18n.NamedDayOpts{UncapitalizedRelativeDay: true})
	if i18n.IsTodayOrYesterday(t, now) {
		return fileDetailsNS.T("Created {date}", "date", d)
	}
	return fileDetailsNS.T("Created on {date}", "date", d)
}

// fileClipAudio and fileClipVideo are lqEY isAudioClip and 7OiV
// isVideoClip (T/K6 isClip).
func fileClipAudio(f *richtext.File) bool {
	switch f.Subtype {
	case "audio_story", "slack_audio", "slack_name_pronunciation":
		return true
	}
	return false
}

func fileClipVideo(f *richtext.File) bool {
	return f.Subtype == "video_story" || f.Subtype == "slack_video"
}

// fileDetailsName is maaT Name's text: a clip's "Audio clip" / "Video
// clip", the title, else R3SR getFileName (the name, else "Untitled").
// richtext keeps no title equal to the name, so the name stands in.
func fileDetailsName(f *richtext.File) string {
	ns := i18n.NS("files")
	switch {
	case fileClipAudio(f):
		return ns.T("Audio clip")
	case fileClipVideo(f):
		return ns.T("Video clip")
	case strings.TrimSpace(f.Title) != "":
		return f.Title
	case strings.TrimSpace(f.Name) != "":
		return f.Name
	}
	return ns.T("Untitled")
}

// owner0 is G+uc getFileOwner and the link's member (canvas_creator_id ??
// user): a bot's or app's name (the file's bot_id, else the member's
// bot), else the member's name, "{name} (you)" for the viewer, else
// file.username.
func (p *fileDetailsPane) owner0(f *richtext.File) (name, user string) {
	c := p.a.Core()
	user = f.User
	if p.info.CanvasCreator != "" {
		user = p.info.CanvasCreator
	}
	if c == nil {
		return p.info.Username, user
	}
	var u core.User
	known := false
	if user != "" {
		u, known = c.User(user)
	}
	bot := ""
	if sf := p.info.File; sf != nil {
		bot = sf.BotID
	}
	if bot == "" && known {
		bot = memberBotID(u)
	}
	if bot != "" {
		if n := c.S.BotName(bot); n != "" {
			return n, user
		}
	}
	if known {
		n := firstNonEmptyStr(u.Name, u.RealName, u.Handle)
		if user == c.S.SelfID() {
			n = i18n.T("file", "{name} (you)", "name", n)
		}
		return n, user
	}
	return p.info.Username, user
}

// alt is R3SR getFileAltText when the file has alt text
// (fileHasAltText): FileDetailsFigure's caption.
func (p *fileDetailsPane) alt() string {
	if p.info.File == nil {
		return ""
	}
	t := strings.TrimSpace(p.info.AltText)
	if t == "" {
		return ""
	}
	if p.info.AltByAI {
		return i18n.T("files", "{alt_text} (AI generated)", "alt_text", t)
	}
	return t
}

// preview2 is the container's .p-file_details__preview (renderPreview) at
// (x, y), FileDetailsFigure's caption under it; its height.
func (p *fileDetailsPane) preview2(gtx layout.Context, f *richtext.File, x, y, w int) int {
	d := at(gtx, image.Pt(x, y), image.Pt(w, 1<<20), func(gtx layout.Context) layout.Dimensions { return p.preview(gtx, f, w) })
	h := d.Size.Y
	if alt := p.alt(); alt != "" {
		th := p.a.th
		col := messages.RichStyle(p.a).Palette.Text2 // sk_foreground_max_solid
		p.drawn.caption = alt
		m := gtx.Dp(8)
		cd := at(gtx, image.Pt(x, y+h+m), image.Pt(w, 1<<20), func(gtx layout.Context) layout.Dimensions {
			return th.Layout(gtx, theme.TextStyle{Size: 13, LineHeight: 18, Color: col}, alt)
		})
		h += m + cd.Size.Y + m
	}
	return h
}

// preview is renderPreview: MessageFile with forceExpanded, largeImage
// and isFileDetails, its actions off (richview's FileDetails).
func (p *fileDetailsPane) preview(gtx layout.Context, f *richtext.File, w int) layout.Dimensions {
	a := p.a
	pf := p.previewFile(f)
	if p.view == nil || !samePreview(p.viewOf, pf) {
		var md richview.Media
		pane := "file-details"
		if !p.header {
			pane = "file-details-tab"
		}
		if m := a.MessageMedia(pane, p.channel, "", fdMediaHost{p}); m != nil {
			md = m
		}
		doc := richtext.Doc{Blocks: []richtext.Block{{Kind: richtext.FileBlock, File: pf}}}
		p.view = richview.New(doc, richview.Options{FileDetails: true, CompactFiles: true, NoOuterMargins: true, Channel: p.channel, Media: md})
		p.viewOf = pf
	}
	gtx.Constraints = layout.Constraints{Min: image.Pt(w, 0), Max: image.Pt(w, gtx.Constraints.Max.Y)}
	d := p.view.Layout(gtx, messages.RichStyle(a), fdHandler{p})
	if pf.IsImage && pf.Thumb != nil {
		p.drawn.previewW, p.drawn.previewH = min(gtx.Dp(unit.Dp(pf.Thumb.W)), w), d.Size.Y
	}
	return d
}

// samePreview reports whether a and b would draw the same preview
// (previewFile makes a new copy each frame).
func samePreview(a, b *richtext.File) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.ID != b.ID || a.URL != b.URL || a.Mode != b.Mode || a.IsVideo != b.IsVideo || a.IsImage != b.IsImage {
		return false
	}
	if (a.Thumb == nil) != (b.Thumb == nil) {
		return false
	}
	return a.Thumb == nil || *a.Thumb == *b.Thumb
}

// previewFile is f as renderPreview's MessageImageFile sizes it: an image's
// thumb is thumb_480 (its animated thumb_480_gif for a GIF), else
// thumb_360, at that thumb's size (`largeImage && thumb_480_w ||
// thumb_360_w`). Before files.info answers, the opener's thumb fitted
// to 480.
func (p *fileDetailsPane) previewFile(f *richtext.File) *richtext.File {
	if !f.IsImage || f.Thumb == nil {
		return f
	}
	cp := *f
	if sf := p.info.File; sf != nil && f == p.full {
		u, w, h := firstNonEmptyStr(sf.Thumb480GIF, sf.Thumb480), sf.Thumb480W, sf.Thumb480H
		if u == "" || w <= 0 || h <= 0 {
			u, w, h = firstNonEmptyStr(sf.Thumb360GIF, sf.Thumb360), sf.Thumb360W, sf.Thumb360H
		}
		if u != "" && w > 0 && h > 0 {
			cp.Thumb = &richtext.Image{URL: u, Alt: f.Name, W: w, H: h}
			return &cp
		}
	}
	w, h := f.Thumb.W, f.Thumb.H
	if f.OrigW > 0 && f.OrigH > 0 {
		w, h = f.OrigW, f.OrigH
	}
	if m := max(w, h); m > 480 {
		w, h = max(w*480/m, 1), max(h*480/m, 1)
	}
	im := *f.Thumb
	im.W, im.H = w, h
	cp.Thumb = &im
	return &cp
}

// errorState is FileDetailsError: role alert, centred 40px down,
// "Couldn’t load file" (20px Bold), the help line with its link, and the
// primary Try Again (reloadFile).
func (p *fileDetailsPane) errorState(gtx layout.Context) layout.Dimensions {
	a := p.a
	th := a.th
	w := gtx.Constraints.Max.X
	y := gtx.Dp(40)
	center := func(tw int) int { return max((w-tw)/2, 0) }
	head := fileDetailsNS.T("Couldn’t load file")
	hw := min(th.Measure(gtx, 20, theme.Bold, head), w)
	at(gtx, image.Pt(center(hw), y), image.Pt(hw+1, gtx.Dp(28)), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, gtx.Dp(28), func(gtx layout.Context) layout.Dimensions { return th.Label(gtx, 20, theme.Bold, th.C.Fg, head) })
	})
	y += gtx.Dp(28) + gtx.Dp(16)
	parts := fileDetailsNS.RT("If the problem persists, please <a>drop us a line.</a>")
	lw := 0
	for _, pt := range parts {
		lw += th.Measure(gtx, 15, theme.Regular, pt.Text)
	}
	lw = min(lw, w)
	lh := gtx.Dp(22)
	x := center(lw)
	for _, pt := range parts {
		tw := th.Measure(gtx, 15, theme.Regular, pt.Text)
		if pt.Tag == "a" {
			at(gtx, image.Pt(x, y), image.Pt(tw+1, lh), func(gtx layout.Context) layout.Dimensions {
				return clickArea(gtx, &p.help, nil, func(gtx layout.Context) layout.Dimensions {
					return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
						return th.Label(gtx, 15, theme.Regular, th.C.Link, pt.Text)
					})
				})
			})
		} else {
			at(gtx, image.Pt(x, y), image.Pt(tw+1, lh), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions { return th.Label(gtx, 15, theme.Regular, th.C.Fg, pt.Text) })
			})
		}
		x += tw
	}
	if clicked(&p.help) {
		openURL(toastHelpURL)
	}
	y += lh + gtx.Dp(16)
	retry := p.retry.Clicked(gtx) // popups.Button's Layout drains its clicks
	label := fileDetailsNS.T("Try Again")
	bw := th.Measure(gtx, 15, theme.Bold, label) + 2*gtx.Dp(12)
	bw = max(bw, gtx.Dp(80))
	at(gtx, image.Pt(center(bw), y), image.Pt(bw, gtx.Dp(36)), func(gtx layout.Context) layout.Dimensions {
		return popups.Button(gtx, th, &p.retry, popups.BtnPrimary, popups.BtnMedium, label, "", 0)
	})
	if retry {
		p.fetch()
	}
	return layout.Dimensions{Size: gtx.Constraints.Max}
}

// tombstone is FileDetailsTombstone / FileDetailsNotFound: role alert,
// centred 40px down, the 36px icon (#868686) 12px over the line (#616061).
func (p *fileDetailsPane) tombstone(gtx layout.Context, iconName, line string) layout.Dimensions {
	th := p.a.th
	w := gtx.Constraints.Max.X
	y := gtx.Dp(40)
	is := gtx.Dp(36)
	if icons.Has(iconName) {
		at(gtx, image.Pt((w-is)/2, y), image.Pt(is, is), func(gtx layout.Context) layout.Dimensions {
			icons.Get(iconName)(gtx, 36, th.C.FgSubtle)
			return layout.Dimensions{Size: image.Pt(is, is)}
		})
	}
	y += is + gtx.Dp(12)
	col := messages.RichStyle(p.a).Palette.Text2
	at(gtx, image.Pt(0, y), image.Pt(w, gtx.Dp(22)*4), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min.X = w
		return th.Layout(gtx, theme.TextStyle{Size: 15, LineHeight: 22, Color: col, Align: text.Middle}, line)
	})
	return layout.Dimensions{Size: gtx.Constraints.Max}
}

// fileShareLabel is getShareLabel(true) for a file.
func fileShareLabel() string { return i18n.T("file", "Share File…") }

// footer is the flexpane's footer: renderReferencesFooter (v1+y FileActions
// with isFileDetails).
func (p *fileDetailsPane) footer(gtx layout.Context, pane image.Point, f *richtext.File) layout.Dimensions {
	a := p.a
	th := a.th
	w := gtx.Constraints.Max.X
	p.drawn.footerDraw = true
	share := p.share.Clicked(gtx) // popups.Button's Layout drains its clicks
	// border-top: rgba(sk_foreground_low,.13)
	theme.Fill(gtx, image.Rect(0, 0, w, max(1, gtx.Dp(1))), th.C.Border)
	y := max(1, gtx.Dp(1)) + gtx.Dp(16)
	x, bh, gap := gtx.Dp(20), gtx.Dp(36), gtx.Dp(8)
	right := w - gtx.Dp(20)
	showShare := fdShowFile(f)
	showDL := showShare && !f.IsExternal && f.Mode != "external" && f.Mode != "post" && f.Mode != "space" && f.Mode != "docs"
	mx := right - bh // More actions at the right
	dx := mx - gap - bh
	org := p.winOrigin(pane).Add(p.foot)
	icol, hov := fdActionColors(a)
	if showShare {
		end := mx
		if showDL {
			end = dx
		}
		sw := max(end-gap-x, 0)
		at(gtx, image.Pt(x, y), image.Pt(sw, bh), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.X = sw
			return popups.Button(gtx, th, &p.share, popups.BtnOutline, popups.BtnMedium, fileShareLabel(), "share-message", float32(sw)/gtx.Metric.PxPerDp)
		})
		p.shareR = image.Rect(x, y, x+sw, y+bh).Add(org)
		p.drawn.share = true
	}
	if showDL {
		at(gtx, image.Pt(dx, y), image.Pt(bh, bh), func(gtx layout.Context) layout.Dimensions {
			d := iconButton(gtx, &p.download, "download", 36, 20, icol, hov, 8)
			// p-file_details__references_actions: 1px otl-sec
			theme.Border(gtx, image.Rectangle{Max: d.Size}, gtx.Dp(8), max(1, gtx.Dp(1)), theme.OtlSec(th))
			p.dlTip.Delay = 60 * time.Millisecond
			popups.Tooltip(gtx, th, &p.dlTip, p.download.Hovered(), d.Size, popups.Above, i18n.T("file", "Download"), "")
			return d
		})
		p.downloadR = image.Rect(dx, y, dx+bh, y+bh).Add(org)
		p.drawn.download = true
	}
	at(gtx, image.Pt(mx, y), image.Pt(bh, bh), func(gtx layout.Context) layout.Dimensions {
		d := iconButton(gtx, &p.more, "ellipsis-vertical-filled", 36, 20, icol, hov, 8)
		p.moreTip.Delay = 60 * time.Millisecond
		popups.Tooltip(gtx, th, &p.moreTip, p.more.Hovered(), d.Size, popups.Above, i18n.T("file", "More actions"), "")
		return d
	})
	p.moreR = image.Rect(mx, y, mx+bh, y+bh).Add(org)
	p.drawn.more = true
	if share && showShare {
		p.openShare(f)
	}
	if clicked(&p.download) && showDL {
		popups.DownloadFile(a, f) // downloadFile({file, action: "download"})
	}
	if clicked(&p.more) {
		if mn := popups.NewFileActionsMenu(a, p.moreR, core.Message{Channel: p.channel}, f, false); mn != nil {
			a.ShowOverlay(mn)
		}
	}
	return layout.Dimensions{Size: image.Pt(w, gtx.Constraints.Max.Y)}
}

// fdActionColors are a MessageActionButton's colour and hover fill
// (.c-message_actions__button: sk_foreground_max_solid, hovering to
// rgba(sk_foreground_min,.04)).
func fdActionColors(a *App) (icon, hover color.NRGBA) {
	icon = messages.RichStyle(a).Palette.Text2
	hover = color.NRGBA{R: 0x1d, G: 0x1c, B: 0x1d, A: 0x0a}
	if a.th.Dark {
		hover = color.NRGBA{R: 0xe8, G: 0xe8, B: 0xe8, A: 0x0a}
	}
	return icon, hover
}

// openShare is FileActions' onShare: openShareDialog({fileId}), the share
// dialog's file entity being the file's tile and title; a share goes to
// its destination (shareMessageOrFile's jump).
func (p *fileDetailsPane) openShare(f *richtext.File) {
	a := p.a
	title := fileDetailsName(f)
	ff := f
	m := popups.NewShareFileModal(a, popups.ShareFileOpts{ID: f.ID, Title: title, Permalink: f.Permalink,
		Preview: func(gtx layout.Context) layout.Dimensions { return fdSharePreview(gtx, a, ff, title) },
		Shared: func(ch, ts string) {
			if ts != "" {
				a.Navigate("c/" + ch + "/m/" + ts)
			} else {
				a.Navigate("c/" + ch)
			}
		}})
	a.ShowOverlay(m)
}

// fdSharePreview is ShareDialogPreview's file entity: the 36px thumb (an
// image's) or svgicon tile, 12px in, the 15px Bold title and its type.
func fdSharePreview(gtx layout.Context, a *App, f *richtext.File, title string) layout.Dimensions {
	th := a.th
	w := gtx.Constraints.Max.X
	pad, is := gtx.Dp(12), gtx.Dp(36)
	at(gtx, image.Pt(pad, pad), image.Pt(is, is), func(gtx layout.Context) layout.Dimensions {
		if f.IsImage && f.Thumb != nil {
			if img, ok := a.Images().Get(f.Thumb.URL, is*2); ok {
				return theme.Image(gtx, img, image.Pt(is, is), gtx.Dp(8))
			}
			return th.Placeholder(gtx, image.Pt(is, is), gtx.Dp(8))
		}
		return icons.Art(gtx, richview.FileIconArt(f), is)
	})
	tx := pad + is + pad
	tw := max(w-tx-pad, 1)
	at(gtx, image.Pt(tx, pad-gtx.Dp(2)), image.Pt(tw, gtx.Dp(22)), func(gtx layout.Context) layout.Dimensions {
		return th.Layout(gtx, theme.TextStyle{Size: 15, LineHeight: 22, Weight: theme.Bold, Color: th.C.Fg, MaxLines: 1}, title)
	})
	at(gtx, image.Pt(tx, pad+gtx.Dp(20)), image.Pt(tw, gtx.Dp(18)), func(gtx layout.Context) layout.Dimensions {
		return th.Layout(gtx, theme.TextStyle{Size: 13, LineHeight: 18, Color: th.C.FgMuted, MaxLines: 1}, f.PrettyType)
	})
	return layout.Dimensions{Size: image.Pt(w, 2*pad+is)}
}

// fdHandler is the preview's links and clicks: in the secondary view an
// image or a video's tile opens the media viewer over it (EPKq
// previewImage: openFileViewer); in the viewer's Details tab nothing (the
// viewer shows it already: displayInMediaViewer("") there, not ported).
type fdHandler struct{ p *fileDetailsPane }

func (h fdHandler) OpenURL(u string)                       { openURL(u) }
func (h fdHandler) OpenUser(id string, anchor image.Point) { h.p.a.OpenProfile(anchor, id) }
func (h fdHandler) OpenChannel(id string)                  { h.p.a.Navigate("c/" + id) }
func (h fdHandler) OpenUsergroup(string, image.Point)      {}
func (h fdHandler) OpenImage(img richtext.Image)           { openURL(img.URL) }
func (h fdHandler) Button(b richtext.Button) {
	if b.URL != "" {
		openURL(b.URL)
	}
}

func (h fdHandler) OpenFile(f *richtext.File) {
	p := h.p
	if !p.header || f == nil {
		return
	}
	of := p.file() // the file, not the preview's 480 copy
	if of == nil || of.ID != f.ID {
		of = f
	}
	if of.IsImage && of.Thumb != nil {
		im := *of.Thumb
		if im.Title == "" {
			im.Title = fileDetailsName(of)
		}
		if popups.OpenFileViewer(p.a, of, im, p.channel) {
			return
		}
	}
	popups.OpenFileCard(p.a, of, openURL)
}

// fdMediaHost is the preview's video player host (messages.MediaRowHost,
// videoplayer.MenuOpener): its corner's More actions opens the video's
// menu.
type fdMediaHost struct{ p *fileDetailsPane }

func (h fdMediaHost) OpenFile(f *richtext.File) { fdHandler(h).OpenFile(f) }
func (h fdMediaHost) FileAction(richview.FileAction, *richtext.File, image.Rectangle) {
}

func (h fdMediaHost) OpenFileMenu(f *richtext.File, win, view image.Rectangle, onClose func()) bool {
	if f == nil || win.Empty() {
		return false
	}
	h.p.a.ShowOverlay(popups.NewVideoActionsMenuClosing(h.p.a, win, core.Message{Channel: h.p.channel}, f, false, onClose))
	return true
}
