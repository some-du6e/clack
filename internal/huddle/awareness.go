package huddle

import (
	"time"
)

// Huddle awareness: the per-person state everyone in a huddle keeps in
// sync over the "awareness-sync-channel" data topic (client-boot's
// AwarenessManager, ChimeChannelBroadcaster and ChimeChannelListener,
// bv1-13):
//
//   - each person broadcasts all of their own entries as one
//     AwarenessBroadcast{channel: the room (call) id, user, awarenessState}
//     when one changes, every 30 s, and when someone asks;
//   - joining sends an AwarenessQuery for the room (everyone answers with
//     a broadcast); leaving sends AwarenessClose (everyone drops yours);
//   - a broadcast replaces that person's entries; an entry nobody renewed
//     for 60 s expires (pRY9's hL).
//
// The entry the huddle draws is "stickyEmoji": a sticker on your tile
// (HuddlePeerTile's p-huddle_peer_tile__sticky_emoji_container). Raise
// hand is the raised_hand sticker (with your skin tone); Slack's other
// stickers are coffee (Be right back), speaker (Audio-only), headphones
// (DJ'ing), +1 (Yes), -1 (No), white_check_mark (Done) and iphone (On
// mobile) (eAbZ). sendStickyReaction sets it, or removes it when it's
// already the one you have on.

// AwarenessSticky is the sticker's awareness field.
const AwarenessSticky = "stickyEmoji"

const (
	awarenessTTL    = 60 * time.Second // pRY9.hL
	awarenessPeriod = 30 * time.Second // broadcastPerodicallyToWatchedTopics
)

// Sticker is your sticker ("" = none).
func (s *Session) Sticker() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sticker
}

// SetSticker puts emoji on your tile for everyone ("" takes it off):
// Slack's sendStickyReaction, an awareness broadcast with stickyEmoji.
// Like Slack's sticky reducer it stamps the entry with the time
// (fieldTimestampMs: Date.now()); the stamp goes out only under
// huddles_awareness_timestamp.
func (s *Session) SetSticker(emoji string) error {
	now := time.Now()
	s.mu.Lock()
	s.sticker = emoji
	s.stickerTS = 0
	if emoji != "" {
		s.stickerTS = now.UnixMilli()
	}
	if p := s.people[s.o.SelfID]; p != nil {
		p.Sticker, p.StickerAt, p.StickerTS = emoji, now, s.stickerTS
	}
	s.mu.Unlock()
	s.changed()
	return s.broadcastAwareness()
}

// SetExperiments changes the experiments the session follows (the
// Experiments plugins flip them live): the next broadcast carries
// timestamps or not, the next message is keyed per device or per person.
func (s *Session) SetExperiments(e Experiments) {
	s.mu.Lock()
	changed := s.exp != e
	s.exp = e
	s.mu.Unlock()
	if changed {
		s.changed()
	}
}

// ToggleSticker is a click on a sticker: on, or off when it's the one you
// have on (sendStickyReaction's field_value === emoji check).
func (s *Session) ToggleSticker(emoji string) error {
	if s.Sticker() == emoji {
		return s.SetSticker("")
	}
	return s.SetSticker(emoji)
}

// awarenessState is your entries as Slack broadcasts them: with
// huddles_awareness_timestamp, each with its field_timestamp_ms.
func (s *Session) awarenessState() []AwarenessPayload {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sticker == "" {
		return nil
	}
	e := AwarenessPayload{FieldName: AwarenessSticky, FieldValue: s.sticker}
	if s.exp.AwarenessTimestamp && s.stickerTS != 0 {
		ts := s.stickerTS
		e.FieldTimestampMS = &ts
	}
	return []AwarenessPayload{e}
}

// sendAware sends an awareness message as Slack's broadcaster does (no
// fromPeerIdString: the topic's messages carry the user in the body).
func (s *Session) sendAware(m *PeerMessage) error {
	b := m.Encode()
	return s.send(encodeData(dataMessage{Topic: TopicAwareness, Data: b}))
}

