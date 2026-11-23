package fakeslack

// The fake app's slash command (blockkit-gallery size): Deploy Bot's
// /ticket, run through chat.command like Slack's chatCommandApi sends it
// ({command, disp, blocks or text, channel, client_token, thread_ts?}). The
// web client's executeCommand sends a command's arguments only as blocks;
// the app gets them as text (commandText). All synthetic.
//
//   - /ticket [title]: the app answers the command's trigger with
//     views.open, which reaches the client as view_opened carrying the
//     command's client_token; a moment later its views.update (view_updated)
//     fills in the title it was given. The modal's submits then go through
//     the shortcut modal's flow (bkgallery_surfaces.go): errors, push,
//     update, clear.
//   - /ticket status: the app replies through response_url with an
//     ephemeral message (is_ephemeral on the socket).
//   - /ticket timeout: Slack gave up on the app (dispatch_failed).
//   - /ticket broken: another API error (invalid_arguments).
//   - any other command: unknown_command.

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// CommandApp is what the fake app's /ticket saw (tests).
type CommandApp struct {
	mu    sync.Mutex
	Calls []url.Values // chat.command calls
}

var (
	cmdAppsMu sync.Mutex
	cmdApps   = map[*Server]*CommandApp{}
)

// CommandApp returns the slash command app's record for s.
func (s *Server) CommandApp() *CommandApp {
	cmdAppsMu.Lock()
	defer cmdAppsMu.Unlock()
	a := cmdApps[s]
	if a == nil {
		a = &CommandApp{}
		cmdApps[s] = a
	}
	return a
}

// Snapshot copies the chat.command calls.
func (a *CommandApp) Snapshot() []url.Values {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]url.Values(nil), a.Calls...)
}

// TicketCommand is the fake app's slash command.
const TicketCommand = "/ticket"

func init() { registerGalleryAPI(commandAPI) }

func commandAPI(s *Server, method string, f url.Values) (any, bool) {
	if !s.Size.Gallery {
		return nil, false
	}
	switch method {
	case "commands.list":
		return ok("commands", map[string]any{
			TicketCommand: map[string]any{"name": TicketCommand, "desc": "File a ticket with Deploy Bot", "usage": "[title]",
				"type": "app", "app": AppAppID, "app_name": "Deploy Bot",
				"icons": map[string]any{"image_32": s.imgURL(32, 32, "app"), "image_72": s.imgURL(72, 72, "app")}},
		}), true
	case "chat.command":
	default:
		return nil, false
	}
	app := s.CommandApp()
	app.mu.Lock()
	app.Calls = append(app.Calls, f)
	app.mu.Unlock()
	text, err := commandText(f)
	if err != nil {
		// not what Slack's executeCommand sends (commandText)
		return map[string]any{"ok": false, "error": "invalid_blocks", "detail": err.Error()}, true
	}
	if f.Get("command") != TicketCommand {
		return map[string]any{"ok": false, "error": "unknown_command"}, true
	}
	switch text {
	case "timeout":
		return map[string]any{"ok": false, "error": "dispatch_failed"}, true
	case "broken":
		return map[string]any{"ok": false, "error": "invalid_arguments"}, true
	case "status":
		ch, thread := f.Get("channel"), f.Get("thread_ts")
		go func() {
			time.Sleep(150 * time.Millisecond) // the app's POST to response_url
			ev := s.cmdEphemeral(ch, thread, "No open tickets. :sparkles:")
			s.emit(ev)
		}()
		return ok(), true
	}
	tok := f.Get("client_token")
	go func() {
		time.Sleep(100 * time.Millisecond) // the app's views.open(trigger_id)
		s.openSurfView(tok, surfTicket())
		if text == "" {
			return
		}
		time.Sleep(250 * time.Millisecond) // then views.update with what it looked up
		s.emit(map[string]any{"type": "view_updated", "view_id": SurfTicketView, "app_id": AppAppID,
			"view": surfTicketTitled(text)})
	}()
	return ok(), true
}

// surfTicketTitled is the ticket modal after the app's views.update for
// "/ticket <title>": the title field filled in.
func surfTicketTitled(title string) map[string]any {
	v := surfTicket()
	v["hash"] = "h1u"
	blocks := v["blocks"].([]any)
	in := blocks[1].(map[string]any)
	el := in["element"].(map[string]any)
	el["initial_value"] = title
	blocks[0] = map[string]any{"type": "section", "block_id": "intro", "text": md(fmt.Sprintf("Filed from `%s %s`. Tell *Deploy Bot* what broke.", TicketCommand, title))}
	return v
}

// cmdEphemeral is an app's response_url reply as the socket delivers it.
func (s *Server) cmdEphemeral(ch, thread, text string) map[string]any {
	ts := fmt.Sprintf("%d.%06d", time.Now().Unix(), time.Now().Nanosecond()/1000)
	m := map[string]any{"type": "message", "channel": ch, "ts": ts, "user": AppUserID, "bot_id": AppBotID, "text": text,
		"is_ephemeral": true, "bot_profile": slack.BotProfile{ID: AppBotID, Name: "Deploy Bot", AppID: AppAppID, UserID: AppUserID,
			TeamID: TeamID, Icons: slack.Icons{Image48: s.imgURL(48, 48, "app"), Image72: s.imgURL(72, 72, "app")}}}
	if thread != "" {
		m["thread_ts"] = thread
	}
	return m
}

