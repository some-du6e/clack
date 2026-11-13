package fakeslack

// The blockkit-gallery's surfaces part: legacy attachments, message
// permalink pills and unfurl cards, an edited bot message, an app unfurl
// with blocks, a button that opens a modal; the "Deploy Bot" app DM with
// its App Home; and the fake app behind them: app shortcuts, a modal
// whose submits answer response_action errors, then push, update and
// clear; views.close; apps.home.dispatchOpenEvent publishing the home
// view; chat.slugifyUrl. All synthetic.

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"sync"

	"github.com/some-du6e/clack/internal/slack"
)

// AppDMID is the viewer's DM with Deploy Bot (App Home: Home, Messages, About).
const AppDMID = "D0BKAPPDM1"

// Gallery message timestamps (2026-11-07 11:30 UTC on).
const (
	SurfLegacyTS  = "1762515000.000100"
	SurfThumbTS   = "1762515060.000100"
	SurfButtonTS  = "1762515120.000100"
	SurfPermaTS   = "1762515180.000100"
	SurfReplyTS   = "1762515240.000100"
	SurfEditedTS  = "1762515300.000100"
	SurfAppUnfTS  = "1762515360.000100"
	surfThreadTS  = "1762515000.000100"
	surfReplyMsTS = "1762515030.000200"
)

// SurfApp is what the fake app saw (tests).
type SurfApp struct {
	mu        sync.Mutex
	submits   map[string]int
	Closes    []url.Values // views.close calls
	HomeOpens []string     // apps.home.dispatchOpenEvent types
	Executes  []url.Values // apps.actions.v2.execute calls
	Submitted []url.Values // views.submit calls
}

var (
	surfAppsMu sync.Mutex
	surfApps   = map[*Server]*SurfApp{}
)

// SurfaceApp returns the fake app's record for s.
func (s *Server) SurfaceApp() *SurfApp {
	surfAppsMu.Lock()
	defer surfAppsMu.Unlock()
	a := surfApps[s]
	if a == nil {
		a = &SurfApp{submits: map[string]int{}}
		surfApps[s] = a
	}
	return a
}

// Snapshot copies the record.
func (a *SurfApp) Snapshot() (closes, executes, submitted []url.Values, homeOpens []string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]url.Values(nil), a.Closes...), append([]url.Values(nil), a.Executes...),
		append([]url.Values(nil), a.Submitted...), append([]string(nil), a.HomeOpens...)
}

func init() {
	registerGallery("surfaces", surfMessages)
	registerGalleryConv(func(s *Server) (slack.Conversation, func(*Server) []slack.Message) {
		return slack.Conversation{ID: AppDMID, IsIM: true, User: AppUserID, IsOpen: true, Created: 1762500000, Priority: 0.9},
			func(s *Server) []slack.Message {
				return []slack.Message{surfBot(s, "1762515400.000100", "Hi! I ship builds. Try the Home tab.", nil, nil)}
			}
	})
	registerGalleryAPI(surfAPI)
}

func pt(s string) map[string]any {
	return map[string]any{"type": "plain_text", "text": s, "emoji": true}
}
func md(s string) map[string]any { return map[string]any{"type": "mrkdwn", "text": s} }

func rawJSON(v any) json.RawMessage { b, _ := json.Marshal(v); return b }

func (s *Server) imgURL(w, h int, seed string) string {
	return s.URL + "img/" + fmt.Sprint(w) + "x" + fmt.Sprint(h) + "/" + seed + ".png"
}

func surfBot(s *Server, ts, text string, blocks any, atts []slack.Attachment) slack.Message {
	m := slack.Message{Type: "message", Subtype: "bot_message", TS: ts, BotID: AppBotID, Text: text, User: AppUserID,
		BotProfile: &slack.BotProfile{ID: AppBotID, Name: "Deploy Bot", AppID: AppAppID, UserID: AppUserID, TeamID: TeamID,
			Icons: slack.Icons{Image48: s.imgURL(48, 48, "app"), Image72: s.imgURL(72, 72, "app")}},
		Attachments: atts}
	if blocks != nil {
		m.Blocks = rawJSON(blocks)
	}
	return m
}

