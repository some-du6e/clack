package composer

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"gioui.org/io/key"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/emoji"
)

// Autocomplete is the @ # : / suggestion popup.
//
// The composer drives it once per frame:
//
//	ac.Update(doc, caret)          // after the editor processed its events
//	if it, s, e, ok := ac.Accepted(); ok {
//		if it.Kind == acCommand {
//			ed.Replace(s, e, "/"+it.Name+" ")
//		} else {
//			ed.InsertAtom(s, e, it.Atom)
//		}
//	}
//	if ac.Open() { ... ac.Layout(gtx, maxWidth) ... }
//
// and plugs ac.Key into Editor.Intercept so Up/Down/Tab/Enter/Escape reach
// the popup first while it is open. Accepted is a one-shot: a pick made by
// keyboard (Key) or mouse (Layout) is returned once, then cleared.
// Editor.InsertAtom already adds the trailing space Slack inserts.
//
// Placement (measured): the popup's left edge is 17dp left of the trigger
// character (Anchor gives its document offset), and its bottom edge is 5dp
// above the top of the caret's line box. It covers the toolbar and the top
// of the composer, and does not move while the query is typed.
//
// Network sources (directory search, channel search, channel members,
// usergroups, commands) load on goroutines and call Host.Invalidate when
// they land. Call Close when the composer goes away to cancel them.

type acKind uint8

const (
	acUser    acKind = iota // users, usergroups, broadcasts (Atom.Kind tells which)
	acChannel               // channels
	acEmoji                 // emoji
	acCommand               // slash commands
)

// acItem is one suggestion row.
type acItem struct {
	Kind       acKind
	ID         string // user/usergroup/channel id, broadcast range, emoji name, command name
	Name       string // bold text: display name, handle, channel name, emoji name, command name (no "/")
	Secondary  string // gray text: real name, usergroup name, broadcast hint, command description
	Avatar     string // user avatar or emoji image URL
	Uni        string // emoji: unicode text for standard emoji
	Presence   string // active | away | ""
	IsBot      bool
	Self       bool          // user: the viewer, drawn with "(you)"
	NotIn      bool          // user: not a member of this conversation ("Not in channel")
	Status     string        // user: custom status text
	StatusURL  string        // user: custom status emoji image
	statusName string        // user: status emoji name, resolved to StatusURL
	Count      int           // usergroup: member count
	Private    bool          // channel: draw a lock instead of #
	Archived   bool          // channel: dimmed, archive icon, " (archived)"
	Member     bool          // user: in this conversation; channel: viewer is a member
	Atom       *Atom         // inserted for users, usergroups, broadcasts, channels, emoji
	Command    *core.Command // acCommand
}

// IsGroup reports a usergroup row.
func (it acItem) IsGroup() bool { return it.Atom != nil && it.Atom.Kind == AtomUsergroup }

// IsBroadcast reports an @here / @channel / @everyone row.
func (it acItem) IsBroadcast() bool { return it.Atom != nil && it.Atom.Kind == AtomBroadcast }

// acTrigger is an autocomplete token before the caret.
type acTrigger struct {
	Kind       acKind
	Start, End int    // replace range [Start, End); End is the caret
	Query      string // token without the trigger character
}

const (
	acMaxItems  = 25                     // Slack caps every list at 25 rows
	acDebounce  = 100 * time.Millisecond // network search debounce
	acSnapEvery = 5 * time.Second        // snapshot refresh while typing
)

// Autocomplete state. Everything but the fields under mu belongs to the UI
// goroutine.
type Autocomplete struct {
	h         Host
	channelID string

	// Live returns the editor's current document and caret. When set, Key
	// re-detects the trigger against it before acting, so a Tab/Enter that
	// arrives in the same input batch as more typing (fast typists, slow
	// frames) sees the real query and range, not last frame's.
	Live func() (*Doc, int)
	// OnAccept, when set, applies a pick immediately (inside the key event
	// that made it), so keys queued behind the Tab/Enter land after the
	// inserted pill. Without it picks are queued for Accepted.
	OnAccept func(it acItem, start, end int)

	ctx    context.Context
	cancel context.CancelFunc

	trig      acTrigger
	active    bool
	dismissed acTrigger // Esc: closed until the token changes
	hasDis    bool

	items    []acItem
	itemsVer uint64 // bumped on every recompute
	sel      int
	pseudo   bool // just opened: row 0 is gray with an Enter hint, not blue
	scroll   bool // selection moved by keyboard: keep it visible
	teamName string
	pending  *acAccept

	// item cache key: recompute when the trigger or the async data change
	itemsFor acTrigger
	itemsGen uint64
	itemsOK  bool

	// local sources, refreshed from the snapshot
	snapAt   time.Time
	known    map[string]core.User // snapshot users + self
	selfID   string
	dmUsers  map[string]bool // IM partners
	convs    []core.Conv     // non-IM conversations in the sidebar
	conv     core.Conv       // the current conversation
	haveConv bool

	searchCancel context.CancelFunc

	// view state (autocomplete_view.go)
	view acView

	mu         sync.Mutex
	gen        uint64               // bumped whenever async data lands
	members    map[string]core.User // current conversation members
	memLoading bool
	searched   map[string]core.User // union of directory search results
	askedUsers map[string]bool
	groups     []core.Usergroup
	grpLoading bool
	chanHits   map[string]core.ChannelHit
	askedChans map[string]bool
	commands   []core.Command
	cmdLoading bool
	emojiIdx   *emojiIndex
	emoLoading bool
}

