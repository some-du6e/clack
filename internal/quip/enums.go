package quip

// Section types (proto.section.Section.Type), the ones a canvas's text
// uses and the rest by number.
const (
	TypeText             int32 = 0
	TypeList             int32 = 1
	TypeImage            int32 = 2
	TypeHighlight        int32 = 9  // CONTROL_HIGHLIGHT_TYPE: a comment's anchor
	TypeHorizontalRule   int32 = 16 // a divider
	TypeLayoutFlexbox    int32 = 26 // columns
	TypeLayoutTable      int32 = 33
	TypeLinkUnfurlCard   int32 = 56
	TypeSlackChannel     int32 = 49 // CONTROL_SLACK_CHANNEL_TYPE
	TypeSlackUser        int32 = 50 // CONTROL_SLACK_USER_TYPE (people and groups)
	TypeSlackEmoji       int32 = 54
	TypeSlackFile        int32 = 58
	TypeSlackDocument    int32 = 60 // a canvas mention
	TypeTitle            int32 = 64
	TypeSlackDate        int32 = 68
	TypeLayoutCallout    int32 = 69
	TypeLayoutBlockquote int32 = 71
	TypeSlackBroadcast   int32 = 73 // @here, @channel, @everyone
)

// Section styles (proto.section.Section.Style).
const (
	StylePlain            int32 = 0
	StyleH1               int32 = 1
	StyleH2               int32 = 2
	StyleH3               int32 = 3
	StyleCode             int32 = 4
	StyleBullet           int32 = 5
	StyleNumbered         int32 = 6
	StyleChecklist        int32 = 7
	StyleImage            int32 = 11
	StyleBlockquote       int32 = 16
	StylePullQuote        int32 = 17
	StyleHorizontalRule   int32 = 18
	StyleH4               int32 = 39
	StyleH5               int32 = 40
	StyleH6               int32 = 41
	StyleCaption          int32 = 47
	StyleTitle            int32 = 48
	StyleLayoutCallout    int32 = 49
	StyleLayoutBlockquote int32 = 50
)

// Section classes (proto.section.Section.Class).
const (
	ClassSection int32 = 0
	ClassControl int32 = 1
)

// Merged states (syncer.Section.merged_state).
const (
	MergedNetwork   int32 = 0
	MergedLocal     int32 = 1
	MergedFromLocal int32 = 2 // a save answer: the stored section merges this client's change
)

// Activity stack updates (syncer.Section.activity_stack_update,
// WJQ2.shouldAddToActivityStack).
const (
	ActivityAddSection     int32 = 0
	ActivityRemoveSection  int32 = 1
	ActivityAddCheckbox    int32 = 2
	ActivityRemoveCheckbox int32 = 3
)

// Default content (proto.section.Section.DefaultContent).
const (
	DefaultContentNone        int32 = 0
	DefaultContentHideOnFocus int32 = 1
	DefaultContentSelect      int32 = 2
)

// Field numbers of syncer.Section, the ones a save names (nTyX: Ix and
// f4 are syncer.Section.FieldNumber).
const (
	FieldID                  = 1
	FieldSequence            = 2
	FieldDeleted             = 3
	FieldPosition            = 8
	FieldType                = 9
	FieldStyle               = 10
	FieldSectionClass        = 11
	FieldContent             = 12
	FieldParents             = 13
	FieldAltRTML             = 14
	FieldStatus              = 15
	FieldAttributes          = 16
	FieldModified            = 19
	FieldLocalSequence       = 20
	FieldPositionPath        = 21
	FieldContentOrigin       = 25
	FieldHasLayoutParent     = 29
	FieldTraceID             = 33
	FieldReadOnly            = 35
	FieldIsProgrammatic      = 36
	FieldCrdtSession         = 38
	FieldActivityStackUpdate = 39
)

// styleTypes is nTyX.pW: each style's section type. A section whose type
// and style aren't such a pair is one Slack's editor doesn't make.
var styleTypes = map[int32]int32{
	StylePlain: TypeText, StyleH1: TypeText, StyleH2: TypeText, StyleH3: TypeText,
	StyleH4: TypeText, StyleH5: TypeText, StyleH6: TypeText, StyleCode: TypeText,
	StyleBlockquote: TypeText, StylePullQuote: TypeText, StyleCaption: TypeText,
	StyleTitle:  TypeTitle,
	StyleBullet: TypeList, StyleNumbered: TypeList, StyleChecklist: TypeList,
	StyleImage: TypeImage, StyleHorizontalRule: TypeHorizontalRule,
	StyleLayoutCallout: TypeLayoutCallout, StyleLayoutBlockquote: TypeLayoutBlockquote,
}

// TypeOfStyle is the section type style belongs to (nTyX.FH), ok false
// for a style Clack doesn't know.
func TypeOfStyle(style int32) (int32, bool) {
	t, ok := styleTypes[style]
	return t, ok
}
