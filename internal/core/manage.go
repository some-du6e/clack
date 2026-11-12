package core

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// Channel management, stars, notification prefs, saved / to-dos and
// activity writes — the methods the Slack web client uses (see
// docs/NATIVE.md; verified 2026-11).

// ---- stars ----

// loadStars reads starred conversations (stars.list lives on the team host).
func (a *App) loadStars(ctx context.Context) error {
	set := map[string]bool{}
	a.mu.Lock()
	a.starEdits = map[string]bool{}
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		a.starEdits = nil
		a.mu.Unlock()
	}()
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
	for ch, on := range a.starEdits {
		if on {
			set[ch] = true
		} else {
			delete(set, ch)
		}
	}
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
	if a.starEdits != nil {
		a.starEdits[ch] = on
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
	sync := false // like the web client: sync only when a synced pref (desktop/mobile) changes
	for _, p := range prefs {
		sync = sync || p.Sync
	}
	err := a.C.Call(ctx, "users.prefs.setNotifications", url.Values{"global": {"false"}, "channel_ids": {ch},
		"prefs": {string(b)}, "sync": {strconv.FormatBool(sync)}}, nil)
	if err != nil {
		return err
	}
	a.notifyRefresh(ctx, ch)
	return nil
}

// notifyRefresh rereads the prefs after a notification write: pref_change
// follows, but callers should see the result at once.
func (a *App) notifyRefresh(ctx context.Context, ch string) {
	if _, err := a.RefreshPrefs(ctx); err == nil {
		p := a.Prefs()
		a.emit(Event{Type: "prefs", Prefs: &p})
		if c, ok := a.Conv(ch); ok {
			a.emit(Event{Type: "conv", Channel: ch, Conv: &c})
		}
	}
}

// SetChannelNotifyPref writes one notification override for ch the way
// Slack's channel notifications modal does (boot-shared AvPY
// setChannelNotificationOverrideAndCallApi -> client-boot nfg+
// setNotificationsChannelOverride): users.prefs.setNotifications with
// name, value, channel_id, global=false and sync=false. name is one of
// desktop_push_enabled, mobile, badge_all_unreads, follow_all_threads,
// suppress_at_channel (and the others Slack's 1pDi o7 names).
func (a *App) SetChannelNotifyPref(ctx context.Context, ch, name, value string) error {
	err := a.C.Call(ctx, "users.prefs.setNotifications", url.Values{"name": {name}, "value": {value},
		"channel_id": {ch}, "global": {"false"}, "sync": {"false"}}, nil)
	if err != nil {
		return err
	}
	a.notifyRefresh(ctx, ch)
	return nil
}

// Mute / unmute a conversation ("Mute channel").
func (a *App) Mute(ctx context.Context, ch string, muted bool) error {
	return a.setNotifications(ctx, ch, []notifyPref{{Name: "muted", Value: strconv.FormatBool(muted)}})
}

// TempMute mutes ch until the Unix time until (Slack's "Temporarily
// mute…": temp_mute_expiration_ts, unsynced; useTempMute in boot-shared).
func (a *App) TempMute(ctx context.Context, ch string, until int64) error {
	return a.setNotifications(ctx, ch, []notifyPref{{Name: "temp_mute_expiration_ts", Value: strconv.FormatInt(until, 10)}})
}

// SetNotifyPrefs writes a conversation's notification override. Desktop
// and Mobile: everything | mentions_dms | nothing ("" leaves them alone).
// Verified: with sync=true Slack makes mobile follow desktop and rejects an
// explicit mobile value (cannot_sync), so a mobile level equal to desktop
// (or unset) is sent synced, and a different one unsynced.
func (a *App) SetNotifyPrefs(ctx context.Context, ch string, n ChannelNotify) error {
	var ps []notifyPref
	synced := n.Desktop != "" && (n.Mobile == "" || n.Mobile == n.Desktop)
	if n.Desktop != "" {
		ps = append(ps, notifyPref{Name: "desktop", Value: n.Desktop, Sync: synced})
	}
	if n.Mobile != "" && !synced {
		ps = append(ps, notifyPref{Name: "mobile", Value: n.Mobile})
	}
	ps = append(ps,
		notifyPref{Name: "follow_all_threads", Value: strconv.FormatBool(n.FollowAllThreads)},
		notifyPref{Name: "suppress_at_channel", Value: strconv.FormatBool(n.SuppressAtChannel)},
		notifyPref{Name: "muted", Value: strconv.FormatBool(n.Muted)})
	return a.setNotifications(ctx, ch, ps)
}

