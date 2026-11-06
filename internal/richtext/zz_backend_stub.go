// Package richtext turns Slack messages into structured documents for
// native layout.
//
// THIS FILE IS A TEMPORARY STUB written by the backend agent so that
// internal/core compiles against the contract in docs/NATIVE.md while the
// real implementation is developed on branch `richtext`. Delete this file
// when merging that branch; it intentionally implements nothing beyond the
// contract's types and a plain-text fallback.
package richtext

import (
	"strings"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
)

type Style uint16

const (
	Bold Style = 1 << iota
	Italic
	Strike
	Code
	Underline
	Highlight
)

type SpanKind uint8

const (
	Text SpanKind = iota
	Link
	User
	Channel
	Usergroup
	Broadcast
	Emoji
	Date
	Break
)

type Span struct {
	Kind  SpanKind
	Text  string
	Style Style
	URL   string
	ID    string
	Self  bool
}

type BlockKind uint8

const (
	Paragraph BlockKind = iota
	Preformatted
	Quote
	List
	Header
	Divider
	Context
	Section
	Image
	Actions
	Attachment
	File
	Tombstone
)

type Block struct {
	Kind       BlockKind
	Spans      []Span
	Items      [][]Span
	Ordered    bool
	Indent     int
	Offset     int
	Border     bool
	Fields     [][]Span
	FieldShort []bool
	Image      *ImageInfo
	Buttons    []Button
	Att        *AttachmentInfo
	File       *FileInfo
	Children   []Block
}

// The contract names these Image, Attachment and File; the stub cannot
// because those identifiers are BlockKind constants here. core only uses
// them through Block fields, so the real package's names drop in.
type ImageInfo struct {
	URL   string
	Alt   string
	W, H  int
	Title string
	Proxy bool
}

type Button struct {
	Text, URL, Style, ActionID, Value, BlockID string
}

type AttachmentInfo struct {
	Color                              string
	Pretext                            []Span
	ServiceName, ServiceIcon           string
	AuthorName, AuthorIcon, AuthorLink string
	Title                              []Span
	TitleLink                          string
	Text                               []Block
	Footer                             []Span
	FooterIcon                         string
	TS                                 int64
	Image, Thumb                       *ImageInfo
	IsUnfurl, IsShare                  bool
	ShareChannel, FromURL              string
}

type FileInfo struct {
	ID, Name, Title, Mime, Type, PrettyType string
	Size                                    int64
	URL, Download, Permalink                string
	Thumb                                   *ImageInfo
	IsImage, IsVideo, IsAudio, IsSnippet    bool
	Preview                                 string
	Lines, DurationMS                       int
}

type Doc struct {
	Blocks []Block
	Jumbo  bool
}

type Resolver interface {
	UserName(id string) string
	ChannelName(id string) string
	UsergroupName(id string) string
	Emoji(name string) (url, text string, ok bool)
	SelfID() string
}

// FromMessage (stub): the message text as one plain paragraph.
func FromMessage(r Resolver, m *slack.Message) Doc {
	return Doc{Blocks: FromMrkdwn(r, m.Text)}
}

// FromMrkdwn (stub): text as one plain paragraph.
func FromMrkdwn(r Resolver, text string) []Block {
	if text == "" {
		return nil
	}
	return []Block{{Kind: Paragraph, Spans: []Span{{Kind: Text, Text: text}}}}
}

// PlainText (stub) joins span text.
func PlainText(d Doc) string {
	var b strings.Builder
	for _, bl := range d.Blocks {
		for _, s := range bl.Spans {
			b.WriteString(s.Text)
		}
	}
	return b.String()
}