// commandText is the text the server hands the app: the text param, or,
// when the client sent the arguments as blocks (Slack's executeCommand
// does), their rich text with mentions as Slack's escapes. It is strict
// about what convertDeltaToBlocks (with executeCommand's options) makes,
// so a client that differs fails: never text and blocks together, one
// rich_text block without a block_id, known element types only, and the
// whitespace Slack trims (none leading the first section, none trailing
// the last one's unformatted text, no spaces before a line break outside
// code: /(^|\S)( +)(\n)/gm).
func commandText(f url.Values) (string, error) {
	t, raw := f.Get("text"), f.Get("blocks")
	switch {
	case f.Has("text") && f.Has("blocks"):
		return "", fmt.Errorf("text and blocks together")
	case raw == "":
		return t, nil
	}
	var blocks []map[string]any
	if json.Unmarshal([]byte(raw), &blocks) != nil {
		return "", fmt.Errorf("blocks aren't a JSON array")
	}
	if len(blocks) != 1 || blocks[0]["type"] != "rich_text" {
		return "", fmt.Errorf("%d blocks, want one rich_text", len(blocks))
	}
	if _, ok := blocks[0]["block_id"]; ok {
		return "", fmt.Errorf("a block_id (convertDeltaToBlocks sets none)")
	}
	var b strings.Builder
	inline := func(els []any, pre bool) error {
		for _, e := range els {
			el, _ := e.(map[string]any)
			str := func(k string) string { v, _ := el[k].(string); return v }
			switch str("type") {
			case "text":
				if !pre && cmdSpacesBeforeBreak(str("text")) {
					return fmt.Errorf("spaces before a line break in %q", str("text"))
				}
				b.WriteString(str("text"))
			case "user":
				b.WriteString("<@" + str("user_id") + ">")
			case "channel":
				b.WriteString("<#" + str("channel_id") + ">")
			case "usergroup":
				b.WriteString("<!subteam^" + str("usergroup_id") + ">")
			case "broadcast":
				b.WriteString("<!" + str("range") + ">")
			case "link":
				if t := str("text"); t != "" {
					b.WriteString("<" + str("url") + "|" + t + ">")
				} else {
					b.WriteString("<" + str("url") + ">")
				}
			case "emoji":
				b.WriteString(":" + str("name") + ":")
			case "color":
				b.WriteString(str("value"))
			case "message_mention":
				b.WriteString("<" + str("url") + ">")
			default:
				return fmt.Errorf("element type %q", str("type"))
			}
		}
		return nil
	}
	secs, _ := blocks[0]["elements"].([]any)
	plainText := func(sec any, at func([]any) any) string {
		m, _ := sec.(map[string]any)
		if m["type"] != "rich_text_section" {
			return ""
		}
		els, _ := m["elements"].([]any)
		if len(els) == 0 {
			return ""
		}
		el, _ := at(els).(map[string]any)
		if _, styled := el["style"]; styled || el["type"] != "text" {
			return ""
		}
		v, _ := el["text"].(string)
		return v
	}
	if len(secs) > 0 {
		if first := plainText(secs[0], func(e []any) any { return e[0] }); first != strings.TrimLeft(first, " \t\n") {
			return "", fmt.Errorf("leading whitespace %q (trimStart)", first)
		}
		if last := plainText(secs[len(secs)-1], func(e []any) any { return e[len(e)-1] }); last != strings.TrimRight(last, " \t\n") {
			return "", fmt.Errorf("trailing whitespace %q (the last text is trimmed)", last)
		}
	}
	for i, sec := range secs {
		m, _ := sec.(map[string]any)
		if i > 0 {
			b.WriteString("\n")
		}
		typ, _ := m["type"].(string)
		els, _ := m["elements"].([]any)
		var err error
		switch typ {
		case "rich_text_section", "rich_text_quote", "rich_text_header":
			err = inline(els, false)
		case "rich_text_preformatted":
			err = inline(els, true)
		case "rich_text_list":
			for j, it := range els {
				item, _ := it.(map[string]any)
				if j > 0 {
					b.WriteString("\n")
				}
				sub, _ := item["elements"].([]any)
				if err = inline(sub, false); err != nil {
					break
				}
			}
		case "rich_text_divider":
		default:
			err = fmt.Errorf("element type %q", typ)
		}
		if err != nil {
			return "", err
		}
	}
	return b.String(), nil
}

// cmdSpacesBeforeBreak reports a match of convertDeltaToBlocks' K,
// /(^|\S)( +)(\n)/gm, with JavaScript's classes: spaces right before a
// "\n" that follow a line's start (after \n, \r, U+2028 or U+2029) or a
// character that isn't JavaScript white space. Go's \S is ASCII only (it
// matches U+00A0), and its (?m)^ follows \n alone.
func cmdSpacesBeforeBreak(s string) bool {
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		if rs[i] != ' ' {
			continue
		}
		j := i
		for j < len(rs) && rs[j] == ' ' {
			j++
		}
		if j < len(rs) && rs[j] == '\n' && (i == 0 || jsLineTerminator(rs[i-1]) || !jsWhiteSpace(rs[i-1])) {
			return true
		}
		i = j
	}
	return false
}

// jsWhiteSpace is JavaScript's \s.
func jsWhiteSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0xa0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}
	return r >= 0x2000 && r <= 0x200a
}

// jsLineTerminator is what JavaScript's multiline ^ follows.
func jsLineTerminator(r rune) bool { return r == '\n' || r == '\r' || r == 0x2028 || r == 0x2029 }
