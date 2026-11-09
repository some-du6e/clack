package fakeslack

// The fake app's slash command (blockkit-gallery size): Deploy Bot's
// /ticket, run through chat.command like Slack's chatCommandApi sends it
// ({command, disp, text, channel, client_token, thread_ts?}). All
// synthetic.
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
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
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
	if f.Get("command") != TicketCommand {
		return map[string]any{"ok": false, "error": "unknown_command"}, true
	}
	text := strings.TrimSpace(f.Get("text"))
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
