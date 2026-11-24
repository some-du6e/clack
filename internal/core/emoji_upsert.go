package core

import "sort"

// UpsertEmoji is Slack's upsertEmoji thunk for the plugins ("upsert emoji
// from flannel", client-boot oaam, build 35b42de8b870): each name's url,
// or "alias:<name>", put over the workspace's custom emoji. They are
// emoji.Custom's upserts: kept until the server writes the name (a list
// reload naming it, emoji_changed), never written to disk (a plugin's
// image can be an object URL that dies with its runtime). The messages
// that draw the names convert again, as for an emoji_changed.
func (a *App) UpsertEmoji(entries map[string]string) {
	if len(entries) == 0 {
		return
	}
	a.S.CustomEmoji.Upsert(entries)
	names := make([]string, 0, len(entries))
	for n := range entries {
		names = append(names, n)
	}
	sort.Strings(names)
	a.emit(Event{Type: "emoji_changed", Users: names})
}
