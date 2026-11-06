package server

import (
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/some-du6e/yoloslackgoclient/internal/emoji"
	"github.com/some-du6e/yoloslackgoclient/internal/render"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/store"
)

// Handler returns the HTTP handler serving the UI (from web) and API.
func (a *App) Handler(web fs.FS) http.Handler {
	mux := http.NewServeMux()
	static := http.FileServer(http.FS(web))
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || !strings.Contains(r.URL.Path[1:], ".") {
			w.Header().Set("Cache-Control", "no-cache")
			r2 := *r
			u := *r.URL
			u.Path = "/"
			r2.URL = &u
			static.ServeHTTP(w, &r2)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		static.ServeHTTP(w, r)
	}))
	mux.HandleFunc("/static/emoji.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		writeMaybeGzip(w, r, emoji.StandardJSON)
	})
	mux.HandleFunc("/api/boot", a.hBoot)
	mux.HandleFunc("/api/history", a.hHistory)
	mux.HandleFunc("/api/replies", a.hReplies)
	mux.HandleFunc("/api/send", a.hSend)
	mux.HandleFunc("/api/edit", a.hEdit)
	mux.HandleFunc("/api/delete", a.hDelete)
	mux.HandleFunc("/api/react", a.hReact)
	mux.HandleFunc("/api/mark", a.hMark)
	mux.HandleFunc("/api/users", a.hUsers)
	mux.HandleFunc("/api/emoji", a.hEmoji)
	mux.HandleFunc("/api/typing", a.hTyping)
	mux.HandleFunc("/api/call", a.hCall)
	mux.HandleFunc("/api/render", a.hRender)
	a.registerMore(mux)
	mux.HandleFunc("/file", a.hFile)
	mux.HandleFunc("/ws", a.hWS)
	return logRequests(mux)
}

func logRequests(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only accept requests that come from our own origin (prevents other
		// sites in the browser from driving the API through localhost).
		if o := r.Header.Get("Origin"); o != "" && r.Method != http.MethodGet {
			if u, err := url.Parse(o); err != nil || u.Host != r.Host {
				http.Error(w, "bad origin", http.StatusForbidden)
				return
			}
		}
		h.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, r *http.Request, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	writeMaybeGzip(w, r, b)
}

func writeMaybeGzip(w http.ResponseWriter, r *http.Request, b []byte) {
	if len(b) > 4096 && strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		w.Header().Set("Content-Encoding", "gzip")
		gz, _ := gzip.NewWriterLevel(w, gzip.BestSpeed)
		gz.Write(b)
		gz.Close()
		return
	}
	w.Write(b)
}

