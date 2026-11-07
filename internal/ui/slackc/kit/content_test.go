package kit

import (
	"testing"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc"
)

func TestPickIcon(t *testing.T) {
	icons := slackc.Props{"image_24": "a24", "image_48": "a48", "image_72": "a72", "image_192": "a192"}
	for _, c := range []struct {
		px   int
		want string
	}{{16, "a24"}, {24, "a24"}, {36, "a48"}, {72, "a72"}, {100, "a192"}, {600, "a192"}} {
		if got := pickIcon(icons, c.px); got != c.want {
			t.Errorf("pickIcon(%d) = %q, want %q", c.px, got, c.want)
		}
	}
	if got := pickIcon(nil, 36); got != "" {
		t.Errorf("nil icons: %q", got)
	}
	if got := pickIcon(slackc.Props{"image_original": "o"}, 36); got != "o" {
		t.Errorf("original fallback: %q", got)
	}
}

func TestAvatarRadius(t *testing.T) {
	for px, want := range map[int]int{8: 4, 20: 4, 36: 8, 72: 12, 200: 12} {
		if got := avatarRadius(px); got != want {
			t.Errorf("avatarRadius(%d) = %d, want %d", px, got, want)
		}
	}
}

func TestMessageOf(t *testing.T) {
	msg := slackc.Props{
		"text":    "hi",
		"ts":      "1.2",
		"onClick": slackc.JSFunc{ID: 3},
		"blocks": []any{slackc.Props{"type": "section", "label": slackc.Element{},
			"text": slackc.Props{"type": "mrkdwn", "text": "*b*"}}},
	}
	m, key := messageOf(msg)
	if m.Text != "hi" || m.TS != "1.2" {
		t.Fatalf("message = %+v", m)
	}
	if key == "" || len(m.Blocks) == 0 {
		t.Fatalf("blocks not kept: %q", key)
	}
	if want := `"label"`; ctContains(string(m.Blocks), want) {
		t.Errorf("element prop leaked into blocks: %s", m.Blocks)
	}
	if ctContains(key, "onClick") {
		t.Errorf("function leaked: %s", key)
	}
	if _, k := messageOf(nil); k != "" {
		t.Errorf("nil msg key %q", k)
	}
}

func TestDecodeBlocks(t *testing.T) {
	if got := decodeBlocks(nil); got == nil || len(got) != 0 {
		t.Errorf("empty: %#v", got)
	}
	got := decodeBlocks([]byte(`[{"type":"rich_text","elements":[]}]`))
	if len(got) != 1 {
		t.Fatalf("got %#v", got)
	}
	if m, ok := got[0].(map[string]any); !ok || m["type"] != "rich_text" {
		t.Errorf("block %#v", got[0])
	}
	if raw := blocksJSON([]any{slackc.Props{"type": "rich_text"}}); string(raw) != `[{"type":"rich_text"}]` {
		t.Errorf("blocksJSON = %s", raw)
	}
}

func TestLimitAndEmoji(t *testing.T) {
	if overLimit(0, 10) || overLimit(10, 10) || !overLimit(10, 11) {
		t.Error("overLimit")
	}
	a := emojiPicked("tada")
	if len(a) != 2 || a[1] != "tada" {
		t.Fatalf("args %#v", a)
	}
	if o := a[0].(slackc.Props); o["colons"] != ":tada:" || o["name"] != "tada" {
		t.Errorf("emoji object %#v", o)
	}
}

func ctContains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
