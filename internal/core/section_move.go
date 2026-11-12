package core

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
)

// Section moves as Slack's moveManyChannels (boot-shared HKiO, build
// 4d0c78085232) makes them: one users.channelSections.channels.bulkUpdate
// for custom sections, stars.add / stars.remove for Starred, and an undo
// that sends the reverse (the custom sections' insert and remove swapped,
// the stars flipped back). channelMoveCount is the number of distinct
// conversations moved. performBulkChannelMove's toast reads it
// (ui/section_menu.go).

// SectionItems is one section's conversations in a move.
type SectionItems struct {
	Section  string   `json:"channel_section_id"`
	Channels []string `json:"channel_ids"`
}

// SectionMove is what one move changed.
type SectionMove struct {
	Insert, Remove []SectionItems // custom sections (bulkUpdate's lists)
	Starred        []string       // starred by the move
	Unstarred      []string       // unstarred by the move
	InsertStars    string         // the Starred section moved into ("" for none)
	RemoveStars    string         // the Starred section moved out of
	Count          int            // channelMoveCount
}

// InsertSections and RemoveSections are the section ids moved into and
// out of (custom and Starred), for the toast's "to"/"from".
func (m SectionMove) InsertSections() []string { return m.sections(m.Insert, m.InsertStars) }

// RemoveSections: see InsertSections.
func (m SectionMove) RemoveSections() []string { return m.sections(m.Remove, m.RemoveStars) }

func (m SectionMove) sections(items []SectionItems, stars string) []string {
	var out []string
	seen := map[string]bool{}
	for _, it := range items {
		if !seen[it.Section] {
			seen[it.Section] = true
			out = append(out, it.Section)
		}
	}
	if stars != "" && !seen[stars] {
		out = append(out, stars)
	}
	return out
}

// Channels are the distinct conversations moved.
func (m SectionMove) Channels() []string {
	var out []string
	seen := map[string]bool{}
	add := func(ids []string) {
		for _, id := range ids {
			if !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	for _, it := range m.Insert {
		add(it.Channels)
	}
	for _, it := range m.Remove {
		add(it.Channels)
	}
	add(m.Starred)
	add(m.Unstarred)
	return out
}

// MoveToSectionRecord is MoveToSection, returning what moved.
func (a *App) MoveToSectionRecord(ctx context.Context, id string, insert, remove []string) (SectionMove, error) {
	var m SectionMove
	s, ok := a.rawSection(id)
	if !ok {
		return m, errors.New("core: no such section")
	}
	if s.Type == "stars" {
		for _, ch := range insert {
			if err := a.Star(ctx, ch, true); err != nil {
				return m, err
			}
			m.Starred = append(m.Starred, ch)
			m.InsertStars = id
		}
		for _, ch := range remove {
			if err := a.Star(ctx, ch, false); err != nil {
				return m, err
			}
			m.Unstarred = append(m.Unstarred, ch)
			m.RemoveStars = id
		}
		m.Count = len(m.Channels())
		a.emit(Event{Type: "sidebar_changed"})
		return m, nil
	}
	ins, rem := []SectionItems{}, []SectionItems{}
	if len(insert) > 0 {
		ins = append(ins, SectionItems{id, insert})
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
				rem = append(rem, SectionItems{o.ID, out})
			}
		}
		a.S.RUnlock()
	}
	if len(remove) > 0 {
		rem = append(rem, SectionItems{id, remove})
	}
	if err := a.bulkUpdateSections(ctx, ins, rem); err != nil {
		return m, err
	}
	m.Insert, m.Remove = ins, rem
	m.Count = len(m.Channels())
	return m, nil
}

func (a *App) bulkUpdateSections(ctx context.Context, ins, rem []SectionItems) error {
	if ins == nil {
		ins = []SectionItems{}
	}
	if rem == nil {
		rem = []SectionItems{}
	}
	bi, _ := json.Marshal(ins)
	br, _ := json.Marshal(rem)
	if err := a.C.Call(ctx, "users.channelSections.channels.bulkUpdate", url.Values{"insert": {string(bi)}, "remove": {string(br)}}, nil); err != nil {
		return err
	}
	a.refreshSections(ctx)
	return nil
}

