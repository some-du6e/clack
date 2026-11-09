package composer

import "testing"

func TestChromeBarWidth(t *testing.T) {
	b := crBar{pad: 4, btn: 28, para: 44, m: 2, sepW: 1, sepM: 4, slot: 36}
	// Slack's full bar: last button (Divider) at 390 + 28, plus 4 padding.
	if got := b.width(0); got != 422 {
		t.Fatalf("full bar width = %d, want 422", got)
	}
	// Slack's slot rule (Composer.getAvailableButtonSlots): 12 buttons
	// need floor((inner-8)/36) >= 12, else the first slots-1 stay.
	cases := []struct{ avail, want int }{
		{996, 0},
		{440, 0}, // bar 432: 12 slots
		{439, 2}, // bar 431: 11 slots, 10 buttons + ⋯
		{422, 2},
		// a 392px thread composer (390 inner, bar 382): 10 slots, 9 + ⋯
		{390, 3},
		{44, 12}, // one slot: ⋯ alone
		{10, 12},
	}
	for _, c := range cases {
		if got := b.hidden(c.avail); got != c.want {
			t.Errorf("hidden(%d) = %d, want %d", c.avail, got, c.want)
		}
	}
}

func TestChromeActionsDrain(t *testing.T) {
	var c chrome
	if a := c.Actions(); a != nil {
		t.Fatalf("empty queue = %v", a)
	}
	c.emit(chromeAction{Kind: caSend})
	c.emit(chromeAction{Kind: caEmoji})
	a := c.Actions()
	if len(a) != 2 || a[0].Kind != caSend || a[1].Kind != caEmoji {
		t.Fatalf("drained %v", a)
	}
	if len(c.Actions()) != 0 {
		t.Fatal("queue not reset")
	}
	c.emit(chromeAction{Kind: caFocus})
	if a := c.Actions(); len(a) != 1 || a[0].Kind != caFocus {
		t.Fatalf("second drain %v", a)
	}
}

func TestNormalizeURL(t *testing.T) {
	for in, want := range map[string]string{
		"":                        "",
		"  example.com ":          "https://example.com",
		"https://example.com":     "https://example.com",
		"http://x.y/z":            "http://x.y/z",
		"localhost:8080/a":        "https://localhost:8080/a",
		"//cdn.example.com":       "https://cdn.example.com",
		"mailto:a@b.c":            "mailto:a@b.c",
		"MAILTO:a@b.c":            "MAILTO:a@b.c",
		"ftp://files.example.com": "ftp://files.example.com",
	} {
		if got := crNormalizeURL(in); got != want {
			t.Errorf("crNormalizeURL(%q) = %q, want %q", in, got, want)
		}
	}
}
