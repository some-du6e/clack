package ui

import (
	"encoding/json"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/taut"
	"github.com/some-du6e/clack/internal/ui/richview"
)

// mentionLabels is richview's MemberLabel over the members slice as
// plugins present it: a user mention names the patched member under the
// same rule as an unpatched one (store.DisplayName), so Nicknames'
// nickname shows in every @mention, as Slack's MemberSlug reads it from
// the store. While nothing patches the slice it answers false at once
// (one atomic load); a patched member is decoded once per change (taut's
// view cache hands back the same bytes until then). UI goroutine only.
type mentionLabels struct {
	t     *taut.Taut
	cache map[string]mentionLabel
}

type mentionLabel struct {
	raw  *byte
	n    int
	name string
}

func newMentionLabels(t *taut.Taut) richview.MemberLabel {
	m := &mentionLabels{t: t, cache: map[string]mentionLabel{}}
	return m.label
}

func (m *mentionLabels) label(id string) (string, bool) {
	if !m.t.SlicePatched("members") {
		return "", false
	}
	raw, ok := m.t.View("members", id)
	if !ok || len(raw) == 0 || string(raw) == "null" {
		return "", false
	}
	if c, hit := m.cache[id]; hit && c.raw == &raw[0] && c.n == len(raw) {
		return c.name, c.name != ""
	}
	var u slack.User
	name := ""
	if json.Unmarshal(raw, &u) == nil {
		name = store.DisplayName(&u)
	}
	if len(m.cache) > 4096 {
		clear(m.cache)
	}
	m.cache[id] = mentionLabel{raw: &raw[0], n: len(raw), name: name}
	return name, name != ""
}
