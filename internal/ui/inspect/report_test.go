package inspect

import (
	"image"
	"image/color"
	"strings"
	"testing"
	"time"
)

func testReport() *Report {
	return &Report{
		When: time.Date(2026, 11, 14, 5, 12, 33, 0, time.UTC), Version: "v0.31.2", OS: "darwin/arm64",
		Window: image.Pt(1440, 900), Scale: 2, Theme: "aubergine", Font: "lato",
		Page: "c/ch:3fa2b1 (private)", Region: "primary",
		Component: "UnstyledButton", Within: []string{"Reaction", "ReactionBar", "Message"},
		Element: "button.c-button-unstyled.c-reaction.c-reaction--reacted[data-qa=reactji]",
		Classes: []string{"div.c-message_kit__reaction_bar", "div.c-message_kit__message"},
		Rect:    image.Rect(412, 318, 464, 342), Pointer: image.Pt(430, 330),
		IDs: []string{"$key=ts:9c01de", "channelId=ch:3fa2b1"}, Kinds: []string{"ch:3fa2b1 private"},
		Colors: []string{"bg #ffffff", "ink #1d1c1d"}, Exps: []string{"reactions_v2=on"},
	}
}

// The report's text, row by row: what a maintainer reads in #clack.
func TestReportText(t *testing.T) {
	want := `Clack inspect report
clack        v0.31.2 · darwin/arm64 · 2026-11-14 05:12:33 UTC
component    UnstyledButton
element      button.c-button-unstyled.c-reaction.c-reaction--reacted[data-qa=reactji]
within       Reaction < ReactionBar < Message
classes      div.c-message_kit__reaction_bar < div.c-message_kit__message
rect         x 412 y 318 w 52 h 24 dp
pointer      430,330 dp
region       primary
page         c/ch:3fa2b1 (private)
ids          $key=ts:9c01de · channelId=ch:3fa2b1
kinds        ch:3fa2b1 private
colors       bg #ffffff · ink #1d1c1d
window       light · aubergine · lato · 1440x900 dp at 2x
experiments  reactions_v2=on
tags         one-way: sha256 with a salt made for this report and thrown away
`
	if got := testReport().Text(); got != want {
		t.Errorf("report text:\n%s\nwant:\n%s", got, want)
	}
	r := testReport()
	r.Component, r.Within, r.Element, r.Classes = "", nil, "", nil
	r.Dark, r.Zoom, r.Version = true, -1, ""
	got := r.Text()
	for _, s := range []string{"component    (no Slack component here)", "window       dark · aubergine · lato · 1440x900 dp at 2x · zoom -1", "clack        dev ·"} {
		if !strings.Contains(got, s) {
			t.Errorf("report without a component, dark, zoomed out, dev build: no %q in\n%s", s, got)
		}
	}
	if strings.Contains(got, "within") || strings.Contains(got, "element ") {
		t.Errorf("empty rows shown:\n%s", got)
	}
}

func TestHexColors(t *testing.T) {
	if got := hexA(color.NRGBA{R: 0x1d, G: 0x1c, B: 0x1d, A: 0xff}); got != "#1d1c1d" {
		t.Errorf("opaque: %s", got)
	}
	if got := hexA(color.NRGBA{R: 0xff, A: 0x80}); got != "#ff000080" {
		t.Errorf("translucent: %s", got)
	}
}
