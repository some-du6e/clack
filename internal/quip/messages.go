package quip

// The messages, field for field from Slack's descriptors (web client
// build 9685e6ce144b; the whole schema was dumped from Slack's chunks
// with round 1's tools, docs/CANVAS-EDIT-PLAN.md). Only what Clack reads
// or writes is here; Unmarshal skips the rest.

// LoadRequest is syncer.LoadData.Request: load-data's request_binary.
type LoadRequest struct {
	ObjectIDs          []string   `pb:"1"`
	IndexIDs           []string   `pb:"2"`
	BulkLoads          []BulkLoad `pb:"3"`
	UserAction         *string    `pb:"5"`
	UserActionSequence *int32     `pb:"6"`
}

// BulkLoad is syncer.LoadData.Request.BulkLoad.
type BulkLoad struct {
	Type               *int32  `pb:"1"` // BulkThread
	Arg0               *string `pb:"2"`
	Arg1               *string `pb:"3"`
	Mobile             *bool   `pb:"4"`
	Minimal            *bool   `pb:"5"`
	SkipSfdcCdoCache   *bool   `pb:"6"`
	IsPreload          *bool   `pb:"7"`
	SkipIndexedDBCache *bool   `pb:"8"`
}

// BulkLoad types (syncer.LoadData.Request.BulkLoad.Type).
const (
	BulkInbox  int32 = 0
	BulkThread int32 = 1
)

// LoadWrapper is syncer.LoadData.ResponseWrapper: load-data's answer.
type LoadWrapper struct {
	Error         *string        `pb:"1"`
	Response      *LoadResponse  `pb:"2"`
	Preserialized *Preserialized `pb:"3"`
}

// LoadResponse is syncer.LoadData.Response.
type LoadResponse struct {
	Success   *bool     `pb:"1"`
	Objects   *Payload  `pb:"2"`
	Indexes   []Index   `pb:"3"`
	Failures  []Failure `pb:"6"`
	FromCache *bool     `pb:"9"`
}

// Index is syncer.LoadData.Response.Index ("Section/<document id>":
// its sections by position_path).
type Index struct {
	Name    *string      `pb:"1"`
	Entries []IndexEntry `pb:"2"`
	Desc    *bool        `pb:"3"`
	Numeric *bool        `pb:"4"`
}

// IndexEntry is syncer.LoadData.Response.Index.Entry.
type IndexEntry struct {
	ObjectID  *string `pb:"1"`
	SortValue *string `pb:"2"`
}

// Failure is syncer.LoadData.Response.Failure.
type Failure struct {
	Reason          *int32       `pb:"1"`
	RequestSubset   *LoadRequest `pb:"2"`
	OrganizationIDs []string     `pb:"3"`
}

// Failure reasons (syncer.LoadData.Response.Failure.Reason).
const (
	FailAuth           int32 = 0
	FailTransient      int32 = 1
	FailNotFound       int32 = 2
	FailNotImplemented int32 = 3
	FailAuthNoRequest  int32 = 4
	FailNotAllowed     int32 = 5
	FailInternal       int32 = 6
	FailPreload        int32 = 7
	FailUnsupported    int32 = 8
)

// Preserialized is syncer.LoadData.PreserializedItems: objects sent as
// their bytes (M455.updateResponseWithPreserialized).
type Preserialized struct {
	Items        []PreserializedItem `pb:"1"`
	CrdtSessions []Raw               `pb:"2"` // each a syncer.CrdtSessionState
}

// PreserializedItem is syncer.LoadData.Preserialized.
type PreserializedItem struct {
	IDType *int32 `pb:"1"` // IDTypeSection: Section bytes
	Values []Raw  `pb:"2"`
}

// IDTypeSection is the id type of preserialized sections (2U5W.idType).
const IDTypeSection int32 = 2

// Payload is syncer.Payload, the objects of an answer or a change.
type Payload struct {
	Threads   []Thread   `pb:"3"`
	Documents []Document `pb:"6"`
	Sections  []Section  `pb:"7"`
	// BacklogReloadModelData: fetch-latest says reload the thread with
	// load-data instead (HVRg.onChangesReceived_).
	BacklogReloadModelData *bool `pb:"37"`
}

