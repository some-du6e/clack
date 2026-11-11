// Package css is Clack's CSS engine for Taut plugins: a parser, a selector
// matcher and a cascade that produce a typed computed Style for host-tree
// nodes (React elements a plugin renders) and for Clack's native widgets,
// which carry Slack's class names (".c-message_kit__message", …).
//
// Nothing here touches Gio, so the engine is testable on its own. The
// layout engine in package slackc consumes Style.
package css

import "image/color"

// Unit-less lengths: a Length is Px + Pct% (+ viewport units), so
// calc(100% - 20px) is {Px: -20, Pct: 100}. Em/rem are resolved to Px at
// compute time; percentages and viewport units at layout time.
type Length struct {
	Px, Pct, Vw, Vh float32
	Kind            LenKind
	// x holds min()/max()/clamp() over mixed units, which don't fold
	// into Px+Pct+Vw+Vh; Resolve evaluates it (Px then holds the value
	// with a zero percentage base).
	x *lexpr
}

// LenKind says whether a Length holds a value or a keyword.
type LenKind uint8

const (
	LenFixed      LenKind = iota // Px/Pct/Vw/Vh hold the value
	LenAuto                      // auto
	LenNone                      // none (max-width/max-height)
	LenMinContent                // min-content
	LenMaxContent                // max-content
	LenFitContent                // fit-content
)

// Auto, None and Zero are the common keyword lengths.
var (
	Auto = Length{Kind: LenAuto}
	None = Length{Kind: LenNone}
	Zero = Length{}
)

// Px returns a fixed pixel length.
func Px(v float32) Length { return Length{Px: v} }

// Pct returns a percentage length.
func Pct(v float32) Length { return Length{Pct: v} }

// IsAuto reports Kind == LenAuto.
func (l Length) IsAuto() bool { return l.Kind == LenAuto }

// Resolve returns the length in px against a percentage base and viewport
// (CSS px; the layout engine multiplies by the display scale). Keywords
// resolve to def.
func (l Length) Resolve(base, vw, vh, def float32) float32 {
	if l.Kind != LenFixed {
		return def
	}
	if l.x != nil {
		return l.x.eval(base, vw, vh)
	}
	return l.Px + l.Pct*base/100 + l.Vw*vw/100 + l.Vh*vh/100
}

// Display values.
type Display uint8

const (
	DisplayInline Display = iota // CSS initial value
	DisplayBlock
	DisplayInlineBlock
	DisplayFlex
	DisplayInlineFlex
	DisplayGrid // laid out as a wrapping row flex (grid-template is approximated)
	DisplayInlineGrid
	DisplayContents
	DisplayNone
	DisplayListItem
	DisplayTable // table, table-row, table-cell: approximated as block/flex
	DisplayTableRow
	DisplayTableCell
)

// Position values.
type Position uint8

const (
	PosStatic Position = iota
	PosRelative
	PosAbsolute
	PosFixed
	PosSticky
)

// FlexDirection values.
type FlexDirection uint8

const (
	Row FlexDirection = iota
	RowReverse
	Column
	ColumnReverse
)

// FlexWrap values.
type FlexWrap uint8

const (
	NoWrap FlexWrap = iota
	Wrap
	WrapReverse
)

// Align covers justify-content, align-items, align-self and align-content.
type Align uint8

const (
	AlignAuto Align = iota // align-self: auto; justify/align initial "normal"
	AlignStart
	AlignEnd
	AlignCenter
	AlignStretch
	AlignBaseline
	AlignSpaceBetween
	AlignSpaceAround
	AlignSpaceEvenly
)

// Overflow values.
type Overflow uint8

const (
	OverflowVisible Overflow = iota
	OverflowHidden
	OverflowScroll
	OverflowAuto
	OverflowClip
)

// BoxSizing values.
type BoxSizing uint8

const (
	ContentBox BoxSizing = iota
	BorderBox
)

// WhiteSpace values.
type WhiteSpace uint8

const (
	WSNormal WhiteSpace = iota
	WSNoWrap
	WSPre
	WSPreWrap
	WSPreLine
	WSBreakSpaces
)

// TextAlign values.
type TextAlign uint8

const (
	TextStart TextAlign = iota
	TextEnd
	TextLeft
	TextRight
	TextCenter
	TextJustify
)

// TextDecoration flags.
type TextDecoration uint8

