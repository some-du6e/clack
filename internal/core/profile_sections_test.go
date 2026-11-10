package core

import (
	"context"
	"testing"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// users.profile.getSections on the fake: the viewer's own sections carry
// the missing elements (Add Phone, Add Mentor), the manager is fetched,
// and someone else's profile has only what's set.
func TestProfileSections(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	a := New(slack.New(fs.Creds()), store.New(""))
	ctx := context.Background()

	ps, err := a.ProfileSections(ctx, fakeslack.SelfID)
	if err != nil {
		t.Fatal(err)
	}
	if !ps.IsSelf || ps.UserID != fakeslack.SelfID {
		t.Fatalf("user %q self %v", ps.UserID, ps.IsSelf)
	}
	var types []string
	for _, s := range ps.Sections {
		types = append(types, s.Type)
	}
	if len(types) != 6 || types[0] != "HEADER" || types[1] != "CONTACT" || types[2] != "PEOPLE" || types[4] != "ADDITIONAL_INFO" {
		t.Fatalf("sections %v", types)
	}
	c, _ := ps.Section("CONTACT")
	if c.Label != "Contact information" || !c.CanEdit || len(c.Elements) != 2 {
		t.Fatalf("contact %+v", c)
	}
	if e := c.Elements[0]; e.ElementKey != "email" || e.Label != "Email Address" || e.Text != fakeslack.SelfEmail || e.IsMissing {
		t.Errorf("email %+v", e)
	}
	if e := c.Elements[1]; e.ElementKey != "Alternate Phone" || !e.IsMissing || !e.EditableInUI {
		t.Errorf("phone %+v", e)
	}
	p, _ := ps.Section("PEOPLE")
	if len(p.Elements) != 2 || p.Elements[0].Typename != "ProfilePersonElement" || len(p.Elements[0].Persons) != 1 {
		t.Fatalf("people %+v", p)
	}
	if _, ok := a.User(p.Elements[0].Persons[0]); !ok {
		t.Error("the manager wasn't fetched")
	}
	ab, _ := ps.Section("ADDITIONAL_INFO")
	got := map[string]ProfileElement{}
	for _, e := range ab.Elements {
		got[e.Label] = e
	}
	if got["Start Date"].Date != "2024-03-01" || got["Website"].URI != "https://example.com/self" || !got["GitHub"].IsMissing {
		t.Errorf("about me %+v", ab.Elements)
	}
	h, _ := ps.Section("HEADER")
	if len(h.Elements) == 0 || h.Elements[0].Text != fakeslack.SelfTitle {
		t.Errorf("header %+v", h)
	}

	other, err := a.ProfileSections(ctx, fakeslack.ProfileID)
	if err != nil {
		t.Fatal(err)
	}
	if other.IsSelf {
		t.Error("someone else's profile is self")
	}
	if c, _ := other.Section("CONTACT"); len(c.Elements) != 0 {
		t.Errorf("someone else's contact section %+v", c)
	}
	for _, s := range other.Sections {
		for _, e := range s.Elements {
			if e.IsMissing {
				t.Errorf("missing element on someone else's profile: %+v", e)
			}
		}
	}
}
