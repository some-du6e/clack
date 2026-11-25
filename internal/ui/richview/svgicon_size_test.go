package richview

import (
	"image"
	"strings"
	"testing"
	"time"
)

// Synthetic fixtures only.

// An event's accepted guests each carry .p-event-avatar_badge's
// check-circle-filled, an SvgIcon with no size: 1em ([data-5jx]{width:1em;
// height:1em;font-size:var(--s,inherit)}) of the badge's 12px, on its
// 12px base-pry circle, at bottom 1px right 1px of the avatar's inline
// wrapper (whose bottom is the line's 3px descent under the avatar).
// Clack drew a 10px check with a 1px ring at bottom -3 right -3, the
// badge's old CSS.
func TestEventAvatarBadgeSize(t *testing.T) {
	now := evClock(t)
	v := layoutAt(newTestStyle(), eventDoc(standup(now, -10*time.Minute, 50*time.Minute)), Options{}, 600)
	var faces []image.Rectangle
	for _, h := range v.d.hits {
		if h.t >= 0 && int(h.t) < len(v.d.targets) && v.d.targets[h.t].kind == tUser && h.r.Dx() == evGlyph && h.r.Dy() == evGlyph {
			faces = append(faces, h.r)
		}
	}
	var badges []imgPrim
	for _, im := range v.d.imgs {
		if strings.HasSuffix(im.url, "check-circle-filled") {
			badges = append(badges, im)
		}
	}
	if len(faces) != 2 || len(badges) != 2 {
		t.Fatalf("%d faces and %d badges, want the two accepted guests'", len(faces), len(badges))
	}
	for i, b := range badges {
		f := faces[i]
		if b.r.Dx() != 12 || b.r.Dy() != 12 || b.px != 12 {
			t.Errorf("guest %d's badge is %v (px %d), want 12x12", i, b.r, b.px)
		}
		if b.r.Max.X != f.Max.X-1 || b.r.Max.Y != f.Max.Y+2 {
			t.Errorf("guest %d's badge %v on the face %v: want its right 1px in, its bottom 2px under the face", i, b.r, f)
		}
		ring := false
		for _, bx := range v.d.boxes {
			if bx.kind == bFill && bx.r == b.r && bx.radius == 6 {
				ring = true
			}
		}
		if !ring {
			t.Errorf("guest %d's badge has no 12px base-pry circle under it", i)
		}
	}
}