type acAccept struct {
	it         acItem
	start, end int
}

func newAutocomplete(h Host, channelID string) *Autocomplete {
	ctx, cancel := context.WithCancel(context.Background())
	return &Autocomplete{
		h: h, channelID: channelID, ctx: ctx, cancel: cancel,
		searched:   map[string]core.User{},
		askedUsers: map[string]bool{},
		chanHits:   map[string]core.ChannelHit{},
		askedChans: map[string]bool{},
	}
}

// Close cancels in-flight loads.
func (ac *Autocomplete) Close() { ac.cancel() }

// Open reports whether the popup is showing.
func (ac *Autocomplete) Open() bool { return ac.active && len(ac.items) > 0 }

// Items returns the current rows and the selected index.
func (ac *Autocomplete) Items() ([]acItem, int) { return ac.items, ac.sel }

// Anchor returns the document offset of the trigger character; the popup's
// left edge goes 17dp left of it.
func (ac *Autocomplete) Anchor() int { return ac.trig.Start }

// Dismiss closes the popup until the trigger token changes.
func (ac *Autocomplete) Dismiss() {
	if ac.active {
		ac.dismissed, ac.hasDis = ac.trig, true
	}
	ac.active = false
}

// Accepted returns the row picked since the last call, with the document
// range [start, end) it replaces. It reports ok once per pick.
func (ac *Autocomplete) Accepted() (it acItem, start, end int, ok bool) {
	p := ac.pending
	if p == nil {
		return acItem{}, 0, 0, false
	}
	ac.pending = nil
	return p.it, p.start, p.end, true
}

// Key handles a key press while the popup is open (Editor.Intercept). It
// returns false, letting the editor handle the key, when closed.
func (ac *Autocomplete) Key(name key.Name, mods key.Modifiers) bool {
	if ac.Live != nil {
		switch name {
		case key.NameTab, key.NameReturn, key.NameEnter, key.NameUpArrow, key.NameDownArrow, key.NameEscape:
			// The popup state is from the last frame; the document may
			// have moved on within this input batch. Sync first.
			if d, caret := ac.Live(); d != nil {
				ac.Update(d, caret)
			}
		}
	}
	if !ac.Open() {
		return false
	}
	switch name {
	case key.NameUpArrow:
		ac.move(-1)
	case key.NameDownArrow:
		ac.move(1)
	case key.NameTab:
		if mods.Contain(key.ModShift) {
			ac.move(-1)
			return true
		}
		ac.accept(ac.sel)
	case key.NameReturn, key.NameEnter:
		if mods.Contain(key.ModShift) {
			return false
		}
		ac.accept(ac.sel)
	case key.NameEscape:
		ac.Dismiss()
	default:
		return false
	}
	return true
}

// move steps the selection, wrapping. The first move after opening only
// turns the gray pseudo-selection on row 0 into a real (blue) one.
func (ac *Autocomplete) move(d int) {
	ac.scroll = true
	if ac.pseudo {
		ac.pseudo = false
		return
	}
	n := len(ac.items)
	ac.sel = ((ac.sel+d)%n + n) % n
}

// hover selects row i (mouse movement).
func (ac *Autocomplete) hover(i int) {
	if i >= 0 && i < len(ac.items) {
		ac.sel, ac.pseudo = i, false
	}
}

// accept queues row i for Accepted and closes the popup.
func (ac *Autocomplete) accept(i int) {
	if i < 0 || i >= len(ac.items) {
		return
	}
	it := ac.items[i]
	if it.Atom != nil {
		a := *it.Atom
		it.Atom = &a
	}
	start, end := ac.trig.Start, ac.trig.End
	// Suppress the token until the composer replaces it, so an Update that
	// runs before Accepted is polled doesn't reopen the popup.
	ac.Dismiss()
	if ac.OnAccept != nil {
		ac.OnAccept(it, start, end)
		return
	}
	ac.pending = &acAccept{it: it, start: start, end: end}
}