func surfPermalink(ch, ts, thread string) string {
	u := "https://fakeclub.slack.com/archives/" + ch + "/p" + strings.ReplaceAll(ts, ".", "")
	if thread != "" {
		u += "?thread_ts=" + thread + "&cid=" + ch
	}
	return u
}

func richText(els ...any) []any {
	return []any{map[string]any{"type": "rich_text", "block_id": "rt", "elements": []any{
		map[string]any{"type": "rich_text_section", "elements": els}}}}
}

func surfMessages(s *Server) []slack.Message {
	other := s.users[1]
	long := strings.Repeat("Build 4211 passed every check on the main branch, and the canary rollout is healthy across all regions. ", 9)
	legacy := []slack.Attachment{{
		ID: 1, Color: "good", Fallback: "Deploy 4211 succeeded", Pretext: "A *new deploy* finished:",
		AuthorName: "Deploy Bot", AuthorSubname: "@deploybot", AuthorLink: "https://example.com/deploybot", AuthorIcon: s.imgURL(16, 16, "app"),
		Title: "Deploy 4211 to production", TitleLink: "https://example.com/deploys/4211", Text: long,
		Fields: []slack.AttachmentField{{Title: "Environment", Value: "production", Short: true}, {Title: "Duration", Value: "4m 12s", Short: true},
			{Title: "Changes", Value: "• Faster startup\n• Fixed the login loop", Short: false}},
		ImageURL: s.imgURL(720, 360, "deploychart"), ImageWidth: 720, ImageHeight: 360,
		Footer: "Deploy Bot", FooterIcon: s.imgURL(16, 16, "app"), TS: json.RawMessage(`1762515000`), MrkdwnIn: []string{"text", "pretext"},
	}}
	thumb := []slack.Attachment{{
		ID: 1, Color: "#3AA3E3", Fallback: "Approve deploy 4212?", Title: "Approve deploy 4212?", Text: "Staging is green. Ship it?",
		ThumbURL: s.imgURL(80, 80, "thumb"), ThumbWidth: 80, ThumbHeight: 80,
		Actions: rawJSON([]map[string]any{{"id": "1", "name": "approve", "text": "Approve", "type": "button", "value": "yes", "style": "primary"},
			{"id": "2", "name": "reject", "text": "Reject", "type": "button", "value": "no", "style": "danger"}}),
	}}
	perma := surfPermalink(BlockKitGalleryID, SurfLegacyTS, "")
	card := slack.Attachment{ID: 1, IsMsgUnfurl: true, AuthorID: AppUserID, AuthorName: "Deploy Bot", AuthorSubname: "Deploy Bot",
		AuthorIcon: s.imgURL(48, 48, "app"), AuthorLink: "https://fakeclub.slack.com/team/" + AppUserID,
		ChannelID: BlockKitGalleryID, ChannelTeam: TeamID, Fallback: "[November 7th, 2026 11:30 AM] Deploy Bot: A new deploy finished",
		Footer: "Slack Conversation", FromURL: perma, OriginalURL: perma, MrkdwnIn: []string{"text"}, Text: "A *new deploy* finished",
		TS: json.RawMessage(`"` + SurfLegacyTS + `"`),
		Blocks: rawJSON(richText(map[string]any{"type": "text", "text": "A "}, map[string]any{"type": "text", "text": "new deploy", "style": map[string]bool{"bold": true}},
			map[string]any{"type": "text", "text": " finished"}))}
	replyURL := surfPermalink(BlockKitGalleryID, surfReplyMsTS, surfThreadTS)
	reply := card
	reply.ID, reply.IsReplyUnfurl, reply.FromURL, reply.OriginalURL = 1, true, replyURL, replyURL
	reply.AuthorID, reply.AuthorName, reply.AuthorIcon = other.ID, other.Name, other.Profile.Image48
	reply.TS = json.RawMessage(`"` + surfReplyMsTS + `"`)
	reply.Text, reply.Blocks = "Rolling back in 5", rawJSON(richText(map[string]any{"type": "text", "text": "Rolling back in 5"}))
	dmURL := surfPermalink(AppDMID, "1762515400.000100", "")
	dm := card
	dm.ID, dm.ChannelID, dm.FromURL, dm.OriginalURL = 2, AppDMID, dmURL, dmURL
	dm.TS = json.RawMessage(`"1762515400.000100"`)
	dm.Text, dm.Blocks = "Hi! I ship builds. Try the Home tab.", rawJSON(richText(map[string]any{"type": "text", "text": "Hi! I ship builds. Try the Home tab."}))
	appUnfurl := slack.Attachment{ID: 1, Fallback: "Build 4211", FromURL: "https://ci.example.com/builds/4211", OriginalURL: "https://ci.example.com/builds/4211",
		Blocks: rawJSON([]any{
			map[string]any{"type": "section", "block_id": "u1", "text": md("*Build 4211* · main\nAll 1,204 tests passed"),
				"accessory": map[string]any{"type": "image", "image_url": s.imgURL(64, 64, "ci"), "alt_text": "CI"}},
			map[string]any{"type": "context", "block_id": "u2", "elements": []any{md("Triggered by <@" + other.ID + "> · 4m 12s")}},
		})}
	edited := surfBot(s, SurfEditedTS, "Deploy 4213 finished in 3m 58s", richText(map[string]any{"type": "text", "text": "Deploy 4213 finished in 3m 58s"}), nil)
	edited.Edited = &slack.Edited{User: AppBotID, TS: "1762515330.000000"}
	return []slack.Message{
		surfBot(s, SurfLegacyTS, "A new deploy finished", nil, legacy),
		surfBot(s, SurfThumbTS, "", nil, thumb),
		surfBot(s, SurfButtonTS, "This message contains interactive elements.", []any{
			map[string]any{"type": "section", "block_id": "s1", "text": md("Need something? File a ticket with Deploy Bot."),
				"accessory": map[string]any{"type": "button", "action_id": "surf_open_modal", "text": pt("Open a modal"), "value": "ticket", "style": "primary"}},
		}, nil),
		{Type: "message", TS: SurfPermaTS, User: other.ID, Text: "see <" + perma + ">",
			Blocks: rawJSON(richText(map[string]any{"type": "text", "text": "see "},
				map[string]any{"type": "message_mention", "message_ts": SurfLegacyTS, "channel_id": BlockKitGalleryID, "text": perma, "url": perma, "author_id": AppUserID})),
			Attachments: []slack.Attachment{card}},
		{Type: "message", TS: SurfReplyTS, User: other.ID, Text: "<" + replyURL + "> and <" + dmURL + ">",
			Attachments: []slack.Attachment{reply, dm}},
		edited,
		{Type: "message", TS: SurfAppUnfTS, User: other.ID, Text: "<https://ci.example.com/builds/4211>", Attachments: []slack.Attachment{appUnfurl}},
	}
}

