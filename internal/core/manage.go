package core

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/store"
)

// Channel management, stars, notification prefs, saved / to-dos and
// activity writes — the methods the Slack web client uses (see
// docs/NATIVE.md; verified 2026-11).

// ---- stars ----

// loadStars reads starred conversations (stars.list lives on the team host).
func (a *App) loadStars(ctx context.Context) error {
	set := map[string]bool{}
	page := 1
	for page <= 10 {
		var r struct {
			Items []struct {
				Type    string `json:"type"`
				Channel string `json:"channel"`
			} `json:"items"`
			Paging struct {
				Pages int `json:"pages"`
			} `json:"paging"`
		}
		if err := a.C.Call(ctx, "stars.list", url.Values{"count": {"100"}, "page": {strconv.Itoa(page)}}, &r); err != nil {
			return err
		}
		for _, it := range r.Items {
			if it.Type == "channel" || it.Type == "im" || it.Type == "group" {
				set[it.Channel] = true
			}
		}
		if page >= r.Paging.Pages {
			break
		}
		page++
	}
	a.mu.Lock()
	a.starred, a.starsOK = set, true
	a.mu.Unlock()
	return nil
}

// Starred reports whether a conversation is starred.
func (a *App) Starred(ch string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.starred[ch]
}

func (a *App) setStarred(ch string, on bool) {
	a.mu.Lock()
	if on {
		a.starred[ch] = true
	} else {
		delete(a.starred, ch)
	}
	a.mu.Unlock()
}

// Star stars (star=true) or unstars a conversation; it moves into or out
// of the Starred sidebar section.
func (a *App) Star(ctx context.Context, ch string, star bool) error {
	m, ignore := "stars.add", "already_starred"
	if !star {
		m, ignore = "stars.remove", "not_starred"
	}
	if err := a.C.Call(ctx, m, url.Values{"channel": {ch}}, nil); err != nil && !isCode(err, ignore) {
		return err
	}
	a.setStarred(ch, star)
	a.emit(Event{Type: "sidebar_changed", Channel: ch})
	return nil
}

// ---- notification prefs & mute ----

// ChannelNotify is a conversation's notification override.
type ChannelNotify = store.ChannelNotify

// NotifyPrefs returns ch's notification settings (zero value: defaults).
func (a *App) NotifyPrefs(ch string) ChannelNotify {
	a.S.RLock()
	defer a.S.RUnlock()
	return a.S.Prefs.ChannelNotify[ch]
}

type notifyPref struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	Sync  bool   `json:"sync,omitempty"`
}

func (a *App) setNotifications(ctx context.Context, ch string, prefs []notifyPref) error {
	b, _ := json.Marshal(prefs)
	err := a.C.Call(ctx, "users.prefs.setNotifications", url.Values{"global": {"false"}, "channel_ids": {ch},
		"prefs": {string(b)}, "sync": {"true"}}, nil)
	if err != nil {
		return err
	}
	// pref_change follows; refresh now so callers see the result at once.
	if _, err := a.RefreshPrefs(ctx); err == nil {
		p := a.Prefs()
		a.emit(Event{Type: "prefs", Prefs: &p})
		if c, ok := a.Conv(ch); ok {
			a.emit(Event{Type: "conv", Channel: ch, Conv: &c})
		}
	}
	return nil
}

// Mute / unmute a conversation ("Mute channel").
func (a *App) Mute(ctx context.Context, ch string, muted bool) error {
	return a.setNotifications(ctx, ch, []notifyPref{{Name: "muted", Value: strconv.FormatBool(muted)}})
}

// SetNotifyPrefs writes a conversation's notification override. Desktop
// and Mobile: everything | mentions_dms | nothing ("" leaves them alone).
func (a *App) SetNotifyPrefs(ctx context.Context, ch string, n ChannelNotify) error {
	var ps []notifyPref
	if n.Desktop != "" {
		ps = append(ps, notifyPref{Name: "desktop", Value: n.Desktop, Sync: true})
	}
	if n.Mobile != "" {
		ps = append(ps, notifyPref{Name: "mobile", Value: n.Mobile, Sync: true})
	}
	ps = append(ps,
		notifyPref{Name: "follow_all_threads", Value: strconv.FormatBool(n.FollowAllThreads)},
		notifyPref{Name: "suppress_at_channel", Value: strconv.FormatBool(n.SuppressAtChannel)},
		notifyPref{Name: "muted", Value: strconv.FormatBool(n.Muted)})
	return a.setNotifications(ctx, ch, ps)
}

// ---- channel admin ----