// broadcastAwareness is maybeSendCurrentAwarenessState: all your entries.
func (s *Session) broadcastAwareness() error {
	s.mu.Lock()
	up := s.phase == Connected && !s.ended
	s.mu.Unlock()
	if !up {
		return nil // sent once connected (awarenessOnConnect)
	}
	return s.sendAware(&PeerMessage{AwarenessBroadcast: &Awareness{Channel: s.join.RoomID, User: s.o.SelfID, State: s.awarenessState()}})
}

// awarenessOnConnect is watch(): query everyone, broadcast yours, and
// keep broadcasting every 30 s while connected.
func (s *Session) awarenessOnConnect(done <-chan struct{}) {
	_ = s.sendAware(&PeerMessage{AwarenessQuery: &Awareness{Channel: s.join.RoomID, User: s.o.SelfID}})
	_ = s.broadcastAwareness()
	t := time.NewTicker(awarenessPeriod)
	defer t.Stop()
	for {
		select {
		case <-done:
			return
		case <-t.C:
			_ = s.broadcastAwareness()
		}
	}
}

// awarenessClose is onShutdownDataChannelSubscriber's close.
func (s *Session) awarenessClose() {
	_ = s.sendAware(&PeerMessage{AwarenessClose: &Awareness{Channel: s.join.RoomID, User: s.o.SelfID}})
}

// onAwareness applies someone's awareness message (ChimeChannelListener).
// Under huddles_multi_device a person's entries are kept per device (the
// sender's attendee: uniqueId), so each of their tiles shows its own
// sticker; otherwise per person, and a broadcast from either device
// replaces what both of their tiles show.
func (s *Session) onAwareness(pm *PeerMessage, senderAtt string) {
	switch {
	case pm.AwarenessQuery != nil && pm.AwarenessQuery.Channel == s.join.RoomID:
		go func() { _ = s.broadcastAwareness() }()
	case pm.AwarenessBroadcast != nil:
		a := pm.AwarenessBroadcast
		if a.User == "" || a.Channel != s.join.RoomID || a.User == s.o.SelfID {
			return
		}
		sticker, ts := "", int64(0)
		for _, e := range a.State {
			if e.FieldName == AwarenessSticky {
				sticker = e.FieldValue
				if e.FieldTimestampMS != nil {
					ts = *e.FieldTimestampMS
				}
			}
		}
		now := time.Now()
		s.mu.Lock()
		key := s.keyFor(senderAtt, a.User)
		if key == "" {
			key = a.User
		}
		p := s.personKey(key, a.User)
		p.Sticker, p.StickerAt, p.StickerTS = sticker, now, ts
		if !s.exp.MultiDevice { // one set of entries per person
			for _, q := range s.people {
				if q.UserID == a.User {
					q.Sticker, q.StickerAt, q.StickerTS = sticker, now, ts
				}
			}
		}
		s.mu.Unlock()
		s.changed()
	case pm.AwarenessClose != nil:
		a := pm.AwarenessClose
		if a.User == "" || a.Channel != s.join.RoomID || a.User == s.o.SelfID {
			return
		}
		s.mu.Lock()
		multi := s.exp.MultiDevice
		for _, p := range s.people {
			if p.UserID == a.User && (!multi || senderAtt == "" || p.AttendeeID == "" || p.AttendeeID == senderAtt) {
				p.Sticker, p.StickerAt, p.StickerTS = "", time.Time{}, 0
			}
		}
		s.mu.Unlock()
		s.changed()
	}
}

// stickerAt is p's sticker at now: someone else's lapses 60 s after their
// last broadcast.
func stickerAt(p Participant, now time.Time) string {
	if p.Self || p.Sticker == "" {
		return p.Sticker
	}
	if now.Sub(p.StickerAt) >= awarenessTTL {
		return ""
	}
	return p.Sticker
}
