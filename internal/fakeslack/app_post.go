package fakeslack

import "github.com/some-du6e/clack/internal/slack"

// PostAppQuiet adds a message an app posted with its bot token to ch's
// history (newest), as Slack stores one: bot_id, app_id and bot_profile,
// no user and no subtype (Owl's confessions in Hack Club's #meta, which
// Karim saw missing from an open channel on α.102), and returns its ts
// and its realtime message event, unpushed.
func (s *Server) PostAppQuiet(ch, botID, appID, name, text string) (ts string, ev map[string]any) {
	ts, ev = s.PostQuiet(ch, "", text)
	icon := func(px string) string { return s.URL + "img/" + px + "x" + px + "/" + botID + ".png" }
	bp := slack.BotProfile{ID: botID, Name: name, AppID: appID, TeamID: TeamID,
		Icons: slack.Icons{Image36: icon("36"), Image48: icon("48"), Image72: icon("72")}}
	s.histMu.Lock()
	if h := s.hist[ch]; len(h) > 0 && h[0].TS == ts {
		h[0].BotID, h[0].AppID, h[0].BotProfile, h[0].ClientMsgID = botID, appID, &bp, ""
	}
	s.histMu.Unlock()
	delete(ev, "user")
	delete(ev, "client_msg_id")
	ev["bot_id"], ev["app_id"] = botID, appID
	ev["bot_profile"] = map[string]any{"id": botID, "name": name, "app_id": appID, "team_id": TeamID, "deleted": false,
		"icons": map[string]any{"image_36": icon("36"), "image_48": icon("48"), "image_72": icon("72")}}
	return ts, ev
}

// EditQuiet changes the text of ts in ch's history with no event, as an
// edit the socket missed. The counts say nothing of it unless
// SetCountMutations moves ch's updated.
func (s *Server) EditQuiet(ch, ts, text string) {
	s.histMu.Lock()
	defer s.histMu.Unlock()
	for i, m := range s.hist[ch] {
		if m.TS == ts {
			s.hist[ch][i].Text = text
			s.hist[ch][i].Blocks = nil
			s.hist[ch][i].Edited = &slack.Edited{User: m.User, TS: ts}
			return
		}
	}
}