// ---- custom emoji ----

// AddEmoji uploads a custom emoji (emoji.add, mode=data, multipart image).
// Slack's error codes come back as *slack.Error (error_name_taken,
// too_large, invalid_name, …). The new emoji reaches CustomEmoji through
// the realtime emoji_changed event; it is also added locally as soon as
// Slack answers with its URL.
func (a *App) AddEmoji(ctx context.Context, name string, image []byte) error {
	name = strings.Trim(strings.TrimSpace(name), ":")
	if name == "" || len(image) == 0 {
		return errors.New("core: AddEmoji needs a name and an image")
	}
	var r struct {
		Emoji struct {
			Name string `json:"name"`
			URL  string `json:"url"`
		} `json:"emoji"`
		URL string `json:"url"`
	}
	if err := a.C.CallMultipart(ctx, "emoji.add", url.Values{"mode": {"data"}, "name": {name}}, "image", name+emojiExt(image), image, &r); err != nil {
		return err
	}
	if u := firstNonEmpty(r.Emoji.URL, r.URL); u != "" {
		a.S.CustomEmoji.Put(name, u)
		a.emit(Event{Type: "emoji_changed", Name: name})
	}
	return nil
}

func emojiExt(b []byte) string {
	switch http.DetectContentType(b) {
	case "image/gif":
		return ".gif"
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	}
	return ".png"
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
	delete(a.memberG, ch)
	delete(a.memberA, ch)
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

// savedFileKey keys saved files in the saved set: savedKey(savedFileKey, id).
const savedFileKey = "file"

// FileSaved reports whether file id is in Later (Slack's file.saved).
func (a *App) FileSaved(id string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.saved[savedKey(savedFileKey, id)]
}

// SaveFile / UnsaveFile add a file to Later or take it out (Slack's
// saveFile / removeFile: saved.add / saved.delete {item_type: file,
// item_id}).
func (a *App) SaveFile(ctx context.Context, id string) error {
	err := a.C.Call(ctx, "saved.add", url.Values{"item_type": {"file"}, "item_id": {id}}, nil)
	if err != nil && !isCode(err, "already_saved") {
		return err
	}
	a.markSaved(savedFileKey, id, true)
	return nil
}

func (a *App) UnsaveFile(ctx context.Context, id string) error {
	if err := a.C.Call(ctx, "saved.delete", url.Values{"item_type": {"file"}, "item_id": {id}}, nil); err != nil {
		return err
	}
	a.markSaved(savedFileKey, id, false)
	return nil
}

// DeleteFile deletes a file (DeleteFileDialog's deleteFile: files.delete
// {file}). Slack then sends file_deleted and the message's update.
func (a *App) DeleteFile(ctx context.Context, id string) error {
	return a.C.Call(ctx, "files.delete", url.Values{"file": {id}}, nil)
}

// loadSaved builds the saved/to-do set used for Message.Saved.
func (a *App) loadSaved(ctx context.Context) error {
	set := map[string]bool{}
	meta := map[string]SavedMeta{} // saved_meta.go
	a.mu.Lock()
	a.savedEdits = map[string]bool{}
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		a.savedEdits = nil
		a.mu.Unlock()
	}()
	for _, f := range []string{SavedFilterSaved, SavedFilterTodos} {
		cursor := ""
		for page := 0; page < 10; page++ {
			p := url.Values{"limit": {"50"}, "filter": {f}}
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
				switch it.ItemType {
				case "message":
					set[savedKey(it.ItemID, it.TS)] = true
					meta[savedKey(it.ItemID, it.TS)] = savedMetaOf(&it)
				case "file":
					set[savedKey(savedFileKey, it.ItemID)] = true
				}
			}
			if r.Meta.NextCursor == "" {
				break
			}
			cursor = r.Meta.NextCursor
		}
	}
	a.mu.Lock()
	for k, on := range a.savedEdits {
		if on {
			set[k] = true
			if m, ok := a.savedMeta[k]; ok { // the edit's metadata
				meta[k] = m
			}
		} else {
			delete(set, k)
			delete(meta, k)
		}
	}
	a.saved, a.savedMeta = set, meta
	a.mu.Unlock()
	return nil
}

