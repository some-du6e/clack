package popups

import "strings"

// LatestConversation is the conversation opened last, as Slack's
// getLatestChannel (gantry boot-render wv6G PA) reads the main window's
// history: Recent places' newest conversation (pages left out), "" when
// none was opened. The Activity "In" picker lists it under "Recent".
func LatestConversation() string {
	srMu.Lock()
	defer srMu.Unlock()
	for _, id := range srRecentConvs {
		if id != "" && !strings.HasPrefix(id, "view:") {
			return id
		}
	}
	return ""
}
