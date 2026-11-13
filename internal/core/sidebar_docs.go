package core

// Canvases and lists in the sidebar (Slack build fa938a086569,
// 2026-11-12; read from the public CDN chunks, nothing fetched with a
// session).
//
// Every standalone canvas and list has a hidden "file channel" for its
// comments: the file's id with F swapped for C (I3+E l/p). The server
// marks it is_file, is_channel, is_private and is_thread_only, and names
// it after the file's title (spaces, capitals and :emoji: included). A
// viewer who commented on a canvas or list is a member of its file
// channel, so users.conversations lists it next to the real private
// channels; drawn as one it came out as "🔒 :shopping_trolley: shop".
//
// Slack's sidebar:
//   - candidates (boot-shared lJBI, useCalculateSidebarSections): a file
//     channel (EK7S jT, deprecatedIsFileChannel: is_file) is dropped
//     unless O2By GH is on — lists_pin_to_sidebar (Hm0j fn),
//     programmable_sidebar or sidebar_ia6 — and a section lists it, it is
//     starred or it is temporary. The open conversation always stays.
//     Dropped channels don't count toward the sidebar's unread totals.
//   - its row (boot-render PZNh, m7We getSidebarDocumentType): "list"
//     when the file is a list (isListsEnabled, the PILOT release tier),
//     "canvas" when it is a canvas and programmable_sidebar is on;
//     anything else draws as the private channel it is. A document row's
//     prefix icon is UGOO getIconTypeForChannelPrefix (canvas: "canvas";
//     list: "lists" under programmable_sidebar, else "bullet-list"), and
//     its name goes through FormatTextWithEmoji (Un00, G6jU), so
//     :shortcodes: draw as emoji; other rows draw the name as text.
//   - its name (W+Ke getDisplayNameForChannel): a canvas file channel
//     shows the file's title, or "<#channel> canvas" / "Canvas for <DM>"
//     for a channel canvas (linked_channel_id); a list's is the channel
//     name, which is the list's title.
//   - a click (r5i1 onChannelSelect → openFileChannelFromSidebar), with
//     GH on: a list opens in the list view, a canvas in the canvas view
//     when programmable_sidebar or sidebar_ia6 is on; otherwise the
//     channel opens. The row is selected while its document is the
//     primary view (x6Wj getViewedDocumentFileId).
//   - elsewhere: the local channel lists skip is_file (I3y6
//     isValidViewableChannel), so a file channel isn't a channel to
//     switch to, mention or pick.

import (
	"context"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
)

const (
	ExpListsPinToSidebar   = "lists_pin_to_sidebar"
	ExpProgrammableSidebar = "programmable_sidebar"
	ExpSidebarIA6          = "sidebar_ia6"
)

func init() {
	experimentReads[ExpListsPinToSidebar] = on
	experimentReads[ExpProgrammableSidebar] = on
	experimentReads[ExpSidebarIA6] = on
}

// fileChannelsInSidebarOn is O2By GH: file channels may stay in the
// sidebar (when a section lists them, or starred or temporary).
func (a *App) fileChannelsInSidebarOn() bool {
	return a.experiment(ExpListsPinToSidebar) == "on" || a.programmableSidebarOn() || a.experiment(ExpSidebarIA6) == "on"
}

// programmableSidebarOn is O2By r2 (isProgrammableSidebarEnabled).
func (a *App) programmableSidebarOn() bool { return a.experiment(ExpProgrammableSidebar) == "on" }

// IsFileChannel reports whether c is a canvas's or list's hidden
// conversation (EK7S jT: is_file).
func IsFileChannel(c *slack.Conversation) bool { return c != nil && c.IsFile }

// FileOfChannel is a file channel's file id (I3+E l: C → F).
func FileOfChannel(ch string) string {
	if strings.HasPrefix(ch, "C") {
		return "F" + ch[1:]
	}
	return ch
}

// sidebarFile is what the sidebar knows of a file channel's file
// (files.info): its type and title.
type sidebarFile struct {
	typ     string // "canvas" | "list" | "" (another kind, or files.info failed)
	title   string
	linked  string // a channel canvas's linked_channel_id
	loading bool
	done    bool
}

type sidebarDocState struct {
	mu    sync.Mutex
	files map[string]*sidebarFile // file id
}

var sidebarDocStates sync.Map // *App -> *sidebarDocState

func (a *App) sidebarDocs() *sidebarDocState {
	if v, ok := sidebarDocStates.Load(a); ok {
		return v.(*sidebarDocState)
	}
	v, _ := sidebarDocStates.LoadOrStore(a, &sidebarDocState{files: map[string]*sidebarFile{}})
	return v.(*sidebarDocState)
}

