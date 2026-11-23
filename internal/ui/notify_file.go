package ui

import (
	"strings"
	"unicode/utf8"

	"github.com/some-du6e/clack/internal/acrank"
	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/richtext"
)

// A notification's text for a message with files, as boot-shared Eley's
// extractTextFromMessage writes it (CDN build 35b42de8b870, 2026-11-21):
//
//   - Clips first: when any file is a clip (Qre6 containsClip: subtype
//     slack_audio or slack_video, e+f5 Y1), applyClipsTemplate's line by
//     the clips (getClipsMetaTemplate: one audio clip, one video clip, or
//     "a few clips") and the message (getClipsTemplate: a reply, SPMg
//     isReply, says "replied with"; a message whose text isn't empty adds
//     the converted text as "and a message, …").
//   - Then the first file (lodash head, 7RVK), when the message's text
//     converts to nothing or its subtype is a file subtype (Jg0n
//     isFileSubtype: file_comment, file_mention, file_share,
//     file_upload): with no text its title (R3SR getFileTitle), else,
//     and always for a file subtype, MXE2's getFileMetaTextForMessage.
//   - Else the text, then the attachments, then the missing text
//     (notifyText).
//
// Slack's i18n (Fbwy t) escapes every string placeholder (lodash escape,
// +Fi8), which the shell's lodash unescape undoes for the toast (toastBody);
// the uploader's possessive goes through t twice, so its name is escaped
// twice, as Slack's is. Clack's i18n.T doesn't escape, so noteT does.

var (
	nsMessageNote = i18n.NS("message")    // MXE2's meta lines (new r.Ay("message"))
	nsStoriesAV   = i18n.NS("stories_av") // Eley's clip lines
)

// noteFileSubtypes is Jg0n's file subtypes.
var noteFileSubtypes = map[string]bool{"file_comment": true, "file_mention": true, "file_share": true, "file_upload": true}

// noteEscape is lodash escape (+Fi8).
var noteEscape = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&#39;").Replace

// noteT is Fbwy's t for a source string with placeholders: each string
// argument escaped (buildTranslationContext), then formatted.
func noteT(ns i18n.NS, src string, args ...string) string {
	if len(args) == 0 {
		return ns.T(src)
	}
	a := make([]any, len(args))
	for i, v := range args {
		if i%2 == 1 {
			v = noteEscape(v)
		}
		a[i] = v
	}
	return ns.T(src, a...)
}

// noteFilesText is extractTextFromMessage's line for a message with files,
// and whether it has one: the clips line, else the first file's title or
// meta line when text (the converted message text) is empty or the
// subtype is a file subtype. ok false leaves text to speak.
func noteFilesText(r richtext.Resolver, m *core.Message, text string) (string, bool) {
	f := m.NoteFiles
	if s, ok := r.(noteFileStore); ok {
		f = s.NoteFilesNow(m)
	}
	if f == nil {
		return "", false
	}
	if f.Clips > 0 {
		return noteClipsText(m, f, text), true
	}
	if text != "" && !noteFileSubtypes[m.Subtype] {
		return "", false
	}
	if text == "" {
		if t := richtext.NoteFileTitle(f.Title); t != "" {
			return t, true
		}
	}
	return noteFileMeta(r, m, f), true
}

// noteClipsText is applyClipsTemplate: getClipsMetaTemplate's set by the
// clips (one audio clip, one video clip, a few), getClipsTemplate's line
// in it: a reply (thread_ts other than ts) "replied with …", else "sent
// …", with ", and a message, "{messageText}"" when the message's own text
// isn't empty (messageText is the converted text, possibly empty).
func noteClipsText(m *core.Message, f *richtext.NoteFiles, text string) string {
	reply := m.ThreadTS != "" && m.ThreadTS != m.TS
	with := m.Text != ""
	var src string
	switch {
	case f.Clips > 1:
		src = pick4(reply, with, "sent a few clips", `sent a few clips and a message, "{messageText}"`,
			"replied with a few clips", `replied with a few clips and a message, "{messageText}"`)
	case f.AudioClip:
		src = pick4(reply, with, "sent an audio clip", `sent an audio clip and a message, "{messageText}"`,
			"replied with an audio clip", `replied with an audio clip and a message, "{messageText}"`)
	default:
		src = pick4(reply, with, "sent a video clip", `sent a video clip and a message, "{messageText}"`,
			"replied with a video clip", `replied with a video clip and a message, "{messageText}"`)
	}
	if with {
		return noteT(nsStoriesAV, src, "messageText", text)
	}
	return noteT(nsStoriesAV, src)
}

func pick4(reply, with bool, sent, sentWith, replied, repliedWith string) string {
	switch {
	case reply && with:
		return repliedWith
	case reply:
		return replied
	case with:
		return sentWith
	}
	return sent
}

// noteFileStore is core.App's NoteFilesNow, through the toast's resolver
// (noteNames): the files as Slack's extractTextFromMessage reads them
// when the toast is built, the first one from the file store
// (getFileById, core note_files.go).
type noteFileStore interface {
	NoteFilesNow(m *core.Message) *richtext.NoteFiles
}

// NoteFilesNow is noteFileStore.
func (r noteNames) NoteFilesNow(m *core.Message) *richtext.NoteFiles {
	if r.b == nil || r.b.core == nil {
		return m.NoteFiles
	}
	return r.b.core.NoteFilesNow(m)
}

