package core

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/some-du6e/clack/internal/slack"
)

// What the sidebar section menus do, ported from Slack's web client (build
// a84662d2d135, 2026-11-08; webpack ids in
// gantry-v2-async-client-boot-{shared,render} and client-boot):
//
//   - Filter and Sort (boot-shared OHTN setChannelSectionPrefByApi): the
//     section's "sidebar" or "sort" value goes into the channel_sections
//     pref (users.prefs.set, a JSON object section id → {pref: value}).
//     When some section's filter then differs from what the global
//     sidebar_behavior gives it, and sidebar_behavior isn't already
//     "hide_read_channels_unless_starred" ("Custom by section"), Slack
//     first pins every section's current filter into the pref and switches
//     sidebar_behavior to Custom by section, so the other sections look the
//     same afterwards. Entries for sections that no longer exist are
//     dropped on write.
//   - Create (hW84 createChannelSection → 2A1t fetchChannelSectionsCreate):
//     users.channelSections.create {name, emoji, next_channel_section_id};
//     the answer's channel_section_id is the new section.
//   - Rename (oS renameChannelSection → 2A1t fetchChannelSectionsSet):
//     users.channelSections.set {channel_section_id, name, emoji}.
//   - Delete (hW84 deleteChannelSection): users.channelSections.delete
//     {channel_section_id}; section_not_found counts as done. Undo
//     re-creates a custom section with the same name, emoji and place and
//     puts its conversations back.
//   - Moving conversations (HKiO moveManyChannels):
//     users.channelSections.channels.bulkUpdate {insert, remove} for custom
//     sections; Starred stars or unstars instead.
//   - Mute all / Unmute all (IDOy bulkMuteUnmuteChannelIds → AvPY):
//     users.prefs.setNotifications {name: muted, value, channel_ids,
//     global: false, sync: false}.
//   - Mark all as read (nEGd markChannelsAsRead → LbeF): conversations.mark
//     for every unread conversation at its latest message; undo marks each
//     back at its old read cursor.
//
// After a section write Clack re-reads users.channelSections.list (Slack's
// channel_sections_* events say the same) and emits "sidebar_changed".

// Section name problems (BaseChannelSectionModalInput's validation states).
const (
	SectionNameEmpty    = "name_empty"
	SectionNameTooLong  = "name_too_long"
	SectionNameTaken    = "name_taken"
	SectionNameNotValid = "name_not_allowed"
)

// MaxSectionNameLength is the modal's 80-character limit.
const MaxSectionNameLength = 80

// SectionNameError checks a section name like Slack's rename and create
// modals: empty, over 80 characters, "ekm_access_denied", or the name of
// another section (case-insensitive, trimmed). except is the section being
// renamed ("" when creating).
func (a *App) SectionNameError(name, except string) string {
	t := strings.ToLower(strings.TrimSpace(name))
	switch {
	case name == "" || t == "":
		return SectionNameEmpty
	case utf8.RuneCountInString(name) > MaxSectionNameLength:
		return SectionNameTooLong
	case t == "ekm_access_denied":
		return SectionNameNotValid
	}
	a.S.RLock()
	defer a.S.RUnlock()
	for _, s := range a.S.Sections {
		if s.ID == except {
			continue
		}
		if strings.ToLower(strings.TrimSpace(SectionDisplayName(s.Type, s.Name))) == t {
			return SectionNameTaken
		}
	}
	return ""
}

// SectionNameErrorText is the alert under the name input ("" for none;
// empty and too-long names only disable Save, as in Slack).
func SectionNameErrorText(code string) string {
	switch code {
	case SectionNameTaken:
		return "You already have a section with this name."
	case SectionNameNotValid:
		return "Unfortunately, that name isn’t valid. Try another?"
	case "emoji_invalid":
		return "Unfortunately, that emoji’s been removed from your workspace."
	case "", SectionNameEmpty, SectionNameTooLong:
		return ""
	}
	return "Something went wrong. Reloading Slack might clear things up."
}

