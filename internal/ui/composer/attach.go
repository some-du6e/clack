package composer

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/color"
	_ "image/gif" // thumbnail decoders
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gioui.org/f32"
	"gioui.org/gesture"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/x/explorer"
	_ "golang.org/x/image/bmp"
	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/localimg"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/shortcut"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/uiguard"
)

// Attachments: the + menu, file pickers, clipboard images and the chip row
// drawn inside the composer box between the text and the footer.
//
// Files stay in memory ([]byte) from the moment they're read until
// core.Upload sends them; nothing here writes them to disk.
//
// OS drag-and-drop: Gio v0.10.3 has no OS file-drop events. io/transfer is
// in-app only (a source and a target inside the same window), so dropping a
// file from a file manager onto the window can't be supported yet.

const (
	attMaxFileSize  = 1 << 30  // Slack's per-file upload limit (1 GB)
	attMaxTotalSize = 1 << 30  // what one message may hold in memory
	attMaxFiles     = 10       // Slack attaches at most 10 files to a message
	attMaxPixels    = 80 << 20 // don't decode thumbnails of images above 80 MP
	attChipH        = 62       // dp, chip height (and image thumbnail side)
	attCardW        = 240      // dp, file card width
	attRadius       = 12       // dp, chip corner radius
)

var (
	errAttTooBig  = errors.New("too big")
	errAttTooMany = errors.New("too many files")
	errAttTotal   = errors.New("total too big")
)

// ---- native file chooser (owned by the ui root) ----

var attExplorer struct {
	sync.Mutex
	e *explorer.Explorer
}

// SetExplorer lets the ui root (which owns the *app.Window and calls
// explorer.ListenEvents) give the composer a native file chooser. Pass nil
// to remove it. Safe to call from any goroutine.
func SetExplorer(e *explorer.Explorer) {
	attExplorer.Lock()
	attExplorer.e = e
	attExplorer.Unlock()
}

func getExplorer() *explorer.Explorer {
	attExplorer.Lock()
	defer attExplorer.Unlock()
	return attExplorer.e
}

// ---- model ----

// attChip is one attached file.
type attChip struct {
	id   int    // pendingFileId (MultiFileUploadItem), unique across composers
	name string // guarded by attachments.mu: plugins may rename it (prepare)
	// title is the name the chip shows and the file is shared under
	// (Slack's pendingFile title), guarded by attachments.mu: plugins
	// rename it (updatePendingFile, attach_pending.go). Starts as name.
	title string
	data  []byte // nil for a file restored from a server draft (already uploaded)
	kind  attKind
	// thumbURL is Slack's thumbnail of a restored file (draft_files.go).
	thumbURL string

	// pf is the eager upload as Slack's send sees it (its uploadPromise):
	// the placeholder draws it and the share waits for it. nil for files
	// restored from a server draft.
	pf *core.PendingFile

	// Guarded by attachments.mu.
	preparing bool  // PrepareUpload (bytes, then Slack's processing) in flight
	sent      int64 // bytes uploaded so far (prepare progress)
	ptotal    int64 // total bytes to upload (slightly > file size)
	prepared  *core.PreparedUpload
	prepErr   error
	thumb     paint.ImageOp
	thumbPx   int // side of thumb in pixels, 0 = none yet
	thumbBusy bool
	thumbBad  bool // not decodable: drawn as a file card

	// preview is data's local image URL while the image viewer may show
	// it (guarded by attachments.mu; dropped with the chip)
	preview string

	// UI goroutine only.
	hover  gesture.Hover
	remove widget.Clickable
	open   gesture.Click // the image tile (PendingImageFile's onClick)
	tip    popups.TipState
}

// openPreview is PendingImageFile's onClick (boot-shared SYnk:
// openPendingFileViewerDispatch with the file and its size): the attached
// image in the image viewer, from its bytes (localimg, memory only). A file
// restored from a server draft shows Slack's thumbnail.
func (a *attachments) openPreview(c *attChip) {
	a.mu.Lock()
	data, thumb, title := c.data, c.thumbURL, c.title
	if c.preview == "" && len(data) > 0 {
		c.preview = localimg.Put(data)
	}
	u := c.preview
	a.mu.Unlock()
	img := richtext.Image{URL: u, Title: title, Alt: title}
	switch {
	case u != "":
		if cfg, _, err := image.DecodeConfig(bytes.NewReader(data)); err == nil {
			img.W, img.H = cfg.Width, cfg.Height
		}
	case thumb != "":
		img.URL, img.Proxy = thumb, true
	default:
		return
	}
	a.h.OpenLightbox(img, core.Message{})
}

// dropPreviews forgets the chips' local image URLs (removed or sent).
func (a *attachments) dropPreviews(chips ...*attChip) {
	a.mu.Lock()
	var us []string
	for _, c := range chips {
		if c.preview != "" {
			us = append(us, c.preview)
			c.preview = ""
		}
	}
	a.mu.Unlock()
	for _, u := range us {
		localimg.Drop(u)
	}
}

// attachments is the composer's pending file list. Every method is safe to
// call from any goroutine except Layout, Menu and the picker modal, which
// run on the UI goroutine.
type attachments struct {
	h    Host
	core *core.App // the session the files were uploaded to

	onChange func() // guarded by mu: the owning composer's attChanged (draft_files.go)

	mu    sync.Mutex
	chips []*attChip
	total int64

	picking atomic.Bool
	pasting atomic.Bool
	browser *attBrowser // UI goroutine only

	// insertLink puts text linked to url at the owning composer's caret
	// (the + menu's Workflow row, WorkflowItem)
	insertLink func(text, url string)
}

func newAttachments(h Host) *attachments {
	a := &attachments{h: h, core: h.Core()}
	registerAttachments(a)
	return a
}

// Len is the number of files that will go with the next send.
func (a *attachments) Len() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.chips)
}

// Files snapshots the attached files. The byte slices are shared, never
// modified.
func (a *attachments) Files() []core.UploadFile {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []core.UploadFile
	for _, c := range a.chips {
		out = append(out, core.UploadFile{Name: c.name, Title: c.title, Data: c.data})
	}
	return out
}

// sendBatch is what one send takes from the composer.
type sendBatch struct {
	chips    []*attChip
	prepared []core.PreparedUpload // uploaded and processed
	pending  []*core.PendingFile   // eager uploads still running
	raw      []core.UploadFile     // no eager upload (it failed): uploaded at send
}

func (b sendBatch) empty() bool { return len(b.chips) == 0 }

// takeForSend removes every attached file from the composer for a send:
// Slack's composer empties at once (the draft goes, the pending files are
// marked isSending) and the placeholder message shows them. Files whose
// eager upload is done go as prepared, those still uploading as pending
// (the share waits for them), and those whose upload failed are uploaded
// again at send.
func (a *attachments) takeForSend() (b sendBatch) {
	a.mu.Lock()
	for _, c := range a.chips {
		b.chips = append(b.chips, c)
		switch {
		case c.prepared != nil:
			p := *c.prepared
			p.Title = c.title // shared under the chip's name
			b.prepared = append(b.prepared, p)
		case c.pf != nil && c.prepErr == nil && a.h.Core() != nil:
			c.pf.Title = c.title
			b.pending = append(b.pending, c.pf)
		default:
			b.raw = append(b.raw, core.UploadFile{Name: c.name, Title: c.title, Data: c.data})
		}
	}
	a.chips, a.total = nil, 0
	a.mu.Unlock()
	if !b.empty() {
		a.h.Invalidate()
		pendingChanged()
		a.notify()
	}
	return b
}

