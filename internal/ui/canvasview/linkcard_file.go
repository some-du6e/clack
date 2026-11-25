package canvasview

// EmbeddedFileUnfurl (client-boot-shared p/yH), the card Slack draws in a
// canvas for a file (a container's file, a SLACK_FILE card, a /files/…
// link): EmptyUnfurl for a deleted or missing file, LoadingFileUnfurl, the
// image, the video's poster, the audio player, or the generic FileCard.
// Build 889ac449a32d; the notes are in
// /root/clack-ops/scratch/link-url-box-3/cards-spec.md §3.

import (
	"context"
	"errors"
	"image"
	"image/color"
	"log"
	"regexp"
	"strings"
	"time"

	"gioui.org/layout"
	"gioui.org/op"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

// fileState is a file the view fetched for its file cards (files.info, the
// batched file object fetcher; never files.open).
type fileState struct {
	done bool
	f    slack.File
	// code is what files.info refused with: file_not_found, file_deleted,
	// access_denied or not_visible (Slack's answer, kept for the view's
	// life). A failure that says nothing about the file (the network, a
	// timeout, ratelimited, a 5xx) isn't one: the file is asked for again
	// after a backoff (tries, retry), and the card waits meanwhile.
	code  string
	tries int
	retry time.Time
}

// fileRetry is the wait before files.info is asked again after the n-th
// failure that says nothing about the file: 2 s, doubling, a minute at most.
func fileRetry(n int) time.Duration {
	d := 2 * time.Second << max(0, n-1)
	if n > 6 || d > time.Minute {
		d = time.Minute
	}
	return d
}

// fileCodeOf is the code a files.info failure keeps ("" with none: a
// failure that says nothing about the file, asked again).
func fileCodeOf(err error) string {
	var se *slack.Error
	switch {
	case err == nil:
		return ""
	case core.IsAPIError(err, "file_deleted"):
		return "file_deleted"
	case core.IsAPIError(err, "access_denied", "not_allowed", "file_access_denied"):
		return "access_denied"
	case core.IsAPIError(err, "not_visible"):
		return "not_visible"
	case core.IsAPIError(err, "ratelimited", "internal_error", "fatal_error", "request_timeout", "service_unavailable", "team_added_to_org"):
		return ""
	case errors.As(err, &se) || errors.Is(err, core.ErrNoFile):
		return "file_not_found" // Slack refused it, or answered without it: the file isn't one the viewer can have
	}
	return ""
}

// fileEmptyLine is EmptyUnfurl's line for a file code.
func fileEmptyLine(code string) string {
	if code == "access_denied" || code == "not_visible" {
		return emptyLine("file_private")
	}
	return emptyLine(code)
}

// fileCardData is what EmbeddedFileUnfurl reads, and its views.
type fileCardData struct {
	f      slack.File
	known  bool
	code   string // an error state: file_not_found, file_deleted
	key    string
	rv     *richview.View // the generic card, or the audio player
	imgURL string         // the image's, or the video's poster
	imgW   int
	imgH   int
}

var cardFileIDRe = regexp.MustCompile(`^F[A-Z0-9]+$`)

// fileStub: a file object that is only a reference (Slack's file_access
// check_file_info, or no url_private): files.info fills it in.
func fileStub(f *slack.File) bool {
	return f.ID == "" || f.FileAccess == "check_file_info" || f.URLPrivate == "" && f.Mode == "hosted"
}

// readFile reads the route's file: the container's own copy, a copy a
// held message carries, or what the view fetched; it reports whether to
// fetch it.
func (v *View) readFile(c *linkCard) bool {
	d := &c.fc
	id := c.rt.fileID
	d.known, d.code = false, ""
	if c.rt.kind != "file" || id == "" {
		return false
	}
	if !cardFileIDRe.MatchString(id) { // makeInvalidFile: isNonExistent
		d.code = "file_not_found"
		return false
	}
	a := v.h.Core()
	if c.held && len(c.msg.Files) > 0 && c.msg.Files[0].ID == id && !fileStub(&c.msg.Files[0]) {
		d.f, d.known = c.msg.Files[0], true
		return false
	}
	if f, ok := a.S.HeldFile(id); ok && !fileStub(&f) {
		d.f, d.known = f, true
		return false
	}
	if v.files == nil {
		v.files = map[string]*fileState{}
	}
	st := v.files[id]
	if st == nil {
		v.files[id] = &fileState{}
		return true
	}
	if st.done {
		d.f, d.known, d.code = st.f, st.code == "", st.code
		return false
	}
	if st.tries > 0 && !time.Now().Before(st.retry) { // a failure that said nothing: ask again
		st.retry = time.Now().Add(time.Hour) // asked: no second fetch until it answers
		return true
	}
	return false
}

// fetchFile fetches file id with files.info, then refreshes the cards.
func (v *View) fetchFile(id string) {
	a := v.h.Core()
	if a == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		f, err := a.CardFileInfo(ctx, id)
		code := fileCodeOf(err)
		transient := err != nil && code == ""
		if transient {
			log.Printf("canvas card file %s: files.info: %v", id, err)
		}
		v.post(func() {
			st := v.files[id]
			if st == nil {
				return
			}
			if !transient {
				st.done, st.f, st.code = true, f, code
				v.refreshCards()
				return
			}
			st.tries++
			wait := fileRetry(st.tries)
			st.retry = time.Now().Add(wait)
			go func() { // the cards read the store again then, and ask once more
				time.Sleep(wait)
				v.post(v.refreshCards)
			}()
		})
	}()
}

