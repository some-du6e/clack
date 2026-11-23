// Package expandable is Slack's expandable keys (boot-shared icC3, build
// 9f450b8ad3db): the strings its expandables store (5vqP, storeKey
// "expandables", persisted) keeps each collapsible part of a message
// under, a file list, a link preview's media, an image block, a section's
// "Show more", and the keys /collapse and /expand set for a message.
//
// getExpandableKey builds each key from the kind, the message's ts and
// the kind's options; the view context (Zhyr Y: "Channel" for the channel
// pane, "Thread" for the thread pane, …) and "_agent_whisper" end the
// keys of the kinds that read them, so a file list collapsed in the
// channel stays open in the thread. Block keys (blockText, blockImage,
// blockVideo) take no view context. getMessageExpandableKeys is the keys
// one message's previews use.
package expandable

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/some-du6e/clack/internal/slack"
)

// Kind is getExpandableKey's first argument.
type Kind string

const (
	File                 Kind = "file"  // a message's legacy file field
	Files                Kind = "files" // the message's files (FilesMetaTextWrapper)
	Call                 Kind = "call"  // a call message's card
	Attachment           Kind = "attachment"
	AttachmentText       Kind = "attachmentText"       // an attachment's "Show more" (AttachmentText)
	AttachmentsOverLimit Kind = "attachmentsOverLimit" // the attachments past the limit
	BlockText            Kind = "blockText"            // a section's "Show more" (WithTextTruncation)
	BlockImage           Kind = "blockImage"           // an image block (Image)
	BlockVideo           Kind = "blockVideo"           // a video block (VideoBlock)
	CanvasRecap          Kind = "canvasRecap"
)

// The view contexts message lists pass (Zhyr Y; MESSAGE_PANE and THREAD
// are 8ouu D's "Channel" and "Thread", THREADS_VIEW its "Threads").
const (
	ViewChannel        = "Channel"
	ViewThread         = "Thread"
	ViewThreads        = "Threads"
	ViewUnreads        = "Punreads" // UNREAD_VIEW (GMW1 c: Unreads)
	ViewSearch         = "search"
	ViewSavedForLater  = "saved-for-later-page"
	ViewActivityInbox  = "activity-inbox"
	ViewPinsFlexpane   = "pins-flexpane"
	ViewSharedFiles    = "shared-files-flexpane"
	ViewSharedMessage  = "shared-message-dialog"
	ViewDeleteMessage  = "delete-msg-dialog"
	ViewHuddleThreads  = "huddle_threads"
	ViewPreviewPopover = "preview_popover"
)

// Opts are getExpandableKey's options. An empty string is JS's undefined
// where Slack's template reads it ("undefined" in the key), as the
// message fields they come from are absent rather than empty.
type Opts struct {
	ID string // file, call, attachment and attachmentText (an attachment's id, "" for none)
	// FileIDs is files' fileIds: nil is undefined (no key); an empty list
	// is a key with no ids, as JS's [] is truthy
	FileIDs                                  []string
	ImageURL, AudioHTML, VideoHTML, ThumbURL string // attachment
	Limit                                    int    // attachmentsOverLimit
	BlockID                                  string // blockText, blockImage, blockVideo
	ViewContext                              string
	Whisper                                  bool // isAgentWhisper
}

// undef is JS's `${x}` of a string option: "undefined" when absent.
func undef(s string) string {
	if s == "" {
		return "undefined"
	}
	return s
}

// Key is getExpandableKey(kind, ts, o): "" for an unknown kind, no ts, a
// files key without file ids or an attachment without media.
func Key(kind Kind, ts string, o Opts) string {
	if kind == "" || ts == "" {
		return ""
	}
	w := ""
	if o.Whisper {
		w = "_agent_whisper"
	}
	i := o.ViewContext + w
	// t.replace(".", "_"): the first only
	tsU := strings.Replace(ts, ".", "_", 1)
	switch kind {
	case File:
		return "inline_file_msg_" + tsU + undef(o.ID) + i
	case Files:
		if o.FileIDs == nil {
			return ""
		}
		return "inline_files_msg_" + tsU + strings.Join(o.FileIDs, "_") + i
	case Call:
		return "call_msg_" + undef(o.ID) + ts + i
	case Attachment:
		// `a.id || a.imageUrl`: the id, else the media's own string
		switch {
		case o.ImageURL != "":
			return "img_msg_" + ts + firstOr(o.ID, o.ImageURL) + i
		case o.AudioHTML != "":
			return "aud_msg_" + ts + firstOr(o.ID, o.AudioHTML) + i
		case o.VideoHTML != "":
			return "vid_msg_" + ts + firstOr(o.ID, undef(o.ThumbURL)) + i
		}
		return ""
	case AttachmentText:
		return "attach_text_" + undef(o.ID) + ts + i
	case AttachmentsOverLimit:
		return "attach_over_" + strconv.Itoa(o.Limit) + "_" + ts + i
	case BlockText:
		return "block_text_" + ts + "_" + undef(o.BlockID) + w
	case BlockImage:
		return "block_image_" + ts + "_" + undef(o.BlockID) + w
	case BlockVideo:
		return "block_video_" + ts + "_" + undef(o.BlockID) + w
	case CanvasRecap:
		return "canvas_recap_" + ts + i
	}
	return ""
}

func firstOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// AttachmentID is an attachment's id as getExpandableKey reads it: ""
// (JS's falsy 0) when it has none.
func AttachmentID(id int) string {
	if id == 0 {
		return ""
	}
	return strconv.Itoa(id)
}

// AttachmentKey is the attachment key of attachment a in the message ts
// shows in view context vc (mbtF and getAttachmentExpandableKeys alike).
func AttachmentKey(a *slack.Attachment, ts, vc string) string {
	return Key(Attachment, ts, Opts{ID: AttachmentID(a.ID), ImageURL: a.ImageURL, AudioHTML: a.AudioHTML,
		VideoHTML: a.VideoHTML, ThumbURL: a.ThumbURL, ViewContext: vc})
}

// imageBlock is what getImageBlockExpandableKeys reads of a block.
type imageBlock struct {
	Type      string  `json:"type"`
	BlockID   *string `json:"block_id"`
	Accessory *struct {
		Type string `json:"type"`
	} `json:"accessory"`
}

// ImageBlockKeys is getImageBlockExpandableKeys: the blockImage key of
// each image block in blocks, and of each section with an image
// accessory, in order. The blocks are the message's own or an
// attachment's; the key is the message's ts either way.
func ImageBlockKeys(blocks json.RawMessage, ts string) []string {
	var bs []imageBlock
	if len(blocks) == 0 || json.Unmarshal(blocks, &bs) != nil {
		return nil
	}
	var out []string
	for _, b := range bs {
		if b.Type != "image" && !(b.Type == "section" && b.Accessory != nil && b.Accessory.Type == "image") {
			continue
		}
		id := "undefined"
		if b.BlockID != nil {
			id = *b.BlockID
		}
		// the key's own `${blockId}` of a present block_id, "" too
		out = append(out, "block_image_"+ts+"_"+id)
	}
	return out
}

// present reports whether a JSON field holds a value JS reads as truthy
// for an array: present and not null.
func present(raw json.RawMessage) bool {
	s := strings.TrimSpace(string(raw))
	return s != "" && s != "null"
}

// MessageKeys is getMessageExpandableKeys(m, vc) (icC3 N, without agent
// whispers: Clack's messages carry no whisper_blocks): the keys of m's
// previews, the ones /collapse and /expand set. Only the first of these
// a message has counts: its files (one files key), a call's card, its
// attachments (each one's media key, then its image blocks), its own
// image blocks. Keys may repeat or be "" (an attachment without media),
// as Slack's list does; its store skips "".
func MessageKeys(m *slack.Message, vc string) []string {
	ts := m.TS
	switch {
	case m.Files != nil: // e.files: a list, empty or not
		ids := make([]string, len(m.Files))
		for i := range m.Files {
			ids[i] = m.Files[i].ID
		}
		return []string{Key(Files, ts, Opts{FileIDs: ids, ViewContext: vc})}
	case m.Subtype == "sh_room_shared" || m.Subtype == "sh_room_created":
		return []string{Key(Call, ts, Opts{ID: CallID(m), ViewContext: vc})}
	case m.Attachments != nil:
		var out []string
		for i := range m.Attachments {
			a := &m.Attachments[i]
			if k := AttachmentKey(a, ts, vc); k != "" {
				out = append(out, k)
			}
			out = append(out, ImageBlockKeys(a.Blocks, ts)...)
		}
		return out
	case present(m.Blocks):
		return ImageBlockKeys(m.Blocks, ts)
	}
	return nil
}

// CallID is a call message's _call_id, its room's id ("" when it has
// none: "undefined" in the key, as in Slack's).
func CallID(m *slack.Message) string {
	var r struct {
		ID string `json:"id"`
	}
	if len(m.Room) > 0 && json.Unmarshal(m.Room, &r) == nil {
		return r.ID
	}
	return ""
}

// OnDisk reports whether key may be written to disk: not the keys whose
// attachment had no id, which carry the media's URL or embed HTML.
func OnDisk(key string) bool {
	return key != "" && !strings.Contains(key, "://") && !strings.ContainsAny(key, "<>")
}