// ---- the fake app ----

func surfView(id, root, prev, title, submit, closeTxt, hash string, blocks []any) map[string]any {
	v := map[string]any{"id": id, "type": "modal", "root_view_id": root, "previous_view_id": prev, "app_id": AppAppID, "bot_id": AppBotID,
		"team_id": TeamID, "callback_id": "ticket", "private_metadata": "ticket:42", "hash": hash, "notify_on_close": true,
		"title": pt(title), "blocks": blocks, "clear_on_close": false, "state": map[string]any{"values": map[string]any{}}}
	if submit != "" {
		v["submit"] = pt(submit)
	}
	if closeTxt != "" {
		v["close"] = pt(closeTxt)
	}
	if prev == "" {
		v["previous_view_id"] = nil
	}
	return v
}

// SurfTicketView is the modal the shortcut and the button open.
const SurfTicketView = "VSURF00001"

func surfTicket() map[string]any {
	return surfView(SurfTicketView, SurfTicketView, "", "New ticket", "Next", "Cancel", "h1", []any{
		map[string]any{"type": "section", "block_id": "intro", "text": md("Tell *Deploy Bot* what broke.")},
		map[string]any{"type": "input", "block_id": "title", "optional": true, "label": pt("Title"),
			"element": map[string]any{"type": "plain_text_input", "action_id": "v", "placeholder": pt("Deploy stuck on canary")}},
		map[string]any{"type": "input", "block_id": "pri", "optional": true, "label": pt("Priority"),
			"hint": pt("P1 pages the on-call engineer."),
			"element": map[string]any{"type": "static_select", "action_id": "v", "placeholder": pt("Pick one"),
				"options": []any{map[string]any{"text": pt("P1"), "value": "p1"}, map[string]any{"text": pt("P2"), "value": "p2"}}}},
	})
}

