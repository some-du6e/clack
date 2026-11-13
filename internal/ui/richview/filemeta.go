package richview

import (
	"image"
	"strconv"
	"strings"

	"gioui.org/font"

	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

// The line above a message's files in channels and threads: Slack's
// FilesMetaTextWrapper, which MessageFiles wraps around the gallery
// whenever shouldUseCompactStack is unset (the message pane and threads
// never set it). .c-message_kit__file__meta: 13px text in
// foreground_max .7, flex and centred in a 22px line (measured on
// Karim's screenshot of Slack), 4px under it; then a non-breaking space
// and the collapse button (caret-down-filled / caret-right-filled, 1em,
// the text color).
//
//	the file's owner and " | " first when someone other than the sender
//	  owns the one file (renderUploader: "BetterClient | Canvas")
//	one image, snippet or video, or any collapsed file: its title
//	one other file, expanded: pretty_type ("PDF"; "Video", "Audio",
//	  "Post" for Arugula)
//	several files: "N files"
//
// The button toggles the files' expandable (getExpandableKey("files",
// ts, {fileIds, viewContext}): kept for the session); collapsed, only the
// line is left. With no state yet, files are expanded unless one is an
// image and expand_internal_inline_imgs is off (FilesMetaTextWrapper's
// isExpanded).

// fileMetaLineH is the meta line's height (dp).
const fileMetaLineH = 22

// filesKey is the files' expandables key in this layout.
func (l *lay) filesKey(files []*richtext.File) string {
	ids := make([]string, len(files))
	for i, f := range files {
		ids[i] = f.ID
	}
	return "files/" + l.opts.Channel + "/" + l.opts.TS + "/" + strings.Join(ids, "_")
}

// filesExpanded is isExpanded: the default flipped by the toggle.
func (l *lay) filesExpanded(files []*richtext.File) bool {
	def := true
	for _, f := range files {
		if f.IsImage && uipref.Get().NoInternalImgs {
			def = false
		}
	}
	if kitIsExpanded(l.filesKey(files)) { // toggled this session
		return !def
	}
	return def
}

// fileMetaText is renderMetaText for the files.
func fileMetaText(files []*richtext.File, expanded bool) string {
	if len(files) != 1 {
		return strconv.Itoa(len(files)) + " files"
	}
	f := files[0]
	if f.IsImage || f.IsSnippet || f.IsVideo || !expanded {
		return firstNonEmptyStr(f.Title, f.Name)
	}
	switch {
	case f.IsAudio:
		return "Audio"
	case f.PrettyType == "Arugula":
		return "Post"
	case richtext.IsCanvasFile(f): // renderSingleFileTitle: isCanvas
		return "Canvas"
	case f.PrettyType != "":
		return f.PrettyType
	}
	return "file"
}

func firstNonEmptyStr(s ...string) string {
	for _, x := range s {
		if x != "" {
			return x
		}
	}
	return ""
}

// fileMeta lays out the meta line for files at (x, y) and returns its
// height (with the 4px under it) and whether the files are shown.
func (l *lay) fileMeta(files []*richtext.File, x, y, w int) (int, bool) {
	return l.fileMetaWith(files, 0, x, y, w)
}

// fileMetaWith is fileMeta with wos work object cards in the gallery
// too (work_objects_rearrangement): renderMetaText counts them, "{n}
// attachments" for more than one item.
func (l *lay) fileMetaWith(files []*richtext.File, wos, x, y, w int) (int, bool) {
	p := &l.st.Palette
	expanded := l.filesExpanded(files)
	lineH := l.dp(fileMetaLineH)
	fs := flowStyle{face: font.Font{Typeface: l.st.Face}, size: l.sp(13), lineH: lineH, col: p.Text2,
		emoji: l.dp(16), maxLines: 1, ellipsis: true}
	label := fileMetaText(files, expanded)
	if wos > 0 {
		label = i18n.T("files", "{fileCount, plural, =1 {attachment} other {# attachments}}", "fileCount", len(files)+wos)
	}
	k := l.faceFor(fs.face, fs.size, 0)
	if l.curPart != 0 {
		k = l.partFace(k, l.curPart)
	}
	if f := files[0]; len(files) == 1 && wos == 0 && f.Uploader != "" {
		// renderUploader: the owner (a member link in the text's color)
		// and the separator (sk_primary_foreground .33)
		ut := l.d.addTarget(target{kind: tUser, id: f.UploaderID, underline: true})
		l.addText(f.Uploader, &fs, k, fs.col, 0, -1, ut)
		l.addText(" | ", &fs, k, rgba(p.Text.R, p.Text.G, p.Text.B, 0.33), 0, -1, -1)
	}
	l.addText(label, &fs, k, fs.col, 0, -1, -1)
	_, tw := l.flow(x, y, max(w-l.dp(24), l.dp(40)), &fs)
	endX := x + tw
	if n := len(l.d.lines); n > 0 {
		if ln := l.d.lines[n-1]; ln.f1 > ln.f0 {
			f := l.d.frags[ln.f1-1]
			endX = int(f.x + f.w + 0.999)
		}
	}
	l.plainAdd("\n")
	// the button: an 8x7 caret 11.5px after the text, on the x-height
	tg := l.d.addTarget(target{kind: tExpand, id: l.filesKey(files)})
	cx := endX + l.dp(11.5) + l.dp(4)
	cy := y + lineH/2 + l.dp(1)
	kind, cr := bCaret, image.Rect(cx-l.dp(4), cy-l.dp(3), cx+l.dp(4), cy+l.dp(4))
	if !expanded {
		kind, cr = bCaretRight, image.Rect(cx-l.dp(3), cy-l.dp(4), cx+l.dp(4), cy+l.dp(4))
	}
	l.d.boxes = append(l.d.boxes, box{kind: kind, r: cr, fill: p.Text, target: -1})
	l.d.hits = append(l.d.hits, hit{r: image.Rect(cx-l.dp(8), y, cx+l.dp(8), y+lineH), t: tg})
	return lineH + l.dp(4), expanded
}
