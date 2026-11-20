package composer

// TextyInput is the topic and description input of Slack's
// useChannelTopicInput and useChannelPurposeInput (boot-shared wD23 and
// QBdp): TextyInputWithAutocomplete (vJdM) without rich text, holding the
// stored mrkdwn as message_to_contents_string made it (texty_mrkdwn.go):
// text, mention pills over their labels and emoji. Formatting keys do
// nothing, Enter adds a line (onTextyEnter), Tab and Escape go on to the
// dialog. Save compares Value with the untouched input's (handleSave's
// `t !== U`) and sends Submit.

import (
	"image/color"

	"gioui.org/io/event"
	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

// TextyHost is what a TextyInput reads: the core (names, emoji) and the
// image cache (emoji images).
type TextyHost interface {
	Core() *core.App
	Images() *imgcache.Cache
}

// TextyInput is one plain Texty input over a stored mrkdwn string.
type TextyInput struct {
	h    TextyHost
	ed   *Editor
	base string // convertContentsToString of the initialText (U)
	ph   string
}

// NewTextyInput loads stored mrkdwn text into a new input with
// placeholder ph, the caret at the end.
func NewTextyInput(h TextyHost, text, ph string) *TextyInput {
	t := &TextyInput{h: h, ph: ph}
	t.ed = NewEditor()
	t.ed.textOnly, t.ed.plainText = true, true
	var names TextyNames
	if a := h.Core(); a != nil {
		if a.S != nil {
			names = a.S
		}
		api := coreAPI{a}
		t.ed.emoji = func(name string) (*Atom, bool) {
			url, txt, ok := api.emoji(name)
			if !ok {
				return nil, false
			}
			return emojiAtomFrom(name, url, txt), true
		}
	}
	if names == nil {
		names = noTextyNames{}
	}
	d := MrkdwnTextyDoc(names, text, t.ed.emoji, uipref.Get().EmojiAsText)
	t.ed.SetDoc(d)
	t.base = TextyValue(d)
	return t
}

// noTextyNames names nothing: every mention shows its id.
type noTextyNames struct{}

func (noTextyNames) User(string) (*slack.User, bool) { return nil, false }
func (noTextyNames) ChannelName(id string) string    { return id }
func (noTextyNames) UsergroupName(id string) string  { return id }
func (noTextyNames) UsergroupsLoaded() bool          { return false }

// Tag is the input's focus tag.
func (t *TextyInput) Tag() event.Tag { return t.ed }

// Text is what the input shows: its text with each pill's label.
func (t *TextyInput) Text() string { return t.ed.Doc().Text() }

// SetText replaces the input's contents with plain text (tests).
func (t *TextyInput) SetText(s string) {
	d := NewDoc()
	d.Insert(0, s, Attr{})
	t.ed.SetDoc(d)
}

// Value is the input's value(): convertContentsToString of its contents.
func (t *TextyInput) Value() string { return TextyValue(t.ed.Doc()) }

// Changed reports whether Value differs from the untouched input's.
func (t *TextyInput) Changed() bool { return t.Value() != t.base }

// Submit is what submitChannelTopic and submitChannelPurpose send for the
// input: linkifyPhoneNumbers(clean_message_text_with_state(value)).
func (t *TextyInput) Submit() string {
	p := uipref.Get()
	return LinkifyPhoneNumbers(CleanMessageText(t.Value(), p.ConvertEmoticons && !p.EmojiAsText))
}

// Layout draws the input's text in Slack's 15/22 with col as its ink
// (the dialog draws the box around it).
func (t *TextyInput) Layout(gtx layout.Context, th *theme.Theme, col color.NRGBA) layout.Dimensions {
	for {
		if _, ok := t.ed.Update(gtx); !ok {
			break
		}
	}
	st := DefaultEditorStyle(th.Shaper)
	p := &th.C
	st.Color, st.Caret, st.Selection, st.Placeholder = col, col, p.Selection, p.FgSubtle
	st.LinkColor, st.MentionColor, st.MentionBg = p.Link, p.Mention, p.MentionBg
	st.BroadcastColor, st.BroadcastBg = p.Mention, p.SelfMention
	st.CodeColor, st.CodeBg, st.CodeBorder = p.CodeFg, p.CodeBg, p.CodeBorder
	st.PreBg, st.PreBorder, st.QuoteBar = p.PreBg, p.CodeBorder, p.QuoteBar
	st.PlaceholderText, st.MaxHeight, st.EmojiSize = t.ph, 0, 22
	if im := t.h.Images(); im != nil {
		st.Images = im.Get
	}
	return t.ed.Layout(gtx, st)
}
