package richview

import (
	"image"
	"sort"
	"strings"
	"testing"
	"time"

	"gioui.org/io/pointer"

	rt "github.com/some-du6e/clack/internal/richtext"
)

// Synthetic fixtures only.

// ccHandler records contact card actions.
type ccHandler struct {
	recHandler
	acts []string
}

func (h *ccHandler) ContactCardAction(act string, c *rt.ContactCard, _ image.Rectangle, _ bool) {
	h.acts = append(h.acts, act+":"+c.UserID)
}

func contactDoc(c rt.ContactCard) rt.Doc {
	return doc(rt.Block{Kind: rt.ContactCardBlock, Kit: true, Contact: &c})
}

var robinCard = rt.ContactCard{UserID: "U1", CardID: "CC1", Member: rt.ContactMember{Name: "Robin", Avatar: "https://img.test/robin.png",
	Team: "Fake Club", CanInteract: true, CanShare: true}}

// SimpleContactCard in its pillow: 328px at most, the 36px avatar 12px in
// with 8px corners, the name 16px bold, the team under it; the actions'
// group at top 12px, right 12px.
func TestContactCardLayout(t *testing.T) {
	v := layoutAt(newTestStyle(), contactDoc(robinCard), Options{}, 600)
	cs := v.ContactCardRects()
	if len(cs) != 1 {
		t.Fatalf("%d contact card bars", len(cs))
	}
	if r := cs[0].R; r.Dx() != 328 || r.Dy() != 36+2*12+2 {
		t.Errorf("card %v, want 328×%d", r, 36+2*12+2)
	}
	var avatar *imgPrim
	for i := range v.d.imgs {
		if v.d.imgs[i].url == robinCard.Member.Avatar {
			avatar = &v.d.imgs[i]
		}
	}
	if avatar == nil || avatar.r != image.Rect(12, 12, 48, 48) || avatar.radii != [4]int{8, 8, 8, 8} {
		t.Errorf("avatar %+v, want 36px at 12,12 with 8px corners", avatar)
	}
	name := findFrag(t, &v.d, "Robin")
	if name.size != 16 || name.x != 60 {
		t.Errorf("name at x %v, %vpx; want 60, 16px", name.x, name.size)
	}
	team := findFrag(t, &v.d, "Fake")
	if team.top <= name.top || team.x != 60 {
		t.Errorf("team at %v,%d under the name at %d", team.x, team.top, name.top)
	}
	var acts []string
	for a, r := range cs[0].Actions {
		acts = append(acts, a)
		if r.Dx() != 32 || r.Min.Y != 12+4 {
			t.Errorf("%s button %v, want 32px at top 16 (12 in, the group's 4px padding)", a, r)
		}
	}
	sort.Strings(acts)
	if strings.Join(acts, " ") != "message share_contact" {
		t.Errorf("actions %q", acts)
	}
	// a team the client hasn't loaded: the bar, no name
	c := robinCard
	c.Member.Team, c.Member.TeamUnknown = "", true
	v = layoutAt(newTestStyle(), contactDoc(c), Options{}, 600)
	for _, f := range textFrags(&v.d) {
		if f.sh.str == "Fake" {
			t.Errorf("unknown team drew its name")
		}
	}
	bar := false
	for _, b := range v.d.boxes {
		if b.kind == bFill && b.r.Min.X == 60 && b.r.Dy() == 13 {
			bar = true
		}
	}
	if !bar {
		t.Errorf("no loading bar for the unknown team")
	}
	// no card id: no actions
	c = robinCard
	c.CardID = ""
	if v := layoutAt(newTestStyle(), contactDoc(c), Options{}, 600); len(v.ContactCardRects()) != 0 {
		t.Errorf("a card without its id has actions")
	}
}

// A click on the card opens the member's profile when the viewer can
// interact with them (a stranger's card takes none); the actions reach
// the handler.
func TestContactCardClicks(t *testing.T) {
	h := &ccHandler{}
	hs := newHarness(contactDoc(robinCard), h)
	hs.w = 600
	hs.frame()
	name := findFrag(t, &hs.v.d, "Robin")
	hs.clickRect(fragRect(name), 0)
	if len(h.users) != 1 || h.users[0] != "U1" {
		t.Errorf("card click: OpenUser %q", h.users)
	}
	hs.send(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: center(name)})
	cs := hs.v.ContactCardRects()
	if len(cs) != 1 {
		t.Fatalf("%d bars", len(cs))
	}
	hs.clickRect(cs[0].Actions["share_contact"], time.Second)
	if len(h.acts) != 1 || h.acts[0] != "share_contact:U1" {
		t.Errorf("actions %q", h.acts)
	}
	// a stranger's card: no profile on click
	s := robinCard
	s.Member.CanInteract = false
	h = &ccHandler{}
	hs = newHarness(contactDoc(s), h)
	hs.w = 600
	hs.frame()
	hs.clickRect(fragRect(findFrag(t, &hs.v.d, "Robin")), 0)
	if len(h.users) != 0 {
		t.Errorf("a stranger's card opened %q", h.users)
	}
}