// SurfNoteView is a settings modal shaped like the one Karim couldn't edit
// (2026-11-10): an italic context line naming the channel, a divider, a
// context line with an icon, and one multiline plain_text_input with an
// initial_value, submit "Edit" and close "Cancel". Global shortcut SURFA3.
const SurfNoteView = "VSURF00003"

// SurfNoteInitial is the note field's initial_value.
const SurfNoteInitial = "pinned test"

// SurfNote is that view (also the blockkit package's fixture).
func SurfNote(s *Server) map[string]any {
	return surfView(SurfNoteView, SurfNoteView, "", "Note settings", "Edit", "Cancel", "n1", []any{
		map[string]any{"type": "context", "block_id": "where", "elements": []any{md("_You are editing the pinned note for <#" + GalleryChannelID + ">._")}},
		map[string]any{"type": "divider", "block_id": "div"},
		map[string]any{"type": "context", "block_id": "state", "elements": []any{
			map[string]any{"type": "image", "image_url": s.imgURL(32, 32, "note"), "alt_text": "note"},
			md("Notes are on for this channel!"),
		}},
		map[string]any{"type": "input", "block_id": "note", "label": pt("Pinned note content"),
			"element": map[string]any{"type": "plain_text_input", "action_id": "v", "multiline": true, "initial_value": SurfNoteInitial}},
	})
}

// SurfInputsView has one input block of every text element, each with an
// initial value (global shortcut SURFA4).
const SurfInputsView = "VSURF00004"

// SurfInputs is that view: block ids are the element types' short names.
func SurfInputs() map[string]any {
	in := func(bid, label string, el map[string]any) map[string]any {
		el["action_id"] = "v"
		return map[string]any{"type": "input", "block_id": bid, "optional": true, "label": pt(label), "element": el}
	}
	rich := map[string]any{"type": "rich_text", "elements": []any{
		map[string]any{"type": "rich_text_section", "elements": []any{map[string]any{"type": "text", "text": "rich start"}}},
	}}
	return surfView(SurfInputsView, SurfInputsView, "", "Every field", "Save", "Cancel", "i1", []any{
		in("single", "Single line", map[string]any{"type": "plain_text_input", "initial_value": "one line"}),
		in("multi", "Multiline", map[string]any{"type": "plain_text_input", "multiline": true, "initial_value": "two\nlines"}),
		in("number", "Number", map[string]any{"type": "number_input", "is_decimal_allowed": false, "initial_value": "4242"}),
		in("email", "Email", map[string]any{"type": "email_text_input", "initial_value": "ada@example.com"}),
		in("url", "URL", map[string]any{"type": "url_text_input", "initial_value": "https://example.com/path"}),
		in("rich", "Rich text", map[string]any{"type": "rich_text_input", "initial_value": rich}),
	})
}

// SurfQuickCheckView is a person check modal like Karim's 2026-11-13 one
// (synthetic wording): one rich_text_input whose label asks for a
// formatted word.
const SurfQuickCheckView = "VSURF00005"

