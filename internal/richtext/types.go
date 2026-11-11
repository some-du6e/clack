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
	// FileStatus is the line above an optimistic message's files while the
	// viewer's uploads finish (PendingFilesMetaTextWrapper,
	// c-message_kit__file__meta): Spans holds "Uploading…" and the like.
	FileStatus
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
	// Level is a header block's level (1–4, 0 for none): Slack's Header
	// (/WQN) classes its text p-header_block--level_1/2/3 (4 draws as 3).
	Level int
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
	// Huddle is a huddle_thread message's huddle (huddle.go): its body is
	// Text's automated sentence, with a Join Huddle button while Joinable.
	Huddle       *HuddleRoom
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

	// Unfurl fields Slack's MessageAttachment reads (gantry-v2 shared boot
	// async, 2026-11): ID is the attachment's id (chat.deleteAttachment's
	// "attachment"), OriginalURL its original_url, ServiceURL service_url.
	// VideoHTML is set when the attachment has video_html (the player
	// Slack embeds after its play button), AudioHTML for audio_html;
	// Thumb then is the video's poster (thumb_url) even with Image set.
	// ImageBytes is image_bytes. BotID is the attachment's bot_id or, for
	// a bot's message, the message's (ax2i: attachment.botId ||
	// msg.bot_id): such attachments draw as the classic v1 card.
	// LinkedInText: from_url is linked in the message's text or blocks
	// (djIX shouldShow: such previews follow expand_non_media_attachments).
	// Hidden is attachment.hidden.
	// Legacy: made from a message's attachments array (not a video or
	// call block, a huddle card or a file).
	Legacy      bool
	ID          string
	OriginalURL string
	ServiceURL  string
	VideoHTML   bool
	AudioHTML   bool
	OtherHTML   bool
	// AudioHTMLCode is audio_html itself (the player's iframe markup) and
	// AudioHTMLHeight audio_html_height: AttachmentMedia's audioHtml and
	// audioHtmlHeight props (ClickToLoad reads the iframe's src). Kept in
	// memory only, like the rest of a message.
	AudioHTMLCode   string
	AudioHTMLHeight int
	ImageBytes      int64
	BotID           string
	IsAppUnfurl     bool
	// BotName is BotID's bot name as Slack's getBotById reads it (the
	// bots store: bots.info, or the message's bot_profile for the same
	// bot), "" while it is unknown: the "Added by" attribution then says
	// "a bot" (AttachmentFooterBotAttribution, renderAttributionMaybe).
	BotName      string
	LinkedInText bool
	Hidden       bool
	// Kit: the attachment has Block Kit blocks (Slack draws it as
	// MessageAttachmentDefaultV2: the color bar and the blocks only);
	// they are Text[KitFrom:].
	Kit     bool
	KitFrom int
	MsgTS   string
	// Context is the card's second line: plain spans and at most one
	// Channel span, drawn as Slack's InlineChannelEntity (hash or, with
	// ContextLock, lock icon + name). ContextLink is where it links.
	Context     []Span
	ContextLock bool
	ContextLink string
	// CardMember: a MsgCard whose AuthorID the Resolver knew (MemberNamer).
	// Slack draws such a card's author as the member, not the attachment's
	// author_name / author_icon (which the server fills from the real name
	// and a 48px picture): CardAuthor is the member's name under Slack's
	// display-name rule (AttachmentAuthorName's MemberLink, boot-shared
	// jnrm/25Rg) and CardAvatar their own picture (AttachmentAuthorAvatar's
	// ConnectedBaseAvatar, SMrF).
	CardMember bool
	CardAuthor string
	CardAvatar string
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
	Mode                                    string // files.mode: hosted, snippet, post, quip (canvases), list…
	Waveform                                []int  // audio_wave_samples (0..100)
	User                                    string // uploader
	Created                                 int64  // unix seconds
	IsExternal                              bool
	// Pending: the viewer's own upload, not finished yet (drawn at .5
	// with a spinner, c-pending_files__thumbnail--pending); Percent is its
	// percentComplete.
	Pending bool
	Percent int
}

// Doc is a whole message body.
type Doc struct {
	Blocks []Block
	Jumbo  bool // emoji-only, render emoji 32px
}

// MemberNamer is an optional Resolver extension: a member's name as
// Slack prints it for messages (getPrefCompliantMemberName, client-boot
// vUCu: the display name, the real name under display_real_names, "Name
// hidden" for someone the viewer hid; never the username) and their
// picture for a 36px avatar. ok is false while the member isn't known
// (the resolver asks for them and the Doc is rebuilt when they arrive).
type MemberNamer interface {
	MemberName(id string) (name string, ok bool)
	MemberAvatar(id string) string
}

// BotNamer is an optional Resolver extension: a bot's name from the
// bots cache (bots.info), "" when it isn't cached yet (the resolver then
// asks for it and the Doc is rebuilt when it arrives).
type BotNamer interface {
	BotName(id string) string
}

// Resolver supplies names and emoji while building a Doc.
type Resolver interface {
	UserName(id string) string
	ChannelName(id string) string
	UsergroupName(id string) string
	Emoji(name string) (url, text string, ok bool)
	SelfID() string
}