func (a *App) convChanged(ctx context.Context, ch string, res *struct {
	Channel slack.Conversation `json:"channel"`
}) {
	if res != nil && res.Channel.ID != "" {
		if old, ok := a.S.Conv(ch); ok {
			res.Channel.LastRead = old.LastRead
		}
		a.S.PutConv(res.Channel)
		if c, ok := a.Conv(ch); ok {
			a.emit(Event{Type: "conv", Channel: ch, Conv: &c})
		}
		return
	}
	a.refreshConv(ctx, ch)
}

type convResult = struct {
	Channel slack.Conversation `json:"channel"`
}

// Rename renames a channel (needs permission; Slack lowercases the name).
func (a *App) Rename(ctx context.Context, ch, name string) error {
	var res convResult
	if err := a.C.Call(ctx, "conversations.rename", url.Values{"channel": {ch}, "name": {name}}, &res); err != nil {
		return err
	}
	a.convChanged(ctx, ch, &res)
	return nil
}

// SetPurpose sets a channel's description.
func (a *App) SetPurpose(ctx context.Context, ch, purpose string) error {
	var res struct {
		Purpose string `json:"purpose"`
	}
	if err := a.C.Call(ctx, "conversations.setPurpose", url.Values{"channel": {ch}, "purpose": {purpose}}, &res); err != nil {
		return err
	}
	a.refreshConv(ctx, ch)
	return nil
}

// SetTopic sets a channel topic.
func (a *App) SetTopic(ctx context.Context, ch, topic string) error {
	var res convResult
	if err := a.C.Call(ctx, "conversations.setTopic", url.Values{"channel": {ch}, "topic": {topic}}, &res); err != nil {
		return err
	}
	a.convChanged(ctx, ch, &res)
	return nil
}

// Invite adds users to a channel.
func (a *App) Invite(ctx context.Context, ch string, users ...string) error {
	if len(users) == 0 {
		return errors.New("core: Invite needs users")
	}
	if err := a.C.Call(ctx, "conversations.invite", url.Values{"channel": {ch}, "users": {strings.Join(users, ",")}}, nil); err != nil {
		return err
	}
	a.mu.Lock()
	delete(a.memberN, ch)
	a.mu.Unlock()
	return nil
}

// Archive archives a channel (it leaves the sidebar).
func (a *App) Archive(ctx context.Context, ch string) error {
	if err := a.C.Call(ctx, "conversations.archive", url.Values{"channel": {ch}}, nil); err != nil {
		return err
	}
	a.S.Lock()
	delete(a.S.Convs, ch)
	a.S.Unlock()
	a.emit(Event{Type: "conv_removed", Channel: ch})
	return nil
}

// ---- saved & to-dos ----

// Saved filters (saved.list), as the web client uses them.
const (
	SavedFilterSaved = "saved_with_archived" // Saved page
	SavedFilterTodos = "todo_with_completed" // To-dos page
)

func savedKey(ch, ts string) string { return ch + "/" + ts }

// loadSaved builds the saved/to-do set used for Message.Saved.
func (a *App) loadSaved(ctx context.Context) error {
	set := map[string]bool{}
	for _, f := range []string{SavedFilterSaved, SavedFilterTodos} {
		cursor := ""
		for page := 0; page < 10; page++ {
			p := url.Values{"limit": {"100"}, "filter": {f}}
			if cursor != "" {
				p.Set("cursor", cursor)
			}
			var r struct {
				Items []savedRaw `json:"saved_items"`
				Meta  struct {
					NextCursor string `json:"next_cursor"`
				} `json:"response_metadata"`
			}
			if err := a.C.Call(ctx, "saved.list", p, &r); err != nil {
				return err
			}
			for _, it := range r.Items {
				if it.ItemType == "message" {
					set[savedKey(it.ItemID, it.TS)] = true
				}
			}
			if r.Meta.NextCursor == "" {
				break
			}
			cursor = r.Meta.NextCursor
		}
	}
	a.mu.Lock()
	a.saved = set
	a.mu.Unlock()
	return nil
}

func (a *App) markSaved(ch, ts string, on bool) {
	a.mu.Lock()
	if on {
		a.saved[savedKey(ch, ts)] = true
	} else {
		delete(a.saved, savedKey(ch, ts))
	}
	a.mu.Unlock()
	if m, ok := a.S.Find(ch, ts); ok {
		a.emitUpdate(ch, &m)
	}
}