// SurfQuickCheck is that view; the input's block id is "check".
func SurfQuickCheck() map[string]any {
	return surfView(SurfQuickCheckView, SurfQuickCheckView, "", "Quick check", "Submit", "Cancel", "q1", []any{
		map[string]any{"type": "section", "block_id": "intro", "text": md("Fill in each box the way its label says.")},
		map[string]any{"type": "input", "block_id": "check", "label": pt("Type the word \"mango\" and strike it through"),
			"element": map[string]any{"type": "rich_text_input", "action_id": "v"}},
	})
}

func surfConfirm(updated bool) map[string]any {
	if updated {
		return surfView("VSURF00002", SurfTicketView, SurfTicketView, "Ticket created", "Finish", "Back", "h3", []any{
			map[string]any{"type": "section", "block_id": "done", "text": md(":white_check_mark: Created *TCK-42* (private_metadata `ticket:42` came back)")},
		})
	}
	return surfView("VSURF00002", SurfTicketView, SurfTicketView, "Confirm", "Create", "Back", "h2", []any{
		map[string]any{"type": "section", "block_id": "sum", "text": md("Create *TCK-42*?")},
		map[string]any{"type": "context", "block_id": "ctx", "elements": []any{md("Pushed onto the stack by response_action push")}},
	})
}

func surfHome(n int) map[string]any {
	return map[string]any{"id": "VSURFHOME1", "type": "home", "app_id": AppAppID, "bot_id": AppBotID, "team_id": TeamID, "hash": fmt.Sprint("home", n),
		"blocks": []any{
			map[string]any{"type": "header", "block_id": "h", "text": pt("Deploy Bot")},
			map[string]any{"type": "section", "block_id": "s", "text": md(fmt.Sprintf("Welcome home. You opened this tab *%d* time(s).", n)),
				"accessory": map[string]any{"type": "button", "action_id": "surf_open_modal", "text": pt("File a ticket")}},
			map[string]any{"type": "divider", "block_id": "d"},
			map[string]any{"type": "context", "block_id": "c", "elements": []any{md("Published with views.publish")}},
		}}
}

func (s *Server) openSurfView(tok string, v map[string]any) {
	s.emit(map[string]any{"type": "view_opened", "view_id": v["id"], "view_type": "modal", "client_token": tok,
		"timeout_range": 10000, "app_id": AppAppID, "view": v})
}

var surfPermaRe = regexp.MustCompile(`/archives/([CDG][A-Z0-9]+)(?:/p(\d{10})(\d{6}))?`)

