package popups

import (
	"fmt"
	"image"
	"strings"
	"testing"
	"time"
)

// The hover card (every trigger builds it with NewHoverCard) goes through
// the same ProfileHoverCard boundary plugins patch: Slick's LastSeen adds
// "Last seen" to a member's hover card, and an app's hover card renders
// ProfileHoverCard → AppHoverCard (Slack's split in SmYd) with no
// MemberProfileHoverCard under it, so member-card patches don't land on
// apps.
func TestSlickViewHoverCard(t *testing.T) {
	e := slNewEnv(t)
	seen := time.Now().Add(-5 * time.Minute)
	e.api.set("search.messages", fmt.Sprintf(`{"ok":true,"messages":{"total":1,"matches":[{"ts":"%d.000100","channel":{"id":%q}}]}}`, seen.Unix(), slGen))
	e.api.set("apps.profile.get", `{"ok":true,"app_profile":{"id":"`+slAppID+`","name":"Helper Bot","desc":"Helps with synthetic things",`+
		`"app_card_color":"4a154b","team_id":"`+slTeam+`","enterprise_id":"`+slEnt+`","bot_user":{"id":"`+slApp+`","username":"helperbot"},`+
		`"auth":{"created_by":"`+slAlice+`"},"icons":{}}}`)
	e.load(t, "LastSeen", nil)
	slPatched(t, "ProfileHoverCard", "LocalTime")

	anchor := image.Rect(200, 500, 260, 522)
	card := NewHoverCard(e.h, anchor, slAlice, "")
	slUntil(t, 15*time.Second, "Last seen on the hover card", slFrame(card), func() bool {
		d := e.rec.dump("ProfileHoverCard")
		return strings.Contains(d, "Last seen") && strings.Contains(d, "@MemberProfileHoverCard")
	})
	if card.focus || card.Dismissed() {
		t.Error("the hover card took focus or closed")
	}
	if r := card.Rect(); r.Max.Y > anchor.Min.Y {
		t.Errorf("hover card %v not above its trigger %v", r, anchor)
	}

	app := NewHoverCard(e.h, anchor, slApp, "")
	slUntil(t, 15*time.Second, "the app's hover card through ProfileHoverCard", slFrame(app), func() bool {
		for _, nodes := range e.rec.treesOf("ProfileHoverCard") {
			if slFind(nodes, "AppHoverCard") != nil {
				return true
			}
		}
		return false
	})
	if !app.IsApp() {
		t.Fatal("a bot user got the member card")
	}
	for _, nodes := range e.rec.treesOf("ProfileHoverCard") {
		if n := slFind(nodes, "AppHoverCard"); n != nil {
			if slFind(nodes, "MemberProfileHoverCard") != nil {
				t.Error("the app's ProfileHoverCard also renders MemberProfileHoverCard")
			}
			if n.P.StrOr("memberId", "") != slApp {
				t.Errorf("AppHoverCard props %v", n.P)
			}
		}
	}
	slUntil(t, 15*time.Second, "the app card's data", slFrame(app), func() bool { return app.app.card != nil })
	if c := app.app.card; c.Name != "Helper Bot" || c.InstallerID != slAlice || !c.Active {
		t.Errorf("app card %+v", c)
	}
}