func (a *App) markSaved(ch, ts string, on bool) {
	a.mu.Lock()
	if on {
		a.saved[savedKey(ch, ts)] = true
	} else {
		delete(a.saved, savedKey(ch, ts))
		delete(a.savedMeta, savedKey(ch, ts))
	}
	if a.savedEdits != nil {
		a.savedEdits[savedKey(ch, ts)] = on
	}
	a.mu.Unlock()
	if m, ok := a.S.Find(ch, ts); ok {
		a.emitUpdate(ch, &m)
	}
}

// AddTodo adds a message to To-dos (dateDue unix s; 0 = now, as the client does).
func (a *App) AddTodo(ctx context.Context, ch, ts string, dateDue int64) error {
	created := time.Now().Unix()
	if dateDue == 0 {
		dateDue = created
	}
	p := savedParams(ch, ts)
	p.Set("todo_state", "to_do")
	p.Set("date_due", strconv.FormatInt(dateDue, 10))
	if err := a.C.Call(ctx, "saved.add", p, nil); err != nil && !isCode(err, "already_saved") {
		return err
	}
	a.setSavedMeta(ch, ts, SavedMeta{TodoState: "to_do", DateCreated: created, DateDue: dateDue})
	return nil
}

// CompleteTodo marks a to-do done.
func (a *App) CompleteTodo(ctx context.Context, ch, ts string) error {
	p := savedParams(ch, ts)
	p.Set("todo_state", "completed")
	p.Set("mark", "completed")
	p.Set("date_due", "0")
	if err := a.C.Call(ctx, "saved.update", p, nil); err != nil {
		return err
	}
	a.patchSavedMeta(ch, ts, func(m *SavedMeta) { m.TodoState, m.DateCompleted, m.DateDue = "completed", time.Now().Unix(), 0 })
	return nil
}

// MoveToSaved turns a to-do (or completed item) back into a plain saved item.
func (a *App) MoveToSaved(ctx context.Context, ch, ts string) error {
	p := savedParams(ch, ts)
	p.Set("todo_state", "saved")
	p.Set("date_due", "0")
	p.Set("mark", "uncompleted")
	if err := a.C.Call(ctx, "saved.update", p, nil); err != nil {
		return err
	}
	a.patchSavedMeta(ch, ts, func(m *SavedMeta) { m.TodoState, m.DateCompleted, m.DateDue = "saved", 0, 0 })
	return nil
}