// SectionDisplayName is p4Ef getChannelSectionName: Slack's fixed names for
// its own sections, the user's for custom ones.
func SectionDisplayName(typ, name string) string {
	switch typ {
	case "channels":
		return "Channels"
	case "direct_messages":
		return "Direct messages"
	case "stars":
		return "Starred"
	case "recent_apps", "apps":
		return "Apps"
	case "temporary_channels":
		if name == "" {
			return "Temporary channels"
		}
	}
	return name
}

func (a *App) rawSection(id string) (slack.Section, bool) {
	a.S.RLock()
	defer a.S.RUnlock()
	for _, s := range a.S.Sections {
		if s.ID == id {
			return s, true
		}
	}
	return slack.Section{}, false
}

// refreshSections re-reads users.channelSections.list and redraws the
// sidebar.
func (a *App) refreshSections(ctx context.Context) {
	secs, err := a.C.SectionsAll(ctx)
	if err != nil {
		return
	}
	a.S.Lock()
	a.S.Sections = secs
	a.S.Unlock()
	a.emit(Event{Type: "sidebar_changed"})
}

// SetSectionPref is setChannelSectionPrefByApi: pref is "sidebar" (the
// Filter submenu) or "sort" (Sort).
func (a *App) SetSectionPref(ctx context.Context, sectionID, pref, value string) error {
	if sectionID == "" {
		return errors.New("core: no section")
	}
	a.S.RLock()
	raw := a.S.Prefs
	secs := append([]slack.Section(nil), a.S.Sections...)
	cur := a.S.RawPrefs["channel_sections"]
	a.S.RUnlock()
	var text string
	if json.Unmarshal(cur, &text) != nil {
		text = string(cur)
	}
	l := map[string]map[string]any{}
	_ = json.Unmarshal([]byte(text), &l)
	if l[sectionID] == nil {
		l[sectionID] = map[string]any{}
	}
	l[sectionID][pref] = value

	e := a.sidebarExperiments()
	behavior := sidebarBehavior(raw)
	typeOf := map[string]string{}
	for _, s := range secs {
		typeOf[s.ID] = s.Type
	}
	differs := false
	for id, kv := range l {
		typ, ok := typeOf[id]
		v, has := kv["sidebar"].(string)
		if !ok || !has {
			continue
		}
		k := v // ij26.K (programmable_sidebar_dm_history aside)
		if isWindowFilter(k) && !(e.dmHistory && typ == "direct_messages") {
			k = filterActive
		}
		def := defaultSectionFilter(typ, behavior, e)
		if v != def && k != "" && k != def {
			differs = true
			break
		}
	}
	if differs && behavior != "hide_read_channels_unless_starred" {
		for _, s := range secs {
			if l[s.ID] == nil {
				l[s.ID] = map[string]any{}
			}
			if _, has := l[s.ID]["sidebar"]; !has {
				l[s.ID]["sidebar"] = defaultSectionFilter(s.Type, behavior, e)
			}
		}
		if err := a.SetPref(ctx, "sidebar_behavior", "hide_read_channels_unless_starred"); err != nil {
			return err
		}
	}
	for id, kv := range l { // setChannelSectionPrefsByApi keeps live sections only
		if _, ok := typeOf[id]; !ok || len(kv) == 0 {
			delete(l, id)
		}
	}
	b, err := json.Marshal(l)
	if err != nil {
		return err
	}
	return a.SetPref(ctx, "channel_sections", string(b))
}

// CreateSection makes a custom section placed before next ("" = Slack's
// default spot) and returns its id.
func (a *App) CreateSection(ctx context.Context, name, emoji, next string) (string, error) {
	p := url.Values{"name": {name}, "emoji": {emoji}}
	if next != "" {
		p.Set("next_channel_section_id", next)
	}
	var r struct {
		ID string `json:"channel_section_id"`
	}
	if err := a.C.Call(ctx, "users.channelSections.create", p, &r); err != nil {
		return "", err
	}
	if r.ID == "" {
		return "", errors.New("core: users.channelSections.create answered no section")
	}
	a.refreshSections(ctx)
	return r.ID, nil
}

