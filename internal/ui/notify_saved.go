package ui

import (
	"context"
	"log"
	"net/url"
	"strings"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/logtag"
)

// A Later reminder coming due (saved_due), as Slack's client shows it
// (build a09b625d90aa; docs/NOTIFY-RULES.md §1, §6):
//
//   - handleSavedDue (boot-render) holds the item and loads its message
//     (core announceSaved: the "saved" event, Name "due"), then asks
//     shouldShowNotificationForSavedItem (client-boot Fpyc x6): the
//     notification permission 15, global_desktop stored as nothing 14
//     (areGlobalDesktopNotificationsDisabled reads the stored value), and
//     for a message a conversation the client knows 3 (24 and 17 are
//     other workspaces'). No Do Not Disturb, mute, level or push check.
//     It logs "saved_due dispatching notification" or "saved_due
//     suppressed: <reason>": clack.log's decision line says the same.
//   - showSavedItemNotification (boot-shared) bounces the dock and, for a
//     message the store holds, shows sharedSavedItemNotificationOptions:
//     "Reminder due" and the message as getContentForNotification writes
//     a message's ("{sender}: " outside a DM; nothing at all with
//     no_text_in_notifications), with desktopSavedItemNotificationOptions:
//     the item's key (getIdForSavedItem) as msg, id type saved, silent, no
//     reply box. A reminder's says its description, a canvas section's
//     its description or the canvas's title, a file's its title; a list
//     record's its name, which Clack doesn't hold here ("").
//   - Its click is openSavedForLaterView (notificationClicked: a msg that
//     looksLikeSavedItemId; slack://saved-for-later?item_id= the same):
//     Later, with the message beside the list, or its thread at it.
//
// Clack ignored saved_due: no reminder ever showed.

// noteSkipGlobalOff: global_desktop stored as nothing (x6's 14); 15: the
// notification permission (Clack's Notifications switch).
const (
	noteSkipGlobalOff  noteSkip = "14"
	noteSkipPermission noteSkip = "15"
)

// savedDecided is the decision line for a saved_due: the item's tags
// (a message's conversation and ts, else the item and the rest of its key).
func savedDecided(it *core.SavedItem, why noteSkip) {
	key := core.SavedItemID(it)
	at := logtag.ID(it.ItemID)
	if rest := strings.TrimPrefix(strings.TrimPrefix(key, it.ItemID), "-"); rest != "" {
		at += "/" + logtag.ID(rest)
	}
	if why == noteShow {
		log.Printf("notify: show saved_due %s %s", it.ItemType, at)
		return
	}
	log.Printf("notify: skip %s saved_due %s %s", why, it.ItemType, at)
}

// savedDue is handleSavedDue's notification for the item of ev.
func (b *backend) savedDue(a *App, ev core.Event) {
	it := ev.Saved
	if it == nil || b.core == nil {
		return
	}
	switch {
	case !a.notifyOn():
		savedDecided(it, noteSkipPermission)
		return
	case b.core.Prefs().GlobalDesktop == "nothing":
		savedDecided(it, noteSkipGlobalOff)
		return
	case it.ItemType == "message":
		if _, ok := b.core.Conv(it.Channel); !ok {
			savedDecided(it, noteSkipNoModel)
			return
		}
		if it.Msg == nil {
			// showSavedItemNotification shows a message's only once the store
			// holds it (getMessageByTs)
			savedDecided(it, noteSkipNoModel)
			return
		}
	}
	savedDecided(it, noteShow)
	note := a.savedNote(it)
	if id := b.savedFileID(it); id != "" {
		// a file's, a canvas section's without a description: handleSavedDue
		// fetched the file first (files.info), off the UI goroutine here
		c := b.core
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			title, _ := c.FileTitle(ctx, id)
			cancel()
			a.Post(func() { a.notifyShow(nsGrowl.T("Reminder due"), title, note) })
		}()
		return
	}
	a.notifyShow(nsGrowl.T("Reminder due"), b.savedBody(it), note)
}

// savedFileID is the file whose title is the reminder's content
// (getSavedFileTitle): a file's, or a canvas section's with no
// description; "" for any other item, and with no_text_in_notifications.
func (b *backend) savedFileID(it *core.SavedItem) string {
	if b.core.Prefs().NoTextInNotifications {
		return ""
	}
	switch it.ItemType {
	case "message", "reminder", "list_record", "list_record_field", "salesforce_task":
		return ""
	case "canvas_section":
		if it.Description != "" {
			return ""
		}
	}
	return it.ItemID
}

// savedBody is the reminder's content (sharedSavedItemNotificationOptions
// and the per-type getters), "" with no_text_in_notifications.
func (b *backend) savedBody(it *core.SavedItem) string {
	if b.core.Prefs().NoTextInNotifications {
		return ""
	}
	switch it.ItemType {
	case "message":
		c, _ := b.noteConv(it.Channel)
		return b.notifyBody(core.Event{Channel: it.Channel, TS: it.TS, Msg: it.Msg}, c)
	case "reminder", "canvas_section": // a section's without one is its canvas's title (savedFileID)
		return it.Description
	}
	return "" // a list record's name, a Salesforce task's subject: Clack holds neither here
}

// savedNote is desktopSavedItemNotificationOptions for item it: the item's
// key as msg (and as the channel for an item that isn't a message), the
// message's thread, silent, no reply box.
func (a *App) savedNote(it *core.SavedItem) *desktopNote {
	team, ws := a.noteTeam()
	key := core.SavedItemID(it)
	ch := key
	if it.ItemType == "message" {
		ch = it.Channel
	}
	n := &desktopNote{Team: team, Workspace: ws, Channel: ch, Msg: key, Silent: true, pushChecked: true, textChecked: true}
	if it.Msg != nil {
		n.ThreadTS = it.Msg.ThreadTS
	}
	if a.noteActionsOn() {
		n.ID = buildNotificationID(team, noteTypeSaved, key, notificationMinuteBucket(time.Now()))
	} else {
		n.ID = shellNoteID(team, key)
	}
	n.Activated = func(act toastActivation) { a.Post(func() { a.toastActivated(n, act) }) }
	return n
}

// openSavedKey is openSavedForLaterView({itemId: key}) from a link: a
// message's thread as the store holds it (resolveSavedMessageTarget).
func (a *App) openSavedKey(key string) {
	ch, ts, ok := core.ParseSavedItemID(key)
	if !ok {
		a.Navigate("later")
		return
	}
	thread := ""
	if c := a.Core(); c != nil && ch != "" {
		if m, found := c.S.Find(ch, ts); found {
			thread = m.ThreadTS
		}
	}
	a.openSavedForLater(ch, ts, thread)
}

// savedForLaterLink is handleParsedDeepLink's "saved-for-later" command:
// the item_id of slack://saved-for-later?team=T&item_id=… ("" for the
// Later page alone).
func savedForLaterLink(uri string) (key string, ok bool) {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "slack" || u.Host != "saved-for-later" {
		return "", false
	}
	return u.Query().Get("item_id"), true
}

// openSavedForLater is openSavedForLaterView for the item with key: the
// Later page, and a message's conversation beside it at the message, or
// its thread at it (the Later row's own open: views savedBase.open).
func (a *App) openSavedForLater(ch, ts, thread string) {
	a.Navigate("later")
	switch {
	case ch == "" || ts == "":
	case thread != "" && thread != ts:
		a.OpenThreadAt(ch, thread, ts)
	default:
		a.OpenMessageSecondary(ch, ts)
	}
}