// putBack returns a failed send's files to the composer, ahead of any
// attached since (Slack gives them back to the draft).
func (a *attachments) putBack(chips []*attChip) {
	if len(chips) == 0 {
		return
	}
	a.mu.Lock()
	a.chips = append(append([]*attChip(nil), chips...), a.chips...)
	if len(a.chips) > attMaxFiles {
		a.chips = a.chips[:attMaxFiles]
	}
	a.total = 0
	for _, c := range a.chips {
		a.total += int64(len(c.data))
	}
	a.mu.Unlock()
	a.h.Invalidate()
	pendingChanged()
	a.notify()
}

// Clear removes every attached file.
func (a *attachments) Clear() {
	a.mu.Lock()
	gone := a.chips
	a.chips, a.total = nil, 0
	a.mu.Unlock()
	a.dropPreviews(gone...)
	a.h.Invalidate()
	pendingChanged()
	a.notify()
}

// Add attaches a file held in memory. Over-limit files are rejected with a
// toast.
func (a *attachments) Add(name string, data []byte) { a.add(name, data) }

func (a *attachments) add(name string, data []byte) bool {
	name = attCleanName(name)
	upload := a.h.Core() != nil // prepare uploads only with a core
	a.mu.Lock()
	err := attCheckAdd(a.pendingLocked(), a.total, int64(len(data)))
	var c *attChip
	if err == nil {
		// The tile is uploading from the first frame it's drawn in (its
		// corner spinner, isUploading): prepare runs on its own goroutine,
		// which may start a frame or more later.
		c = &attChip{id: int(attNextID.Add(1)), name: name, title: name, data: data, kind: attKindOf(name, data),
			pf: core.NewPendingFile(name, name, data), preparing: upload, ptotal: int64(len(data))}
		a.chips = append(a.chips, c)
		a.total += int64(len(data))
	}
	a.mu.Unlock()
	if err != nil {
		a.toast(attRejectText(name, err))
		return false
	}
	go a.prepare(c) // upload the bytes now, like Slack; send just shares them
	a.h.Invalidate()
	pendingChanged()
	a.notify()
	return true
}

// prepare uploads a chip's bytes to Slack immediately (files.getUploadURL,
// the multipart POST, files.completeUpload) and waits for Slack to process
// them (core.PrepareUpload), so sending only shares. The tile's spinner
// runs meanwhile; a failure shows the warning and falls back to uploading
// at send time (raw Files).
func (a *attachments) prepare(c *attChip) {
	if c == nil {
		return
	}
	if a.h.Core() == nil { // nothing to upload with: the send uploads the bytes
		a.mu.Lock()
		c.preparing = false
		a.mu.Unlock()
		a.h.Invalidate()
		return
	}
	a.mu.Lock()
	c.preparing, c.ptotal = true, int64(len(c.data))
	name, data := c.name, c.data
	a.mu.Unlock()
	a.h.Invalidate()

	// The name the file goes out under: Slack's upload reads File.name,
	// which plugins may redefine (AnonymiseFileNames).
	if n := uploadName(name, attMime(name)); n != name {
		a.mu.Lock()
		if c.title == c.name { // not renamed meanwhile
			c.title = n
		}
		c.name = n
		if c.pf != nil {
			c.pf.Name = n
		}
		a.mu.Unlock()
		name = n
		pendingChanged()
	}

	var last time.Time
	prog := func(sent, total int64) {
		a.mu.Lock()
		c.sent, c.ptotal = sent, total
		a.mu.Unlock()
		if c.pf != nil {
			c.pf.Progress(sent, total)
		}
		if now := time.Now(); now.Sub(last) > 40*time.Millisecond || sent >= total {
			last = now
			a.h.Invalidate()
		}
	}
	// addAndUploadPendingFile (AnonymizeFilenames renames the file here).
	pu, err := prepareFile(context.Background(), coreAPI{a.h.Core()}, name, data, prog)
	a.mu.Lock()
	c.preparing = false
	var p core.PreparedUpload
	if err != nil {
		c.prepErr = err
	} else {
		p = pu
		p.Title = c.title // a rename while the bytes went up
		c.prepared = &p
	}
	pf := c.pf
	a.mu.Unlock()
	if pf != nil {
		pf.Finish(p, err) // a send waiting for it shares it now
	}
	a.h.Invalidate()
	pendingChanged() // its Slack file id (state.files)
	a.notify()       // … which the draft carries now
}

func (a *attachments) pendingLocked() int { return len(a.chips) }

// precheck rejects a file by size before it is read into memory.
func (a *attachments) precheck(name string, size int64) bool {
	a.mu.Lock()
	err := attCheckAdd(a.pendingLocked(), a.total, size)
	a.mu.Unlock()
	if err != nil {
		a.toast(attRejectText(name, err))
		return false
	}
	return true
}

func (a *attachments) remove(c *attChip) {
	a.mu.Lock()
	for i, x := range a.chips {
		if x == c {
			a.chips = append(a.chips[:i:i], a.chips[i+1:]...)
			a.total -= int64(len(c.data))
			break
		}
	}
	a.mu.Unlock()
	a.dropPreviews(c)
	a.h.Invalidate()
	pendingChanged()
	a.notify()
}

// attCheckAdd applies Slack's limits to one more file of size bytes when
// count files totalling total bytes are already pending.
func attCheckAdd(count int, total, size int64) error {
	switch {
	case size > attMaxFileSize:
		return errAttTooBig
	case count >= attMaxFiles:
		return errAttTooMany
	case total+size > attMaxTotalSize:
		return errAttTotal
	}
	return nil
}

func attRejectText(name string, err error) string {
	switch {
	case errors.Is(err, errAttTooBig):
		return fmt.Sprintf("%s is too big to upload. Files must be 1 GB or smaller.", name)
	case errors.Is(err, errAttTooMany):
		return fmt.Sprintf("You can only attach %d files to a message.", attMaxFiles)
	case errors.Is(err, errAttTotal):
		return fmt.Sprintf("Couldn't attach %s: attachments are limited to 1 GB per message.", name)
	}
	return fmt.Sprintf("Couldn't attach %s: %v", name, err)
}

func attCleanName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	if name == "" || name == "." || name == "/" {
		return "file"
	}
	return name
}

// toast shows s from any goroutine (via the composer's UI queue).
func (a *attachments) toast(s string) { a.later(func() { a.h.Toast(s) }) }

// later runs f on the UI goroutine at the next composer Layout.
func (a *attachments) later(f func()) {
	restoreQ.Lock()
	restoreQ.fns = append(restoreQ.fns, f)
	restoreQ.Unlock()
	a.h.Invalidate()
}

// ---- file types ----

// attKind is how a file is drawn: an image thumbnail or a card with a
// colored type tile.
type attKind struct {
	Label string // type line: "PDF", "Zip", "PNG"
	Color uint32 // tile color
	Icon  string // icons name for the tile
	Image bool   // decodable image: drawn as a thumbnail
}

const (
	attBlue   = 0x1264a3
	attRed    = 0xe01e5a
	attGray   = 0x616061
	attGreen  = 0x2bac76
	attOrange = 0xe8912d
	attPurple = 0x7c3085
	attTeal   = 0x1d9bd1
)

var attLabels = map[string]string{
	"pdf": "PDF", "zip": "Zip", "gz": "GZip", "tgz": "GZip", "jpg": "JPEG", "jpeg": "JPEG",
	"doc": "Word Document", "docx": "Word Document", "xls": "Excel Spreadsheet",
	"xlsx": "Excel Spreadsheet", "ppt": "PowerPoint Presentation", "pptx": "PowerPoint Presentation",
	"key": "Keynote", "pages": "Pages", "numbers": "Numbers", "txt": "Plain Text", "log": "Plain Text",
	"md": "Markdown (raw)", "go": "Go", "py": "Python", "js": "JavaScript", "mjs": "JavaScript",
	"jsx": "JavaScript", "ts": "TypeScript", "tsx": "TypeScript", "java": "Java", "c": "C",
	"h": "C", "cpp": "C++", "cc": "C++", "hpp": "C++", "cs": "C#", "rs": "Rust", "rb": "Ruby",
	"php": "PHP", "sh": "Shell", "bash": "Shell", "zsh": "Shell", "swift": "Swift", "kt": "Kotlin",
	"sql": "SQL", "json": "JSON", "yaml": "YAML", "yml": "YAML", "toml": "TOML", "xml": "XML",
	"html": "HTML", "htm": "HTML", "css": "CSS", "svg": "SVG",
}