// Update re-detects the trigger before caret and refreshes the rows. It is
// cheap when nothing changed, so it can run every frame.
func (ac *Autocomplete) Update(d *Doc, caret int) {
	t, ok := detectTrigger(d, caret)
	if ok && ac.hasDis {
		if t == ac.dismissed {
			ok = false
		} else {
			ac.hasDis = false // the token changed: reopen
		}
	}
	if !ok {
		ac.active = false
		return
	}
	if !ac.active || t.Query != ac.trig.Query || t.Kind != ac.trig.Kind {
		ac.sel, ac.pseudo, ac.scroll = 0, true, true
	}
	ac.active = true
	ac.trig = t
	ac.mu.Lock()
	gen := ac.gen
	ac.mu.Unlock()
	if ac.itemsOK && ac.itemsFor.Kind == t.Kind && ac.itemsFor.Query == t.Query && ac.itemsGen == gen {
		return
	}
	ac.refresh()
	ac.load(t)
	ac.mu.Lock()
	gen = ac.gen
	ac.mu.Unlock()
	selID := ""
	if !ac.pseudo && ac.sel < len(ac.items) {
		selID = ac.items[ac.sel].ID
	}
	ac.items = ac.compute(t)
	ac.itemsVer++
	ac.watchPresence()
	ac.itemsFor, ac.itemsGen, ac.itemsOK = t, gen, true
	// async rows landed: keep the row the user picked selected
	if selID != "" {
		for i, it := range ac.items {
			if it.ID == selID {
				ac.sel = i
				break
			}
		}
	}
	if ac.sel >= len(ac.items) {
		ac.sel = max(len(ac.items)-1, 0)
	}
}

// detectTrigger finds an autocomplete token ending at caret: "@query",
// "#query", ":query" (2+ chars, not closed by ':'), or "/query" at the very
// start of the message. Tokens inside code are ignored.
func detectTrigger(d *Doc, caret int) (acTrigger, bool) {
	if d == nil || caret < 0 || caret > d.Len() {
		return acTrigger{}, false
	}
	start := d.WordBefore(caret)
	if start >= caret {
		// No word immediately before the caret: a mention may still be
		// open with trailing spaces ("@karim h"), so try the @ scan.
		return mentionTrigger(d, caret)
	}
	if d.Attrs[start].Style&Code != 0 {
		return acTrigger{}, false
	}
	if l := d.LineOf(start); l < len(d.Lines) && d.Lines[l].Kind == CodeBlock {
		return acTrigger{}, false
	}
	tok := d.Runes[start:caret]
	q := string(tok[1:])
	t := acTrigger{Start: start, End: caret, Query: q}
	switch tok[0] {
	case '@':
		t.Kind = acUser
		if !validQuery(q, ".-_'") {
			return acTrigger{}, false
		}
	case '#':
		t.Kind = acChannel
		if !validQuery(q, ".-_") {
			return acTrigger{}, false
		}
	case ':':
		t.Kind = acEmoji
		if len(tok) < 3 || strings.HasSuffix(q, ":") || !validQuery(q, "_-+'") {
			return acTrigger{}, false
		}
	case '/':
		t.Kind = acCommand
		if start != 0 || !validQuery(q, "-_") {
			return acTrigger{}, false
		}
	default:
		// The word before the caret isn't a trigger token, but an @ mention
		// with spaces may still be open further back ("@karim h").
		return mentionTrigger(d, caret)
	}
	return t, true
}

// mentionTrigger scans back from the caret for an @ at a word boundary whose
// query may contain single internal spaces, so "@karim h" matches
// "Karim Hacker" as in Slack. It stops at a newline, an atom, a double
// space, another trigger char, or 48 runes.
func mentionTrigger(d *Doc, caret int) (acTrigger, bool) {
	const maxLen = 48
	lineStart := d.LineStart(d.LineOf(caret))
	i := caret
	for i > lineStart && caret-i < maxLen {
		r := d.Runes[i-1]
		if d.Attrs[i-1].Atom != nil {
			break
		}
		if r == '@' {
			// @ must be at a word boundary (line start or after a space).
			if i-1 > lineStart {
				p := d.Runes[i-2]
				if p != ' ' && p != '\t' && p != '\u00a0' {
					break
				}
			}
			if d.Attrs[i-1].Style&Code != 0 {
				return acTrigger{}, false
			}
			if l := d.LineOf(i - 1); l < len(d.Lines) && d.Lines[l].Kind == CodeBlock {
				return acTrigger{}, false
			}
			q := string(d.Runes[i:caret])
			if strings.TrimSpace(q) == "" || !validQuery(q, ".-_' ") || strings.Contains(q, "  ") {
				return acTrigger{}, false
			}
			return acTrigger{Kind: acUser, Start: i - 1, End: caret, Query: q}, true
		}
		if r == '\n' || r == '#' || r == ':' || r == '/' {
			break
		}
		i--
	}
	return acTrigger{}, false
}

// validQuery reports whether q holds only letters, digits and extra.
func validQuery(q, extra string) bool {
	for _, r := range q {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && !strings.ContainsRune(extra, r) {
			return false
		}
	}
	return true
}

// ---- sources ----

