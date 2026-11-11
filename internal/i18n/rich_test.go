package i18n

import (
	"fmt"
	"testing"
)

func partsString(ps []Part) string {
	s := ""
	for _, p := range ps {
		s += fmt.Sprintf("[%s|%s|%s|%s]", p.Text, p.Tag, p.Class, p.Arg)
	}
	return s
}

// rt(): tags read from the formatted translation (one with a className,
// one with a link), values put in after, so a "<" in a name stays text.
func TestRT(t *testing.T) {
	c := testBundle(t, "ja-JP", map[string]map[string]any{
		"reactions": {
			`{user} <span className="c-tooltip__subtitle">reacted with {emoji}</span>`: `{user}<span className="c-tooltip__subtitle">さんが {emoji} でリアクションしました</span>`,
		},
		"threads":   {"replied to <a>a thread</a>": "<a>スレッド</a>に返信しました"},
		"msg_input": {"Message {msg}{govSlackSuffix}": "{msg}{govSlackSuffix} へのメッセージ"},
	})
	t.Cleanup(func() { Use(EnUS, nil) })
	tip := `{user} <span className="c-tooltip__subtitle">reacted with {emoji}</span>`
	re := NS("reactions")
	if got := partsString(re.RT(tip, "user", "Ann <3", "emoji", ":tada:")); got != "[Ann <3|||user][ |||][reacted with |span|c-tooltip__subtitle|][:tada:|span|c-tooltip__subtitle|emoji]" {
		t.Errorf("en-US tip = %s", got)
	}
	if got := partsString(NS("threads").RT("replied to <a>a thread</a>")); got != "[replied to |||][a thread|a||]" {
		t.Errorf("en-US preamble = %s", got)
	}
	Use("ja-JP", c)
	if got := partsString(re.RT(tip, "user", "Ann", "emoji", ":tada:")); got != "[Ann|||user][さんが |span|c-tooltip__subtitle|][:tada:|span|c-tooltip__subtitle|emoji][ でリアクションしました|span|c-tooltip__subtitle|]" {
		t.Errorf("ja-JP tip = %s", got)
	}
	if got := partsString(NS("threads").RT("replied to <a>a thread</a>")); got != "[スレッド|a||][に返信しました|||]" {
		t.Errorf("ja-JP preamble = %s", got)
	}
	ps := NS("msg_input").RT("Message {msg}{govSlackSuffix}", "msg", "general", "govSlackSuffix", "")
	if got := partsString(ps); got != "[general|||msg][ へのメッセージ|||]" {
		t.Errorf("ja-JP placeholder = %s", got)
	}
	if Plain(ps) != "general へのメッセージ" {
		t.Errorf("Plain = %q", Plain(ps))
	}
	// unclosed and attribute-carrying tags are text, as textToDomJson leaves them
	Use(EnUS, nil)
	if got := Plain(NS("x").RT(`<a href="x">a</a> <b>b`)); got != `<a href="x">a</a> <b>b` {
		t.Errorf("literal tags = %q", got)
	}
	if got := partsString(NS("x").RT("<b>{n, plural, =1 {# file} other {# files}}</b> left", "n", 2)); got != "[2 files|b||][ left|||]" {
		t.Errorf("plural in a tag = %s", got)
	}
}

// Slack's listify (nTGe): the serial comma in en-US, "y" in es-ES, ja-JP's
// 、 and no conjunction.
func TestListify(t *testing.T) {
	t.Cleanup(func() { Use(EnUS, nil) })
	abc := []string{"A", "B", "C"}
	for _, c := range []struct {
		loc   string
		items []string
		or    bool
		want  string
	}{
		{EnUS, []string{"A"}, false, "A"},
		{EnUS, abc[:2], false, "A and B"},
		{EnUS, abc, false, "A, B, and C"},
		{EnUS, abc, true, "A, B, or C"},
		{"es-ES", abc[:2], false, "A y B"},
		{"es-ES", abc, false, "A, B y C"},
		{"ja-JP", abc, false, "A、 B、 C"},
		{"ja-JP", abc[:2], true, "A または B"},
	} {
		var cat *Catalog
		if c.loc != EnUS {
			cat = testBundle(t, c.loc, map[string]map[string]any{"general": {"and": map[string]string{"es-ES": "y", "ja-JP": "そして"}[c.loc], "or": map[string]string{"es-ES": "o", "ja-JP": "または"}[c.loc]}})
		}
		Use(c.loc, cat)
		if got := Listify(c.items, c.or); got != c.want {
			t.Errorf("%s %v or=%v: %q, want %q", c.loc, c.items, c.or, got, c.want)
		}
	}
}

func TestLineChunks(t *testing.T) {
	for in, want := range map[string][]string{
		"Mensajes": {"Mensajes"},
		"アクティビティ":  {"ア", "ク", "ティ", "ビ", "ティ"}, // no line starts with a small kana
		"「設定」を開く":  {"「設", "定」", "を", "開", "く"}, // nor after 「 or before 」
		"Slackの設定": {"Slack", "の", "設", "定"},
		"ツール":      {"ツー", "ル"}, // nor with ー
	} {
		if got := LineChunks(in); fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("LineChunks(%q) = %q, want %q", in, got, want)
		}
	}
}