// RenameSection is users.channelSections.set with a new name and emoji.
func (a *App) RenameSection(ctx context.Context, id, name, emoji string) error {
	p := url.Values{"channel_section_id": {id}, "name": {name}, "emoji": {emoji}}
	if err := a.C.Call(ctx, "users.channelSections.set", p, nil); err != nil {
		return err
	}
	a.refreshSections(ctx)
	return nil
}

// DeletedSection is what Undo needs to bring a section back.
type DeletedSection struct {
	Section  slack.Section
	Channels []string
}

// DeleteSection removes a section (its conversations go back to Channels
// and Direct messages; Starred unstars nothing here, as in Slack).
func (a *App) DeleteSection(ctx context.Context, id string) (DeletedSection, error) {
	s, _ := a.rawSection(id)
	d := DeletedSection{Section: s, Channels: append([]string(nil), s.ChannelIDsPage.ChannelIDs...)}
	err := a.C.Call(ctx, "users.channelSections.delete", url.Values{"channel_section_id": {id}}, nil)
	if err != nil && !isCode(err, "section_not_found") {
		return d, err
	}
	a.refreshSections(ctx)
	return d, nil
}

// UndoDeleteSection re-creates a deleted custom section in its old place
// with its conversations.
func (a *App) UndoDeleteSection(ctx context.Context, d DeletedSection) (string, error) {
	id, err := a.CreateSection(ctx, d.Section.Name, d.Section.Emoji, d.Section.Next)
	if err != nil || len(d.Channels) == 0 {
		return id, err
	}
	return id, a.MoveToSection(ctx, id, d.Channels, nil)
}

// MoveToSection puts conversations into (insert) and out of (remove) a
// section. Conversations moved into a custom section leave the other
// custom sections they were in (moveManyChannels). Starred stars and
// unstars.
func (a *App) MoveToSection(ctx context.Context, id string, insert, remove []string) error {
	s, ok := a.rawSection(id)
	if !ok {
		return errors.New("core: no such section")
	}
	if s.Type == "stars" {
		for _, ch := range insert {
			if err := a.Star(ctx, ch, true); err != nil {
				return err
			}
		}
		for _, ch := range remove {
			if err := a.Star(ctx, ch, false); err != nil {
				return err
			}
		}
		a.emit(Event{Type: "sidebar_changed"})
		return nil
	}
	type item struct {
		Section  string   `json:"channel_section_id"`
		Channels []string `json:"channel_ids"`
	}
	ins, rem := []item{}, []item{}
	if len(insert) > 0 {
		ins = append(ins, item{id, insert})
		moving := map[string]bool{}
		for _, ch := range insert {
			moving[ch] = true
		}
		a.S.RLock()
		for _, o := range a.S.Sections {
			if o.ID == id || o.Type != "standard" {
				continue
			}
			var out []string
			for _, ch := range o.ChannelIDsPage.ChannelIDs {
				if moving[ch] {
					out = append(out, ch)
				}
			}
			if len(out) > 0 {
				rem = append(rem, item{o.ID, out})
			}
		}
		a.S.RUnlock()
	}
	if len(remove) > 0 {
		rem = append(rem, item{id, remove})
	}
	bi, _ := json.Marshal(ins)
	br, _ := json.Marshal(rem)
	if err := a.C.Call(ctx, "users.channelSections.channels.bulkUpdate", url.Values{"insert": {string(bi)}, "remove": {string(br)}}, nil); err != nil {
		return err
	}
	a.refreshSections(ctx)
	return nil
}

// OtherSectionOf is the custom section (other than id) ch is filed in, ""
// for none: the create modal's speed bump asks before moving those.
func (a *App) OtherSectionOf(ch, id string) string {
	a.S.RLock()
	defer a.S.RUnlock()
	for _, s := range a.S.Sections {
		if s.ID == id || s.Type != "standard" {
			continue
		}
		for _, c := range s.ChannelIDsPage.ChannelIDs {
			if c == ch {
				return s.ID
			}
		}
	}
	return ""
}