// refresh reloads the local sources from the snapshot at most every
// acSnapEvery.
func (ac *Autocomplete) refresh() {
	if ac.h == nil || ac.h.Core() == nil {
		return
	}
	if !ac.snapAt.IsZero() && time.Since(ac.snapAt) < acSnapEvery {
		return
	}
	ac.snapAt = time.Now()
	api := coreAPI{ac.h.Core()}
	s := api.snapshot()
	known := make(map[string]core.User, len(s.Users)+1)
	for id, u := range s.Users {
		known[id] = u
	}
	if s.Self.ID != "" {
		known[s.Self.ID] = s.Self
		ac.selfID = s.Self.ID
	}
	ac.teamName = s.Team.Name
	dm := map[string]bool{}
	convs := ac.convs[:0]
	for _, c := range s.Convs {
		switch c.Kind {
		case "im":
			if c.User != "" {
				dm[c.User] = true
			}
		case "mpim":
		default:
			convs = append(convs, c)
		}
		if c.ID == ac.channelID {
			ac.conv, ac.haveConv = c, true
		}
	}
	if !ac.haveConv {
		ac.conv, ac.haveConv = api.conv(ac.channelID)
	}
	ac.known, ac.dmUsers, ac.convs = known, dm, convs
}

// load starts the async loads trigger t needs.
func (ac *Autocomplete) load(t acTrigger) {
	if ac.h == nil || ac.h.Core() == nil {
		return
	}
	api := coreAPI{ac.h.Core()}
	ac.mu.Lock()
	defer ac.mu.Unlock()
	switch t.Kind {
	case acUser:
		if ac.members == nil && !ac.memLoading {
			ac.memLoading = true
			go ac.loadMembers(api)
		}
		if ac.groups == nil && !ac.grpLoading {
			ac.grpLoading = true
			go func() {
				gs, _ := api.usergroups(ac.ctx)
				if gs == nil {
					gs = []core.Usergroup{}
				}
				ac.land(func() { ac.groups = gs })
			}()
		}
		q := strings.ToLower(t.Query)
		if q != "" && !ac.askedUsers[q] {
			ac.askedUsers[q] = true
			ac.debounced(func() { delete(ac.askedUsers, q) }, func(ctx context.Context) {
				us, err := api.searchUsers(ctx, q)
				if err != nil {
					ac.land(func() { delete(ac.askedUsers, q) })
					return
				}
				ac.land(func() {
					for _, u := range us {
						ac.searched[u.ID] = u
					}
				})
			})
		}
	case acChannel:
		q := strings.ToLower(t.Query)
		if q != "" && !ac.askedChans[q] {
			ac.askedChans[q] = true
			ac.debounced(func() { delete(ac.askedChans, q) }, func(ctx context.Context) {
				hs, err := api.searchChannels(ctx, q)
				if err != nil {
					ac.land(func() { delete(ac.askedChans, q) })
					return
				}
				ac.land(func() {
					for _, h := range hs {
						ac.chanHits[h.ID] = h
					}
				})
			})
		}
	case acEmoji:
		if ac.emojiIdx == nil && !ac.emoLoading {
			ac.emoLoading = true
			go func() {
				idx := sharedEmojiIndex(api.customEmoji())
				ac.land(func() { ac.emojiIdx = idx })
			}()
		}
	case acCommand:
		if ac.commands == nil && !ac.cmdLoading {
			ac.cmdLoading = true
			go func() {
				cs, _ := api.commands(ac.ctx)
				if cs == nil {
					cs = []core.Command{}
				}
				ac.land(func() { ac.commands = cs })
			}()
		}
	}
}

// loadMembers fetches the current conversation's members. IMs and group
// DMs use the conversation's own member list.
func (ac *Autocomplete) loadMembers(api coreAPI) {
	c, ok := api.conv(ac.channelID)
	out := map[string]core.User{}
	switch {
	case ok && c.Kind == "im":
		out = api.users([]string{c.User})
	case ok && c.Kind == "mpim":
		out = api.users(c.Members)
	default:
		us, _ := api.memberUsers(ac.ctx, ac.channelID, 500)
		for _, u := range us {
			out[u.ID] = u
		}
	}
	ac.land(func() { ac.members = out })
}

// debounced runs f after acDebounce on a goroutine, cancelling the
// previous pending search. If it is cancelled before f starts, unask runs
// under mu so the query can be asked again. Called with mu held.
func (ac *Autocomplete) debounced(unask func(), f func(ctx context.Context)) {
	if ac.searchCancel != nil {
		ac.searchCancel()
	}
	ctx, cancel := context.WithCancel(ac.ctx)
	ac.searchCancel = cancel
	go func() {
		t := time.NewTimer(acDebounce)
		defer t.Stop()
		select {
		case <-t.C:
		case <-ctx.Done():
			ac.mu.Lock()
			unask()
			ac.mu.Unlock()
			return
		}
		f(ctx)
	}()
}

// land applies an async result under mu, bumps gen and repaints.
func (ac *Autocomplete) land(f func()) {
	ac.mu.Lock()
	f()
	ac.gen++
	ac.mu.Unlock()
	if ac.h != nil {
		ac.h.Invalidate()
	}
}