func attExtSet(s string) map[string]bool {
	m := map[string]bool{}
	for _, e := range strings.Fields(s) {
		m[e] = true
	}
	return m
}

var (
	attImageExts   = attExtSet("png jpg jpeg gif webp bmp")
	attPictureExts = attExtSet("heic heif tif tiff svg ico avif psd")
	attArchiveExts = attExtSet("zip gz tgz tar 7z rar bz2 xz zst dmg iso")
	attDocExts     = attExtSet("doc docx odt rtf pages")
	attSheetExts   = attExtSet("xls xlsx ods csv tsv numbers")
	attSlideExts   = attExtSet("ppt pptx odp key")
	attVideoExts   = attExtSet("mp4 mov webm mkv avi m4v mpg mpeg wmv")
	attAudioExts   = attExtSet("mp3 wav m4a ogg oga flac aac opus wma")
	attCodeExts    = attExtSet("go py js mjs jsx ts tsx java c h cpp cc hpp cs rs rb php sh bash zsh swift kt sql json yaml yml toml xml html htm css")
	attTextExts    = attExtSet("txt log md")
)

// attKindOf maps a file name (and its first bytes, for images with a wrong
// or missing extension) to its chip look.
func attKindOf(name string, data []byte) attKind {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
	label := attLabels[ext]
	if label == "" {
		label = strings.ToUpper(ext)
	}
	if ext == "" {
		label = "File"
	}
	k := attKind{Label: label, Color: attBlue, Icon: "files"}
	switch {
	case attImageExts[ext]:
		k.Color, k.Icon, k.Image = attGreen, "image", true
	case attPictureExts[ext]:
		k.Color, k.Icon = attGreen, "image"
	case ext == "pdf":
		k.Color = attRed
	case attArchiveExts[ext]:
		k.Color = attGray
	case attDocExts[ext]:
		k.Color = attBlue
	case attSheetExts[ext]:
		k.Color, k.Icon = attGreen, "list-view"
	case attSlideExts[ext]:
		k.Color = attOrange
	case attVideoExts[ext]:
		k.Color, k.Icon = attPurple, "video"
	case attAudioExts[ext]:
		k.Color, k.Icon = attOrange, "volume"
	case attCodeExts[ext]:
		k.Color, k.Icon = attTeal, "code"
	case attTextExts[ext]:
		k.Color = attGray
	}
	if !k.Image && len(data) > 0 {
		if mt := http.DetectContentType(data[:min(len(data), 512)]); attDecodable(mt) {
			k.Image, k.Color, k.Icon = true, attGreen, "image"
			if ext == "" {
				k.Label = strings.ToUpper(strings.TrimPrefix(mt, "image/"))
			}
		}
	}
	return k
}

func attDecodable(mime string) bool {
	switch mime {
	case "image/png", "image/jpeg", "image/gif", "image/webp", "image/bmp":
		return true
	}
	return false
}

// attThumbnail decodes data and returns a px×px center-cropped thumbnail.
func attThumbnail(data []byte, px int) (*image.RGBA, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > attMaxPixels {
		return nil, fmt.Errorf("image is %dx%d", cfg.Width, cfg.Height)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	b := img.Bounds()
	s := min(b.Dx(), b.Dy())
	sr := image.Rect(b.Min.X+(b.Dx()-s)/2, b.Min.Y+(b.Dy()-s)/2, 0, 0)
	sr.Max = sr.Min.Add(image.Pt(s, s))
	dst := image.NewRGBA(image.Rect(0, 0, px, px))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, sr, xdraw.Src, nil)
	return dst, nil
}

func (a *attachments) makeThumb(c *attChip, px int) {
	img, err := attThumbnail(a.thumbBytes(c), px)
	a.mu.Lock()
	c.thumbBusy = false
	if err != nil {
		c.thumbBad = true
	} else {
		c.thumb = paint.NewImageOp(img)
		c.thumbPx = px
	}
	a.mu.Unlock()
	a.h.Invalidate()
}

// attHumanSize formats a byte count like Slack ("12 KB", "3.4 MB").
func attHumanSize(n int64) string {
	const k = 1024
	switch {
	case n < k:
		return fmt.Sprintf("%d B", n)
	case n < k*k:
		return fmt.Sprintf("%d KB", (n+k/2)/k)
	case n < k*k*k:
		return attOneDecimal(float64(n)/(k*k)) + " MB"
	}
	return attOneDecimal(float64(n)/(k*k*k)) + " GB"
}

func attOneDecimal(f float64) string {
	s := fmt.Sprintf("%.1f", f)
	return strings.TrimSuffix(s, ".0")
}

// ---- the + menu ----

// The + menu is Slack's ShortcutsMenu (boot-render, Dv) over
// useComposerIAFormatter: its rows, in Slack's order, are
//
//   - the file rows: with unified_file_picker on, one row ("File, canvas,
//     or list", canvas-browser) opening the UnifiedFilePicker; off,
//     buildOriginalItems' Canvas (the canvas picker), List (the list
//     picker) and Recent file (recently uploaded files);
//   - New Google Calendar meeting (channel_meeting_invite), GIF
//     (allow_native_gif_picker: the emoji and GIF picker on its GIFs tab,
//     the picked GIF added to the draft), Enable GIFs for your team,
//     Salesforce record;
//   - Text snippet (text-snippet, ⌘⇧Enter / Ctrl+Shift+Enter): the snippet
//     dialog;
//   - Record video clip and Record audio clip (only with a MediaRecorder);
//   - Workflow (hermes__read: the workflow picker), Email
//     (productivity_email_in_composer), Poll (polls);
//   - Upload from your computer (laptop; ⌘O / Ctrl+O in the desktop app).
//
// Clack draws the rows it can run Slack's way: Text snippet and Upload
// from your computer, then the app shortcuts. Not drawn, because the
// surface they open isn't in Clack: the file rows (UnifiedFilePicker,
// the canvas picker, the composer's list insert, the recent-files menu),
// GIF (Clack's GIF picker sends a GIF at once; Slack's adds it to the
// draft's gifUnfurls), Workflow (no workflow picker), the clip rows (no
// recorder) and the Calendar, Salesforce, Email and Poll rows (their
// gates are off for the account). Slack has no "Paste image from
// clipboard" row (pasting does that).

// ShortcutItems, when set (internal/ui/blockkit), adds the app
// shortcuts section to the "+" menu for channel ch: recent global
// shortcuts and "Browse all shortcuts".
var ShortcutItems func(h popups.Host, ch string, anchor image.Rectangle) []popups.MenuItem

// WorkflowItem, when set (internal/ui/workflows_wire.go), is the + menu's
// "Workflow" row (Slack's attach-workflow option): the workflow picker,
// whose Insert puts the workflow's name, linked, at the caret (insert).
var WorkflowItem func(h popups.Host, ch string, insert func(text, url string)) *popups.MenuItem

// plusMenu is what the composer hands the + menu.
type plusMenu struct {
	ch      string
	snippet func() // Text snippet: the snippet dialog
}

