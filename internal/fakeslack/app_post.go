package fakeslack

import "github.com/some-du6e/clack/internal/slack"

// PostAppQuiet adds a message an app posted with its bot token to ch's
// history (newest), as Slack stores one: bot_id, app_id and bot_profile,
// no user and no subtype (Owl's confessions in Hack Club's #meta, which
// Karim saw missing from an open channel on α.102), and returns its ts
// and its realtime message event, unpushed.
func (s *Server) PostAppQuiet(ch, botID, appID, name, text string) (ts string, ev map[string]any) {
	ts, ev = s.PostQuiet(ch, "", text)
	bp := slack.BotProfile{ID: botID, Name: name, AppID: appID, TeamID: TeamID}
	s.histMu.Lock()
	if h := s.hist[ch]; len(h) > 0 && h[0].TS == ts {
		h[0].BotID, h[0].AppID, h[0].BotProfile, h[0].ClientMsgID = botID, appID, &bp, ""
	}
	s.histMu.Unlock()
	delete(ev, "user")
	delete(ev, "client_msg_id")
	ev["bot_id"], ev["app_id"] = botID, appID
	ev["bot_profile"] = map[string]any{"id": botID, "name": name, "app_id": appID, "team_id": TeamID, "deleted": false,
		"icons": map[string]any{"image_36": s.URL + "img/36x36/" + botID + ".png", "image_48": s.URL + "img/48x48/" + botID + ".png", "image_72": s.URL + "img/72x72/" + botID + ".png"}}
	return ts, ev
}
