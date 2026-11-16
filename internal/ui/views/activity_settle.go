package views

import (
	"reflect"
	"time"

	"github.com/some-du6e/clack/internal/core"
)

// Activity keeps each row's text (actDesc) across loads and describes a
// row again only when what it was described from changed: its item in a
// load's answer (unbuildChanged), or the store under it (noteStale: a
// profile, a conversation, the team's names). A refresh that brought
// the same items, the peek's or a realtime one, so doesn't describe every
// row again in one frame, and a row still follows a rename the way
// Slack's rows render again when the member or channel store changes.

// unbuildChanged is what settle describes again after a load: the rows
// whose item the answer changed (or brought back), whose conversation
// reads differently now, or whose timestamp text moved on (a day turned
// over: "Yesterday"). old is the page as the load found it, page its
// answer.
func (v *Activity) unbuildChanged(old, page core.ActivityPage) {
	was := make(map[string]*core.ActivityItem, len(old.Items))
	for i := range old.Items {
		was[actKey(&old.Items[i])] = &old.Items[i]
	}
	now := time.Now()
	for i := range page.Items {
		it := &page.Items[i]
		k := actKey(it)
		r := v.rows[k]
		if r == nil || !r.built {
			continue
		}
		o := was[k]
		if o == nil || !reflect.DeepEqual(o, it) || old.Convs[it.Channel] != page.Convs[it.Channel] ||
			actWhen(now, r.d.at, false) != r.d.when || actWhen(now, r.d.at, true) != r.d.whenNarrow {
			r.built = false
		}
	}
}

// noteStale records what ev changed under the rows' descriptions, for
// unbuildStale on the next draw (the event goroutine; one pending pass
// however many events come), and reports whether it recorded anything:
//
//   - "user" (user_change, user_profile_changed): the rows that looked the
//     member up (actDesc.reads: the sender's name and picture, a DM's
//     title, the names its preview mentions). A member no built row read
//     (notes.readers) draws on no row, as most of a large workspace's
//     profile changes don't: one that other views read (ev.Seen) no more
//     wakes the page than a stranger. While rows are being described any
//     member might be one (describing).
//   - "conv": the rows about that conversation (its name, topic, a group
//     DM's custom name).
//   - "team" (the team's or a user group's name, which previews mention):
//     every row.
//
// Prefs changes ("prefs", a pref_change echo of every frecency save)
// aren't: no row's text follows a pref (names are display names, and a
// clock format change shows in unbuildChanged's timestamp check).
func (v *Activity) noteStale(ev core.Event) bool {
	user := ""
	switch ev.Type {
	case "user":
		if user = ev.UserID; user == "" && ev.User != nil {
			user = ev.User.ID
		}
		if user == "" || ev.Seen.IsZero() || core.CardOnly(ev.Prev, ev.User) { // a row draws a name and a picture
			return false
		}
	case "conv", "team":
	default:
		return false
	}
	n := &v.notes
	n.mu.Lock()
	defer n.mu.Unlock()
	if !n.describing && (user != "" && !n.readers[user] || ev.Type == "conv" && ev.Channel != "" && !n.chans[ev.Channel]) {
		return false
	}
	switch {
	case user != "":
		if n.staleUsers == nil {
			n.staleUsers = map[string]bool{}
		}
		n.staleUsers[user] = true
	case ev.Type == "conv" && ev.Channel != "":
		if n.staleConvs == nil {
			n.staleConvs = map[string]bool{}
		}
		n.staleConvs[ev.Channel] = true
	default:
		n.staleAll = true
	}
	if !n.staleQueued {
		n.staleQueued = true
		n.fns = append(n.fns, v.unbuildStale)
	}
	return true
}

// unbuildStale describes again the rows noteStale recorded changes under
// (the UI goroutine, notes.drain), keeping their widgets.
func (v *Activity) unbuildStale() {
	n := &v.notes
	n.mu.Lock()
	users, convs, all := n.staleUsers, n.staleConvs, n.staleAll
	n.staleUsers, n.staleConvs, n.staleAll, n.staleQueued = nil, nil, false, false
	n.mu.Unlock()
	unbuilt := false
	for _, r := range v.rows {
		if !r.built {
			continue
		}
		if all {
			r.built, unbuilt = false, true
			continue
		}
		for _, id := range r.d.reads {
			if users[id] {
				r.built, unbuilt = false, true
				break
			}
		}
	}
	if len(convs) > 0 {
		page, _, _ := v.data.get()
		for i := range page.Items {
			if it := &page.Items[i]; convs[it.Channel] {
				if r := v.rows[actKey(it)]; r != nil && r.built {
					r.built, unbuilt = false, true
				}
			}
		}
	}
	if unbuilt { // the list builds again only for a row that changed
		v.dirty = true
	}
}

// describingRows brackets rebuild's descriptions (UI goroutine): while
// on, every profile or conversation change may be one a row is reading;
// when it ends, readers is what the built rows read and chans the
// conversations the loaded rows are about.
func (v *Activity) describingRows(on bool) {
	var readers, chans map[string]bool
	if !on {
		readers, chans = map[string]bool{}, map[string]bool{}
		for _, r := range v.rows {
			if r.built {
				for _, id := range r.d.reads {
					readers[id] = true
				}
			}
		}
		page, _, _ := v.data.get()
		for i := range page.Items {
			if ch := page.Items[i].Channel; ch != "" {
				chans[ch] = true
			}
		}
	}
	n := &v.notes
	n.mu.Lock()
	n.describing = on
	if !on {
		n.readers, n.chans = readers, chans
	}
	n.mu.Unlock()
}

// rowsAbout reports whether a loaded row is about conversation ch (any
// goroutine): ch "" is about everything, and while rows are being
// described any may be.
func (v *Activity) rowsAbout(ch string) bool {
	n := &v.notes
	n.mu.Lock()
	defer n.mu.Unlock()
	return ch == "" || n.describing || n.chans[ch]
}