func writeErr(w http.ResponseWriter, err error) {
	code := http.StatusBadGateway
	msg := err.Error()
	var se *slack.Error
	if errors.As(err, &se) {
		msg = se.Code
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": msg})
}

func decode(r *http.Request, v any) error {
	return json.NewDecoder(io.LimitReader(r.Body, 8<<20)).Decode(v)
}

// ---- boot ----

type UserView struct {
	ID             string `json:"id"`
	Name           string `json:"name"` // display name as Slack shows it
	RealName       string `json:"real_name"`
	Handle         string `json:"handle"`
	Avatar         string `json:"avatar"`
	Avatar192      string `json:"avatar192"`
	Title          string `json:"title,omitempty"`
	Pronouns       string `json:"pronouns,omitempty"`
	StatusText     string `json:"status_text,omitempty"`
	StatusEmoji    string `json:"status_emoji,omitempty"`
	StatusEmojiURL string `json:"status_emoji_url,omitempty"`
	StatusExp      int64  `json:"status_exp,omitempty"`
	TZ             string `json:"tz,omitempty"`
	TZOffset       int    `json:"tz_offset,omitempty"`
	TZLabel        string `json:"tz_label,omitempty"`
	IsBot          bool   `json:"is_bot,omitempty"`
	Deleted        bool   `json:"deleted,omitempty"`
	IsAdmin        bool   `json:"is_admin,omitempty"`
	IsOwner        bool   `json:"is_owner,omitempty"`
	Guest          bool   `json:"guest,omitempty"`
	Color          string `json:"color,omitempty"`
	Presence       string `json:"presence,omitempty"`
	Huddle         bool   `json:"huddle,omitempty"`
}

func avatarURL(u *slack.User, size int) string {
	p := u.Profile
	switch {
	case size <= 72 && p.Image72 != "":
		return p.Image72
	case size > 72 && p.Image192 != "":
		return p.Image192
	case p.Image48 != "":
		return p.Image48
	}
	if p.AvatarHash != "" {
		team := u.TeamID
		if team == "" {
			team = p.Team
		}
		if team == "" {
			team = "T0266FRGM"
		}
		return "https://ca.slack-edge.com/" + team + "-" + u.ID + "-" + p.AvatarHash + "-" + strconv.Itoa(size)
	}
	return "https://a.slack-edge.com/80588/img/avatars/ava_0001-" + strconv.Itoa(size) + ".png"
}

func (a *App) userView(id string) *UserView {
	u, ok := a.S.User(id)
	if !ok {
		return nil
	}
	v := &UserView{
		ID: u.ID, Name: store.DisplayName(u), RealName: strings.TrimSpace(u.Profile.RealName), Handle: u.Name,
		Avatar: avatarURL(u, 72), Avatar192: avatarURL(u, 192), Title: u.Profile.Title, Pronouns: u.Profile.Pronouns,
		StatusText: u.Profile.StatusText, StatusEmoji: u.Profile.StatusEmoji, StatusExp: u.Profile.StatusExpiration,
		TZ: u.TZ, TZOffset: u.TZOffset, TZLabel: u.TZLabel, IsBot: u.IsBot || u.ID == "USLACKBOT", Deleted: u.Deleted,
		IsAdmin: u.IsAdmin, IsOwner: u.IsOwner, Guest: u.IsRestricted || u.IsUltraRestricted, Color: u.Color,
		Huddle: u.Profile.HuddleState == "in_a_huddle",
	}
	if v.RealName == "​" {
		v.RealName = ""
	}
	if v.StatusEmoji != "" {
		v.StatusEmojiURL, _, _ = a.S.Emoji(strings.Trim(v.StatusEmoji, ":"))
		if len(u.Profile.StatusEmojiDisplay) > 2 {
			var di []struct {
				DisplayURL string `json:"display_url"`
			}
			if json.Unmarshal(u.Profile.StatusEmojiDisplay, &di) == nil && len(di) > 0 && di[0].DisplayURL != "" {
				v.StatusEmojiURL = di[0].DisplayURL
			}
		}
	}
	a.S.RLock()
	v.Presence = a.S.Presence[id]
	a.S.RUnlock()
	return v
}

func (a *App) usersView(ids []string) map[string]*UserView {
	out := map[string]*UserView{}
	for _, id := range ids {
		if id == "" {
			continue
		}
		if _, ok := out[id]; ok {
			continue
		}
		if v := a.userView(id); v != nil {
			out[id] = v
		}
	}
	return out
}

func (a *App) hBoot(w http.ResponseWriter, r *http.Request) {
	<-a.ready
	if a.bootErr != nil {
		writeErr(w, a.bootErr)
		return
	}
	a.S.RLock()
	convs := make([]ConvView, 0, len(a.S.Convs))
	var ids []string
	cs := make([]*slack.Conversation, 0, len(a.S.Convs))
	for _, c := range a.S.Convs {
		cs = append(cs, c)
	}
	team := map[string]any{"id": a.S.TeamID, "name": a.S.TeamName, "icon": a.S.TeamIcon, "domain": a.S.TeamDomain}
	a.S.RUnlock()
	for _, c := range cs {
		v := a.convView(c)
		convs = append(convs, v)
		if v.User != "" {
			ids = append(ids, v.User)
		}
	}
	sort.Slice(convs, func(i, j int) bool { return convs[i].Name < convs[j].Name })
	ids = append(ids, a.S.Self.ID)
	a.mu.Lock()
	connected := a.connected
	a.mu.Unlock()
	writeJSON(w, r, map[string]any{
		"ok": true, "self": a.userView(a.S.Self.ID), "team": team, "convs": convs,
		"sections": a.sectionsView(convs), "counts": a.countsView(), "threads": a.threadsView(), "users": a.usersView(ids),
		"connected": connected, "emoji_count": a.S.CustomEmoji.Len(), "enterprise_id": a.C.Creds.EnterpriseID,
		"prefs": a.prefsView(), "usergroup_count": len(a.S.Groups),
	})
}

// ---- history & threads ----

type historyResp struct {
	OK       bool                 `json:"ok"`
	Channel  string               `json:"channel"`
	Messages []render.View        `json:"messages"`
	HasMore  bool                 `json:"has_more"`
	Users    map[string]*UserView `json:"users"`
	Conv     *ConvView            `json:"conv,omitempty"`
	LastRead string               `json:"last_read,omitempty"`
}

func (a *App) hHistory(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ch := r.URL.Query().Get("channel")
	before := r.URL.Query().Get("before")
	fresh := r.URL.Query().Get("fresh") == "1"
	if ch == "" {
		http.Error(w, "channel required", 400)
		return
	}
	var msgs []slack.Message
	var hasMore bool
	t0 := time.Now()
	source := "cache"
	defer func() {
		log.Printf("history %s: %s (%s)", ch, time.Since(t0).Round(time.Millisecond), source)
	}()
	if before == "" {
		t := a.S.Timeline(ch)
		if t == nil || fresh {
			source = "fetch"
			if err := a.loadLatest(ctx, ch); err != nil {
				writeErr(w, err)
				return
			}
			t = a.S.Timeline(ch)
		}
		if t != nil {
			msgs, hasMore = t.Msgs, t.HasMore
		}
	} else {
		source = "older"
		p, err := a.C.History(ctx, ch, slack.HistoryOpts{Latest: before, Limit: 100})
		if err != nil {
			writeErr(w, err)
			return
		}
		a.ensureUsers(ctx, referencedUsers(p.Messages))
		a.S.PrependOlder(ch, p.Messages, p.HasMore)
		msgs, hasMore = p.Messages, p.HasMore
		store.SortAsc(msgs)
	}
	resp := historyResp{OK: true, Channel: ch, Messages: a.views(ch, msgs), HasMore: hasMore}
	resp.Users = a.usersView(referencedUsers(msgs))
	if c, ok := a.S.Conv(ch); ok {
		if c.NumMembers == 0 && !c.IsIM && before == "" {
			if full, err := a.C.ConversationInfo(ctx, ch); err == nil {
				a.S.PutConv(*full)
				c = full
			}
		}
		v := a.convView(c)
		resp.Conv = &v
	}
	a.S.RLock()
	if e, ok := a.S.Counts[ch]; ok {
		resp.LastRead = e.LastRead
	}
	a.S.RUnlock()
	a.subscribePresence(referencedUsers(msgs))
	writeJSON(w, r, resp)
}

func (a *App) hReplies(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ch, ts := r.URL.Query().Get("channel"), r.URL.Query().Get("ts")
	msgs, ok := a.S.Thread(ch, ts)
	if !ok || r.URL.Query().Get("fresh") == "1" {
		var all []slack.Message
		cursor := ""
		for {
			page, next, err := a.C.Replies(ctx, ch, ts, cursor)
			if err != nil {
				writeErr(w, err)
				return
			}
			all = append(all, page...)
			if next == "" {
				break
			}
			cursor = next
		}
		a.ensureUsers(ctx, referencedUsers(all))
		a.S.SetThread(ch, ts, all)
		msgs, _ = a.S.Thread(ch, ts)
	}
	a.subscribePresence(referencedUsers(msgs))
	writeJSON(w, r, map[string]any{"ok": true, "channel": ch, "ts": ts, "messages": a.threadViews(ch, msgs), "users": a.usersView(referencedUsers(msgs))})
}

// ---- writes ----

func (a *App) hSend(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Channel   string          `json:"channel"`
		Text      string          `json:"text"`
		ThreadTS  string          `json:"thread_ts"`
		Broadcast bool            `json:"broadcast"`
		Blocks    json.RawMessage `json:"blocks"`
	}
	if err := decode(r, &req); err != nil || req.Channel == "" {
		http.Error(w, "bad request", 400)
		return
	}
	id := make([]byte, 16)
	rand.Read(id)
	h := hex.EncodeToString(id)
	cid := h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
	m, err := a.C.Post(r.Context(), slack.PostOpts{Channel: req.Channel, Text: req.Text, ThreadTS: req.ThreadTS, Broadcast: req.Broadcast, Blocks: req.Blocks, ClientMsgID: cid})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, r, map[string]any{"ok": true, "msg": render.Message(a.S, req.Channel, m)})
}

