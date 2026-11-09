// Package richtext turns Slack messages (mrkdwn, rich_text, Block Kit,
// attachments, unfurls, files) into a structured document (Doc) that the
// native UI lays out. It replaces the HTML renderer in internal/render.
//
// Everything here is pure: no I/O, no globals mutated, safe for concurrent use
// as long as the Resolver is.
package richtext

// Style is a bit set of inline text styles.
type Style uint16

const (
	Bold Style = 1 << iota
	Italic
	Strike
	Code
	Underline
	Highlight
)

// SpanKind says what an inline span is.
type SpanKind uint8

const (
	Text SpanKind = iota
	Link
	User
	Channel
	Usergroup
	Broadcast // @here @channel @everyone
	Emoji
	Date
	Break // hard line break

	// Additions beyond the original contract (append-only):
	ColorSwatch // rich_text color element: Text is the hex value ("#1D9BD1"); draw a swatch before it
	InlineImage // small inline image (context block image element): URL, Text = alt text
	// MessageMention is a message_mention (a pasted Slack message
	// permalink): Slack's link pill "💬 karim in #clack". Text is the
	// label, URL the permalink, ID the message ts; Lock draws the private
	// channel's lock before the channel name, Off is the disabled pill
	// ("Private message").
	MessageMention
)

// Span is one run of inline content.
type Span struct {
	Kind  SpanKind
	Text  string // display text, already unescaped and resolved ("@karim", "#general", "@here", formatted date); never contains "\n" (see Break)
	Style Style
	URL   string // Link: target. Emoji: image URL (custom or Slack CDN standard).
	ID    string // User/Channel/Usergroup id; Emoji: name (with ::skin-tone-N); Broadcast: "here"|"channel"|"everyone"; Date: unix seconds
	Self  bool   // User mention of the viewer, or a broadcast (yellow highlight)

	// Additions (MessageMention and Channel; channel.go):
	Lock bool // the label's channel is private (lock icon before its name)
	Off  bool // the message or channel can't be shown ("Private message", "Private channel"): not a link
	Pre  int  // MessageMention with Lock: bytes of Text before the channel name
}

// BlockKind says what a block is.
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
	ImageBlock // named *Block because Image, Attachment and File are also struct types
	Actions
	AttachmentBlock
	FileBlock
	Tombstone
	Table // Block Kit table: Rows of cells (first row is the header row Slack draws bold)
	Agent // plan / task_card / context_actions: Buttons[0] names the block (agent.go); Spans is the text fallback
	// Unsupported is a block Slack's renderer can't draw (one that doesn't
	// decode, an image block without an image): Slack's per-block error
	// fallback, "This content could not be displayed."
	Unsupported
	// UnknownBlock is a block whose type Slack's client doesn't know (an
	// agent's newer artifact type, say): UnsupportedBlockFallback (l2Ua),
	// a card reading "This message can’t be fully displayed."
	UnknownBlock
)

// Block is one vertical unit of a document.
type Block struct {
	Kind       BlockKind
	Spans      []Span   // inline content (Paragraph, Preformatted, Quote, Header, Context, Section text, Tombstone)
	Items      [][]Span // List items
	Ordered    bool     // List
	Indent     int      // List
	Offset     int      // List
	Border     bool     // List (also Quote/Preformatted from rich_text border)
	Fields     [][]Span // Section fields / attachment fields (Short flag in FieldShort)
	FieldShort []bool
	Image      *Image   // Image block, section accessory, attachment image/thumb
	Buttons    []Button // Actions block / section button accessory
	Att        *Attachment
	File       *File
	Children   []Block    // nested blocks: attachment files; Section text that has quotes/pre (then Spans is nil); Context elements (Paragraph / ImageBlock each, Spans keeps the joined text)
	Rows       [][][]Span // Table: rows → cells → inline spans (multi-paragraph cells joined by Break)
	ColAlign   []string   // Table: per-column "left" | "center" | "right" ("" = left)
	ColWrap    []bool     // Table: per-column is_wrapped
	// Kit marks a block made from a Block Kit layout block (section,
	// context, header, divider, image, actions, table): it gets Slack's
	// p-*_block margins and chrome. Canvases and attachments reuse the
	// kinds without it.
	Kit bool
	// BlockID is the Block Kit block_id (sections: the key their "Show
	// more" state is kept under); Expand is a section's "expand": true,
	// which turns that truncation off.
	BlockID string
	Expand  bool
}

// Image is a remote image reference.
type Image struct {
	URL   string
	Alt   string
	W, H  int
	Title string
	Proxy bool  // needs the session cookie (files.slack.com)
	Bytes int64 // image block image_bytes (the title row's "(12 kB)")
}

// Button is a Block Kit button (or a placeholder for another interactive element).
type Button struct {
	Text     string
	URL      string
	Style    string // primary|danger|""
	ActionID string
	Value    string
	BlockID  string
	Kind     string // addition: "button", "overflow", "select", "datepicker", "timepicker", "datetimepicker", "input", "checkboxes", "radio_buttons"
}

// Attachment is a legacy attachment, link unfurl or shared/forwarded message.
type Attachment struct {
	Color        string
	Pretext      []Span
	ServiceName  string
	ServiceIcon  string
	AuthorName   string
	AuthorIcon   string
	AuthorLink   string
	Title        []Span
	TitleLink    string
	Text         []Block
	Footer       []Span
	FooterIcon   string
	TS           int64
	Image        *Image
	Thumb        *Image
	IsUnfurl     bool
	IsShare      bool
	ShareChannel string // channel name without "#"
	FromURL      string

	// Additions beyond the original contract (append-only):
	AuthorSubname  string // e.g. "@handle" next to the author name in unfurls
	ShareChannelID string // shared message's channel id
	VideoURL       string // video block: the playable/embeddable URL

	// MsgCard is a message permalink unfurl (is_msg_unfurl) drawn as
	// Slack's MessageAttachmentSlackMessageCard: 36px avatar, author,
	// time, the Context line ("Posted in # clack"), then Text and the
	// message's files (Block.Children). AuthorID is the linked message's
	// author, MsgTS its ts.
	MsgCard  bool
	AuthorID string
	MsgTS    string
	// Context is the card's second line: plain spans and at most one
	// Channel span, drawn as Slack's InlineChannelEntity (hash or, with
	// ContextLock, lock icon + name). ContextLink is where it links.
	Context     []Span
	ContextLock bool
	ContextLink string
}

// File is a file shared in a message.
type File struct {
	ID, Name, Title, Mime, Type, PrettyType string
	Size                                    int64
	URL, Download, Permalink                string
	Thumb                                   *Image // best thumb for images/videos/pdfs
	IsImage, IsVideo, IsAudio, IsSnippet    bool
	Preview                                 string
	Lines                                   int
	DurationMS                              int
	OrigW, OrigH                            int    // original_w/h (videos: original, else thumb_video)
	Subtype                                 string // "slack_audio", "slack_video" (clips)
	Waveform                                []int  // audio_wave_samples (0..100)
	User                                    string // uploader
	Created                                 int64  // unix seconds
	IsExternal                              bool
}

// Doc is a whole message body.
type Doc struct {
	Blocks []Block
	Jumbo  bool // emoji-only, render emoji 32px
}

// Resolver supplies names and emoji while building a Doc.
type Resolver interface {
	UserName(id string) string
	ChannelName(id string) string
	UsergroupName(id string) string
	Emoji(name string) (url, text string, ok bool)
	SelfID() string
}