// plusMenuItems is useComposerIAFormatter's rows that Clack draws, in
// Slack's order: Text snippet, Workflow, Upload from your computer.
func (a *attachments) plusMenuItems(pm plusMenu) []popups.MenuItem {
	// Slack's own labels: "Ctrl+Shift+Enter" / "⌘⇧Enter" (a literal,
	// ⌘ before ⇧, unlike AppKit's order that shortcut.Format follows) and
	// "Ctrl+O" / "⌘O"
	snippetKey, uploadKey := shortcut.FormatAsWritten("Mod+Shift+Enter"), shortcut.Format("Mod+O")
	var items []popups.MenuItem
	if pm.snippet != nil {
		items = append(items, popups.MenuItem{Label: i18n.T("menu_file", "Text snippet"), Icon: "text-snippet", Hint: snippetKey, Do: pm.snippet})
	}
	if WorkflowItem != nil && a.insertLink != nil {
		if it := WorkflowItem(a.h, pm.ch, a.insertLink); it != nil {
			items = append(items, *it)
		}
	}
	return append(items, popups.MenuItem{Label: i18n.T("menu_file", "Upload from your computer"), Icon: "laptop", Hint: uploadKey, Do: a.Pick})
}

// Menu opens the + menu above the button; anchor is the button in window
// coordinates.
func (a *attachments) Menu(anchor image.Rectangle, pm plusMenu) {
	items := a.plusMenuItems(pm)
	if ShortcutItems != nil {
		if more := ShortcutItems(a.h, pm.ch, anchor); len(more) > 0 {
			items = append(append(items, popups.Sep()), more...)
		}
	}
	// the shortcuts menu's template menu is 360 wide
	a.h.ShowOverlay(popups.NewMenuOpts(a.h, items, popups.MenuOpts{Anchor: anchor, Side: popups.Above, Width: 360}))
}

// ---- picking files ----

// Pick opens a file chooser without blocking: the ui root's native chooser
// (SetExplorer) if set, else zenity/kdialog (Linux) or osascript (macOS) if
// installed, else an in-app picker modal. Chosen files are read into memory.
func (a *attachments) Pick() {
	if !a.picking.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer a.picking.Store(false)
		e := getExplorer()
		single := false // the explorer can only pick one file (macOS)
		if e != nil && attExplorerUsable() {
			rcs, err, timedOut := attChooseFiles(e)
			switch {
			case timedOut:
				// The portal never answered; fall back to the in-app picker.
				log.Printf("composer: native file chooser timed out; using the in-app picker")
			case err == nil:
				a.addReaders(rcs)
				return
			case errors.Is(err, explorer.ErrUserDecline):
				return
			case errors.Is(err, explorer.ErrNotAvailable):
				single = true
			default:
				log.Printf("composer: native file chooser: %v", err)
			}
		}
		paths, ok, err := attNativePick(context.Background())
		if ok && err == nil {
			a.addPaths(paths) // nil when cancelled
			return
		}
		if err != nil {
			log.Printf("composer: file chooser: %v", err)
		}
		if single {
			rc, err := e.ChooseFile()
			if err == nil {
				a.addReaders([]io.ReadCloser{rc})
				return
			}
			if errors.Is(err, explorer.ErrUserDecline) {
				return
			}
		}
		a.later(a.openBrowser)
	}()
}

// ErrPickCancelled: the user closed the file chooser without a file.
var ErrPickCancelled = errors.New("composer: no file chosen")

// ChooseFile asks for one file with the same native choosers Pick uses
// (the ui root's explorer, else zenity/kdialog or osascript) and reads it
// into memory. It blocks; call it off the UI goroutine. Other hosts (a
// canvas's Insert file) use it to upload what the user picks.
func ChooseFile(ctx context.Context) (name string, data []byte, err error) {
	var rc io.ReadCloser
	e := getExplorer()
	if e != nil && attExplorerUsable() {
		rcs, err, timedOut := attChooseFiles(e)
		switch {
		case timedOut:
		case err == nil && len(rcs) > 0:
			rc = rcs[0]
			for _, x := range rcs[1:] {
				x.Close()
			}
		case err == nil, errors.Is(err, explorer.ErrUserDecline):
			return "", nil, ErrPickCancelled
		case errors.Is(err, explorer.ErrNotAvailable):
			if r, err := e.ChooseFile(); err == nil {
				rc = r
			} else if errors.Is(err, explorer.ErrUserDecline) {
				return "", nil, ErrPickCancelled
			}
		}
	}
	if rc == nil {
		paths, ok, err := attNativePick(ctx)
		switch {
		case err != nil:
			return "", nil, err
		case !ok:
			return "", nil, errors.New("there is no file chooser on this system")
		case len(paths) == 0:
			return "", nil, ErrPickCancelled
		}
		name = filepath.Base(paths[0])
		st, err := os.Stat(paths[0])
		if err != nil {
			return "", nil, err
		}
		if st.IsDir() || st.Size() > attMaxFileSize {
			return "", nil, errors.New(name + " can't be uploaded")
		}
		data, err = os.ReadFile(paths[0])
		return name, data, err
	}
	defer rc.Close()
	name = "file"
	if n, ok := rc.(interface{ Name() string }); ok && n.Name() != "" {
		name = filepath.Base(n.Name())
	}
	data, err = io.ReadAll(io.LimitReader(rc, attMaxFileSize+1))
	if err == nil && len(data) > attMaxFileSize {
		err = errors.New(name + " is over 1 GB")
	}
	return name, data, err
}

// attChooseFiles runs the Gio explorer's multi-file chooser with a watchdog.
// attExplorerUsable already rules out the common no-portal case; this guards
// the rarer portal-present-but-wedged setup so the picker can't hang forever.
// On timeout any late result's readers are drained and closed.
func attChooseFiles(e *explorer.Explorer) (rcs []io.ReadCloser, err error, timedOut bool) {
	type result struct {
		rcs []io.ReadCloser
		err error
	}
	ch := make(chan result, 1)
	go func() {
		r, err := e.ChooseFiles()
		ch <- result{r, err}
	}()
	select {
	case r := <-ch:
		return r.rcs, r.err, false
	case <-time.After(25 * time.Second):
		go func() {
			r := <-ch
			for _, rc := range r.rcs {
				rc.Close()
			}
		}()
		return nil, nil, true
	}
}

// attPickResult turns a picker command's output into paths. Exit status 1
// is a cancel (zenity, kdialog and osascript all use it).
func attPickResult(out []byte, err error) ([]string, bool, error) {
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && ee.ExitCode() == 1 {
			return nil, true, nil
		}
		return nil, true, err
	}
	return attParseLines(string(out)), true, nil
}

// attParseLines splits newline-separated picker output into paths (paths
// keep their spaces; blank lines are dropped).
func attParseLines(out string) []string {
	var paths []string
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimRight(l, "\r")
		if strings.TrimSpace(l) != "" {
			paths = append(paths, l)
		}
	}
	return paths
}

// attHave looks tool up in PATH (file system reads: off the UI goroutine).
func attHave(tool string) bool {
	uiguard.OffUI("exec.LookPath " + tool)
	_, err := exec.LookPath(tool)
	return err == nil
}

// addPaths reads files from disk into memory (any goroutine).
func (a *attachments) addPaths(paths []string) {
	for _, p := range paths {
		name := filepath.Base(p)
		st, err := os.Stat(p)
		if err != nil {
			a.toast("Couldn't read " + name + ": " + err.Error())
			continue
		}
		if st.IsDir() {
			a.toast(name + " is a folder. Only files can be uploaded.")
			continue
		}
		if !a.precheck(name, st.Size()) {
			continue
		}
		data, err := os.ReadFile(p)
		if err != nil {
			a.toast("Couldn't read " + name + ": " + err.Error())
			continue
		}
		a.add(name, data)
	}
}