// sidebarFileOf is the known state of a file channel's file (zero until
// files.info answered).
func (a *App) sidebarFileOf(fileID string) sidebarFile {
	st := a.sidebarDocs()
	st.mu.Lock()
	defer st.mu.Unlock()
	if f := st.files[fileID]; f != nil {
		return *f
	}
	return sidebarFile{}
}

// ensureSidebarFile fetches a shown file channel's file once (Slack's
// getSidebarDocumentType with fetchFileIfMissing); the sidebar is rebuilt
// when it arrives.
func (a *App) ensureSidebarFile(fileID string) {
	if a.C == nil || fileID == "" {
		return
	}
	st := a.sidebarDocs()
	st.mu.Lock()
	if f := st.files[fileID]; f != nil {
		st.mu.Unlock()
		return
	}
	st.files[fileID] = &sidebarFile{loading: true}
	st.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(a.bg, 20*time.Second)
		defer cancel()
		var r struct {
			File struct {
				Filetype        string `json:"filetype"`
				Mode            string `json:"mode"`
				PrettyType      string `json:"pretty_type"`
				Title           string `json:"title"`
				Name            string `json:"name"`
				LinkedChannelID string `json:"linked_channel_id"`
			} `json:"file"`
		}
		err := a.C.Call(ctx, "files.info", url.Values{"file": {fileID}, "count": {"0"}}, &r)
		f := &sidebarFile{done: true}
		if err == nil {
			ff := r.File
			switch {
			case ff.Filetype == "list" || ff.Mode == "list":
				f.typ = "list"
			case IsCanvasFile(ff.Filetype, ff.Mode, ff.PrettyType):
				f.typ = "canvas"
			}
			f.title, f.linked = strings.TrimSpace(ff.Title), ff.LinkedChannelID
		}
		st.mu.Lock()
		st.files[fileID] = f
		st.mu.Unlock()
		if err == nil {
			a.emit(Event{Type: "sidebar_changed"})
		}
	}()
}

// sidebarDocType is m7We getSidebarDocumentType: "list", "canvas" or ""
// (the row draws as a private channel).
func (a *App) sidebarDocType(c *slack.Conversation, f sidebarFile) string {
	if !IsFileChannel(c) {
		return ""
	}
	switch {
	case f.typ == "list": // isListsEnabled: Clack has lists
		return "list"
	case f.typ == "canvas" && a.programmableSidebarOn():
		return "canvas"
	}
	return ""
}

// fileChannelName is W+Ke getDisplayNameForChannel's file channel case:
// a canvas's title (or its linked channel's "… canvas"); "" keeps the
// channel's own name (lists, and files not loaded yet).
func (a *App) fileChannelName(c *slack.Conversation, f sidebarFile) string {
	if !IsFileChannel(c) || f.typ != "canvas" {
		return ""
	}
	if f.linked != "" {
		if lc, ok := a.S.Conv(f.linked); ok {
			switch {
			case lc.IsIM:
				return "Canvas for DM with " + a.S.UserName(lc.User)
			case lc.IsMPIM:
				return "Canvas for " + lc.Name
			default:
				return "#" + lc.Name + " canvas"
			}
		}
	}
	return f.title
}

// SidebarDocIcon is a document row's prefix icon (UGOO
// getIconTypeForChannelPrefix), "" for other rows.
func (a *App) SidebarDocIcon(doc string) string {
	switch doc {
	case "canvas":
		return "canvas"
	case "list":
		if a.programmableSidebarOn() {
			return "lists"
		}
		return "bullet-list"
	}
	return ""
}

// SidebarFileTarget is where a click on a sidebar row goes when the row
// is a file channel (r5i1 openFileChannelFromSidebar): "list" or
// "canvas" with the file id, or "" to open the channel.
func (a *App) SidebarFileTarget(ch string) (kind, fileID string) {
	c, ok := a.S.Conv(ch)
	if !ok || !IsFileChannel(c) || !a.fileChannelsInSidebarOn() {
		return "", ""
	}
	fileID = FileOfChannel(ch)
	switch a.sidebarFileOf(fileID).typ {
	case "list":
		return "list", fileID
	case "canvas":
		if a.programmableSidebarOn() || a.experiment(ExpSidebarIA6) == "on" {
			return "canvas", fileID
		}
	}
	return "", ""
}

// fileChannelConv fills a file channel's Conv: its document type, name
// and (for a document row) the name's emoji.
func (a *App) fileChannelConv(c *slack.Conversation, v *Conv) {
	if !IsFileChannel(c) {
		return
	}
	v.File, v.FileID = true, FileOfChannel(c.ID)
	f := a.sidebarFileOf(v.FileID)
	v.Doc = a.sidebarDocType(c, f)
	if n := a.fileChannelName(c, f); n != "" {
		v.Name = n
	}
	if v.Doc != "" {
		v.NameSpans = richtext.EmojiText(a.S, v.Name)
	}
}