const (
	DecoUnderline TextDecoration = 1 << iota
	DecoLineThrough
	DecoOverline
	// DecoWavy is text-decoration-style: wavy (a style bit carried with the
	// lines, so it propagates to inline children with them).
	DecoWavy TextDecoration = 1 << 7
)

// DecoLines are the line bits (underline, line-through, overline).
const DecoLines = DecoUnderline | DecoLineThrough | DecoOverline

// Lines is d's line bits, without the style.
func (d TextDecoration) Lines() TextDecoration { return d & DecoLines }

// TextTransform values.
type TextTransform uint8

const (
	TTNone TextTransform = iota
	TTUppercase
	TTLowercase
	TTCapitalize
)

// VerticalAlign values (inline boxes).
type VerticalAlign uint8

const (
	VABaseline VerticalAlign = iota
	VAMiddle
	VATop
	VABottom
	VATextTop
	VATextBottom
	VASub
	VASuper
)

// Visibility values.
type Visibility uint8

const (
	Visible Visibility = iota
	Hidden
	Collapse
)

// Cursor values (mapped to Gio's pointer.Cursor by the layout engine).
type Cursor uint8

const (
	CursorAuto Cursor = iota
	CursorDefault
	CursorPointer
	CursorText
	CursorMove
	CursorGrab
	CursorGrabbing
	CursorNotAllowed
	CursorWait
	CursorProgress
	CursorCrosshair
	CursorHelp
	CursorNone
	CursorColResize
	CursorRowResize
	CursorEWResize
	CursorNSResize
	CursorNESWResize
	CursorNWSEResize
)

// BorderStyle values; anything drawn as solid except None/Hidden.
type BorderStyle uint8

const (
	BorderNone BorderStyle = iota
	BorderSolid
	BorderDashed
	BorderDotted
	BorderDouble
	BorderHidden
)

// ObjectFit values for images.
type ObjectFit uint8

const (
	FitFill ObjectFit = iota
	FitContain
	FitCover
	FitNone
	FitScaleDown
)

// TransformKind names one transform function.
type TransformKind uint8

const (
	TTranslate TransformKind = iota // X, Y lengths (percent of own box)
	TScale                          // SX, SY
	TRotate                         // Angle (radians)
	TSkew                           // SX, SY as radians
	TMatrix                         // M: a b c d e f
)

// Transform is one function in a transform list, applied in order.
type Transform struct {
	Kind   TransformKind
	X, Y   Length
	SX, SY float32
	Angle  float32
	M      [6]float32
}

// Shadow is one box-shadow / text-shadow layer.
type Shadow struct {
	X, Y, Blur, Spread float32 // px
	Color              color.NRGBA
	Inset              bool
}

// Filter is the subset of filter functions Clack draws. StreamerMode-style
// blur is the important one.
type Filter struct {
	Blur       float32 // px; 0 = none
	Grayscale  float32 // 0..1
	Brightness float32 // 1 = identity (0 means unset → 1)
	Opacity    float32 // 1 = identity (0 means unset → 1)
	Invert     float32
	Sepia      float32
	Saturate   float32 // 1 = identity (0 means unset → 1)
	Contrast   float32 // 1 = identity (0 means unset → 1)
	HueRotate  float32 // radians
}

// IsZero reports a filter that changes nothing.
func (f Filter) IsZero() bool { return f == Filter{} }

// Background is the background layer. Only one image layer is drawn.
type Background struct {
	Color color.NRGBA
	Image string // url(...) target, "" = none
	// Gradient stops for linear-gradient/radial-gradient (Image == "").
	Gradient *Gradient
	Size     string // cover|contain|auto|"<x> <y>"
	Position string
	Repeat   bool
}

// Gradient is a parsed linear or radial gradient.
type Gradient struct {
	Radial    bool
	Repeating bool    // repeating-linear-gradient / repeating-radial-gradient
	Angle     float32 // radians, linear only (CSS: 0 = to top, clockwise)
	Stops     []Stop
}

// Stop is a gradient color stop; Pos < 0 means auto-distributed.
type Stop struct {
	Color color.NRGBA
	Pos   float32 // 0..1
}