// addReaders reads (and closes) the explorer's files.
func (a *attachments) addReaders(rcs []io.ReadCloser) {
	for _, rc := range rcs {
		name := "file"
		if n, ok := rc.(interface{ Name() string }); ok && n.Name() != "" {
			name = filepath.Base(n.Name())
		}
		if st, ok := rc.(interface{ Stat() (os.FileInfo, error) }); ok {
			if fi, err := st.Stat(); err == nil && !a.precheck(name, fi.Size()) {
				rc.Close()
				continue
			}
		}
		data, err := io.ReadAll(io.LimitReader(rc, attMaxFileSize+1))
		rc.Close()
		if err != nil {
			a.toast("Couldn't read " + name + ": " + err.Error())
			continue
		}
		a.add(name, data) // rejects > 1 GB
	}
}

// ---- clipboard images ----

var (
	errAttNoClipImage = errors.New("There's no image on the clipboard")
	errAttClipTools   = errors.New("Pasting images needs xclip (X11) or wl-clipboard (Wayland)")
	errAttClipNone    = errors.New("Pasting images isn't supported on this system yet")
)

// PasteClipboardImage probes the system clipboard for an image (async) and
// attaches it as "image.png", like Slack. Called on every Ctrl+V; it skips
// office-app copies (rich text plus a rendered picture of it).
func (a *attachments) PasteClipboardImage() { a.pasteImage(false) }

func (a *attachments) pasteImage(explicit bool) {
	if !a.pasting.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer a.pasting.Store(false)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		data, mime, err := attClipboardImage(ctx, explicit)
		if err == nil && !strings.HasPrefix(http.DetectContentType(data), "image/") {
			err = errAttNoClipImage
		}
		if err != nil {
			if explicit {
				a.toast(err.Error())
			}
			return
		}
		a.add(attPastedName(mime), data)
	}()
}

// attClipImageTypes are the image targets we fetch, best first.
var attClipImageTypes = []string{"image/png", "image/jpeg", "image/jpg", "image/gif", "image/webp", "image/bmp"}

// attPickImageType picks the image target to fetch from a clipboard's
// target list (xclip TARGETS / wl-paste --list-types), or "" for none.
// Unless explicit, copies that carry rich text (LibreOffice, Word, Excel
// put a rendered image next to the text) don't count as images.
func attPickImageType(targets []string, explicit bool) string {
	if !explicit {
		for _, t := range targets {
			lt := strings.ToLower(t)
			if lt == "text/rtf" || lt == "application/rtf" || lt == "text/richtext" ||
				strings.HasPrefix(lt, "application/x-openoffice") ||
				strings.HasPrefix(lt, "application/vnd.oasis.opendocument") ||
				strings.Contains(lt, "rich text format") {
				return ""
			}
		}
	}
	for _, want := range attClipImageTypes {
		for _, t := range targets {
			if strings.EqualFold(t, want) {
				return t
			}
		}
	}
	return ""
}

// attParseTargets splits a newline-separated target list.
func attParseTargets(out string) []string {
	var ts []string
	for _, l := range strings.Split(out, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			ts = append(ts, l)
		}
	}
	return ts
}

func attPastedName(mime string) string {
	switch strings.ToLower(mime) {
	case "image/jpeg", "image/jpg":
		return "image.jpg"
	case "image/gif":
		return "image.gif"
	case "image/webp":
		return "image.webp"
	case "image/bmp":
		return "image.bmp"
	}
	return "image.png"
}

// attMacClipHasImage reports whether osascript's `clipboard info` output
// lists a picture. Unless explicit, rich-text copies don't count.
func attMacClipHasImage(info string, explicit bool) bool {
	if !explicit && strings.Contains(info, "«class RTF »") {
		return false
	}
	for _, s := range []string{"«class PNGf»", "«class TIFF»", "TIFF picture", "JPEG picture", "GIF picture", "«class JPEG»", "«class GIFf»"} {
		if strings.Contains(info, s) {
			return true
		}
	}
	return false
}

// attParseAppleData decodes osascript's «data PNGf89504E47…» output.
func attParseAppleData(out string) ([]byte, error) {
	s := strings.TrimSpace(out)
	if !strings.HasPrefix(s, "«data ") || !strings.HasSuffix(s, "»") {
		return nil, errAttNoClipImage
	}
	s = strings.TrimSuffix(strings.TrimPrefix(s, "«data "), "»")
	if len(s) < 4 {
		return nil, errAttNoClipImage
	}
	b, err := hex.DecodeString(s[4:])
	if err != nil || len(b) == 0 {
		return nil, errAttNoClipImage
	}
	return b, nil
}

// attRun runs a tool and returns at most limit bytes of its stdout.
func attRun(ctx context.Context, limit int64, name string, args ...string) ([]byte, error) {
	uiguard.OffUI("exec " + name)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	// Stdout through Go's copier (not StdoutPipe) so WaitDelay applies: a
	// child that exits or is killed while a grandchild still holds its
	// stdout can't keep us waiting.
	out := &attCapped{limit: limit, over: cancel}
	cmd.Stdout = out
	cmd.WaitDelay = attWaitDelay
	err := cmd.Run()
	if out.tooBig {
		return nil, errAttTooBig
	}
	if err != nil {
		return nil, err
	}
	return out.b, nil
}

// attWaitDelay bounds every tool's Wait after it exits or is killed.
const attWaitDelay = 2 * time.Second

// attCapped keeps at most limit bytes and kills the tool past them.
type attCapped struct {
	b      []byte
	limit  int64
	tooBig bool
	over   func()
}

func (w *attCapped) Write(p []byte) (int, error) {
	if int64(len(w.b)+len(p)) > w.limit {
		w.tooBig = true
		w.over()
		return 0, errAttTooBig
	}
	w.b = append(w.b, p...)
	return len(p), nil
}

// ---- chip row ----

type attView struct {
	c         *attChip
	name      string // the file name it uploads under
	title     string // the name the chip shows
	thumb     paint.ImageOp
	thumbPx   int
	preparing bool
	failed    bool
	asImage   bool
	size      int64 // bytes (or Slack's size of a restored file)
}

// Layout draws the file gallery (zero size when empty): Slack's
// p-multi_file_upload__file_gallery in the message input (padding 12px,
// tiles 12px apart; .p-multi_file_upload__container--medium: 62px tiles).
// The constraints carry the composer box's inner width.
func (a *attachments) Layout(gtx layout.Context) layout.Dimensions {
	px := gtx.Dp(attChipH)
	a.mu.Lock()
	if len(a.chips) == 0 {
		a.mu.Unlock()
		return layout.Dimensions{}
	}
	views := make([]attView, len(a.chips))
	for i, c := range a.chips {
		if c.kind.Image && !c.thumbBad && c.thumbPx != px && !c.thumbBusy {
			c.thumbBusy = true
			go a.makeThumb(c, px)
		}
		views[i] = attView{c: c, name: c.name, title: c.title, thumb: c.thumb, thumbPx: c.thumbPx,
			preparing: c.preparing, failed: c.prepErr != nil && c.prepared == nil,
			asImage: c.kind.Image && !c.thumbBad, size: c.size()}
	}
	a.mu.Unlock()

	for _, v := range views {
		if v.c.remove.Clicked(gtx) {
			a.remove(v.c)
		}
	}

	pad, gap := gtx.Dp(12), gtx.Dp(12)
	avail := max(gtx.Constraints.Max.X-2*pad, px)
	x, y := 0, 0
	for _, v := range views {
		w := px
		if !v.asImage {
			w = min(gtx.Dp(attCardW), avail)
		}
		if x > 0 && x+w > avail {
			x, y = 0, y+px+gap
		}
		sz := image.Pt(w, px)
		offset(gtx, pad+x, pad+y, func(gtx layout.Context) layout.Dimensions {
			// MultiFileUploadItem (attach_slack.go): the chip.
			return slackc.Component(gtx, "MultiFileUploadItem", attItem{a: a, v: v, sz: sz}, attItem.props, attItem.layout)
		})
		x += w + gap
	}
	return layout.Dimensions{Size: image.Pt(max(gtx.Constraints.Max.X, gtx.Constraints.Min.X), pad+y+px+pad)}
}

