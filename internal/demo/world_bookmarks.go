package demo

import "strconv"

// The channels' bookmarks (bookmarks.list, the tab bar's Bookmarks
// folder). Links go to made-up example.com pages; nothing is fetched.

var demoBookmarks = map[string][][2]string{ // channel -> {emoji, title}; link from the title
	GeneralID: {{":book:", "Branch handbook (v4 FINAL final)"}, {":trophy:", "Quillies nomination form"}, {":broom:", "Denise visit checklist"}},
	RandomID:  {{":cat:", "Mr. Pemberton fan club"}, {":bulb:", "Kip's startup ideas (do not invest)"}},
	SalesID:   {{":chart_with_upwards_trend:", "Q4 leaderboard (live)"}, {":page_facing_up:", "Ultra Bright 24lb price sheet"}, {":handshake:", "The Bet: official terms"}},
	PartyID:   {{":moneybag:", "Party budget ($40 + Gordon's card)"}, {":balloon:", "Theme ideas, ranked by Marisol"}},
	ITHelpID:  {{":printer:", "Prinny (PR-7) troubleshooting guide"}, {":key:", "Wi-Fi password (ask Theo)"}, {":ticket:", "Open a ticket (or just yell)"}},
	WareID:    {{":wrench:", "Big Linda maintenance log"}, {":truck:", "Loading dock schedule"}, {":rotating_light:", "Safety rules (Dale's annotated edition)"}},
	LunchID:   {{":green_book:", "The menu binder"}, {":hot_pepper:", "Gordon's chili: allergen list"}},
	KudosID:   {{":sparkles:", "Quillies 2026 categories"}, {":clap:", "How to give kudos (Ruth's guide)"}},
	SantaID:   {{":gift:", "Wishlists"}, {":scroll:", "Rules: $25 cap, no honey"}},
	CodeID:    {{":link:", "PR #217: " + DemoPRTitle}, {":package:", DemoRepo}},
}

// bookmarksFor is ch's bookmarks in bookmarks.list's shape.
func (w *World) bookmarksFor(ch string) []map[string]any {
	out := []map[string]any{}
	for i, b := range demoBookmarks[ch] {
		link := "https://quillfeather.example.com/" + bookmarkSlug(b[1])
		if ch == CodeID {
			link = DemoPRURL
			if i == 1 {
				link = "https://github.com/" + DemoRepo
			}
		}
		out = append(out, map[string]any{"id": "Bk0DEMO" + ch[len(ch)-4:] + strconv.Itoa(i), "channel_id": ch, "title": b[1],
			"link": link, "emoji": b[0], "icon_url": "", "type": "link", "entity_id": "", "date_created": w.t0.Unix() - 86400*int64(20-i),
			"date_updated": 0, "rank": string(rune('a' + i)), "last_updated_by_user_id": "U0DEMOMARI",
			"last_updated_by_team_id": TeamID, "shortcut_id": "", "app_id": "", "parent_id": ""})
	}
	return out
}

func bookmarkSlug(s string) string {
	b := make([]byte, 0, len(s))
	dash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b, dash = append(b, byte(r)), false
		case r >= 'A' && r <= 'Z':
			b, dash = append(b, byte(r-'A'+'a')), false
		default:
			if !dash && len(b) > 0 {
				b, dash = append(b, '-'), true
			}
		}
	}
	for len(b) > 0 && b[len(b)-1] == '-' {
		b = b[:len(b)-1]
	}
	return string(b)
}