// compute builds the rows for t from the local and async sources.
func (ac *Autocomplete) compute(t acTrigger) []acItem {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	switch t.Kind {
	case acUser:
		users := make([]core.User, 0, len(ac.known)+len(ac.members)+len(ac.searched))
		seen := map[string]bool{}
		boost := map[string]bool{}
		add := func(u core.User) {
			if u.ID == "" || seen[u.ID] {
				return
			}
			seen[u.ID] = true
			users = append(users, u)
		}
		for id, u := range ac.members {
			boost[id] = true
			add(u)
		}
		for id := range ac.dmUsers {
			boost[id] = true
		}
		for _, u := range ac.known {
			add(u)
		}
		for _, u := range ac.searched {
			add(u)
		}
		items := rankUsers(t.Query, users, boost, ac.members, ac.selfID, ac.groups, acMaxItems)
		ac.resolveStatusEmoji(items)
		kind, general := "", false
		if ac.haveConv {
			kind, general = ac.conv.Kind, ac.conv.IsGeneral
		}
		return append(items, rankBroadcasts(t.Query, kind, general)...)
	case acChannel:
		hits := make([]core.ChannelHit, 0, len(ac.chanHits))
		for _, h := range ac.chanHits {
			hits = append(hits, h)
		}
		return rankChannels(t.Query, ac.convs, hits, acMaxItems)
	case acEmoji:
		if ac.emojiIdx == nil {
			return nil
		}
		return ac.emojiIdx.search(t.Query, acMaxItems)
	case acCommand:
		return rankCommands(t.Query, ac.commands, acMaxItems)
	}
	return nil
}

// resolveStatusEmoji fills StatusURL for users whose status emoji came as
// a name only. Called with mu held (it only reads core).
func (ac *Autocomplete) resolveStatusEmoji(items []acItem) {
	if ac.h == nil || ac.h.Core() == nil {
		return
	}
	api := coreAPI{ac.h.Core()}
	for i := range items {
		it := &items[i]
		if it.StatusURL != "" || it.statusName == "" {
			continue
		}
		if url, _, ok := api.emoji(strings.Trim(it.statusName, ":")); ok {
			it.StatusURL = url
		}
	}
}

// ---- ranking (pure) ----

// acNow is the clock for status expiry (tests replace it).
var acNow = time.Now

// Match tiers, best first.
const (
	tierExact = iota
	tierPrefix
	tierHandle
	tierWord
	tierSub
	tierNone
)

// userName is the name Slack shows for u.
func userName(u core.User) string {
	switch {
	case u.Name != "":
		return u.Name
	case u.RealName != "":
		return u.RealName
	}
	return u.Handle
}

// userTier rates how well q (lowercase) matches u.
func userTier(q string, u core.User) int {
	name, handle, real := strings.ToLower(userName(u)), strings.ToLower(u.Handle), strings.ToLower(u.RealName)
	// A trailing space ("@karim ", before the surname) shouldn't drop the
	// match; multi-token queries match word-prefixes across name+real+handle.
	q = strings.TrimRight(q, " ")
	if strings.Contains(q, " ") {
		if allWordsPrefix(name+" "+real+" "+handle, q) {
			return tierWord
		}
		if strings.Contains(name+" "+real, q) {
			return tierSub
		}
		return tierNone
	}
	switch {
	case q == "":
		return tierPrefix
	case name == q || handle == q:
		return tierExact
	case strings.HasPrefix(name, q):
		return tierPrefix
	case strings.HasPrefix(handle, q):
		return tierHandle
	case wordPrefix(real, q) || wordPrefix(name, q) || strings.HasPrefix(real, q):
		return tierWord
	case allWordsPrefix(name+" "+real+" "+handle, q):
		// Multi-word queries ("karim h", "hac karim"): every query token is
		// a word-prefix somewhere in the combined name/handle/real name.
		return tierWord
	case strings.Contains(name, q) || strings.Contains(handle, q) || strings.Contains(real, q):
		return tierSub
	}
	return tierNone
}

// allWordsPrefix reports whether every whitespace-separated token of q is a
// wordPrefix of s. Only useful for multi-token queries.
func allWordsPrefix(s, q string) bool {
	toks := strings.Fields(q)
	if len(toks) < 2 {
		return false
	}
	for _, t := range toks {
		if !wordPrefix(s, t) {
			return false
		}
	}
	return true
}

// wordPrefix reports whether some word of s (split on space, '-', '_',
// '.') starts with q.
func wordPrefix(s, q string) bool {
	if q == "" {
		return true
	}
	for i := 0; i < len(s); i++ {
		if (i == 0 || isWordSep(s[i-1])) && strings.HasPrefix(s[i:], q) {
			return true
		}
	}
	return false
}

func isWordSep(b byte) bool { return b == ' ' || b == '-' || b == '_' || b == '.' }