// UndoSectionMove is moveManyChannels' undo: the custom sections' move
// reversed in one bulkUpdate, the stars put back.
func (a *App) UndoSectionMove(ctx context.Context, m SectionMove) error {
	if len(m.Insert)+len(m.Remove) > 0 {
		if err := a.bulkUpdateSections(ctx, m.Remove, m.Insert); err != nil {
			return err
		}
	}
	for _, ch := range m.Starred {
		if err := a.Star(ctx, ch, false); err != nil {
			return err
		}
	}
	for _, ch := range m.Unstarred {
		if err := a.Star(ctx, ch, true); err != nil {
			return err
		}
	}
	if len(m.Starred)+len(m.Unstarred) > 0 {
		a.emit(Event{Type: "sidebar_changed"})
	}
	return nil
}

// Merge adds another move's changes (several calls that are one move in
// Slack: the header's Move to… leaves the old section and enters the new
// one).
func (m SectionMove) Merge(o SectionMove) SectionMove {
	m.Insert = append(append([]SectionItems(nil), m.Insert...), o.Insert...)
	m.Remove = append(append([]SectionItems(nil), m.Remove...), o.Remove...)
	m.Starred = append(append([]string(nil), m.Starred...), o.Starred...)
	m.Unstarred = append(append([]string(nil), m.Unstarred...), o.Unstarred...)
	if o.InsertStars != "" {
		m.InsertStars = o.InsertStars
	}
	if o.RemoveStars != "" {
		m.RemoveStars = o.RemoveStars
	}
	m.Count = len(m.Channels())
	return m
}

// MoveMany is moveManyChannels (boot-shared HKiO) for one
// performBulkChannelMove: a conversation both taken out of and put into
// the same section moves nowhere (withoutIgnoredMovements), the custom
// sections' share goes in one users.channelSections.channels.bulkUpdate,
// and Starred's in stars.add / stars.remove per conversation. Count is
// the distinct conversations left moving. (Slack also opens a closed DM
// moved into a section; Clack's sidebar lists open DMs only, so there is
// none to open.)
func (a *App) MoveMany(ctx context.Context, insert, remove []SectionItems) (SectionMove, error) {
	removing := map[string]map[string]bool{} // channel -> sections it leaves
	for _, it := range remove {
		for _, ch := range it.Channels {
			if removing[ch] == nil {
				removing[ch] = map[string]bool{}
			}
			removing[ch][it.Section] = true
		}
	}
	ignored := map[string]map[string]bool{} // section -> channels both left and entered
	for _, it := range insert {
		for _, ch := range it.Channels {
			if removing[ch][it.Section] {
				if ignored[it.Section] == nil {
					ignored[it.Section] = map[string]bool{}
				}
				ignored[it.Section][ch] = true
			}
		}
	}
	keep := func(items []SectionItems) []SectionItems {
		var out []SectionItems
		for _, it := range items {
			var chs []string
			for _, ch := range it.Channels {
				if !ignored[it.Section][ch] {
					chs = append(chs, ch)
				}
			}
			if len(chs) > 0 {
				out = append(out, SectionItems{it.Section, chs})
			}
		}
		return out
	}
	rem, ins := keep(remove), keep(insert)
	var m SectionMove
	typeOf := map[string]string{}
	a.S.RLock()
	for _, s := range a.S.Sections {
		typeOf[s.ID] = s.Type
	}
	a.S.RUnlock()
	var stdIns, stdRem []SectionItems
	for _, it := range rem {
		switch typeOf[it.Section] {
		case "standard":
			stdRem = append(stdRem, it)
		case "stars":
			for _, ch := range it.Channels {
				if err := a.Star(ctx, ch, false); err != nil {
					return m, err
				}
				m.Unstarred = append(m.Unstarred, ch)
			}
			m.RemoveStars = it.Section
		}
	}
	for _, it := range ins {
		switch typeOf[it.Section] {
		case "standard":
			stdIns = append(stdIns, it)
		case "stars":
			for _, ch := range it.Channels {
				if err := a.Star(ctx, ch, true); err != nil {
					return m, err
				}
				m.Starred = append(m.Starred, ch)
			}
			m.InsertStars = it.Section
		}
	}
	if len(stdIns)+len(stdRem) > 0 {
		if err := a.bulkUpdateSections(ctx, stdIns, stdRem); err != nil {
			return m, err
		}
		m.Insert, m.Remove = stdIns, stdRem
	}
	if len(m.Starred)+len(m.Unstarred) > 0 {
		a.emit(Event{Type: "sidebar_changed"})
	}
	m.Count = len(m.Channels())
	return m, nil
}