// attStatusBtn is RemoveFileButton (p-multi_file_upload__file_status_btn):
// a 22px circle of the primary background 10px past the tile's top-right
// corner. While the upload runs (IDLE, UPLOADING, PROCESSING) it holds a
// medium InfiniteSpinner; an ERROR shows warning-filled (20); otherwise,
// or while the button itself is hovered, the remove icon: a 20px
// base-inv-pry circle with the content-inv-pry ×, which (--remove) only
// shows while the tile is hovered.
const attStatusBtn = 22

func (a *attachments) layoutChip(gtx layout.Context, v attView, sz image.Point) {
	th := a.h.Theme()
	c := v.c
	hovered := c.hover.Update(gtx.Source)
	rad := gtx.Dp(attRadius)
	d := gtx.Dp(attStatusBtn)
	over := gtx.Dp(10) // top:-10px; right:-10px
	r := image.Rectangle{Max: sz}

	// Hover area: the tile plus the status button's overhang, so moving
	// onto the button keeps the tile hovered.
	area := clip.Rect(image.Rect(0, -over, sz.X+over, sz.Y)).Push(gtx.Ops)
	c.hover.Add(gtx.Ops)

	if v.asImage {
		a.drawThumb(gtx, th, v, r, rad)
		// PendingImageFile is a button: a click opens the image viewer
		// (the remove button, drawn after, takes its own clicks)
		for {
			e, ok := c.open.Update(gtx.Source)
			if !ok {
				break
			}
			if e.Kind == gesture.KindClick {
				a.openPreview(c)
			}
		}
		func() {
			defer clip.Rect(r).Push(gtx.Ops).Pop()
			c.open.Add(gtx.Ops)
			pointer.CursorPointer.Add(gtx.Ops)
		}()
	} else {
		a.drawCard(gtx, th, v, r, rad)
	}
	onBtn := c.remove.Hovered()
	var mode int // 0 hidden, 1 spinner, 2 warning, 3 remove
	switch {
	case onBtn:
		mode = 3
	case v.preparing:
		mode = 1
	case v.failed:
		mode = 2
	case hovered:
		mode = 3
	}
	if mode != 0 {
		offset(gtx, sz.X-d+over, -over, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Exact(image.Pt(d, d))
			return c.remove.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				ctr := image.Pt(d/2, d/2)
				switch mode {
				case 1:
					fillCircle(gtx, ctr, d/2, th.C.Bg)
					s := gtx.Dp(16)
					offset(gtx, (d-s)/2, (d-s)/2, func(gtx layout.Context) layout.Dimensions {
						attSpinner(gtx, s, th.C.Link, th.C.FgMuted)
						return layout.Dimensions{Size: image.Pt(s, s)}
					})
				case 2:
					fillCircle(gtx, ctr, d/2, th.C.Bg)
					ic := gtx.Dp(20)
					offset(gtx, (d-ic)/2, (d-ic)/2, func(gtx layout.Context) layout.Dimensions {
						return icons.Get("warning-filled")(gtx, unit.Dp(20), th.C.Danger)
					})
				default:
					fillCircle(gtx, ctr, gtx.Dp(10), th.C.Tooltip)
					ic := gtx.Dp(14)
					offset(gtx, (d-ic)/2, (d-ic)/2, func(gtx layout.Context) layout.Dimensions {
						return icons.Get("close")(gtx, unit.Dp(14), th.C.TooltipText)
					})
				}
				pointer.CursorPointer.Add(gtx.Ops)
				return layout.Dimensions{Size: image.Pt(d, d)}
			})
		})
	}
	area.Pop()
	if hovered {
		// FileFieldset > EditFileDialogTitle: Slack's rename dialog, at
		// the chip's edit spot. Clack has no such dialog; both draw
		// nothing unless a plugin adds to them.
		slackc.Component(gtx, "FileFieldset", attItem{a: a, v: v, sz: sz}, attItem.fieldsetProps, attItem.layoutFieldset)
	}
	tip := v.title
	if onBtn {
		tip = attRemoveTip(v.name) // RemoveFileButton's tooltip
	}
	popups.Tooltip(gtx, th, &c.tip, hovered, sz, popups.Above, tip, "")
}

// attRemoveTip is RemoveFileButton's tooltip.
func attRemoveTip(name string) string {
	m := attMime(name)
	switch {
	case strings.HasPrefix(m, "audio/"):
		return "Remove audio clip"
	case strings.HasPrefix(m, "video/"):
		return "Remove video clip"
	}
	return "Remove file"
}

// attSpinner is Slack's InfiniteSpinner (s6So) in a d-pixel box: a
// stroke-8 (of 100) track at .25 of track, and two 55-unit arcs in col
// turning once per 1.2s on the ring's and tail's cubic-beziers. It keeps
// frames coming while drawn.
func attSpinner(gtx layout.Context, d int, col, track color.NRGBA) {
	sc := float32(d) / 100
	r := 35 * sc
	sw := 8 * sc
	ctr := f32.Pt(float32(d)/2, float32(d)/2)
	arc := func(a0, sweep float32, c color.NRGBA) {
		var p clip.Path
		p.Begin(gtx.Ops)
		p.MoveTo(f32.Pt(ctr.X+r*float32(math.Cos(float64(a0))), ctr.Y+r*float32(math.Sin(float64(a0)))))
		p.ArcTo(ctr, ctr, sweep)
		paint.FillShape(gtx.Ops, c, clip.Stroke{Path: p.End(), Width: sw}.Op())
	}
	track.A = uint8(float32(track.A)*.25 + .5)
	arc(0, 2*math.Pi-0.001, track)
	t := float64(gtx.Now.UnixMilli()%1200) / 1200
	dash := float32(55.0 / 219.9 * 2 * math.Pi)
	base := float32(-90.0/219.9*2*math.Pi) - float32(math.Pi/4)
	arc(base+float32(2*math.Pi*attBezier(.25, .29, .54, .86, t)), dash, col)
	arc(base+float32(2*math.Pi*attBezier(.41, .24, .64, .69, t)), dash, col)
	gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(33 * time.Millisecond)})
}

// attBezier is CSS cubic-bezier(x1, y1, x2, y2) at progress x.
func attBezier(x1, y1, x2, y2, x float64) float64 {
	lo, hi := 0.0, 1.0
	t := x
	for range 30 {
		t = (lo + hi) / 2
		mt := 1 - t
		if 3*mt*mt*t*x1+3*mt*t*t*x2+t*t*t < x {
			lo = t
		} else {
			hi = t
		}
	}
	mt := 1 - t
	return 3*mt*mt*t*y1 + 3*mt*t*t*y2 + t*t*t
}

func (a *attachments) drawThumb(gtx layout.Context, th *theme.Theme, v attView, r image.Rectangle, rad int) {
	if v.thumbPx > 0 {
		st := clip.UniformRRect(r, rad).Push(gtx.Ops)
		sc := float32(r.Dx()) / float32(v.thumbPx)
		tr := op.Affine(f32.Affine2D{}.Scale(f32.Point{}, f32.Pt(sc, sc))).Push(gtx.Ops)
		v.thumb.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		tr.Pop()
		st.Pop()
	} else {
		fillRRect(gtx, r, rad, th.C.Skeleton)
		ic := gtx.Dp(24)
		offset(gtx, (r.Dx()-ic)/2, (r.Dy()-ic)/2, func(gtx layout.Context) layout.Dimensions {
			return icons.Get("image")(gtx, unit.Dp(24), th.C.FgSubtle)
		})
	}
	strokeRRect(gtx, r, rad, max(1, gtx.Dp(1)), th.C.Border)
}