// rankUsers ranks people, bots and usergroups for an @ query. Members of
// the conversation come first (as in Slack), then by match tier: exact
// display-name/handle match, display-name prefix, handle prefix, real-name
// word prefix, substring. Within a tier people come before bots and
// usergroups, boosted users (DM partners) before the rest, then shorter
// names. Deleted users are dropped. An empty query lists only boosted
// users and members, then the viewer. members (nil while loading) marks
// "Not in channel"; selfID marks "(you)".
func rankUsers(q string, users []core.User, boost map[string]bool, members map[string]core.User, selfID string, groups []core.Usergroup, limit int) []acItem {
	q = strings.ToLower(q)
	type cand struct {
		it    acItem
		tier  int
		order int // 0 person, 1 bot, 2 usergroup
		boost bool
		self  bool
		key   string
	}
	cs := make([]cand, 0, 32)
	for _, u := range users {
		if u.Deleted || u.ID == "" {
			continue
		}
		if q == "" && !boost[u.ID] && u.ID != selfID {
			continue
		}
		tier := userTier(q, u)
		if tier == tierNone {
			continue
		}
		name := userName(u)
		_, mem := members[u.ID]
		order := 0
		if u.IsBot {
			order = 1
		}
		it := acItem{
			Kind: acUser, ID: u.ID, Name: name, Secondary: u.RealName, Avatar: u.Avatar,
			Presence: u.Presence, IsBot: u.IsBot, Member: mem, Self: u.ID == selfID,
			NotIn: members != nil && !mem && u.ID != selfID,
			Atom:  &Atom{Kind: AtomUser, ID: u.ID, Text: "@" + name},
		}
		if u.StatusExp == 0 || u.StatusExp > acNow().Unix() {
			it.Status, it.StatusURL, it.statusName = u.StatusText, u.StatusEmojiURL, u.StatusEmoji
		}
		cs = append(cs, cand{
			it: it, tier: tier, order: order, boost: boost[u.ID] || mem, self: it.Self,
			key: strings.ToLower(name),
		})
	}
	if q != "" {
		for _, g := range groups {
			h, n := strings.ToLower(g.Handle), strings.ToLower(g.Name)
			tier := tierNone
			switch {
			case h == q:
				tier = tierExact
			case strings.HasPrefix(h, q):
				tier = tierPrefix
			case wordPrefix(n, q) || wordPrefix(h, q):
				tier = tierWord
			case strings.Contains(h, q) || strings.Contains(n, q):
				tier = tierSub
			}
			if tier == tierNone {
				continue
			}
			name := g.Name
			if name == "" {
				name = g.Handle
			}
			cs = append(cs, cand{
				it: acItem{
					Kind: acUser, ID: g.ID, Name: name, Secondary: g.Handle, Count: g.UserCount,
					Atom: &Atom{Kind: AtomUsergroup, ID: g.ID, Text: "@" + g.Handle},
				},
				tier: tier, order: 2, key: h,
			})
		}
	}
	sort.SliceStable(cs, func(i, j int) bool {
		a, b := cs[i], cs[j]
		if q == "" && a.self != b.self {
			return b.self
		}
		if a.it.Member != b.it.Member {
			return a.it.Member
		}
		if a.tier != b.tier {
			return a.tier < b.tier
		}
		if a.order != b.order {
			return a.order < b.order
		}
		if a.boost != b.boost {
			return a.boost
		}
		if q != "" && len(a.key) != len(b.key) && a.tier <= tierHandle {
			return len(a.key) < len(b.key)
		}
		return a.key < b.key
	})
	if len(cs) > limit {
		cs = cs[:limit]
	}
	out := make([]acItem, len(cs))
	for i := range cs {
		out[i] = cs[i].it
	}
	return out
}

// rankBroadcasts lists @here, @channel and @everyone that start with q.
// IMs get none; @everyone only shows in the workspace's general channel.
func rankBroadcasts(q, convKind string, general bool) []acItem {
	if convKind == "im" {
		return nil
	}
	q = strings.ToLower(q)
	all := []struct{ id, hint string }{
		{"here", "Notify every online member in this channel."},
		{"channel", "Notify everyone in this channel."},
		{"everyone", "Notify everyone in your workspace."},
	}
	var out []acItem
	for _, b := range all {
		if b.id == "everyone" && !general {
			continue
		}
		if !strings.HasPrefix(b.id, q) {
			continue
		}
		out = append(out, acItem{
			Kind: acUser, ID: b.id, Name: b.id, Secondary: b.hint,
			Atom: &Atom{Kind: AtomBroadcast, ID: b.id, Text: "@" + b.id},
		})
	}
	return out
}