// AddTodo adds a message to To-dos (dateDue unix s; 0 = now, as the client does).
func (a *App) AddTodo(ctx context.Context, ch, ts string, dateDue int64) error {
	if dateDue == 0 {
		dateDue = time.Now().Unix()
	}
	p := savedParams(ch, ts)
	p.Set("todo_state", "to_do")
	p.Set("date_due", strconv.FormatInt(dateDue, 10))
	if err := a.C.Call(ctx, "saved.add", p, nil); err != nil && !isCode(err, "already_saved") {
		return err
	}
	a.markSaved(ch, ts, true)
	return nil
}

// CompleteTodo marks a to-do done.
func (a *App) CompleteTodo(ctx context.Context, ch, ts string) error {
	p := savedParams(ch, ts)
	p.Set("todo_state", "completed")
	p.Set("mark", "completed")
	p.Set("date_due", "0")
	return a.C.Call(ctx, "saved.update", p, nil)
}

// MoveToSaved turns a to-do (or completed item) back into a plain saved item.
func (a *App) MoveToSaved(ctx context.Context, ch, ts string) error {
	p := savedParams(ch, ts)
	p.Set("todo_state", "saved")
	p.Set("date_due", "0")
	p.Set("mark", "uncompleted")
	return a.C.Call(ctx, "saved.update", p, nil)
}

// MoveToTodos turns a saved item into a to-do due at dateDue (0 = now).
func (a *App) MoveToTodos(ctx context.Context, ch, ts string, dateDue int64) error {
	if dateDue == 0 {
		dateDue = time.Now().Unix()
	}
	p := savedParams(ch, ts)
	p.Set("todo_state", "to_do")
	p.Set("date_due", strconv.FormatInt(dateDue, 10))
	return a.C.Call(ctx, "saved.update", p, nil)
}

type savedRaw struct {
	ItemID        string `json:"item_id"`
	ItemType      string `json:"item_type"`
	TS            string `json:"ts"`
	State         string `json:"state"`
	TodoState     string `json:"todo_state"`
	DateCreated   int64  `json:"date_created"`
	DateDue       int64  `json:"date_due"`
	DateCompleted int64  `json:"date_completed"`
	IsArchived    bool   `json:"is_archived"`
}

func savedItemOf(it *savedRaw) SavedItem {
	v := SavedItem{ItemID: it.ItemID, ItemType: it.ItemType, TS: it.TS, State: firstNonEmpty(it.TodoState, it.State),
		TodoState: it.TodoState, Progress: it.State, DateCreated: it.DateCreated, DateDue: it.DateDue,
		DateCompleted: it.DateCompleted, Archived: it.IsArchived}
	if it.ItemType == "message" {
		v.Channel = it.ItemID
	}
	return v
}

// ---- activity writes ----

// MarkActivityRead marks activity items read.
func (a *App) MarkActivityRead(ctx context.Context, items ...ActivityItem) error {
	for _, it := range items {
		if err := a.C.Call(ctx, "activity.markRead", url.Values{"type": {it.Type}, "feed_ts": {it.FeedTS}, "key": {it.Key}}, nil); err != nil {
			return err
		}
	}
	return nil
}

// MarkActivityUnread marks activity items unread.
func (a *App) MarkActivityUnread(ctx context.Context, items ...ActivityItem) error {
	type entry struct {
		Type   string `json:"type"`
		Key    string `json:"key"`
		TS     string `json:"ts"`
		FeedTS string `json:"feed_ts"`
	}
	es := make([]entry, 0, len(items))
	for _, it := range items {
		es = append(es, entry{Type: it.Type, Key: it.Key, TS: firstNonEmpty(it.TS, it.FeedTS), FeedTS: it.FeedTS})
	}
	b, _ := json.Marshal(es)
	return a.C.Call(ctx, "activity.markUnread", url.Values{"entries": {string(b)}}, nil)
}

// ClearActivity archives ("clears") activity items; UnclearActivity undoes it.
func (a *App) ClearActivity(ctx context.Context, items ...ActivityItem) error {
	return a.activityEach(ctx, "activity.archive", items)
}

func (a *App) UnclearActivity(ctx context.Context, items ...ActivityItem) error {
	return a.activityEach(ctx, "activity.unarchive", items)
}

func (a *App) activityEach(ctx context.Context, method string, items []ActivityItem) error {
	for _, it := range items {
		if err := a.C.Call(ctx, method, url.Values{"type": {it.Type}, "key": {it.Key}, "ts": {firstNonEmpty(it.FeedTS, it.TS)}}, nil); err != nil {
			return err
		}
	}
	return nil
}