// Thread is syncer.Thread.
type Thread struct {
	ID         *string `pb:"1"`
	Sequence   *int64  `pb:"2"`
	Deleted    *bool   `pb:"3"`
	Title      *string `pb:"6"`
	AuthorID   *string `pb:"7"`
	DocumentID *string `pb:"10"`
}

// Document is syncer.Document.
type Document struct {
	ID                 *string      `pb:"1"`
	Sequence           *int64       `pb:"2"`
	Deleted            *bool        `pb:"3"`
	ThreadID           *string      `pb:"6"`
	Title              *string      `pb:"7"`
	UpdatedUsec        *int64       `pb:"8"`
	ShardChars         *string      `pb:"42"`
	MaxSectionSequence *int64       `pb:"48"`
	TitleRichText      *RichTextAPI `pb:"69"`
}

// Section is syncer.Section: a block, a list, or a control (a mention,
// an emoji, a date), flat in its document.
type Section struct {
	ID                  *string     `pb:"1"`
	Sequence            *int64      `pb:"2"`
	Deleted             *bool       `pb:"3"`
	Dirty               []int32     `pb:"4"`
	Pending             []int32     `pb:"5"`
	ThreadID            *string     `pb:"6"`
	DocumentID          *string     `pb:"7"`
	Position            *string     `pb:"8"`
	Type                *int32      `pb:"9"`
	Style               *int32      `pb:"10"`
	SectionClass        *int32      `pb:"11"`
	Content             *Content    `pb:"12"`
	Parents             *Parents    `pb:"13"`
	AltRTML             *string     `pb:"14"`
	Status              Raw         `pb:"15"` // section locks; never written
	Attributes          *Attributes `pb:"16"`
	TempID              *string     `pb:"17"`
	MergedState         *int32      `pb:"18"`
	Modified            *bool       `pb:"19"`
	LocalSequence       *int64      `pb:"20"`
	PositionPath        *string     `pb:"21"`
	Transient           *bool       `pb:"22"`
	CopyThreadID        *string     `pb:"23"`
	CopySecretPath      *string     `pb:"24"`
	ContentOrigin       *int32      `pb:"25"`
	CreatedUsec         *int64      `pb:"26"`
	UpdatedUsec         *int64      `pb:"27"`
	HasLayoutParent     *bool       `pb:"29"`
	AuthorID            *string     `pb:"30"`
	EditedByUserIDs     []string    `pb:"31"`
	DisplayEditedUsec   *int64      `pb:"32"`
	TraceID             *string     `pb:"33"`
	ReadOnly            *bool       `pb:"35"`
	IsProgrammatic      *bool       `pb:"36"`
	CrdtSession         Raw         `pb:"38"` // read and dropped; Clack never writes one
	ActivityStackUpdate *int32      `pb:"39"`
	// Other: remote_editor_id, is_antimatter_section, root_id,
	// overlay_sequence, the table and spreadsheet fields, source.
	Other   []byte  `pb:"extra" slack:"28,34,102,115,200,202-205,208-215,300"`
	Unknown []int32 `pb:"unknown"`
}

// Content is proto.section.Section.Content: one of its kinds is set.
type Content struct {
	Text             *ContentText      `pb:"1"`
	List             *Empty            `pb:"17"`
	SlackChannel     *SlackChannel     `pb:"42"`
	HorizontalRule   *HorizontalRule   `pb:"43"`
	SlackUser        *SlackUser        `pb:"44"`
	SlackEmoji       *SlackEmoji       `pb:"48"`
	Title            *ContentTitle     `pb:"58"`
	SlackDate        *SlackDate        `pb:"62"`
	LayoutBlockquote *LayoutBlockquote `pb:"65"`
	SlackBroadcast   *SlackBroadcast   `pb:"68"`
	// Other holds any other kind (an image, a table, a callout, …): a
	// section with one isn't one Clack edits.
	Other []byte `pb:"extra" slack:"1-11,14,15,17,18,21-44,48,50,52,54-56,58-70"`
}