// rankChannels ranks channels for a # query: sidebar (member) channels and
// search hits. Tiers: exact, prefix, word prefix (split on '-', '_', '.'),
// substring; members before non-members, then shorter names; archived
// channels last. An empty query lists unarchived member channels by
// sidebar priority, then name.
func rankChannels(q string, convs []core.Conv, hits []core.ChannelHit, limit int) []acItem {
	q = strings.ToLower(strings.TrimPrefix(q, "#"))
	type cand struct {
		it   acItem
		tier int
		prio float64
		key  string
	}
	seen := map[string]bool{}
	var cs []cand
	add := func(id, name, kind string, member, archived bool, prio float64) {
		if id == "" || seen[id] || kind == "im" || kind == "mpim" || (archived && q == "") {
			return
		}
		n := strings.ToLower(name)
		tier := tierNone
		switch {
		case q == "":
			if !member {
				return
			}
			tier = tierPrefix
		case n == q:
			tier = tierExact
		case strings.HasPrefix(n, q):
			tier = tierPrefix
		case wordPrefix(n, q):
			tier = tierWord
		case strings.Contains(n, q):
			tier = tierSub
		}
		if tier == tierNone {
			return
		}
		seen[id] = true
		cs = append(cs, cand{
			it: acItem{
				Kind: acChannel, ID: id, Name: name, Private: kind == "private", Member: member, Archived: archived,
				Atom: &Atom{Kind: AtomChannel, ID: id, Text: "#" + name},
			},
			tier: tier, prio: prio, key: n,
		})
	}
	for _, c := range convs {
		add(c.ID, c.Name, c.Kind, true, c.IsArchived, c.Priority)
	}
	for _, h := range hits {
		add(h.ID, h.Name, h.Kind, h.IsMember, h.IsArchived, 0)
	}
	sort.SliceStable(cs, func(i, j int) bool {
		a, b := cs[i], cs[j]
		if a.it.Archived != b.it.Archived {
			return b.it.Archived
		}
		if a.tier != b.tier {
			return a.tier < b.tier
		}
		if a.it.Member != b.it.Member {
			return a.it.Member
		}
		if q == "" {
			if a.prio != b.prio {
				return a.prio > b.prio
			}
			return a.key < b.key
		}
		if len(a.key) != len(b.key) {
			return len(a.key) < len(b.key)
		}
		return a.key < b.key
	})
	if len(cs) > limit {
		cs = cs[:limit]
	}
	out := make([]acItem, len(cs))
	for i := range cs {
		out[i] = cs[i].it
	}
	return out
}

// rankCommands ranks slash commands: name prefix, then substring, then
// shorter names. Names are stored without the leading '/'.
func rankCommands(q string, cmds []core.Command, limit int) []acItem {
	q = strings.ToLower(q)
	type cand struct {
		it   acItem
		tier int
		key  string
	}
	var cs []cand
	seen := map[string]bool{}
	for i := range cmds {
		c := cmds[i]
		name := strings.TrimPrefix(c.Name, "/")
		n := strings.ToLower(name)
		if name == "" || seen[n] {
			continue
		}
		tier := tierNone
		switch {
		case n == q:
			tier = tierExact
		case strings.HasPrefix(n, q):
			tier = tierPrefix
		case strings.Contains(n, q):
			tier = tierSub
		}
		if tier == tierNone {
			continue
		}
		seen[n] = true
		cs = append(cs, cand{
			it:   acItem{Kind: acCommand, ID: name, Name: name, Secondary: c.Desc, Command: &c},
			tier: tier, key: n,
		})
	}
	sort.SliceStable(cs, func(i, j int) bool {
		a, b := cs[i], cs[j]
		if a.tier != b.tier {
			return a.tier < b.tier
		}
		if q != "" && len(a.key) != len(b.key) {
			return len(a.key) < len(b.key)
		}
		return a.key < b.key
	})
	if len(cs) > limit {
		cs = cs[:limit]
	}
	out := make([]acItem, len(cs))
	for i := range cs {
		out[i] = cs[i].it
	}
	return out
}

// ---- emoji index ----

type emojiEnt struct {
	name     string
	url, uni string
	custom   bool
}

// emojiIndex is every emoji name (standard aliases included), sorted, so a
// prefix lookup is a binary search.
type emojiIndex struct {
	ents  []emojiEnt
	names []string // ents[i].name
}

var emojiShared struct {
	sync.Mutex
	idx  *emojiIndex
	nCus int
}

// sharedEmojiIndex returns the process-wide index, rebuilding it when the
// custom emoji set changed size. Composers share it.
func sharedEmojiIndex(custom map[string]string) *emojiIndex {
	emojiShared.Lock()
	defer emojiShared.Unlock()
	if emojiShared.idx == nil || emojiShared.nCus != len(custom) {
		emojiShared.idx = buildEmojiIndex(emoji.StandardJSON, custom)
		emojiShared.nCus = len(custom)
	}
	return emojiShared.idx
}

