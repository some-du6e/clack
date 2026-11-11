package ui

import (
	"fmt"
	"testing"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The rail's labels wrap as Slack's 52px tab content does (white-space
// normal, text-align center): at spaces and between CJK characters, the
// Agents & tools label balanced, a word wider than the tab on a line of
// its own (Clack has no hyphenation dictionaries). The tab grows 12px a
// line.
func TestRailLabelLines(t *testing.T) {
	a := &App{th: theme.New(false)}
	gtx := layout.Context{Ops: new(op.Ops), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	check := func(id, label string, want ...string) {
		t.Helper()
		got := a.railLabelLines(gtx, railTab{id: id, label: label})
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("%s %q: lines %q, want %q", id, label, got, want)
		}
	}
	check(core.TabDMs, "DMs", "DMs")
	check(core.TabDMs, "Mensajes directos", "Mensajes", "directos") // es-ES
	check(core.TabLater, "Más tarde", "Más tarde")
	check(core.TabPlatform, "Agents & tools", "Agents", "& tools") // text-wrap balance
	check(core.TabPlatform, "Agentes y herramientas", "Agentes y", "herramientas")
	check(core.TabPlatform, "Herramientas", "Herramientas")
	if railTabH(1) != 68 || railTabH(2) != 80 {
		t.Errorf("tab heights %d, %d; want Slack's 68 and 80", railTabH(1), railTabH(2))
	}
	if a.th.Measure(gtx, 11, theme.Bold, "アア") != 2*gtx.Sp(11) {
		t.Log("no full-width CJK face here (the i18n workflow installs fonts-noto-cjk): ja-JP cases skipped")
		return
	}
	check(core.TabActivity, "アクティビティ", "アクティ", "ビティ") // ja-JP: no line starts with ィ
	check(core.TabHome, "ホーム", "ホーム")
}