// Empty is ContentList, a message without fields.
type Empty struct{}

// ContentText is proto.section.Section.ContentText.
type ContentText struct {
	Text     *string `pb:"1"` // RTML
	Language *string `pb:"2"`
}

// ContentTitle is proto.section.Section.ContentTitle.
type ContentTitle struct {
	Text                    *string     `pb:"1"` // RTML
	HeaderImage             Raw         `pb:"2"`
	HeaderImageUploadUserID *string     `pb:"3"`
	EmojiInfo               []EmojiInfo `pb:"4"`
}

// HorizontalRule is proto.section.Section.ContentHorizontalRule.
type HorizontalRule struct {
	AsPageBreak *bool `pb:"1"`
}

// LayoutBlockquote is proto.section.Section.ContentLayoutBlockquote.
type LayoutBlockquote struct {
	SectionIDs []string `pb:"1"`
}

// SlackUser is proto.section.Section.ContentControlSlackUser.
type SlackUser struct {
	SlackUserID           *string `pb:"1"` // "su:U…" (2U5W.syncerIdForSlackUser), a group "su:S…"
	DisplayName           *string `pb:"2"`
	CreatorUserID         *string `pb:"3"`
	ProfileURL            *string `pb:"4"`
	SlackTeamID           *string `pb:"5"`
	FromSameDocumentPaste *bool   `pb:"6"`
}

// SlackChannel is proto.section.Section.ContentControlSlackChannel.
type SlackChannel struct {
	SlackChannelID *string `pb:"1"` // "sc:C…"
	Name           *string `pb:"2"`
	IsPrivate      *bool   `pb:"3"`
}

// SlackEmoji is proto.section.Section.ContentControlSlackEmoji.
type SlackEmoji struct {
	EmojiName      *string `pb:"2"`
	SlackEmojiID   *string `pb:"3"` // "se:<name>/<top-level team>"
	CustomEmojiURL *string `pb:"4"`
	EmojiSize      *int32  `pb:"5"`
	NonAnimatedURL *string `pb:"6"`
}

// SlackBroadcast is proto.section.Section.ContentControlSlackBroadcast.
type SlackBroadcast struct {
	Range *string `pb:"1"` // "here", "channel", "everyone"
}

// SlackDate is proto.section.Section.ContentControlSlackDate.
type SlackDate struct {
	UTCDateMs  *int64  `pb:"1"`
	DateFormat *string `pb:"2"`
}

// Parents is proto.section.Section.Parents.
type Parents struct {
	IDs            []string `pb:"1"`
	ContainerID    *string  `pb:"2"`
	ContainerStyle *int32   `pb:"4"`
}

// Attributes is proto.section.Section.Attributes: the ones text sections
// carry by name; the rest come back as they went (Other).
type Attributes struct {
	Indentation                   *int32  `pb:"1"`
	Checked                       *bool   `pb:"2"`
	BackgroundColor               *string `pb:"3"`
	VerticalMargin                *bool   `pb:"4"`
	ListPosition                  *int32  `pb:"11"`
	DefaultContent                *int32  `pb:"13"`
	TextAlignment                 *int32  `pb:"26"`
	VerticalAlignment             *int32  `pb:"30"` // [default = 5]: a save sends it when the server didn't
	IsNewDocumentDefaultContent   *bool   `pb:"34"`
	ContinuePreviousListNumbering *bool   `pb:"47"`
	RestartListNumberingAt        *int32  `pb:"48"`
	ParagraphIndentation          *int32  `pb:"51"`
	IsCustomizedDefaultContent    *bool   `pb:"57"`
	// Other: the spreadsheet and layout attributes Clack doesn't name,
	// kept to go back as they came (Slack's toPb re-sends every attribute).
	Other []byte `pb:"extra" slack:"1-13,15-24,26-35,37-57"`
}

// EmojiInfo is proto.section.SlackEmojiInfo.
type EmojiInfo struct {
	TeamID      *string `pb:"1"`
	DisplayName *string `pb:"2"`
	Unicode     *string `pb:"3"`
	DisplayURL  *string `pb:"4"`
}