// MoveToTodos turns a saved item into a to-do due at dateDue (0 = now).
func (a *App) MoveToTodos(ctx context.Context, ch, ts string, dateDue int64) error {
	if dateDue == 0 {
		dateDue = time.Now().Unix()
	}
	p := savedParams(ch, ts)
	p.Set("todo_state", "to_do")
	p.Set("date_due", strconv.FormatInt(dateDue, 10))
	p.Set("mark", "uncompleted") // a completed item would otherwise stay completed
	if err := a.C.Call(ctx, "saved.update", p, nil); err != nil {
		return err
	}
	a.patchSavedMeta(ch, ts, func(m *SavedMeta) { m.TodoState, m.DateCompleted, m.DateDue = "to_do", 0, dateDue })
	return nil
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

// ActivityFilters narrow activity.clearBefore to a view's filters (uby+
// o in the web client: the fetcher snake-cases the keys).
type ActivityFilters struct {
	EntryTypes  []string `json:"entry_types,omitempty"`
	ChannelIDs  []string `json:"channel_ids,omitempty"`
	UnreadOnly  bool     `json:"unread_only,omitempty"`
	ArchiveOnly bool     `json:"archive_only,omitempty"`
}

// ClearActivityBefore clears every item older than beforeTS (a feed_ts),
// as Slack's "Clear all before…" does (ClearAllBeforeDialog:
// activity.clearBefore {before_ts, filters}). f nil clears the whole inbox.
func (a *App) ClearActivityBefore(ctx context.Context, beforeTS string, f *ActivityFilters) error {
	p := url.Values{"before_ts": {beforeTS}}
	if f != nil {
		b, _ := json.Marshal(f)
		p.Set("filters", string(b))
	}
	return a.C.Call(ctx, "activity.clearBefore", p, nil)
}

// ActivityDensity reads the Activity page's layout the way Slack's
// fetchActivityViews does: activity.views answers the saved views (each
// with its density) and, with activity_global_settings on, the global
// prefs whose density applies to every view (formatFilterViewState,
// getDensityFromApiDensity: "dense", else Detailed). ok is false when
// neither says.
func (a *App) ActivityDensity(ctx context.Context) (dense, ok bool, err error) {
	var res struct {
		Views []struct {
			ViewType string `json:"view_type"`
			Density  string `json:"density"`
		} `json:"views"`
		Prefs *struct {
			Density string `json:"density"`
		} `json:"prefs"`
	}
	if err := a.C.Call(ctx, "activity.views", url.Values{}, &res); err != nil {
		return false, false, err
	}
	if a.Experiment("activity_global_settings") == "on" && res.Prefs != nil && res.Prefs.Density != "" {
		return res.Prefs.Density == "dense", true, nil
	}
	for _, v := range res.Views {
		if v.ViewType == "all" && v.Density != "" {
			return v.Density == "dense", true, nil
		}
	}
	return false, false, nil
}

// SetActivityDensity saves the layout toggle. With activity_global_settings
// on (the toggle in the page header) Slack writes the global prefs:
// activity.prefs.update {density: "dense"|"compact"}
// (updateAllActivityViewsByApi). Without it the All view's density only
// lives in the page (updateActivityViewsByApi skips the API for the All
// view), so there is nothing to write: saved reports which happened.
func (a *App) SetActivityDensity(ctx context.Context, dense bool) (saved bool, err error) {
	if a.Experiment("activity_global_settings") != "on" {
		return false, nil
	}
	d := "compact"
	if dense {
		d = "dense"
	}
	return true, a.SetActivityPrefs(ctx, "", d)
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
	if e.Saved.ItemType == "file" && e.Saved.ItemID != "" {
		a.markSaved(savedFileKey, e.Saved.ItemID, typ != "saved_deleted")
	}
	it := savedItemOf(&e.Saved)
	if it.Channel != "" {
		if typ != "saved_deleted" {
			a.setSavedMeta(it.Channel, it.TS, savedMetaOf(&e.Saved)) // saved_meta.go
		} else {
			a.markSaved(it.Channel, it.TS, false)
		}
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
			LastRead string `json:"last_read"`
		} `json:"subscription"`
	}
	_ = json.Unmarshal(raw, &e)
	ch, ts := e.Subscription.Channel, e.Subscription.ThreadTS
	if ch == "" || ts == "" {
		return
	}
	// threadSubscribed / threadUnsubscribed: {subscribed, lastRead}
	if m, ok := a.S.Mutate(ch, ts, func(m *slack.Message) {
		m.Subscribed = on
		if lr := e.Subscription.LastRead; lr != "" {
			m.LastRead = lr
		}
	}); ok {
		a.emitUpdate(ch, &m)
	}
	a.emit(Event{Type: "thread_subscription", Channel: ch, ThreadTS: ts, TS: ts, Name: map[bool]string{true: "follow", false: "unfollow"}[on], Raw: raw})
}

// onThreadMarked is the thread_marked event ({subscription: {channel,
// thread_ts, last_read}}): another client (or this one) read the thread.
func (a *App) onThreadMarked(raw json.RawMessage) {
	var e struct {
		Subscription struct {
			Channel  string `json:"channel"`
			ThreadTS string `json:"thread_ts"`
			LastRead string `json:"last_read"`
		} `json:"subscription"`
	}
	_ = json.Unmarshal(raw, &e)
	a.setThreadLastRead(e.Subscription.Channel, e.Subscription.ThreadTS, e.Subscription.LastRead)
}

// setThreadLastRead records a thread's last read reply on its cached root
// (Slack's thread subscription lastRead) and tells the views.
func (a *App) setThreadLastRead(ch, threadTS, lastRead string) {
	if ch == "" || threadTS == "" || lastRead == "" {
		return
	}
	if m, ok := a.S.Mutate(ch, threadTS, func(m *slack.Message) { m.LastRead = lastRead }); ok {
		a.emitUpdate(ch, &m)
	}
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