// MuteMany mutes or unmutes several conversations in one
// users.prefs.setNotifications call.
func (a *App) MuteMany(ctx context.Context, ids []string, muted bool) error {
	if len(ids) == 0 {
		return nil
	}
	p := url.Values{"name": {"muted"}, "value": {strconv.FormatBool(muted)}, "channel_ids": {strings.Join(ids, ",")},
		"global": {"false"}, "sync": {"false"}}
	if err := a.C.Call(ctx, "users.prefs.setNotifications", p, nil); err != nil {
		return err
	}
	if _, err := a.RefreshPrefs(ctx); err == nil {
		pr := a.Prefs()
		a.emit(Event{Type: "prefs", Prefs: &pr})
	}
	return nil
}

// MarkManyRead marks every unread conversation in ids read at its latest
// message and returns the old read cursors (for Undo) of those it marked.
func (a *App) MarkManyRead(ctx context.Context, ids []string) (map[string]string, error) {
	prev := map[string]string{}
	var firstErr error
	for _, id := range ids {
		e, ok := a.S.Count(id)
		if !ok || (!e.HasUnreads && e.MentionCount == 0) || e.Latest == "" {
			continue
		}
		old := e.LastRead
		if _, err := a.Mark(ctx, id, e.Latest); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		prev[id] = old
	}
	return prev, firstErr
}

// UndoMarkRead puts read cursors back (MarkManyRead's result).
func (a *App) UndoMarkRead(ctx context.Context, prev map[string]string) error {
	var firstErr error
	for id, ts := range prev {
		if ts == "" {
			ts = "0000000000.000000"
		}
		if _, err := a.Mark(ctx, id, ts); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// SectionLink is users.channelSections.createLink: a link that offers the
// section (its name, emoji and channels) to whoever opens it.
func (a *App) SectionLink(ctx context.Context, name, emoji string, channels []string) (string, error) {
	var r struct {
		URL string `json:"url"`
	}
	p := url.Values{"section_name": {name}, "emoji": {emoji}, "channels": {strings.Join(channels, ",")}}
	if err := a.C.Call(ctx, "users.channelSections.createLink", p, &r); err != nil {
		return "", err
	}
	if r.URL == "" {
		return "", errors.New("core: no link")
	}
	return r.URL, nil
}

// ValidateShareSection is users.channelSections.validateShare (Slack's
// usersChannelSectionsValidateShareFetcher, share-dialog-channel-section):
// which of channels can't be shared into target, each with its errors
// (target_user_not_in_workspace, actor_cannot_invite_member, …). Empty:
// all of them can.
func (a *App) ValidateShareSection(ctx context.Context, target string, channels []string) (map[string][]string, error) {
	var r struct {
		Errors []struct {
			Channel string `json:"channel_id"`
			Error   string `json:"error"`
		} `json:"validation_errors"`
	}
	p := url.Values{"channels": {strings.Join(channels, ",")}, "target_channel": {target}}
	if err := a.C.Call(ctx, "users.channelSections.validateShare", p, &r); err != nil {
		return nil, err
	}
	out := map[string][]string{}
	for _, e := range r.Errors {
		if e.Channel != "" && !slices.Contains(out[e.Channel], e.Error) {
			out[e.Channel] = append(out[e.Channel], e.Error)
		}
	}
	return out, nil
}

// ShareSection is Slack's shareChannelSection: a message into ch with
// the note (text, as rich_text) and the section as an attachment block
// {type: channel_section, channel_set_name, channel_ids, emoji}. It
// returns the sent message.
func (a *App) ShareSection(ctx context.Context, ch, name, emoji string, channels []string, note string) (Message, error) {
	att, err := json.Marshal([]map[string]any{{"blocks": []map[string]any{{
		"type": "channel_section", "channel_set_name": name, "channel_ids": channels, "emoji": emoji}}}})
	if err != nil {
		return Message{}, err
	}
	o := SendOpts{Channel: ch, Text: note, Attachments: att}
	if note != "" {
		o.Blocks, _ = json.Marshal([]map[string]any{{"type": "rich_text", "elements": []map[string]any{{
			"type": "rich_text_section", "elements": []map[string]any{{"type": "text", "text": note}}}}}})
	}
	return a.Send(ctx, o)
}