func (a *App) hEdit(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Channel, TS, Text string
		Blocks            json.RawMessage
	}
	if err := decode(r, &req); err != nil {
		http.Error(w, "bad request", 400)
		return
	}
	if err := a.C.Update(r.Context(), req.Channel, req.TS, req.Text, req.Blocks); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, r, map[string]any{"ok": true})
}

func (a *App) hDelete(w http.ResponseWriter, r *http.Request) {
	var req struct{ Channel, TS string }
	if err := decode(r, &req); err != nil {
		http.Error(w, "bad request", 400)
		return
	}
	if err := a.C.Delete(r.Context(), req.Channel, req.TS); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, r, map[string]any{"ok": true})
}

func (a *App) hReact(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Channel, TS, Name string
		Add               bool
	}
	if err := decode(r, &req); err != nil {
		http.Error(w, "bad request", 400)
		return
	}
	if err := a.C.React(r.Context(), req.Channel, req.TS, req.Name, req.Add); err != nil {
		var se *slack.Error
		if !(errors.As(err, &se) && (se.Code == "already_reacted" || se.Code == "no_reaction")) {
			writeErr(w, err)
			return
		}
	}
	writeJSON(w, r, map[string]any{"ok": true})
}

func (a *App) hMark(w http.ResponseWriter, r *http.Request) {
	var req struct{ Channel, TS string }
	if err := decode(r, &req); err != nil || req.Channel == "" || req.TS == "" {
		http.Error(w, "bad request", 400)
		return
	}
	if err := a.C.Mark(r.Context(), req.Channel, req.TS); err != nil {
		writeErr(w, err)
		return
	}
	// Marking at an earlier ts is "mark unread": the channel stays unread.
	e := a.S.MarkRead(req.Channel, req.TS)
	cv := a.countOf(req.Channel)
	a.Hub.Broadcast(map[string]any{"type": "marked", "channel": req.Channel, "ts": req.TS, "unread": e.HasUnreads, "mentions": e.MentionCount, "count": cv})
	if e.HasUnreads {
		a.refreshCountsSoon() // server knows the mention count below the new cursor
	}
	writeJSON(w, r, map[string]any{"ok": true, "count": cv})
}