func (a *attachments) drawCard(gtx layout.Context, th *theme.Theme, v attView, r image.Rectangle, rad int) {
	k := v.c.kind
	fillRRect(gtx, r, rad, th.C.Bg)
	strokeRRect(gtx, r, rad, max(1, gtx.Dp(1)), th.C.Border)
	pad := gtx.Dp(11)
	tile := gtx.Dp(40)
	fillRRect(gtx, image.Rect(pad, pad, pad+tile, pad+tile), gtx.Dp(8), rgb(k.Color))
	ic := gtx.Dp(22)
	offset(gtx, pad+(tile-ic)/2, pad+(tile-ic)/2, func(gtx layout.Context) layout.Dimensions {
		return icons.Get(k.Icon)(gtx, unit.Dp(22), rgb(0xffffff))
	})
	tx := pad + tile + gtx.Dp(12)
	tw := max(r.Dx()-tx-gtx.Dp(12), 0)
	sh := th.Shaper
	offset(gtx, tx, pad, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(tw, gtx.Dp(22))}
		return label(gtx, sh, textStyle{Size: 15, LineHeight: 22, Weight: theme.Bold, Color: th.C.Fg, MaxLines: 1}, v.title)
	})
	offset(gtx, tx, pad+gtx.Dp(22), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(tw, gtx.Dp(18))}
		return label(gtx, sh, textStyle{Size: 13, LineHeight: 18, Color: th.C.FgMuted, MaxLines: 1}, k.Label)
	})
}

// ---- in-app file picker (no native chooser available) ----

type attEntry struct {
	name string
	path string
	dir  bool
	up   bool // the ".." row
	size int64
}

// attReadDir lists dir for the picker: ".." first (unless dir is a root),
// then folders, then files, each sorted case-insensitively. Hidden entries
// are skipped.
func attReadDir(dir string) ([]attEntry, error) {
	des, err := os.ReadDir(dir)
	var out []attEntry
	if parent := filepath.Dir(dir); parent != dir {
		out = append(out, attEntry{name: "..", path: parent, dir: true, up: true})
	}
	if err != nil {
		return out, err
	}
	var dirs, files []attEntry
	for _, de := range des {
		name := de.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		p := filepath.Join(dir, name)
		fi, err := os.Stat(p) // follows symlinks
		if err != nil {
			continue
		}
		e := attEntry{name: name, path: p, dir: fi.IsDir(), size: fi.Size()}
		if e.dir {
			dirs = append(dirs, e)
		} else if fi.Mode().IsRegular() {
			files = append(files, e)
		}
		if len(dirs)+len(files) >= 5000 {
			break
		}
	}
	less := func(s []attEntry) func(i, j int) bool {
		return func(i, j int) bool { return strings.ToLower(s[i].name) < strings.ToLower(s[j].name) }
	}
	sort.Slice(dirs, less(dirs))
	sort.Slice(files, less(files))
	out = append(out, dirs...)
	return append(out, files...), nil
}

type attBrowser struct {
	a       *attachments
	m       *popups.Modal
	dir     string
	entries []attEntry
	err     string
	cursor  int
	sel     map[string]bool
	order   []string // selection order
	clicks  []widget.Clickable
	list    layout.List
	focused bool
	reveal  bool // scroll the cursor into view
}

func (a *attachments) openBrowser() {
	if a.browser != nil && !a.browser.m.Dismissed() {
		return
	}
	b := &attBrowser{a: a, sel: map[string]bool{}}
	b.list.Axis = layout.Vertical
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = string(filepath.Separator)
	}
	b.open(home)
	attach := &popups.ModalButton{Label: "Attach", Kind: popups.BtnPrimary,
		Enabled: func() bool { return len(b.order) > 0 },
		Do:      func() bool { b.confirm(); return true }}
	b.m = popups.NewModal(a.h, popups.ModalOpts{
		Title:   "Upload from your computer",
		Width:   560,
		Body:    b.layout,
		Border:  true,
		Buttons: []*popups.ModalButton{{Label: "Cancel", Kind: popups.BtnOutline}, attach},
	})
	a.browser = b
	a.h.ShowOverlay(b.m)
}

func (b *attBrowser) open(dir string) {
	ents, err := attReadDir(dir)
	b.dir, b.entries, b.err, b.cursor = dir, ents, "", 0
	if err != nil {
		b.err = err.Error()
	}
	if len(b.clicks) < len(ents) {
		b.clicks = make([]widget.Clickable, len(ents))
	}
	b.list.Position = layout.Position{}
}

func (b *attBrowser) toggle(p string) {
	if b.sel[p] {
		delete(b.sel, p)
		for i, q := range b.order {
			if q == p {
				b.order = append(b.order[:i:i], b.order[i+1:]...)
				break
			}
		}
		return
	}
	b.sel[p] = true
	b.order = append(b.order, p)
}

func (b *attBrowser) confirm() {
	paths := append([]string(nil), b.order...)
	if len(paths) > 0 {
		go b.a.addPaths(paths)
	}
}

// activate opens a folder or acts on a file; it reports whether the entry
// list changed or the modal closed (stop handling this frame's clicks).
func (b *attBrowser) activate(i int, attach bool) bool {
	if i < 0 || i >= len(b.entries) {
		return false
	}
	e := b.entries[i]
	if e.dir {
		b.open(e.path)
		return true
	}
	if attach {
		if !b.sel[e.path] {
			b.toggle(e.path)
		}
		b.confirm()
		b.m.Dismiss()
		return true
	}
	b.toggle(e.path)
	return false
}

func (b *attBrowser) move(d int) {
	if len(b.entries) == 0 {
		return
	}
	b.cursor = min(max(b.cursor+d, 0), len(b.entries)-1)
	b.reveal = true
}

func (b *attBrowser) keys(gtx layout.Context) {
	for {
		ev, ok := gtx.Event(
			key.FocusFilter{Target: b},
			key.Filter{Focus: b, Name: key.NameUpArrow},
			key.Filter{Focus: b, Name: key.NameDownArrow},
			key.Filter{Focus: b, Name: key.NamePageUp},
			key.Filter{Focus: b, Name: key.NamePageDown},
			key.Filter{Focus: b, Name: key.NameHome},
			key.Filter{Focus: b, Name: key.NameEnd},
			key.Filter{Focus: b, Name: key.NameLeftArrow},
			key.Filter{Focus: b, Name: key.NameRightArrow},
			key.Filter{Focus: b, Name: key.NameDeleteBackward},
			key.Filter{Focus: b, Name: key.NameSpace},
			key.Filter{Focus: b, Name: key.NameReturn, Optional: key.ModShortcut},
			key.Filter{Focus: b, Name: key.NameEnter, Optional: key.ModShortcut},
		)
		if !ok {
			return
		}
		ke, ok := ev.(key.Event)
		if !ok || ke.State != key.Press {
			continue
		}
		page := 10
		switch ke.Name {
		case key.NameUpArrow:
			b.move(-1)
		case key.NameDownArrow:
			b.move(1)
		case key.NamePageUp:
			b.move(-page)
		case key.NamePageDown:
			b.move(page)
		case key.NameHome:
			b.move(-len(b.entries))
		case key.NameEnd:
			b.move(len(b.entries))
		case key.NameLeftArrow, key.NameDeleteBackward:
			if parent := filepath.Dir(b.dir); parent != b.dir {
				b.open(parent)
			}
		case key.NameRightArrow:
			if b.cursor < len(b.entries) && b.entries[b.cursor].dir {
				b.activate(b.cursor, false)
			}
		case key.NameSpace:
			if b.cursor < len(b.entries) && !b.entries[b.cursor].dir {
				b.toggle(b.entries[b.cursor].path)
			}
		case key.NameReturn, key.NameEnter:
			if ke.Modifiers.Contain(key.ModShortcut) && len(b.order) > 0 {
				b.confirm()
				b.m.Dismiss()
				return
			}
			if b.activate(b.cursor, true) && b.m.Dismissed() {
				return
			}
		}
	}
}