// buildEmojiIndex parses the standard emoji JSON (names only; images and
// unicode come from emoji.Standard) and merges the custom map, resolving
// "alias:name" chains. Custom names win over standard ones.
func buildEmojiIndex(stdJSON []byte, custom map[string]string) *emojiIndex {
	var f struct {
		Emoji []struct {
			Names []string `json:"n"`
		} `json:"emoji"`
	}
	_ = json.Unmarshal(stdJSON, &f)
	byName := make(map[string]emojiEnt, len(f.Emoji)*2+len(custom))
	addStd := func(n string) {
		img, uni, ok := emoji.Standard(n)
		if !ok {
			return
		}
		byName[n] = emojiEnt{name: n, url: emoji.CDN + "apple-medium/" + img + ".png", uni: uni}
	}
	for _, e := range f.Emoji {
		for _, n := range e.Names {
			addStd(n)
		}
	}
	for n, v := range custom {
		target := n
		for i := 0; strings.HasPrefix(v, "alias:") && i < 5; i++ {
			target = strings.TrimPrefix(v, "alias:")
			nv, ok := custom[target]
			if !ok {
				v = ""
				break
			}
			v = nv
		}
		switch {
		case v != "" && !strings.HasPrefix(v, "alias:"):
			byName[n] = emojiEnt{name: n, url: v, custom: true}
		case v == "":
			if img, uni, ok := emoji.Standard(target); ok {
				byName[n] = emojiEnt{name: n, url: emoji.CDN + "apple-medium/" + img + ".png", uni: uni, custom: true}
			}
		}
	}
	idx := &emojiIndex{ents: make([]emojiEnt, 0, len(byName))}
	for _, e := range byName {
		idx.ents = append(idx.ents, e)
	}
	sort.Slice(idx.ents, func(i, j int) bool { return idx.ents[i].name < idx.ents[j].name })
	idx.names = make([]string, len(idx.ents))
	for i := range idx.ents {
		idx.names[i] = idx.ents[i].name
	}
	return idx
}

// emojiScanCap bounds the word-prefix/substring candidates collected per
// pass, so a short query over 65k custom emoji stays cheap.
const emojiScanCap = 400

// search returns up to limit emoji for q: exact name, then names starting
// with q (shorter first), then names with a word ('_', '-') starting with
// q, then any substring.
func (x *emojiIndex) search(q string, limit int) []acItem {
	q = strings.ToLower(strings.Trim(q, ":"))
	if q == "" || x == nil || limit <= 0 {
		return nil
	}
	lo := sort.SearchStrings(x.names, q)
	hi := lo
	for hi < len(x.names) && strings.HasPrefix(x.names[hi], q) {
		hi++
	}
	picked := make(map[int]bool, limit)
	var out []acItem
	take := func(idxs []int) {
		sort.SliceStable(idxs, func(i, j int) bool {
			a, b := x.names[idxs[i]], x.names[idxs[j]]
			if len(a) != len(b) {
				return len(a) < len(b)
			}
			return a < b
		})
		for _, i := range idxs {
			if len(out) >= limit {
				return
			}
			if picked[i] {
				continue
			}
			picked[i] = true
			e := x.ents[i]
			out = append(out, acItem{
				Kind: acEmoji, ID: e.name, Name: e.name, Avatar: e.url, Uni: e.uni,
				Atom: &Atom{Kind: AtomEmoji, ID: e.name, Text: ":" + e.name + ":", URL: e.url, Uni: e.uni},
			})
		}
	}
	// prefix range: keep the shortest names without sorting the whole range
	pre := make([]int, 0, min(hi-lo, limit*2))
	for i := lo; i < hi; i++ {
		pre = insertShortest(pre, i, x.names, limit)
	}
	take(pre)
	if len(out) >= limit {
		return out
	}
	var word, sub []int
	for i, n := range x.names {
		if i >= lo && i < hi {
			continue
		}
		if !strings.Contains(n, q) {
			continue
		}
		if wordPrefix(n, q) {
			if len(word) < emojiScanCap {
				word = append(word, i)
			}
		} else if len(sub) < emojiScanCap {
			sub = append(sub, i)
		}
		if len(word) >= emojiScanCap && len(sub) >= emojiScanCap {
			break
		}
	}
	take(word)
	take(sub)
	return out
}

// insertShortest keeps s as the (at most limit) shortest names seen,
// ordered by (length, name).
func insertShortest(s []int, i int, names []string, limit int) []int {
	less := func(a, b int) bool {
		if len(names[a]) != len(names[b]) {
			return len(names[a]) < len(names[b])
		}
		return names[a] < names[b]
	}
	if len(s) == limit && !less(i, s[len(s)-1]) {
		return s
	}
	j := sort.Search(len(s), func(k int) bool { return less(i, s[k]) })
	if len(s) < limit {
		s = append(s, 0)
	}
	copy(s[j+1:], s[j:len(s)-1])
	s[j] = i
	return s
}

// watchPresence subscribes to presence for the user rows on screen, so the
// dots fill in (Slack shows away rings until presence is known).
func (ac *Autocomplete) watchPresence() {
	var ids []string
	for i, it := range ac.items {
		if i >= 12 {
			break
		}
		if it.Kind == acUser && it.Atom != nil && it.Atom.Kind == AtomUser && !it.IsBot && it.Presence == "" {
			ids = append(ids, it.ID)
		}
	}
	if len(ids) == 0 || ac.h == nil || ac.h.Core() == nil {
		return
	}
	go coreAPI{ac.h.Core()}.subscribePresence(ids)
}