// RichTextAPI is proto.threads.RichTextApi: rich_text as protobuf.
type RichTextAPI struct {
	Type     *string           `pb:"1"` // "rich_text"
	Elements []RichTextSection `pb:"2"`
}

// RichTextSection is proto.threads.RichTextApi.RichTextSection.
type RichTextSection struct {
	Type     *string         `pb:"1"` // "rich_text_section"
	Elements []RichTextBlock `pb:"2"`
}

// RichTextBlock is proto.threads.RichTextApi.RichTextSection.RichTextBlock.
type RichTextBlock struct {
	Type      *string        `pb:"1"`
	Text      *string        `pb:"2"`
	ChannelID *string        `pb:"3"`
	UserID    *string        `pb:"4"`
	Style     *RichTextStyle `pb:"5"`
	Name      *string        `pb:"6"`
	URL       *string        `pb:"7"`
	SkinTone  *string        `pb:"8"`
	Timestamp *int64         `pb:"17"`
	Unicode   *string        `pb:"20"`
	Other     []byte         `pb:"extra" slack:"1-28"`
}

// RichTextStyle is proto.threads.RichTextApi.RichTextSection.RichTextBlock.Style.
type RichTextStyle struct {
	Highlight *bool `pb:"1"`
	Bold      *bool `pb:"2"`
	Italic    *bool `pb:"3"`
	Strike    *bool `pb:"4"`
	Code      *bool `pb:"5"`
	Underline *bool `pb:"6"`
}

// FetchRequest is syncer.FetchLatest.Request: fetch-latest's
// request_binary.
type FetchRequest struct {
	Requests []FetchThread `pb:"1"`
}

// FetchThread is syncer.FetchLatest.ThreadRequest.
type FetchThread struct {
	ThreadID                 *string `pb:"1"`
	ClientThreadSequence     *int64  `pb:"2"`
	ClientMaxSectionSequence *int64  `pb:"3"`
}

// FetchResponse is syncer.FetchLatest.Response.
type FetchResponse struct {
	Changes                *Changes `pb:"1"`
	SectionConsistencyHash *int32   `pb:"2"`
}

// Changes is syncer.ChangesData.
type Changes struct {
	IDs          []string      `pb:"1"`
	Indexes      []IndexChange `pb:"2"`
	TempIDs      []TempID      `pb:"3"`
	IndexTempIDs []TempID      `pb:"4"`
	Data         *Payload      `pb:"7"`
}

// IndexChange is syncer.ChangesData.Index.
type IndexChange struct {
	IndexID        *string        `pb:"1"`
	InPlace        *bool          `pb:"2"`
	IDs            []string       `pb:"3"`
	ChangedEntries []ChangedEntry `pb:"4"`
	Reload         *bool          `pb:"5"`
}

// ChangedEntry is syncer.ChangesData.Index.ChangedEntry.
type ChangedEntry struct {
	ID        *string `pb:"1"`
	Remove    *bool   `pb:"2"`
	SortValue *string `pb:"3"`
}

// TempID is syncer.ChangesData.TempId.
type TempID struct {
	TempID *string `pb:"1"`
	RealID *string `pb:"2"`
}

// DocumentData is syncer.DocumentData: edit-document's data_binary (the
// sections to save) and its answer's final_data.
type DocumentData struct {
	Sections []Section `pb:"1"`
	// Other: the paste and spreadsheet fields (2-12), never in a save.
	Other               []byte       `pb:"extra" slack:"2-12"`
	TitleSlackEmojiInfo []EmojiInfo  `pb:"13"`
	TitleRichText       *RichTextAPI `pb:"14"`
	DocumentID          *string      `pb:"15"`
}

// EditAnswer is syncer.EditDocumentResponseWrapper: edit-document's
// answer.
type EditAnswer struct {
	Changed          *bool         `pb:"1"`
	FinalData        *DocumentData `pb:"2"`
	IsNewEditSession *bool         `pb:"6"`
}
