package huddleui

import (
	"context"
	"log"
	"strings"
	"time"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/huddle"
	"github.com/some-du6e/clack/internal/ui/popups"
)

// Huddle topics, from gantry-v2-async-client-boot-shared a+g1
// (HuddleEditTopicMenuItem, HuddleTopicDialog), H1A2 (the topic input)
// and client-boot setHuddleTopic / rky3 renameCall (build 4d0c78085232):
//
//   - the ⋯ menu's "Add topic", or "Edit topic" once the huddle has one,
//     after Copy huddle link / Go to… (not in a standalone huddle's own
//     channel, is_huddle_channel, which Clack doesn't have);
//   - HuddleTopicDialog: a DialogModal titled "Add a topic" / "Edit
//     topic", Cancel and "Add" / "Save", around one input (the current
//     topic, placeholder "What is this huddle about?", at most 250
//     characters, focused, Enter submits);
//   - submitting is setHuddleTopic: screenhero.rooms.rename {room: the call,
//     room_name: the topic}, then the call's name; HuddleMvaWindowTitle
//     shows the topic in place of "Huddle in …".

// topicItem is HuddleEditTopicMenuItem.
func (c *Controller) topicItem(s *huddle.Session, st huddle.State, ph popups.Host) popups.MenuItem {
	label := nsHuddles.T("Add topic")
	if strings.TrimSpace(st.Name) != "" {
		label = nsHuddles.T("Edit topic")
	}
	return popups.MenuItem{Key: "huddle-edit-topic-menu-item", Label: label, Do: func() {
		if ph != nil {
			c.openTopicDialog(s, st.Name, ph)
		}
	}}
}

// openTopicDialog opens HuddleTopicDialog on ph.
func (c *Controller) openTopicDialog(s *huddle.Session, topic string, ph popups.Host) {
	var ed widget.Editor
	ed.SingleLine, ed.Submit = true, true
	ed.MaxLen = 250
	ed.SetText(topic)
	ed.SetCaret(len([]rune(topic)), len([]rune(topic)))
	title, submit := nsHuddles.T("Add a topic"), nsHuddles.T("Add")
	if topic != "" {
		title, submit = nsHuddles.T("Edit topic"), nsHuddles.T("Save")
	}
	save := func() {
		name := ed.Text()
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if err := s.Rename(ctx, name); err != nil {
				log.Printf("huddle: setHuddleTopic: %v", err)
			}
			c.changed()
		}()
	}
	var m *popups.Modal
	focused := false
	ok := &popups.ModalButton{Label: submit, Kind: popups.BtnPrimary, Do: func() bool { save(); return true }}
	m = popups.NewModal(ph, popups.ModalOpts{
		Title: title,
		Class: "p-huddle_topic_dialog",
		Body: func(gtx layout.Context) layout.Dimensions {
			if !focused { // focusOnMount
				focused = true
				gtx.Execute(key.FocusCmd{Tag: &ed})
			}
			for {
				e, ok := ed.Update(gtx)
				if !ok {
					break
				}
				if _, sub := e.(widget.SubmitEvent); sub { // onEnter
					save()
					m.Dismiss()
				}
			}
			return popups.Field(gtx, ph.Theme(), &ed, nsHuddles.T("What is this huddle about?"), "", 40, 15)
		},
		FocusStops: func() []event.Tag { return []event.Tag{&ed} },
		Buttons:    []*popups.ModalButton{{Label: nsHuddles.T("Cancel"), Kind: popups.BtnOutline}, ok},
	})
	ph.ShowOverlay(m)
	ph.Invalidate()
}