func (a *App) hTyping(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Channel  string `json:"channel"`
		ThreadTS string `json:"thread_ts"`
	}
	if err := decode(r, &req); err == nil {
		_ = a.RTM.Typing(req.Channel, req.ThreadTS)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) hUsers(w http.ResponseWriter, r *http.Request) {
	ids := strings.Split(r.URL.Query().Get("ids"), ",")
	a.ensureUsers(r.Context(), ids)
	writeJSON(w, r, map[string]any{"ok": true, "users": a.usersView(ids)})
}

func (a *App) hEmoji(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, max-age=300")
	b, _ := json.Marshal(map[string]any{"ok": true, "emoji": a.S.CustomEmoji.Snapshot()})
	w.Header().Set("Content-Type", "application/json")
	writeMaybeGzip(w, r, b)
}

// hRender renders mrkdwn text for previews (e.g. edits, topics).
func (a *App) hRender(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, r, map[string]any{"html": render.Mrkdwn(a.S, r.URL.Query().Get("text"))})
}

// allowedCalls is the explicit list of passthrough methods the UI may use.
var allowedCalls = map[string]bool{
	"search.messages": true, "search.modules": true, "conversations.info": true, "conversations.members": true,
	"pins.list": true, "pins.add": true, "pins.remove": true, "bookmarks.list": true, "stars.add": true,
	"stars.remove": true, "saved.add": true, "saved.delete": true, "saved.list": true, "users.profile.get": true,
	"users.profile.set": true, "users.setPresence": true, "users.getPresence": true, "dnd.info": true, "dnd.setSnooze": true, "dnd.endSnooze": true,
	"conversations.join": true, "conversations.leave": true, "conversations.open": true, "conversations.setTopic": true,
	"conversations.setPurpose": true, "conversations.create": true, "conversations.invite": true, "conversations.list": true,
	"chat.getPermalink": true, "chat.meMessage": true, "chat.scheduleMessage": true, "reminders.add": true,
	"files.info": true, "files.list": true, "usergroups.list": true, "activity.feed": true, "subscriptions.thread.getView": true,
	"subscriptions.thread.add": true, "subscriptions.thread.remove": true, "subscriptions.thread.mark": true,
	"users.channelSections.list": true, "users.prefs.get": true, "drafts.list": true, "emoji.list": true,
	"apps.profile.get": true, "bots.info": true, "team.info": true, "users.counts": true, "client.counts": true,
	"conversations.history": true, "conversations.replies": true, "conversations.mark": true, "users.info": true,
	"search.autocomplete": true, "search.inline": true, "users.search": true, "conversations.suggestions": true,
	"commands.list": true, "chat.command": true, "conversations.close": true, "users.channelSections.channels.bulkUpdate": true,
	"chat.unfurl": true, "blocks.actions": true, "views.submit": true, "huddles.info": true, "rooms.join": true,
	"conversations.view": true, "client.dms": true, "later.list": true, "files.completeUploadExternal": true, "files.getUploadURLExternal": true,
}