// layout is the modal body (called twice a frame: measure, then draw).
func (b *attBrowser) layout(gtx layout.Context) layout.Dimensions {
	th := b.a.h.Theme()
	if !b.focused {
		gtx.Execute(key.FocusCmd{Tag: b})
		b.focused = true
	}
	b.keys(gtx)
	if b.m.Dismissed() {
		return layout.Dimensions{}
	}
clicks:
	for i := range b.entries {
		for {
			cl, ok := b.clicks[i].Update(gtx)
			if !ok {
				break
			}
			b.cursor = i
			if b.activate(i, cl.NumClicks >= 2) {
				break clicks
			}
		}
	}
	if b.m.Dismissed() {
		return layout.Dimensions{}
	}

	w := gtx.Constraints.Max.X
	rowH := gtx.Dp(32)
	pathH := gtx.Dp(26)
	boxY := pathH + gtx.Dp(4)
	boxH := gtx.Dp(320)
	hintY := boxY + boxH + gtx.Dp(8)
	total := hintY + gtx.Dp(18)
	sh := th.Shaper

	// focus target over the whole body
	body := clip.Rect(image.Rect(0, 0, w, total)).Push(gtx.Ops)
	event.Op(gtx.Ops, b)
	body.Pop()

	// current folder
	offset(gtx, 0, gtx.Dp(2), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(w, pathH)}
		return label(gtx, sh, textStyle{Size: 13, LineHeight: 18, Color: th.C.FgMuted, MaxLines: 1}, b.dir)
	})

	// the list
	box := image.Rect(0, boxY, w, boxY+boxH)
	rad := gtx.Dp(8)
	fillRRect(gtx, box, rad, th.C.Bg)
	if b.reveal && len(b.entries) > 0 {
		b.reveal = false
		vis := max(boxH/rowH, 1)
		p := &b.list.Position
		if b.cursor < p.First || (b.cursor == p.First && p.Offset > 0) {
			p.First, p.Offset = b.cursor, 0
		} else if b.cursor >= p.First+vis {
			p.First, p.Offset = b.cursor-vis+1, 0
		}
	}
	offset(gtx, 0, boxY, func(gtx layout.Context) layout.Dimensions {
		inner := image.Rect(0, 0, w, boxH)
		defer clip.UniformRRect(inner, rad).Push(gtx.Ops).Pop()
		if b.err != "" && len(b.entries) <= 1 {
			offset(gtx, gtx.Dp(12), rowH*len(b.entries)+gtx.Dp(8), func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Constraints{Max: image.Pt(w-gtx.Dp(24), boxH)}
				return label(gtx, sh, textStyle{Size: 13, LineHeight: 18, Color: th.C.Danger}, b.err)
			})
		}
		gtx.Constraints = layout.Exact(image.Pt(w, boxH))
		return b.list.Layout(gtx, len(b.entries), func(gtx layout.Context, i int) layout.Dimensions {
			return b.row(gtx, th, i, w, rowH)
		})
	})
	strokeRRect(gtx, box, rad, max(1, gtx.Dp(1)), th.C.BorderStrong)

	// hint
	hint := "Click a file to select it, double-click or Enter to attach. Space selects, Backspace goes up."
	if n := len(b.order); n > 0 {
		hint = fmt.Sprintf("%d selected. %s attaches them.", n, shortcut.Format("Mod+Enter"))
	}
	offset(gtx, 0, hintY, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(w, gtx.Dp(18))}
		return label(gtx, sh, textStyle{Size: 13, LineHeight: 18, Color: th.C.FgMuted, MaxLines: 1}, hint)
	})
	return layout.Dimensions{Size: image.Pt(w, total)}
}

func (b *attBrowser) row(gtx layout.Context, th *theme.Theme, i, w, h int) layout.Dimensions {
	e := b.entries[i]
	click := &b.clicks[i]
	return click.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		r := image.Rect(0, 0, w, h)
		cur := i == b.cursor
		fg, sub := th.C.Fg, th.C.FgMuted
		switch {
		case cur:
			fillRect(gtx, r, rgb(attBlue))
			fg, sub = rgb(0xffffff), rgb(0xffffff)
		case click.Hovered():
			fillRect(gtx, r, th.C.BgHover)
		}
		pad := gtx.Dp(12)
		is := gtx.Dp(18)
		offset(gtx, pad, (h-is)/2, func(gtx layout.Context) layout.Dimensions {
			switch {
			case e.up:
				return icons.Get("arrow-up")(gtx, unit.Dp(18), sub)
			case e.dir:
				attFolder(gtx, is, sub)
				return layout.Dimensions{Size: image.Pt(is, is)}
			}
			return icons.Get(attKindOf(e.name, nil).Icon)(gtx, unit.Dp(18), sub)
		})
		right := w - pad
		checked := !e.dir && b.sel[e.path]
		if checked {
			col := th.C.Send
			if cur {
				col = rgb(0xffffff)
			}
			right -= is
			offset(gtx, right, (h-is)/2, func(gtx layout.Context) layout.Dimensions {
				return icons.Get("check")(gtx, unit.Dp(18), col)
			})
			right -= gtx.Dp(8)
		}
		var meta string
		switch {
		case e.up:
			meta = "Up"
		case e.dir:
			meta = ""
		default:
			meta = attHumanSize(e.size)
		}
		if e.dir && !e.up {
			cs := gtx.Dp(16)
			right -= cs
			offset(gtx, right, (h-cs)/2, func(gtx layout.Context) layout.Dimensions {
				return icons.Get("chevron-right")(gtx, unit.Dp(16), sub)
			})
		} else if meta != "" {
			mw := th.Measure(gtx, 13, theme.Regular, meta)
			right -= mw
			offset(gtx, right, (h-gtx.Dp(18))/2, func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Constraints{Max: image.Pt(mw+gtx.Dp(2), gtx.Dp(18))}
				return label(gtx, th.Shaper, textStyle{Size: 13, LineHeight: 18, Color: sub, MaxLines: 1}, meta)
			})
		}
		tx := pad + is + gtx.Dp(10)
		tw := max(right-gtx.Dp(12)-tx, 0)
		weight := theme.Regular
		if checked {
			weight = theme.Bold
		}
		offset(gtx, tx, (h-gtx.Dp(22))/2, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(tw, gtx.Dp(22))}
			return label(gtx, th.Shaper, textStyle{Size: 15, LineHeight: 22, Weight: weight, Color: fg, MaxLines: 1}, e.name)
		})
		pointer.CursorPointer.Add(gtx.Ops)
		return layout.Dimensions{Size: r.Size()}
	})
}

// attFolder draws a simple folder glyph in an s×s box.
func attFolder(gtx layout.Context, s int, col color.NRGBA) {
	f := func(v float32) int { return int(v*float32(s) + .5) }
	fillRRect(gtx, image.Rect(f(.08), f(.18), f(.48), f(.36)), f(.06), col)
	fillRRect(gtx, image.Rect(f(.08), f(.28), f(.92), f(.82)), f(.08), col)
}