// Style is a computed style. Every field holds a resolved value; the zero
// value is NOT the initial style, use Initial().
type Style struct {
	Display    Display
	Position   Position
	Visibility Visibility
	ZIndex     int32
	ZAuto      bool

	Top, Right, Bottom, Left Length // insets; auto when unset

	Width, Height       Length
	MinWidth, MinHeight Length
	MaxWidth, MaxHeight Length
	BoxSizing           BoxSizing
	AspectRatio         float32 // width/height; 0 = auto

	Margin  [4]Length // top, right, bottom, left (auto allowed)
	Padding [4]Length // top, right, bottom, left

	BorderWidth [4]float32 // px, top, right, bottom, left (0 when style none)
	BorderStyle [4]BorderStyle
	BorderColor [4]color.NRGBA
	Radius      [4]Length // top-left, top-right, bottom-right, bottom-left
	Outline     struct {
		Width  float32
		Style  BorderStyle
		Color  color.NRGBA
		Offset float32
	}

	FlexDirection  FlexDirection
	FlexWrap       FlexWrap
	JustifyContent Align
	AlignItems     Align // AlignAuto = normal (stretch)
	AlignSelf      Align // AlignAuto = inherit from parent's align-items
	AlignContent   Align
	RowGap, ColGap Length
	FlexGrow       float32
	FlexShrink     float32
	FlexBasis      Length // auto by default
	Order          int32
	// GridColumns is the column count for display:grid when
	// grid-template-columns is repeat(N, …) or N tracks; 0 = auto-flow row.
	GridColumns int

	OverflowX, OverflowY Overflow
	Opacity              float32
	Transform            []Transform
	TransformOrigin      [2]Length // default 50% 50%
	Filter               Filter
	BackdropBlur         float32
	BoxShadow            []Shadow
	Background           Background
	MixBlend             string

	// Text (inherited).
	Color                color.NRGBA
	FontFamily           string
	FontSize             float32 // px
	FontWeight           int     // 100..900
	Italic               bool
	LineHeight           Length  // LenAuto = normal; Px set = absolute; Pct = multiplier*100 of font size
	LetterSpacing        float32 // px
	WordSpacing          float32
	TextAlign            TextAlign
	TextDecoration       TextDecoration // not inherited, but propagated to inline children by the layout engine
	DecoColor            color.NRGBA    // zero = currentColor
	TextTransform        TextTransform
	WhiteSpace           WhiteSpace
	TextOverflowEllipsis bool
	LineClamp            int  // -webkit-line-clamp, 0 = none
	WordBreakAll         bool // word-break: break-all / overflow-wrap: anywhere
	VerticalAlign        VerticalAlign
	TextShadow           []Shadow

	Cursor        Cursor
	PointerEvents bool // false = pointer-events: none (inherited)
	UserSelect    bool // false = user-select: none
	Pixelated     bool // image-rendering: pixelated / crisp-edges: images scale nearest-neighbour (inherited)

	// Generated content for ::before/::after styles ("" = none).
	Content    string
	HasContent bool

	// SVG paint (inherited).
	Fill        color.NRGBA
	FillNone    bool
	Stroke      color.NRGBA
	StrokeNone  bool
	StrokeWidth float32

	ObjectFit ObjectFit

	// Vars are the custom properties in scope (inherited). Shared with the
	// parent when the element declares none; never mutate in place.
	// Env.RootVars (Slack's design tokens) sit underneath: a name missing
	// here resolves there, so the large token map is never copied. A value
	// of "\x00" marks a guaranteed-invalid property that hides a root one.
	Vars map[string]string

	// word-break / overflow-wrap, kept apart so they inherit separately
	// (WordBreakAll is their combination).
	wordBreak, overflowWrap uint8
}

// Initial is CSS's initial style with Clack's root defaults: Lato 15px,
// the theme foreground, pointer events on.
func Initial(fg color.NRGBA) Style {
	return Style{
		Width: Auto, Height: Auto,
		MinWidth: Auto, MinHeight: Auto,
		MaxWidth: None, MaxHeight: None,
		Top: Auto, Right: Auto, Bottom: Auto, Left: Auto,
		ZAuto:           true,
		FlexShrink:      1,
		FlexBasis:       Auto,
		Opacity:         1,
		TransformOrigin: [2]Length{Pct(50), Pct(50)},
		Color:           fg,
		FontFamily:      "Lato",
		FontSize:        15,
		FontWeight:      400,
		LineHeight:      Auto,
		PointerEvents:   true,
		UserSelect:      true,
		StrokeNone:      true,
		StrokeWidth:     1,
		Fill:            color.NRGBA{A: 0xff},
		Background:      Background{Repeat: true},
		BorderColor:     [4]color.NRGBA{fg, fg, fg, fg},
	}
}
