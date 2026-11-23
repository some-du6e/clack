package popups

import (
	"image"
	"strings"
	"testing"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// A modal's title wraps as Slack's .c-sk-modal_title_bar__text does
// (modal_title.go): Karim's alpha.157 dialogs cut long titles to one line
// with an ellipsis ("Are you sure you want to add the..."). The bar
// (min-height 70px, padding 20px 28px) grows by whole 30px lines, the
// close button stays at c-sk-modal__close_button's top 20px, right 20px,
// and a channel entity wraps with its icon.

// mtFrame lays m out once in a 1200×800 window at 1px per dp and returns
// its header's height: the body's top less the card's.
func mtFrame(t *testing.T, m *Modal) int {
	t.Helper()
	var ops op.Ops
	gtx := layout.Context{Ops: &ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(1200, 800))}
	m.Layout(gtx)
	if m.rect.Empty() {
		t.Fatal("the modal drew no card")
	}
	return m.bodyOrg.Y - m.rect.Min.Y
}

// mtModal is a 520px modal titled title with a one-line body and Cancel.
func mtModal(o ModalOpts) *Modal {
	o.Body = func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{Size: image.Pt(100, 22)} }
	o.Buttons = []*ModalButton{{Label: "Cancel", Kind: BtnOutline}}
	return NewModal(&tzHost{th: theme.New(false)}, o)
}

const mtLong = "Remove Avery Admin from Channel Managers of #agents-lab-release-coordination?"

func TestModalTitleWraps(t *testing.T) {
	short := mtModal(ModalOpts{Title: "Delete file"})
	shortHead := mtFrame(t, short)
	if shortHead != 70 {
		t.Fatalf("a one-line title's header is %dpx, want Slack's 70px", shortHead)
	}
	m := mtModal(ModalOpts{Title: mtLong})
	head := mtFrame(t, m)
	if head < 100 || (head-70)%30 != 0 {
		t.Fatalf("the long title's header is %dpx, want it to wrap: 70px and 30px for each line past the first", head)
	}
	lines := mtLines(m)
	if want := 40 + 30*len(lines); head != want {
		t.Errorf("%d lines %q in a %dpx header, want %dpx (padding 20px 28px, 30px lines)", len(lines), lines, head, want)
	}
	if got := strings.Join(lines, " "); got != mtLong {
		t.Errorf("the lines read %q, want the whole title %q (no ellipsis)", got, mtLong)
	}
	// each line fits beside the close button: padding-right 76px
	th := theme.New(false)
	var ops op.Ops
	gtx := layout.Context{Ops: &ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	for _, ln := range lines {
		_, d := record(loose(gtx, image.Pt(1<<20, 1<<20)), func(gtx layout.Context) layout.Dimensions {
			return para(gtx, th, 22, 30, theme.Black, colorsOf(th).Fg, ln, 1)
		})
		if d.Size.X > 520-28-76 {
			t.Errorf("line %q is %dpx, wider than the title's 416px", ln, d.Size.X)
		}
	}
	if cr := mtClose(m); cr.Min.Y != 20 || cr.Max.X != 520-20 || cr.Dx() != 36 {
		t.Errorf("the close button is at %v in the card, want 36px at top 20px, right 20px", cr)
	}
	// nothing else moves: the card grows by the extra lines only
	if dy := m.rect.Dy() - short.rect.Dy(); dy != head-70 {
		t.Errorf("the card grew %dpx for %dpx of title lines", dy, head-70)
	}
	if m.rect.Dx() != 520 {
		t.Errorf("the card is %dpx wide, want 520px", m.rect.Dx())
	}
}

// TestModalTitleWrapsChannelEntity: "Make {memberName} a Channel Manager of
// {channelName}?" with a private channel's InlineChannelEntity wraps with
// the lock kept beside the name (.c-base_inline_entity is white-space:
// pre); Clack drew such a title as plain text, without the icon.
func TestModalTitleWrapsChannelEntity(t *testing.T) {
	pre, after := "Make Avery Admin a Channel Manager of ", "agents-lab-release-coordination?"
	m := mtModal(ModalOpts{Title: pre, TitleIcon: "lock", TitleAfter: after})
	head := mtFrame(t, m)
	if head < 100 || (head-70)%30 != 0 {
		t.Fatalf("the entity title's header is %dpx, want it to wrap by whole 30px lines", head)
	}
	lines := mtLines(m)
	if want := 40 + 30*len(lines); head != want {
		t.Errorf("%d lines %q in a %dpx header, want %dpx", len(lines), lines, head, want)
	}
	icons := 0
	for _, ln := range lines {
		icons += strings.Count(ln, "[lock]")
		if i := strings.Index(ln, "[lock]"); i >= 0 && !strings.HasPrefix(ln[i+len("[lock]"):], "agents-") {
			t.Errorf("line %q: the lock isn't right before the channel's name", ln)
		}
	}
	if icons != 1 {
		t.Errorf("lines %q draw the lock %d times, want once, beside the name", lines, icons)
	}
	got := strings.ReplaceAll(strings.Join(lines, " "), "[lock]", "")
	if strings.ReplaceAll(got, "- ", "-") != pre+after {
		t.Errorf("the lines read %q, want %q", got, pre+after)
	}
	if cr := mtClose(m); cr.Min.Y != 20 {
		t.Errorf("the close button is at %v, want top 20px", cr)
	}
}

// TestModalTitleTruncates: Slack's DeprecatedDialog cuts its title with
// an ellipsis (truncateTitle, true by default: .overflow_ellipsis), as
// AttachmentActionConfirmDialog and RemoveMemberDialog leave it; so do
// OldDialog's ports. Their header stays 70px.
func TestModalTitleTruncates(t *testing.T) {
	m := mtModal(mtTruncated(ModalOpts{Title: mtLong}))
	if head := mtFrame(t, m); head != 70 {
		t.Errorf("a truncated title's header is %dpx, want 70px", head)
	}
	if lines := mtLines(m); len(lines) != 1 {
		t.Errorf("a truncated title drew %d lines %q, want one", len(lines), lines)
	}
	o := mtModal(ModalOpts{Title: mtLong}).OldDialog()
	if head := mtFrame(t, o); head != 70 {
		t.Errorf("an OldDialog's header is %dpx, want 70px (one line, cut)", head)
	}
}
