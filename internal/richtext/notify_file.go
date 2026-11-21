package richtext

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/some-du6e/clack/internal/slack"
)

// NoteFiles is what a notification's text reads of a message's files
// (boot-shared Eley extractTextFromMessage): its first file (lodash head,
// 7RVK), for the title or the meta line (MXE2 getFileMetaTextForMessage),
// and its clips (Qre6 findClips), for the clips line. The message keeps
// it in place of the files themselves (core.Message.NoteFiles).
type NoteFiles struct {
	// The first file's title as sent, its uploader (file.user), its
	// pretty_type, and its kind (FileType, 2grX getFileType).
	Title, User, PrettyType, Kind string
	// Commented: the first file carries an initial_comment.
	Commented bool
	// Clips counts the files whose subtype is a clip's (e+f5 Y1:
	// slack_audio, slack_video); AudioClip is whether the first of them
	// is an audio clip (isAudioClipSubtype).
	Clips     int
	AudioClip bool
}

// NoteFilesOf is fs's NoteFiles, nil for a message with no files (Eley's
// head(t.files) is undefined for an empty list too).
func NoteFilesOf(fs []slack.File) *NoteFiles {
	if len(fs) == 0 {
		return nil
	}
	f := &fs[0]
	n := &NoteFiles{Title: f.Title, User: f.User, PrettyType: f.PrettyType, Kind: FileType(f), Commented: bool(f.InitialComment)}
	for i := range fs {
		switch fs[i].Subtype {
		case "slack_audio", "slack_video":
			if n.Clips == 0 {
				n.AudioClip = fs[i].Subtype == "slack_audio"
			}
			n.Clips++
		}
	}
	return n
}

// NoteFileTitle is getFileTitle (client-boot R3SR): the title with
// lodash unescape's entities decoded (L7LR), then trimmed of JavaScript's
// \s (lodash trim, fEyw).
func NoteFileTitle(title string) string {
	return strings.TrimFunc(lodashUnescape(title), isJSSpace)
}

// isJSSpace is JavaScript's \s: Go's unicode.IsSpace without U+0085, with
// U+FEFF.
func isJSSpace(r rune) bool { return r != 0x85 && unicode.IsSpace(r) || r == 0xfeff }

var lodashUnescape = strings.NewReplacer("&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`, "&#39;", "'").Replace

// FileType is Slack's getFileType (client-boot 2grX) over vITP's
// predicates, in its order: "image" (fileIsImage, which starts with
// shouldShowFile), "audio" (fileIsPlayableAudio), "video"
// (fileIsPlayableVideo), "post" (fileIsPost: mode post, space or docs,
// not a quip), "list", "slides", "thread_share", "quip" (fileIsCanvas),
// "huddle_transcript", then by mode "snippet", "email" or "docs", else
// "generic". (vITP's isNonExistentRecord, a store placeholder, can't be a
// message's own file.)
func FileType(f *slack.File) string {
	switch {
	case f == nil:
		return ""
	case fileShown(f) && slack.FileIsImage(f):
		return "image"
	case fileIsPlayableAudio(f):
		return "audio"
	case slack.FileIsPlayableVideo(f):
		return "video"
	case f.Filetype != "quip" && (f.Mode == "post" || f.Mode == "space" || f.Mode == "docs"):
		return "post"
	case f.Mode == "list" || f.Filetype == "list":
		return "list"
	case f.Filetype == "slides" && f.Mimetype == "application/vnd.slack-slides":
		return "slides"
	case f.Filetype == "thread_share" || f.Mimetype == "application/vnd.slack-thread-share":
		return "thread_share"
	case f.Mode == "quip" || f.Filetype == "quip":
		return "quip"
	case f.Mode == "huddle_transcript" || f.Filetype == "huddle_transcript":
		return "huddle_transcript"
	case f.Mode == "snippet", f.Mode == "email", f.Mode == "docs":
		return f.Mode
	}
	return "generic"
}

// fileShown is shouldShowFile over the fields a message's file carries
// (FileStatusOf "normal").
func fileShown(f *slack.File) bool {
	return FileStatusOf(FileFacts{Mode: f.Mode, FileAccess: f.FileAccess, IsDeleted: f.IsDeleted,
		HiddenByLimit: f.IsHiddenByLimit, Tombstoned: f.IsTombstoned}) == "normal"
}

var (
	supportedAudio = regexp.MustCompile(`^(m4a|mp3|wav)$`)
	supportedVideo = regexp.MustCompile(`^(mp4|mov|webm)$`)
)

// fileIsPlayableAudio is vITP's fileIsPlayableAudio: not a tombstone, a
// hosted file of a supported audio or video filetype (m4a, mp3, wav; mp4,
// mov, webm; or an audio story), and an audio clip (lqEY) or of a
// supported audio filetype. The filetype checks are case-sensitive.
func fileIsPlayableAudio(f *slack.File) bool {
	if f.Mode == "tombstone" {
		return false
	}
	audio := supportedAudio.MatchString(f.Filetype) || f.Subtype == "audio_story"
	if !(audio || supportedVideo.MatchString(f.Filetype)) || !slack.FileIsHosted(f) {
		return false
	}
	return isAudioClip(f.Subtype) || audio
}