// decideFile is EmbeddedFileUnfurl's order.
func (c *linkCard) decideFile(loading bool) (cardKind, string) {
	d := &c.fc
	f := &d.f
	switch {
	case d.code != "":
		return cardEmpty, fileEmptyLine(d.code)
	case d.known && (f.IsDeleted || f.Mode == "tombstone"): // isFileDeleted || isTombstone
		return cardEmpty, emptyLine("file_deleted")
	case d.known && (f.Mode == "not_found" || f.FileAccess == "file_not_found"):
		return cardEmpty, emptyLine("file_not_found")
	case !d.known || loading:
		return cardFileLoading, ""
	}
	normal := richtext.FileStatusOf(richtext.FileFacts{Mode: f.Mode, FileAccess: f.FileAccess, IsDeleted: f.IsDeleted,
		HiddenByLimit: f.IsHiddenByLimit, Tombstoned: f.IsTombstoned}) == "normal"
	switch {
	case !(f.IsExternal && f.ExternalType == "app") && normal && slack.FileIsImage(f):
		return cardFileImage, ""
	case slack.FileIsPlayableVideo(f):
		return cardFileVideo, ""
	case slack.FileIsPlayableAudio(f):
		return cardFileAudio, ""
	}
	return cardFileGeneric, ""
}

// cardDate is toCalendarDateOrNamedDay with shortenMonth: "Today",
// "Yesterday", "Nov 3rd", "Nov 3rd, 2025".
func cardDate(t, now time.Time) string {
	loc := i18n.Locale()
	if d := i18n.RelativeDay(loc, t, now, false); d != "" {
		return d
	}
	days := now.Sub(t)
	exclude := t.Year() == now.Year() || t.Year() <= now.Year() && days >= 0 && days <= 31*24*time.Hour
	return i18n.CalendarDate(loc, t, i18n.CalendarOpts{ShortenMonth: true, ExcludeYear: exclude})
}

// fileDetail is FileEntityUserTimestamp (e8MH) with a documentUnfurlTs:
// "uploader \u00a0\u00a0·\u00a0\u00a0 date \u00a0\u00a0·\u00a0\u00a0 Added …" (the
// file's date left out when it reads the same as when it was added).
func (v *View) fileDetail(f *slack.File, addedTS string, now time.Time) string {
	a := v.h.Core()
	const sep = "\u00a0\u00a0·\u00a0\u00a0"
	var parts []string
	if a != nil && f.User != "" {
		if n := a.S.UserName(f.User); n != "" {
			parts = append(parts, n)
		}
	}
	date := ""
	if f.Created > 0 {
		date = cardDate(time.Unix(f.Created, 0), now)
	}
	added := ""
	if t, ok := tsTime(addedTS); ok {
		d := cardDate(t, now)
		if strings.Contains(d, "Today") || strings.Contains(d, "Yesterday") {
			added = nsFiles.T("Added {readableTs}", "readableTs", d)
		} else {
			added = nsFiles.T("Added on {readableTs}", "readableTs", d)
		}
		if d == date {
			date = ""
		}
	}
	if date != "" {
		parts = append(parts, date)
	}
	if added != "" {
		parts = append(parts, added)
	}
	return strings.Join(parts, sep)
}