// MarkAllActivityRead marks every unread item of a view read. It tries
// activity.markAllRead and falls back to marking each unread item.
func (a *App) MarkAllActivityRead(ctx context.Context, view string) error {
	types := activityTypes[view]
	if types == "" {
		types = activityTypes["all"]
	}
	if err := a.C.Call(ctx, "activity.markAllRead", url.Values{"types": {types}}, nil); err == nil {
		return nil
	}
	page, err := a.Activity(ctx, ActivityOpts{View: view, Unread: true, Limit: 50})
	if err != nil {
		return err
	}
	var unread []ActivityItem
	for _, it := range page.Items {
		if it.Unread {
			unread = append(unread, it)
		}
	}
	return a.MarkActivityRead(ctx, unread...)
}

// ---- realtime: saved, activity, tabs, thread subscriptions, stars ----

func (a *App) onSavedEvent(ctx context.Context, typ string, raw json.RawMessage) {
	var e struct {
		Saved savedRaw `json:"saved"`
	}
	_ = json.Unmarshal(raw, &e)
	it := savedItemOf(&e.Saved)
	if it.Channel != "" {
		a.markSaved(it.Channel, it.TS, typ != "saved_deleted")
	}
	if typ != "saved_deleted" && it.Channel != "" {
		got := a.hydrate(ctx, map[string][]string{it.Channel: {it.TS}})
		if m, ok := got[savedKey(it.Channel, it.TS)]; ok {
			it.Msg = a.messagePtr(it.Channel, &m)
		}
	}
	name := map[string]string{"saved_added": "add", "saved_updated": "update", "saved_deleted": "delete"}[typ]
	a.emit(Event{Type: "saved", Name: name, Channel: it.Channel, TS: it.TS, Saved: &it, Raw: raw})
}

func (a *App) onActivityEvent(ctx context.Context, raw json.RawMessage) {
	var e struct {
		Subtype string      `json:"subtype"`
		Key     string      `json:"key"`
		Entry   activityRaw `json:"entry"`
	}
	_ = json.Unmarshal(raw, &e)
	ev := Event{Type: "activity", Name: e.Subtype, ID: firstNonEmpty(e.Key, e.Entry.Key), Raw: raw}
	if e.Entry.Key != "" || e.Entry.Item.Type != "" {
		items := a.activityItems(ctx, []activityRaw{e.Entry})
		if len(items) == 1 {
			ev.Activity = &items[0]
			ev.Channel, ev.TS = items[0].Channel, items[0].TS
		}
	}
	a.emit(ev)
}

func (a *App) onThreadSubscription(raw json.RawMessage, on bool) {
	var e struct {
		Subscription struct {
			Channel  string `json:"channel"`
			ThreadTS string `json:"thread_ts"`
		} `json:"subscription"`
	}
	_ = json.Unmarshal(raw, &e)
	ch, ts := e.Subscription.Channel, e.Subscription.ThreadTS
	if ch == "" || ts == "" {
		return
	}
	if m, ok := a.S.Mutate(ch, ts, func(m *slack.Message) { m.Subscribed = on }); ok {
		a.emitUpdate(ch, &m)
	}
	a.emit(Event{Type: "thread_subscription", Channel: ch, ThreadTS: ts, TS: ts, Name: map[bool]string{true: "follow", false: "unfollow"}[on], Raw: raw})
}

func (a *App) onStarEvent(raw json.RawMessage, on bool) {
	var e struct {
		Item struct {
			Type    string `json:"type"`
			Channel string `json:"channel"`
		} `json:"item"`
	}
	_ = json.Unmarshal(raw, &e)
	if e.Item.Channel == "" || (e.Item.Type != "channel" && e.Item.Type != "im" && e.Item.Type != "group") {
		return
	}
	a.setStarred(e.Item.Channel, on)
	a.emit(Event{Type: "sidebar_changed", Channel: e.Item.Channel})
}

// loadExtras fills the saved and starred sets after boot (non-blocking).
func (a *App) loadExtras(ctx context.Context) {
	t0 := time.Now()
	errS := a.loadStars(ctx)
	errV := a.loadSaved(ctx)
	if errS != nil || errV != nil {
		log.Printf("boot extras: stars=%v saved=%v", errS, errV)
	}
	if errS == nil {
		a.emit(Event{Type: "sidebar_changed"})
	}
	a.mu.Lock()
	ns, nv := len(a.starred), len(a.saved)
	a.mu.Unlock()
	log.Printf("boot extras: %d starred, %d saved in %s", ns, nv, time.Since(t0).Round(time.Millisecond))
}