// hCall is a guarded passthrough so the UI can reach the long tail of methods.
func (a *App) hCall(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Method string            `json:"method"`
		Params map[string]string `json:"params"`
		Edge   string            `json:"edge"`
		Body   map[string]any    `json:"body"`
	}
	if err := decode(r, &req); err != nil {
		http.Error(w, "bad request", 400)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	var raw json.RawMessage
	var err error
	if req.Edge != "" {
		if req.Body == nil {
			req.Body = map[string]any{}
		}
		err = a.C.Edge(ctx, req.Edge, req.Body, &raw)
	} else {
		if !allowedCalls[req.Method] {
			http.Error(w, "method not allowed: "+req.Method, 403)
			return
		}
		p := url.Values{}
		for k, v := range req.Params {
			p.Set(k, v)
		}
		raw, err = a.C.Raw(ctx, req.Method, p)
	}
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	writeMaybeGzip(w, r, raw)
}

// ---- authenticated file proxy (memory-only cache) ----

type fileEntry struct {
	ctype string
	data  []byte
	at    time.Time
}

var (
	fileMu    sync.Mutex
	fileCache = map[string]*fileEntry{}
	fileBytes int
)

const fileCacheMax = 256 << 20

func (a *App) hFile(w http.ResponseWriter, r *http.Request) {
	u := r.URL.Query().Get("u")
	pu, err := url.Parse(u)
	if err != nil || pu.Scheme != "https" || !(strings.HasSuffix(pu.Host, ".slack.com") || strings.HasSuffix(pu.Host, ".slack-edge.com")) {
		http.Error(w, "bad url", 400)
		return
	}
	fileMu.Lock()
	e := fileCache[u]
	fileMu.Unlock()
	if e != nil {
		w.Header().Set("Content-Type", e.ctype)
		w.Header().Set("Cache-Control", "private, max-age=3600")
		w.Write(e.data)
		return
	}
	resp, err := a.C.Download(r.Context(), u)
	if err != nil {
		http.Error(w, err.Error(), 502)
		return
	}
	defer resp.Body.Close()
	ct := resp.Header.Get("Content-Type")
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "private, max-age=3600")
	if resp.ContentLength > 0 && resp.ContentLength < 8<<20 && resp.StatusCode == 200 {
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			return
		}
		w.Write(data)
		fileMu.Lock()
		fileCache[u] = &fileEntry{ctype: ct, data: data, at: time.Now()}
		fileBytes += len(data)
		for fileBytes > fileCacheMax {
			var oldK string
			var oldT time.Time
			for k, v := range fileCache {
				if oldK == "" || v.at.Before(oldT) {
					oldK, oldT = k, v.at
				}
			}
			fileBytes -= len(fileCache[oldK].data)
			delete(fileCache, oldK)
		}
		fileMu.Unlock()
		return
	}
	if cl := resp.Header.Get("Content-Length"); cl != "" {
		w.Header().Set("Content-Length", cl)
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// ---- UI push hub ----

type Hub struct {
	mu    sync.Mutex
	conns map[*websocket.Conn]chan []byte
}

func newHub() *Hub { return &Hub{conns: map[*websocket.Conn]chan []byte{}} }

func (h *Hub) Broadcast(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	h.mu.Lock()
	for _, ch := range h.conns {
		select {
		case ch <- b:
		default:
		}
	}
	h.mu.Unlock()
}

func (a *App) hWS(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{CompressionMode: websocket.CompressionContextTakeover})
	if err != nil {
		return
	}
	out := make(chan []byte, 256)
	a.Hub.mu.Lock()
	a.Hub.conns[c] = out
	a.Hub.mu.Unlock()
	defer func() {
		a.Hub.mu.Lock()
		delete(a.Hub.conns, c)
		a.Hub.mu.Unlock()
		c.CloseNow()
	}()
	ctx := c.CloseRead(r.Context())
	a.mu.Lock()
	hello, _ := json.Marshal(map[string]any{"type": "connection", "connected": a.connected})
	a.mu.Unlock()
	out <- hello
	for {
		select {
		case <-ctx.Done():
			return
		case b := <-out:
			wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := c.Write(wctx, websocket.MessageText, b)
			cancel()
			if err != nil {
				log.Printf("ws write: %v", err)
				return
			}
		}
	}
}