// fileViews makes the file card's views.
func (v *View) fileViews(c *linkCard) {
	d := &c.fc
	f := &d.f
	key := string(rune('0'+c.kind)) + "\x00" + f.ID + "\x00" + f.Title + "\x00" + f.Thumb480 + "\x00" + c.msg.TS
	if key == d.key {
		return
	}
	d.key, d.rv, d.imgURL, d.imgW, d.imgH = key, nil, "", 0, 0
	switch c.kind {
	case cardFileImage: // the 480, 360 or 800 thumb's size; Slack draws url_private
		d.imgURL, d.imgW, d.imgH = firstNonEmpty(f.Thumb480, f.Thumb360, f.Thumb800), firstInt(f.Thumb480W, f.Thumb360W), firstInt(f.Thumb480H, f.Thumb360H)
		if u := firstNonEmpty(f.Thumb1024, f.Thumb960, f.Thumb720); u != "" {
			d.imgURL = u
		}
	case cardFileVideo: // the poster, thumb_video at its size (else 1280×720)
		d.imgURL, d.imgW, d.imgH = f.ThumbVideo, firstInt(f.ThumbVideoW, 1280), firstInt(f.ThumbVideoH, 720)
	case cardFileAudio, cardFileGeneric:
		rf := richtext.FileOf(f)
		if c.kind == cardFileGeneric {
			rf.Embedded = true
			ts := ""
			if c.held && len(c.msg.Files) > 0 && c.msg.Files[0].ID == f.ID {
				ts = c.msg.TS // documentUnfurlTs: the container's
			}
			rf.EmbedDetail = v.fileDetail(f, ts, time.Now())
			if f.Thumb360 != "" || f.ThumbPDF != "" { // getThumbSrcForFile: the best thumb up to 480
				rf.EmbedPreview = &richtext.Image{URL: firstNonEmpty(f.Thumb480, f.Thumb360, f.ThumbPDF), W: firstInt(f.Thumb480W, f.Thumb360W, f.ThumbPDFW), H: firstInt(f.Thumb480H, f.Thumb360H, f.ThumbPDFH)}
			}
		}
		d.rv = richview.New(richtext.Doc{Blocks: []richtext.Block{{Kind: richtext.FileBlock, File: rf}}}, richview.Options{})
	}
}

func firstInt(ns ...int) int {
	for _, n := range ns {
		if n > 0 {
			return n
		}
	}
	return 0
}

