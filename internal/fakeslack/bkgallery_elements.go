package fakeslack

import (
	"encoding/json"
	"net/url"
	"strings"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// Interactive elements in the Block Kit gallery, and the app side of
// clicking them:
//
//   - blocks.actions {service_id, app_id, service_team_id, actions,
//     container, client_token, state} is recorded (BlockActionCalls, and
//     BlockActions' raw forms for agents.go's tests) and
//     answered ok. An action_id ending "_slow" answers after 1.5 s (the
//     block's loading state), "_fail" answers ok:false error=post_error
//     with a message (the warning icon's tooltip), "_disabled" answers
//     ok:false error=action_disabled message=action_disabled (Slack's
//     inline "This action isn’t available").
//   - blocks.suggestions {service_id, container, action_id, block_id,
//     value} is recorded (Suggestions) and answers the fruits containing
//     value: as option_groups when the action_id has "groups".
//
// Synthetic only: nothing here comes from a real workspace.

// BlockActionCall is one blocks.actions request.
type BlockActionCall struct {
	Form      url.Values
	Actions   []map[string]any // the actions JSON array
	Container map[string]any
	State     map[string]any // {"values": …}, nil when not sent
}

// BlockActionCalls returns the blocks.actions calls so far, oldest
// first, decoded (BlockActions, agents.go, has the raw forms).
func (s *Server) BlockActionCalls() []BlockActionCall {
	s.bkMu.Lock()
	defer s.bkMu.Unlock()
	return append([]BlockActionCall(nil), s.bkActions...)
}

// Suggestions returns the blocks.suggestions forms so far.
func (s *Server) Suggestions() []url.Values {
	s.bkMu.Lock()
	defer s.bkMu.Unlock()
	return append([]url.Values(nil), s.bkSuggest...)
}

// GalleryFailMessage is the "_fail" answer's message.
const GalleryFailMessage = "This app responded with Status Code 500 (synthetic fixture)."

var galleryFruits = []string{"Apple", "Apricot", "Banana", "Blackberry", "Blueberry", "Cherry", "Grape", "Kiwi", "Lemon", "Mango", "Orange", "Peach", "Pear", "Plum"}

func (s *Server) handleBlockActions(method string, f url.Values) (any, bool) {
	switch method {
	case "blocks.actions":
		c := BlockActionCall{Form: f}
		_ = json.Unmarshal([]byte(f.Get("actions")), &c.Actions)
		_ = json.Unmarshal([]byte(f.Get("container")), &c.Container)
		if st := f.Get("state"); st != "" {
			_ = json.Unmarshal([]byte(st), &c.State)
		}
		s.bkMu.Lock()
		s.bkActions = append(s.bkActions, c)
		s.bkMu.Unlock()
		r := s.agentRec() // agents.go's recorder sees every call too
		r.mu.Lock()
		r.actions = append(r.actions, f)
		r.mu.Unlock()
		aid := ""
		if len(c.Actions) > 0 {
			aid, _ = c.Actions[0]["action_id"].(string)
		}
		switch {
		case strings.HasSuffix(aid, "_slow"):
			time.Sleep(1500 * time.Millisecond)
		case strings.HasSuffix(aid, "_fail"):
			return map[string]any{"ok": false, "error": "post_error", "message": GalleryFailMessage}, true
		case strings.HasSuffix(aid, "_disabled"):
			return map[string]any{"ok": false, "error": "action_disabled", "message": "action_disabled"}, true
		}
		return ok(), true
	case "blocks.suggestions":
		s.bkMu.Lock()
		s.bkSuggest = append(s.bkSuggest, f)
		s.bkMu.Unlock()
		q := strings.ToLower(f.Get("value"))
		opt := func(name string) map[string]any {
			return map[string]any{"text": map[string]any{"type": "plain_text", "text": name, "emoji": true}, "value": strings.ToLower(name)}
		}
		var opts []map[string]any
		for _, n := range galleryFruits {
			if strings.Contains(strings.ToLower(n), q) {
				opts = append(opts, opt(n))
			}
		}
		if opts == nil {
			opts = []map[string]any{}
		}
		if strings.Contains(f.Get("action_id"), "groups") {
			var berries, others []map[string]any
			for _, o := range opts {
				if strings.Contains(o["value"].(string), "berry") {
					berries = append(berries, o)
				} else {
					others = append(others, o)
				}
			}
			var gs []map[string]any
			if len(berries) > 0 {
				gs = append(gs, map[string]any{"label": map[string]any{"type": "plain_text", "text": "Berries"}, "options": berries})
			}
			if len(others) > 0 {
				gs = append(gs, map[string]any{"label": map[string]any{"type": "plain_text", "text": "Everything else"}, "options": others})
			}
			return ok("option_groups", gs), true
		}
		return ok("options", opts), true
	}
	return nil, false
}

func init() { RegisterGalleryMessages("50-elements", galleryElements) }

// GalleryBotID, GalleryAppID and GalleryBotUserID are the elements
// gallery's app: its messages carry them (bot_id, bot_profile), so
// blocks.actions gets a service_id and app_id.
const (
	GalleryBotID     = "B0BKGALLRY"
	GalleryAppID     = "A0BKGALLRY"
	GalleryBotUserID = "U0BKGALLRY"
)

// GalleryElementsTS is the elements gallery messages' ts by family name
// (GalleryElementNames).
func (s *Server) GalleryElementsTS() map[string]string {
	out := map[string]string{}
	for _, e := range s.galleryEntries() {
		if n, ok := strings.CutPrefix(e.msg.ClientMsgID, "bk-elements-"); ok {
			out[n] = e.msg.TS
		}
	}
	return out
}

// GalleryElementNames is the elements gallery, in order.
var GalleryElementNames = []string{"buttons", "overflow", "selects", "people", "multi", "pickers", "options", "inputs", "workflow", "more"}

func galleryElements(s *Server) []slack.Message {
	pt := func(t string) map[string]any { return map[string]any{"type": "plain_text", "text": t, "emoji": true} }
	md := func(t string) map[string]any { return map[string]any{"type": "mrkdwn", "text": t} }
	o := func(t, v string) map[string]any { return map[string]any{"text": pt(t), "value": v} }
	od := func(t, v, d string) map[string]any {
		return map[string]any{"text": pt(t), "value": v, "description": pt(d)}
	}
	sec := func(id, text string) map[string]any {
		return map[string]any{"type": "section", "block_id": id, "text": md(text)}
	}
	act := func(id string, els ...map[string]any) map[string]any {
		return map[string]any{"type": "actions", "block_id": id, "elements": els}
	}
	btn := func(aid, text, value string) map[string]any {
		return map[string]any{"type": "button", "action_id": aid, "text": pt(text), "value": value}
	}
	confirm := func(style string) map[string]any {
		c := map[string]any{"title": pt("Delete the build?"), "text": md("This removes *build 1234* for everyone."),
			"confirm": pt("Delete"), "deny": pt("Keep it")}
		if style != "" {
			c["style"] = style
		}
		return c
	}
	fruits := []map[string]any{o("Apple", "apple"), o("Banana", "banana"), od("Cherry", "cherry", "Sour or sweet"), o("Grape", "grape")}
	msgs := map[string][]map[string]any{
		"buttons": {
			sec("bk_btn_intro", "*Buttons* — default, primary, danger, a link, a confirm, an accessibility label, a slow and a failing app."),
			act("bk_buttons",
				btn("btn_default", "Default", "d"),
				func() map[string]any { b := btn("btn_primary", "Approve", "yes"); b["style"] = "primary"; return b }(),
				func() map[string]any { b := btn("btn_danger", "Reject", "no"); b["style"] = "danger"; return b }(),
				func() map[string]any {
					b := btn("btn_url", "Open docs", "url")
					b["url"] = "https://example.com/docs"
					return b
				}(),
				func() map[string]any {
					b := btn("btn_confirm", "Delete build", "del")
					b["style"] = "danger"
					b["confirm"] = confirm("danger")
					return b
				}(),
				func() map[string]any {
					b := btn("btn_a11y", "⋯", "a11y")
					b["accessibility_label"] = "More details about the build"
					return b
				}(),
			),
			act("bk_buttons_state", btn("btn_slow", "Slow app", "s"), btn("btn_fail", "Broken app", "f"), btn("btn_disabled", "Disabled action", "x")),
		},
		"overflow": {
			{"type": "section", "block_id": "bk_overflow_sec", "text": md("*Overflow menu* as a section accessory"),
				"accessory": map[string]any{"type": "overflow", "action_id": "overflow_acc", "options": []map[string]any{
					o("Edit", "edit"), o("Duplicate", "dup"), od("Archive", "archive", "Hides it from the list"),
					{"text": pt("View on the web"), "value": "web", "url": "https://example.com/build/1234"}, o("Delete", "delete")}}},
			act("bk_overflow_act", map[string]any{"type": "overflow", "action_id": "overflow_confirm", "confirm": confirm(""),
				"options": []map[string]any{o("Restart", "restart"), o("Stop", "stop")}}),
		},
		"selects": {
			sec("bk_sel_intro", "*Selects* — static (option groups, initial option) and external (min_query_length 0 and 3)."),
			act("bk_selects",
				map[string]any{"type": "static_select", "action_id": "static_groups", "placeholder": pt("Choose a fruit"),
					"option_groups": []map[string]any{
						{"label": pt("Common"), "options": []map[string]any{o("Apple", "apple"), o("Banana", "banana")}},
						{"label": pt("Stone fruit"), "options": []map[string]any{od("Cherry", "cherry", "Sour or sweet"), o("Peach", "peach")}}}},
				map[string]any{"type": "static_select", "action_id": "static_initial", "options": fruits, "initial_option": fruits[2]},
				map[string]any{"type": "external_select", "action_id": "external_any", "placeholder": pt("Search fruit"), "min_query_length": 0},
				map[string]any{"type": "external_select", "action_id": "external_groups", "initial_option": o("Mango", "mango")},
			),
		},
		"people": {
			sec("bk_people_intro", "*People and conversations* — users_select, conversations_select (filter, default_to_current_conversation), channels_select."),
			act("bk_people",
				map[string]any{"type": "users_select", "action_id": "users_one", "placeholder": pt("Pick a person"), "initial_user": ProfileID},
				map[string]any{"type": "conversations_select", "action_id": "convs_current", "default_to_current_conversation": true,
					"filter": map[string]any{"include": []string{"public", "private"}, "exclude_bot_users": true}},
				map[string]any{"type": "channels_select", "action_id": "channels_one", "initial_channel": "C0FAKE00001", "response_url_enabled": true},
				map[string]any{"type": "users_select", "action_id": "users_empty"},
			),
		},
		"multi": {
			sec("bk_multi_intro", "*Multi-selects* — static (max 2), external, users, conversations, channels."),
			act("bk_multi",
				map[string]any{"type": "multi_static_select", "action_id": "multi_static", "options": fruits, "max_selected_items": 2,
					"initial_options": []map[string]any{fruits[0]}},
				map[string]any{"type": "multi_external_select", "action_id": "multi_external", "placeholder": pt("Fruit basket"), "min_query_length": 1},
				map[string]any{"type": "multi_users_select", "action_id": "multi_users", "initial_users": []string{ProfileID, AdminID}},
				map[string]any{"type": "multi_conversations_select", "action_id": "multi_convs", "placeholder": pt("Where to post")},
				map[string]any{"type": "multi_channels_select", "action_id": "multi_channels", "initial_channels": []string{"C0FAKE00002"}},
			),
		},
		"pickers": {
			sec("bk_pick_intro", "*Pickers* — datepicker, timepicker (with a time zone) and datetimepicker."),
			act("bk_pickers",
				map[string]any{"type": "datepicker", "action_id": "date_one", "initial_date": "2026-11-20", "placeholder": pt("Pick a date")},
				map[string]any{"type": "timepicker", "action_id": "time_one", "initial_time": "13:30", "timezone": "Europe/Berlin"},
				map[string]any{"type": "datetimepicker", "action_id": "datetime_one", "initial_date_time": 1795000000},
				map[string]any{"type": "datepicker", "action_id": "date_empty"},
			),
		},
		"options": {
			{"type": "section", "block_id": "bk_checks", "text": md("*Checkboxes* with descriptions and an initial option"),
				"accessory": map[string]any{"type": "checkboxes", "action_id": "checks", "options": []map[string]any{
					{"text": md("*Email* me"), "value": "email", "description": md("A digest every _morning_")},
					od("Push", "push", "Right away"), o("Nothing", "none")},
					"initial_options": []map[string]any{{"text": md("*Email* me"), "value": "email", "description": md("A digest every _morning_")}}}},
			act("bk_radios", map[string]any{"type": "radio_buttons", "action_id": "radios", "options": []map[string]any{
				od("Daily", "daily", "Every weekday at 9"), o("Weekly", "weekly"), o("Never", "never")}, "initial_option": o("Weekly", "weekly")}),
		},
		"inputs": {
			sec("bk_inputs_intro", "*Inputs* in a message — dispatch_action on Enter and on every character."),
			{"type": "input", "block_id": "bk_in_enter", "dispatch_action": true, "label": pt("Ticket title"),
				"element": map[string]any{"type": "plain_text_input", "action_id": "in_enter", "placeholder": pt("What broke?"),
					"min_length": 3, "max_length": 80, "dispatch_action_config": map[string]any{"trigger_actions_on": []string{"on_enter_pressed"}}}},
			{"type": "input", "block_id": "bk_in_chars", "dispatch_action": true, "label": pt("Live search"),
				"element": map[string]any{"type": "plain_text_input", "action_id": "in_chars",
					"dispatch_action_config": map[string]any{"trigger_actions_on": []string{"on_character_entered"}}}},
			{"type": "input", "block_id": "bk_in_number", "dispatch_action": true, "label": pt("Amount"),
				"element": map[string]any{"type": "number_input", "action_id": "in_number", "is_decimal_allowed": true, "min_value": "0", "max_value": "100"}},
			{"type": "input", "block_id": "bk_in_email", "label": pt("Email"), "element": map[string]any{"type": "email_text_input", "action_id": "in_email"}},
			{"type": "input", "block_id": "bk_in_url", "label": pt("Link"), "element": map[string]any{"type": "url_text_input", "action_id": "in_url"}},
			{"type": "input", "block_id": "bk_in_multi", "label": pt("Notes"), "optional": true,
				"element": map[string]any{"type": "plain_text_input", "action_id": "in_multi", "multiline": true, "initial_value": "First line"}},
		},
		"workflow": {
			sec("bk_wf_intro", "*Workflow button* and *icon buttons*"),
			act("bk_workflow",
				map[string]any{"type": "workflow_button", "action_id": "wf_start", "text": pt("Run deploy"), "style": "primary",
					"workflow": map[string]any{"trigger": map[string]any{"url": "https://slack.com/shortcuts/Ft0FAKEWF01/abcdef0123456789",
						"customizable_input_parameters": []map[string]any{{"name": "env", "value": "staging"}}}}},
				map[string]any{"type": "icon_button", "action_id": "icon_trash", "icon": "trash", "text": pt("Delete"), "value": "trash-1",
					"accessibility_label": "Delete this build"},
				map[string]any{"type": "icon_button", "action_id": "icon_copy", "icon": "clipboard", "text": pt("Copy link"), "value": "copy-1"},
			),
		},
		"more": {
			sec("bk_more_intro", "*Truncation* — six elements show; seven show five and “+ 2 more”."),
			act("bk_six", btn("six_1", "One", "1"), btn("six_2", "Two", "2"), btn("six_3", "Three", "3"), btn("six_4", "Four", "4"),
				btn("six_5", "Five", "5"), btn("six_6", "Six", "6")),
			act("bk_seven", btn("seven_1", "One", "1"), btn("seven_2", "Two", "2"), btn("seven_3", "Three", "3"), btn("seven_4", "Four", "4"),
				btn("seven_5", "Five", "5"), btn("seven_6", "Six", "6"), btn("seven_7", "Seven", "7")),
		},
	}
	var out []slack.Message
	for _, name := range GalleryElementNames {
		b, _ := json.Marshal(msgs[name])
		out = append(out, slack.Message{Type: "message", Text: "Block Kit gallery: " + name, Blocks: b, ClientMsgID: "bk-elements-" + name,
			BotID: GalleryBotID, BotProfile: &slack.BotProfile{ID: GalleryBotID, Name: "Gallery Bot", AppID: GalleryAppID,
				UserID: GalleryBotUserID, TeamID: TeamID}})
	}
	return out
}