func surfAPI(s *Server, method string, f url.Values) (any, bool) {
	app := s.SurfaceApp()
	switch method {
	case "conversations.info":
		if f.Get("channel") != AppDMID {
			return nil, false
		}
		c, _ := s.conv(AppDMID)
		out := ok("channel", c)
		if f.Get("return_app_home") == "true" {
			out["app_home"] = map[string]any{"app_id": AppAppID, "app_installed_team_id": TeamID, "conversation_id": AppDMID,
				"home_tab_enabled": true, "messages_tab_enabled": true, "messages_tab_read_only_enabled": false, "home_view_id": "VSURFHOME1"}
			out["home_view"] = surfHome(0)
		}
		return out, true
	case "apps.home.dispatchOpenEvent":
		if f.Get("id") != AppDMID {
			return nil, false
		}
		app.mu.Lock()
		app.HomeOpens = append(app.HomeOpens, f.Get("type"))
		n := 0
		for _, t := range app.HomeOpens {
			if t == "home" {
				n++
			}
		}
		app.mu.Unlock()
		if f.Get("type") == "home" { // the app answers app_home_opened with views.publish
			s.emit(map[string]any{"type": "view_opened", "view_id": "VSURFHOME1", "view_type": "home", "channel_id": AppDMID, "view": surfHome(n)})
		}
		return ok(), true
	case "apps.actions.v2.list":
		return ok("app_actions", []any{map[string]any{"app_id": AppAppID, "app_name": "Deploy Bot", "team_id": TeamID,
			"icons": map[string]any{"image_32": s.imgURL(32, 32, "app"), "image_64": s.imgURL(64, 64, "app")},
			"actions": []any{
				map[string]any{"action_id": "SURFA1", "app_id": AppAppID, "name": "File a ticket", "desc": "Opens the ticket form", "type": "global_action", "callback_id": "ticket"},
				map[string]any{"action_id": "SURFA2", "app_id": AppAppID, "name": "Ticket from message", "desc": "Files this message", "type": "message_action", "callback_id": "ticket_msg"},
			}}}), true
	case "apps.actions.v2.execute":
		if f.Get("app_id") != AppAppID {
			return nil, false
		}
		app.mu.Lock()
		app.Executes = append(app.Executes, f)
		app.mu.Unlock()
		switch f.Get("action_id") {
		case "SURFA3":
			// Slack follows view_opened with view_updated carrying the
			// same view (core/interactive.go)
			v := SurfNote(s)
			s.openSurfView(f.Get("client_token"), v)
			s.emit(map[string]any{"type": "view_updated", "view_id": SurfNoteView, "app_id": AppAppID, "view": v})
		case "SURFA4":
			v := SurfInputs()
			s.openSurfView(f.Get("client_token"), v)
			s.emit(map[string]any{"type": "view_updated", "view_id": SurfInputsView, "app_id": AppAppID, "view": v})
		default:
			s.openSurfView(f.Get("client_token"), surfTicket())
		}
		return ok(), true
	case "blocks.actions":
		if !strings.Contains(f.Get("actions"), `"surf_open_modal"`) {
			return nil, false
		}
		s.openSurfView(f.Get("client_token"), surfTicket())
		return ok(), true
	case "views.submit":
		id := f.Get("view_id")
		if !strings.HasPrefix(id, "VSURF") {
			return nil, false
		}
		app.mu.Lock()
		app.submits[id]++
		n := app.submits[id]
		app.Submitted = append(app.Submitted, f)
		app.mu.Unlock()
		switch {
		case id == SurfNoteView || id == SurfInputsView:
			return ok(), true // no response_action: the modal closes
		case id == SurfTicketView && n == 1:
			return map[string]any{"ok": false, "error": "validation_failed",
				"errors": map[string]string{"title": "Give the ticket a title of at least 3 characters"}}, true
		case id == SurfTicketView:
			return ok("response_action", "push", "view", surfConfirm(false)), true
		case n == 1:
			return ok("response_action", "update", "view", surfConfirm(true)), true
		}
		return ok("response_action", "clear", "toast_message", "Ticket TCK-42 created"), true
	case "views.close":
		if !strings.HasPrefix(f.Get("view_id"), "VSURF") {
			return nil, false
		}
		app.mu.Lock()
		app.Closes = append(app.Closes, f)
		app.mu.Unlock()
		return ok(), true
	case "chat.slugifyUrl":
		var urls []string
		_ = json.Unmarshal([]byte(f.Get("urls")), &urls)
		slugs := map[string]any{}
		for _, u := range urls {
			m := surfPermaRe.FindStringSubmatch(u)
			if m == nil {
				continue
			}
			if m[2] == "" {
				if c, found := s.conv(m[1]); found {
					slugs[u] = map[string]any{"slug_type": "channel", "id": c.ID, "name": c.Name, "channel": c}
				}
				continue
			}
			ts := m[2] + "." + m[3]
			author := s.users[1].ID
			if hs, gallery := s.galleryHistoryOf(m[1]); gallery {
				for _, h := range hs {
					if h.TS == ts {
						author = h.User
					}
				}
			}
			slugs[u] = map[string]any{"slug_type": "message", "author_id": author, "channel_id": m[1], "ts": ts,
				"message": map[string]any{"type": "message", "ts": ts, "user": author, "text": ""}}
		}
		return ok("slugs", slugs), true
	}
	return nil, false
}