// noteFileMeta is getFileMetaTextForMessage (MXE2): the template set by
// the file's kind (getFileMetaTemplate: snippet, email, post, image, else
// file) and the line in it by chooseFileMetaTemplate: someone else's file
// the message didn't upload (file.user isn't the sender:
// getMsgSenderEntity's userId, NoteFiles.Sender) is "shared {name’s}
// …"; an upload "uploaded this …" ("added this … snippet" for a
// snippet), "… and commented on …" when the file has an initial_comment;
// any other "shared this …". The name is memberNameFromFile
// (getPrefCompliantMemberName of getMemberById(file.user), "" for one
// the store doesn't hold) with its possessive affix (ajnp), and the
// kind's word for a snippet is the file's pretty_type, else "file".
// getMemberById asks for a member it doesn't hold (fetchIfMissing, the
// UnknownFetchManager), and ensureModelsArePresent never asked for the
// uploader (its members are the mentions, msg.user and the DM's people),
// so the toast names what the store holds and the ask goes out from here
// (store.MemberName: a miss is noted for the members pass).
func noteFileMeta(r richtext.Resolver, m *core.Message, f *richtext.NoteFiles) string {
	var set noteMetaSet
	switch f.Kind {
	case "snippet":
		set = noteMetaSnippet
	case "email":
		set = noteMetaEmail
	case "post":
		set = noteMetaPost
	case "image":
		set = noteMetaImage
	default:
		set = noteMetaFile
	}
	var src string
	switch {
	case !m.Upload && f.User != f.Sender:
		name := ""
		if n, ok := r.(noteMemberNamer); ok {
			name, _ = n.MemberName(f.User)
		}
		possessive := noteT(nsMessageNote, "{uploader_name}{uploader_name_affix}",
			"uploader_name", name, "uploader_name_affix", notePossessive(name, i18n.Locale()))
		return noteT(nsMessageNote, set.sharedPossessive,
			"uploader_name_possessive_html", possessive, "file_pretty_name", notePrettyType(f))
	case m.Upload && f.Kind == "snippet" && f.Commented:
		src = set.addedCommented
	case m.Upload && f.Kind == "snippet":
		src = set.added
	case m.Upload && f.Commented:
		src = set.uploadedCommented
	case m.Upload:
		src = set.uploaded
	default:
		src = set.shared
	}
	if set.takesName {
		return noteT(nsMessageNote, src, "file_pretty_name", notePrettyType(f))
	}
	return noteT(nsMessageNote, src)
}

// noteMemberNamer is store.Store's MemberName: getPrefCompliantMemberName
// ("Name hidden" for a member the viewer hid) of getMemberById, a member
// the store doesn't hold asked for.
type noteMemberNamer interface {
	MemberName(id string) (string, bool)
}

// notePrettyType is getFileMetaTextForMessage's file_pretty_name: the
// file's pretty_type, else "file".
func notePrettyType(f *richtext.NoteFiles) string {
	if f.PrettyType != "" {
		return f.PrettyType
	}
	return nsMessageNote.T("file")
}

// notePossessive is ajnp's possessive: the affix after a name in loc
// (getLocaleForLastViewedTeam).
func notePossessive(name, loc string) string {
	switch loc {
	case "fr-FR":
		if d := acrank.Deburr(name); d != "" && strings.ContainsRune("aeiouyAEIOUY", rune(d[0])) {
			return "d’"
		}
		return "de "
	case "es-ES", "es-LA", "pt-BR":
		return "de "
	case "de-DE":
		if c, _ := utf8.DecodeLastRuneInString(name); strings.ContainsRune("sSxXzZß", c) {
			return "’"
		}
		return "s"
	case "ja-JP":
		return "の"
	case "ko-KR":
		return "의"
	case "it-IT":
		return "di "
	case "zh-CN", "zh-TW":
		return " 的"
	case "en-US", "en-GB":
		return "’s"
	}
	return ""
}

// noteMetaSet is one of MXE2's template sets, the lines
// chooseFileMetaTemplate can pick (never a mentioned or another
// possessive one, and "added" only for a snippet). takesName: the set's
// non-possessive lines name the file's pretty_type (the snippet's).
type noteMetaSet struct {
	sharedPossessive, shared, uploaded, uploadedCommented, added, addedCommented string
	takesName                                                                    bool
}

var (
	noteMetaSnippet = noteMetaSet{
		sharedPossessive:  "shared {uploader_name_possessive_html} {file_pretty_name} snippet",
		shared:            "shared this {file_pretty_name} snippet",
		uploaded:          "uploaded this {file_pretty_name} snippet",
		uploadedCommented: "uploaded and commented on this {file_pretty_name} snippet",
		added:             "added this {file_pretty_name} snippet",
		addedCommented:    "added and commented on this {file_pretty_name} snippet",
		takesName:         true,
	}
	noteMetaEmail = noteMetaSet{
		sharedPossessive:  "shared {uploader_name_possessive_html} email",
		shared:            "shared this email",
		uploaded:          "uploaded this email",
		uploadedCommented: "uploaded and commented on this email",
	}
	noteMetaPost = noteMetaSet{
		sharedPossessive:  "shared {uploader_name_possessive_html} post",
		shared:            "shared this post",
		uploaded:          "uploaded this post",
		uploadedCommented: "uploaded and commented on this post",
	}
	noteMetaImage = noteMetaSet{
		sharedPossessive:  "shared {uploader_name_possessive_html} image",
		shared:            "shared this image",
		uploaded:          "uploaded this image",
		uploadedCommented: "uploaded and commented on this image",
	}
	noteMetaFile = noteMetaSet{
		sharedPossessive:  "shared {uploader_name_possessive_html} file",
		shared:            "shared this file",
		uploaded:          "uploaded this file",
		uploadedCommented: "uploaded and commented on this file",
	}
)
