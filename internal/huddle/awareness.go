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
func (s *Session) SetSticker(emoji string) error {
	s.mu.Lock()
	s.sticker = emoji
	if p := s.people[s.o.SelfID]; p != nil {
		p.Sticker, p.StickerAt = emoji, time.Now()
	}
	s.mu.Unlock()
	s.changed()
	return s.broadcastAwareness()
}

// ToggleSticker is a click on a sticker: on, or off when it's the one you
// have on (sendStickyReaction's field_value === emoji check).
func (s *Session) ToggleSticker(emoji string) error {
	if s.Sticker() == emoji {
		return s.SetSticker("")
	}
	return s.SetSticker(emoji)
}

// awarenessState is your entries as Slack broadcasts them.
func (s *Session) awarenessState() []AwarenessPayload {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sticker == "" {
		return nil
	}
	return []AwarenessPayload{{FieldName: AwarenessSticky, FieldValue: s.sticker}}
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
func (s *Session) onAwareness(pm *PeerMessage, senderAtt string) {
	switch {
	case pm.AwarenessQuery != nil && pm.AwarenessQuery.Channel == s.join.RoomID:
		go func() { _ = s.broadcastAwareness() }()
	case pm.AwarenessBroadcast != nil:
		a := pm.AwarenessBroadcast
		if a.User == "" || a.Channel != s.join.RoomID || a.User == s.o.SelfID {
			return
		}
		sticker := ""
		for _, e := range a.State {
			if e.FieldName == AwarenessSticky {
				sticker = e.FieldValue
			}
		}
		s.mu.Lock()
		key := s.keyFor(senderAtt, a.User)
		if key == "" {
			key = a.User
		}
		p := s.personKey(key, a.User)
		p.Sticker, p.StickerAt = sticker, time.Now()
		s.mu.Unlock()
		s.changed()
	case pm.AwarenessClose != nil:
		a := pm.AwarenessClose
		if a.User == "" || a.Channel != s.join.RoomID || a.User == s.o.SelfID {
			return
		}
		s.mu.Lock()
		for _, p := range s.people {
			if p.UserID == a.User && (senderAtt == "" || p.AttendeeID == "" || p.AttendeeID == senderAtt) {
				p.Sticker, p.StickerAt = "", time.Time{}
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
