package richview

import (
	"sync"
	"sync/atomic"

	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

// Expander is Slack's expandables store and the defaults its readers use
// (core.App: expandables.go). The message parts that collapse read and
// write it under Slack's expandable keys (internal/expandable): the file
// list's caret (files), a link preview's media (attachment: its
// ExpandableContainer and "Hide image"), image and video blocks
// (blockImage, blockVideo: their title's trigger), and the "Show more" of
// a section (blockText) or an attachment's text (attachmentText). The
// store is the account's, kept across restarts, so /collapse and /expand
// reach every view that shows the message.
type Expander interface {
	// Expanded is getObjectById: the key's state, ok false for none.
	Expanded(key string) (on, ok bool)
	// SetExpanded is setExpanded.
	SetExpanded(key string, on bool)
	// AttachmentExpanded is isAttachmentExpanded (FDIC).
	AttachmentExpanded(key string, bytes int64, sender, ch string) bool
	// ImageTooLarge is isImageTooLarge (WGgb).
	ImageTooLarge(key string, bytes int64) bool
	// UnverifiedDisclosure is ExpandableContainer's
	// shouldShowUnverifiedDisclosure (pOD6).
	UnverifiedDisclosure(key string, bytes int64, sender, ch string) bool
	// CollapseFromUnverifiedUser is shouldCollapseFromUnverifiedUser
	// (Ns/U), FilesMetaTextWrapper's collapseFromUnverifiedUser.
	CollapseFromUnverifiedUser(sender, ch string) bool
	// ExpandablesGen moves whenever an answer above may have changed.
	ExpandablesGen() uint64
	// PrefMemberName is getPrefCompliantMemberNameById: the disclosure's
	// {memberName}.
	PrefMemberName(id string) string
	// AllowPreviewsFrom is the disclosure's "Allow all previews from
	// {memberName}": the member joins allowed_unfurl_senders (in the
	// background).
	AllowPreviewsFrom(member string)
	// ImageFile is the Slack file an image's url is, as the client holds
	// it with a download URL (AttachmentImage's getFileIdFromUrlWithState
	// and getFileById): the hover group's Download.
	ImageFile(url string) (*richtext.File, bool)
}

var expanderNow atomic.Pointer[expanderBox]

type expanderBox struct{ e Expander }

// SetExpander makes e the store every View reads (the UI sets core's at
// start); nil goes back to a store of the process's own, with the
// defaults read from uipref and no unverified senders.
func SetExpander(e Expander) {
	if e == nil {
		expanderNow.Store(nil)
		return
	}
	expanderNow.Store(&expanderBox{e})
}

// SetExpandable is setExpanded on the store every View reads (a menu's
// "Hide image").
func SetExpandable(key string, on bool) { xp().SetExpanded(key, on) }

// Expandable is getObjectById on the store every View reads: key's
// state, ok false for none (the reaction bar's isExpanded).
func Expandable(key string) (on, ok bool) { return xp().Expanded(key) }

// xp is the store in use.
func xp() Expander {
	if b := expanderNow.Load(); b != nil {
		return b.e
	}
	return &localExpander
}

// localExp is the store Views use without the app's (tests, previews).
type localExp struct {
	mu  sync.Mutex
	m   map[string]bool
	gen atomic.Uint64
}

var localExpander localExp

func (x *localExp) Expanded(key string) (bool, bool) {
	x.mu.Lock()
	defer x.mu.Unlock()
	on, ok := x.m[key]
	return on, ok
}

func (x *localExp) SetExpanded(key string, on bool) {
	if key == "" {
		return
	}
	x.mu.Lock()
	if old, ok := x.m[key]; ok && old == on {
		x.mu.Unlock()
		return
	}
	if x.m == nil {
		x.m = map[string]bool{}
	}
	x.m[key] = on
	x.mu.Unlock()
	x.gen.Add(1)
}

func (x *localExp) ImageTooLarge(key string, bytes int64) bool {
	if _, ok := x.Expanded(key); ok {
		return false
	}
	p := uipref.Get()
	return !p.NoInlineImgs && bytes > inlineImgLimit && !p.LargeImgs
}

func (x *localExp) AttachmentExpanded(key string, bytes int64, _, _ string) bool {
	if on, ok := x.Expanded(key); ok {
		return on
	}
	return !(uipref.Get().NoInlineImgs || x.ImageTooLarge(key, bytes))
}

func (x *localExp) UnverifiedDisclosure(string, int64, string, string) bool { return false }

func (x *localExp) CollapseFromUnverifiedUser(string, string) bool { return false }

func (x *localExp) ExpandablesGen() uint64 { return x.gen.Load() }

func (x *localExp) PrefMemberName(id string) string { return id }

func (x *localExp) AllowPreviewsFrom(string) {}

func (x *localExp) ImageFile(string) (*richtext.File, bool) { return nil, false }

// expGen is the store's generation as Layout compares it, with whose it
// is (a store set later is a change too).
func expGen() uint64 {
	if b := expanderNow.Load(); b != nil {
		return b.e.ExpandablesGen()<<1 | 1
	}
	return localExpander.ExpandablesGen() << 1
}

// containerExpanded is ExpandableContainer's isExpanded (pOD6
// mapStateToProps): with useAttachmentRules the attachment rules, else
// the key's state; no state is def (shouldBeExpandedByDefault).
func containerExpanded(key string, bytes int64, sender, ch string, attachmentRules, def bool) bool {
	if attachmentRules {
		return xp().AttachmentExpanded(key, bytes, sender, ch)
	}
	if on, ok := xp().Expanded(key); ok {
		return on
	}
	return def
}