// layoutFile draws the file card w wide and returns its height.
func (v *View) layoutFile(gtx layout.Context, c *linkCard, w int, now time.Time) int {
	d := &c.fc
	box := max(w*400/799, gtx.Dp(235)) // the 799×400 AspectBox, at least 235 tall
	switch c.kind {
	case cardFileLoading:
		return v.layoutFileSkeleton(gtx, w, box, now)
	case cardFileImage: // the thumb's ratio at the card's full width, no cap
		if d.imgW <= 0 || d.imgH <= 0 {
			return 0
		}
		h := w * d.imgH / d.imgW
		if img, ok := v.h.Images().Get(d.imgURL, max(w, h)); ok && d.imgURL != "" {
			theme.Image(gtx, img, image.Pt(w, h), gtx.Dp(8))
		}
		c.fileHit = image.Rect(0, 0, w, h)
		return h
	case cardFileVideo: // black, the poster contained, at most 520 wide and 400 tall
		theme.Fill(gtx, image.Rect(0, 0, w, box), color.NRGBA{A: 255})
		maxW := gtx.Dp(520)
		if d.imgW > 0 && 520*d.imgH/d.imgW > 400 {
			maxW = gtx.Dp(400) * d.imgW / d.imgH
		}
		pw := min(w, maxW)
		ph := pw * d.imgH / max(1, d.imgW)
		if ph > box {
			ph, pw = box, box*d.imgW/max(1, d.imgH)
		}
		if img, ok := v.h.Images().Get(d.imgURL, max(pw, ph)); ok && d.imgURL != "" {
			func() {
				defer op.Offset(image.Pt((w-pw)/2, (box-ph)/2)).Push(gtx.Ops).Pop()
				theme.Image(gtx, img, image.Pt(pw, ph), 0)
			}()
		}
		c.fileHit = image.Rect(0, 0, w, box)
		return box
	case cardFileAudio: // the player, padded 12
		if d.rv == nil {
			return 0
		}
		pad := gtx.Dp(12)
		var h int
		func() {
			defer op.Offset(image.Pt(pad, pad)).Push(gtx.Ops).Pop()
			g := gtx
			g.Constraints = layout.Constraints{Max: image.Pt(max(0, w-2*pad), gtx.Constraints.Max.Y)}
			h = d.rv.Layout(g, v.styles.card, v.handler).Size.Y
		}()
		return h + 2*pad
	case cardFileGeneric:
		if d.rv == nil {
			return 0
		}
		g := gtx
		g.Constraints = layout.Constraints{Max: image.Pt(w, gtx.Constraints.Max.Y)}
		return d.rv.Layout(g, v.styles.card, v.handler).Size.Y
	}
	return 0
}

// layoutFileSkeleton is LoadingFileUnfurl: the box, its title row padded
// 12 (a 36px MissingIcon, two bars 120 and 80 12 from it) over a 1px
// foreground_low_solid rule, the rest primary_foreground at 13%,
// shimmering.
func (v *View) layoutFileSkeleton(gtx layout.Context, w, box int, now time.Time) int {
	col := v.skeletonInk(gtx, now)
	ink := cardInks(v.h.Theme().Dark)
	pad, ic, lh := gtx.Dp(12), gtx.Dp(36), gtx.Dp(8)
	theme.RRect(gtx, image.Rect(pad, pad, pad+ic, pad+ic), gtx.Dp(8), col)
	tx := pad + ic + gtx.Dp(12)
	gap := (ic - 2*lh) / 2
	theme.RRect(gtx, image.Rect(tx, pad+gap/2, tx+gtx.Dp(120), pad+gap/2+lh), lh/2, col)
	theme.RRect(gtx, image.Rect(tx, pad+gap/2+lh+gap, tx+gtx.Dp(80), pad+gap/2+2*lh+gap), lh/2, col)
	y := pad + ic + pad
	theme.Fill(gtx, image.Rect(0, y, w, y+max(1, gtx.Dp(1))), ink.lowSolid)
	y += max(1, gtx.Dp(1))
	fill := ink.primary
	fill.A = 33
	if !uipref.Get().NoAnimations {
		fill.A = uint8(int(fill.A) * int(col.A) / max(1, int(v.styles.card.Palette.Missing.A)))
	}
	theme.Fill(gtx, image.Rect(0, y, w, box), fill)
	return box
}

// fileClick is the image's previewImage (the lightbox) and the video
// poster's click (Clack plays it in the file's own page: canvasview has no
// player of its own here).
func (v *View) fileClick(gtx layout.Context, c *linkCard) {
	d := &c.fc
	for c.fileBtn.Clicked(gtx) {
		switch c.kind {
		case cardFileImage:
			v.h.OpenLightbox(richtext.Image{URL: d.imgURL, W: d.imgW, H: d.imgH, Title: firstNonEmpty(d.f.Title, d.f.Name), Proxy: true}, core.Message{})
		case cardFileVideo:
			if d.f.Permalink != "" {
				v.handler.OpenURL(d.f.Permalink)
			}
		case cardList: // handleFileContainerClick: the list at its view, or the item
			v.handler.OpenURL(c.url)
		}
	}
	if c.fileHit.Empty() {
		return
	}
	defer op.Offset(c.fileHit.Min).Push(gtx.Ops).Pop()
	g := gtx
	g.Constraints = layout.Exact(c.fileHit.Size())
	c.fileBtn.Layout(g, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{Size: gtx.Constraints.Max} })
}
