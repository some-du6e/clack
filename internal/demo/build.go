package demo

// Build reads the demo's content (data/*.json, data/history/*.json,
// data/canvases/*.md) and resolves it into a Workspace: handles become
// user ids, "@handle" and "#channel" in text become Slack's <@U…> and
// <#C…|name>, relative times become real timestamps against now, and
// image keys become URLs on the fake that Image draws.
//
// History files are written oldest first, one message per entry:
//
//	{"at": "-3 09:12", "by": "gordon", "text": "…",
//	 "reactions": ["joy priya dale"], "edited": "+4m", "pinned": "marisol",
//	 "files": [{"image": "laminator", "name": "lamby.png"}],
//	 "replies": [{"at": "+6m", "by": "priya", "text": "…"}]}
//
// "at" is "-N HH:MM" (N workdays before today, at that clock time in the
// sender's timezone), "40m" / "1h15m" (that long before now) or, in a
// reply, "+6m" (after its parent). Files are {"image": key}, {"pdf": key},
// {"canvas": id} or {"list": id}; bots may carry raw Block Kit "blocks".

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/canvas"
	"github.com/some-du6e/yoloslackgoclient/internal/fakeslack"
	"github.com/some-du6e/yoloslackgoclient/internal/lists"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
)

//go:embed data/people.json data/channels.json data/sections.json data/emoji.json data/canvases.json data/lists.json data/scheduled.json data/history/*.json data/canvases/*.md
var content embed.FS

// Content ids other parts of the demo refer to.
const (
	PartyCanvasID   = "F0DEMOCANV1" // Holiday Party Plan, shared in #party-planning-crew
	PlanCanvasID    = "F0DEMOPLAN1" // Inkwell's plan canvas (a code channel tab)
	RestockListID   = "F0DEMOLIST1" // Supply Closet Restock, shared in #general
	CodeDiffFileID  = "F0DEMODIFF1" // the code channel's diff_file_id
	CodeSummaryView = "V0DEMOSUMM"  // the code channel's block_kit view
	CodePRView      = "V0DEMOPR217" // the code channel's pull_request view
)

// branchTZ is the Wexley Falls branch's timezone: "today" and workdays
// are counted there.
const branchTZ = "America/New_York"

type convSpec struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Kind     string   `json:"kind"` // channel, private, code, im, mpim
	General  bool     `json:"general"`
	Creator  string   `json:"creator"`
	AgeDays  int      `json:"age_days"`
	Created  string   `json:"created"` // an "at" time, instead of AgeDays
	Members  []string `json:"members"` // nil: everyone (channels)
	User     string   `json:"user"`    // im
	Topic    string   `json:"topic"`
	Purpose  string   `json:"purpose"`
	Unread   int      `json:"unread"` // the newest n top-level messages are unread
	Mentions int      `json:"mentions"`
}

type msgSpec struct {
	At        string          `json:"at"`
	By        string          `json:"by"`
	Text      string          `json:"text"`
	Reactions []string        `json:"reactions"`
	Edited    string          `json:"edited"`
	Pinned    string          `json:"pinned"`
	Files     []fileSpec      `json:"files"`
	Blocks    json.RawMessage `json:"blocks"`
	Subtype   string          `json:"subtype"`
	Replies   []msgSpec       `json:"replies"`
}

type fileSpec struct {
	Image  string `json:"image"`
	PDF    string `json:"pdf"`
	Canvas string `json:"canvas"`
	List   string `json:"list"`
	Name   string `json:"name"`
	Title  string `json:"title"`
	Pages  int    `json:"pages"`
}

type sectionSpec struct {
	ID, Name, Emoji, Type string
	Channels              []string
}

type canvasSpec struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	File    string `json:"file"`
	Creator string `json:"creator"`
	AgeDays int    `json:"age_days"`
	Updated string `json:"updated"`
}

type listSpec struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Creator string `json:"creator"`
	Channel string `json:"channel"`
	AgeDays int    `json:"age_days"`
	Updated string `json:"updated"`
	Icon    string `json:"icon"`
	Items   []struct {
		Task string `json:"task"`
		Done bool   `json:"done"`
		Who  string `json:"who"`
		Due  int    `json:"due"` // days from today
	} `json:"items"`
}

type schedSpec struct {
	Channel string `json:"channel"`
	Thread  string `json:"thread"`
	At      string `json:"at"` // "tomorrow HH:MM" or "+N HH:MM" (N workdays ahead)
	Created string `json:"created"`
	Text    string `json:"text"`
}

// builder holds what Build has resolved so far.
type builder struct {
	base string
	now  time.Time
	ws   *Workspace

	byHandle map[string]*Person
	locs     map[string]*time.Location
	convID   map[string]string // channel name, "dm-<handle>", "group" -> id
	convName map[string]string // channel id -> name
	nfile    int

	pending []*pendingMsg // every message, for ts assignment
}

// pendingMsg is a message whose time is known but whose ts isn't yet.
type pendingMsg struct {
	ch     string
	t      time.Time
	seq    int
	msg    *slack.Message
	edited time.Duration
	pinner string
	parent *pendingMsg
}

// Build loads data/ into a Workspace whose URLs point at base (the fake's
// URL, ending in "/"). The result depends only on base and now.
func Build(base string, now time.Time) (*Workspace, error) {
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	b := &builder{base: base, now: now, byHandle: map[string]*Person{}, locs: map[string]*time.Location{},
		convID: map[string]string{}, convName: map[string]string{}}
	b.ws = &Workspace{Now: now, Base: base, History: map[string][]slack.Message{},
		Replies: map[string]map[string][]slack.Message{}, Pins: map[string][]string{}, Unread: map[string]Unread{}}
	steps := []func() error{b.people, b.emoji, b.convs, b.histories, b.timestamps, b.sections, b.canvases, b.lists, b.scheduled}
	for _, f := range steps {
		if err := f(); err != nil {
			return nil, err
		}
	}
	return b.ws, nil
}

func readJSON(name string, v any) error {
	raw, err := content.ReadFile("data/" + name)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields() // a typo in the data is an error, not a silently missing field
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("data/%s: %w", name, err)
	}
	return nil
}

// ---- people ----

var castOnce struct {
	people []Person
	err    error
	done   bool
}

// cast is data/people.json (Image needs the avatars without a Build).
func cast() ([]Person, error) {
	artMu.Lock()
	defer artMu.Unlock()
	if !castOnce.done {
		castOnce.err = readJSON("people.json", &castOnce.people)
		castOnce.done = true
	}
	return castOnce.people, castOnce.err
}

// avatarSpec is a cast member's avatar by handle.
func avatarSpec(handle string) (Avatar, bool) {
	ps, _ := cast()
	for _, p := range ps {
		if p.Handle == handle {
			return p.Avatar, true
		}
	}
	return Avatar{}, false
}

func (b *builder) avatar(handle string, px int) string {
	return b.base + "img/" + imgAvatar + handle + "/" + strconv.Itoa(px) + ".png"
}

func (b *builder) people() error {
	ps, err := cast()
	if err != nil {
		return err
	}
	b.ws.People = append([]Person(nil), ps...)
	for i := range b.ws.People {
		p := &b.ws.People[i]
		if p.TZ == "" {
			p.TZ = branchTZ
		}
		if _, dup := b.byHandle[p.Handle]; dup {
			return fmt.Errorf("people.json: %s twice", p.Handle)
		}
		b.byHandle[p.Handle] = p
		b.locs[p.Handle] = location(p.TZ)
		u := b.user(p)
		b.ws.Users = append(b.ws.Users, u)
		if p.Self {
			b.ws.Self = u
		}
		if p.Bot {
			b.ws.Bots = append(b.ws.Bots, b.bot(p))
		}
	}
	if b.ws.Self.ID != SelfID {
		return errors.New("people.json: the viewer must be " + SelfID)
	}
	return nil
}

func (b *builder) user(p *Person) slack.User {
	h := fnv.New64a()
	h.Write([]byte(p.Handle))
	first, last, _ := strings.Cut(p.Name, " ")
	loc := b.locs[p.Handle]
	abbr, off := b.now.In(loc).Zone()
	u := slack.User{ID: p.ID, TeamID: TeamID, Name: p.Handle, RealName: p.Name, Color: strings.TrimPrefix(p.Avatar.Bg, "#"),
		TZ: p.TZ, TZLabel: tzLabel(abbr, p.TZ), TZOffset: off, IsAdmin: p.Admin, IsBot: p.Bot, Updated: b.now.Add(-30 * 24 * time.Hour).Unix(),
		Profile: slack.Profile{Title: p.Title, RealName: p.Name, FirstName: first, LastName: last, Pronouns: p.Pronouns,
			StatusText: p.Status.Text, StatusEmoji: p.Status.Emoji, AvatarHash: fmt.Sprintf("%012x", h.Sum64()>>16),
			Image24: b.avatar(p.Handle, 24), Image32: b.avatar(p.Handle, 32), Image48: b.avatar(p.Handle, 48),
			Image72: b.avatar(p.Handle, 72), Image192: b.avatar(p.Handle, 192), Image512: b.avatar(p.Handle, 512),
			ImageOriginal: b.avatar(p.Handle, 1024), IsCustomImage: true, Team: TeamID, Fields: json.RawMessage(`{}`)}}
	if !p.Bot {
		u.Profile.Email = p.Handle + "@quillfeather.example"
	}
	switch p.ID {
	case InkwellID:
		u.Profile.BotID, u.Profile.APIAppID, u.AgentStatus = InkwellBotID, InkwellAppID, "consistently_an_agent"
	case PulseID:
		u.Profile.BotID, u.Profile.APIAppID = PulseBotID, PulseAppID
	}
	return u
}

func (b *builder) bot(p *Person) Bot {
	bt := Bot{UserID: p.ID, Name: p.Name, Desc: p.Persona, Color: strings.TrimPrefix(p.Avatar.Bg, "#"), Icons: map[string]string{}}
	switch p.ID {
	case InkwellID:
		bt.BotID, bt.AppID = InkwellBotID, InkwellAppID
	case PulseID:
		bt.BotID, bt.AppID = PulseBotID, PulseAppID
	}
	for _, px := range []int{36, 48, 72, 192, 512} {
		bt.Icons["image_"+strconv.Itoa(px)] = b.avatar(p.Handle, px)
	}
	return bt
}

// botProfile is what a bot's messages carry (fakeslack.AgentMessage's shape).
func (b *builder) botProfile(p *Person) *slack.BotProfile {
	bt := b.bot(p)
	return &slack.BotProfile{ID: bt.BotID, Name: p.Name, AppID: bt.AppID, UserID: p.ID, TeamID: TeamID,
		Icons: slack.Icons{Image36: bt.Icons["image_36"], Image48: bt.Icons["image_48"], Image72: bt.Icons["image_72"]}}
}

// zones is the fallback when the system has no tz database (Windows
// without Go's embedded one): standard offsets, no daylight saving.
var zones = map[string]struct {
	abbr string
	off  int
}{
	"America/New_York": {"EST", -5 * 3600}, "America/Chicago": {"CST", -6 * 3600},
	"America/Denver": {"MST", -7 * 3600}, "America/Los_Angeles": {"PST", -8 * 3600},
	"Europe/London": {"GMT", 0},
}

func location(name string) *time.Location {
	if l, err := time.LoadLocation(name); err == nil {
		return l
	}
	if z, ok := zones[name]; ok {
		return time.FixedZone(z.abbr, z.off)
	}
	return time.UTC
}

func tzLabel(abbr, name string) string {
	labels := map[string]string{"EST": "Eastern Standard Time", "EDT": "Eastern Daylight Time",
		"CST": "Central Standard Time", "CDT": "Central Daylight Time", "MST": "Mountain Standard Time",
		"MDT": "Mountain Daylight Time", "PST": "Pacific Standard Time", "PDT": "Pacific Daylight Time",
		"GMT": "Greenwich Mean Time", "BST": "British Summer Time"}
	if l, ok := labels[abbr]; ok {
		return l
	}
	return name
}

func (b *builder) id(handle string) (string, error) {
	p, ok := b.byHandle[handle]
	if !ok {
		return "", fmt.Errorf("unknown handle %q", handle)
	}
	return p.ID, nil
}

// ---- emoji ----

func (b *builder) emoji() error {
	var e struct {
		Aliases map[string]string `json:"aliases"`
	}
	if err := readJSON("emoji.json", &e); err != nil {
		return err
	}
	b.ws.Emoji = map[string]string{}
	for _, n := range customEmoji {
		b.ws.Emoji[n] = b.base + "img/" + imgEmoji + n + ".png"
	}
	for a, n := range e.Aliases {
		if _, ok := b.ws.Emoji[n]; !ok {
			return fmt.Errorf("emoji.json: alias %s of unknown %s", a, n)
		}
		b.ws.Emoji[a] = "alias:" + n
	}
	return nil
}

// ---- conversations ----

func (b *builder) convs() error {
	var specs []convSpec
	if err := readJSON("channels.json", &specs); err != nil {
		return err
	}
	var humans []string
	for _, p := range b.ws.People {
		if !p.Bot {
			humans = append(humans, p.Handle)
		}
	}
	for _, s := range specs {
		switch s.Kind {
		case "im":
			b.convID["dm-"+s.User] = s.ID
		case "mpim":
			b.convID["group"] = s.ID
		default:
			b.convID[s.Name] = s.ID
		}
		b.convName[s.ID] = s.Name
	}
	for _, s := range specs {
		members := s.Members
		if members == nil && s.Kind != "im" {
			members = humans
		}
		var ids []string
		for _, h := range members {
			id, err := b.id(h)
			if err != nil {
				return fmt.Errorf("channels.json %s: %w", s.ID, err)
			}
			ids = append(ids, id)
		}
		created := b.now.Add(-time.Duration(s.AgeDays) * 24 * time.Hour).Truncate(time.Hour)
		if s.Created != "" {
			t, err := b.at(s.Created, s.Creator, time.Time{})
			if err != nil {
				return fmt.Errorf("channels.json %s: %w", s.ID, err)
			}
			created = t
		}
		creator := ""
		if s.Creator != "" {
			var err error
			if creator, err = b.id(s.Creator); err != nil {
				return fmt.Errorf("channels.json %s: %w", s.ID, err)
			}
		}
		c := slack.Conversation{ID: s.ID, Name: s.Name, Created: created.Unix(), Creator: creator, ContextTeamID: TeamID}
		purpose := func(v string) *slack.Purpose {
			return &slack.Purpose{Value: v, Creator: creator, LastSet: created.Unix()}
		}
		switch s.Kind {
		case "channel", "private", "code":
			c.IsChannel, c.IsMember, c.IsGeneral, c.IsPrivate = true, true, s.General, s.Kind == "private"
			c.NumMembers, c.Members = len(ids), ids
			c.Topic, c.Purpose = purpose(s.Topic), purpose(s.Purpose)
			if s.Kind == "code" {
				c.Properties = b.codeProps()
			}
		case "im":
			uid, err := b.id(s.User)
			if err != nil {
				return fmt.Errorf("channels.json %s: %w", s.ID, err)
			}
			c.IsIM, c.IsOpen, c.User, c.Creator = true, true, uid, ""
		case "mpim":
			c.IsMPIM, c.IsGroup, c.IsPrivate, c.IsOpen, c.IsMember = true, true, true, true, true
			c.NumMembers, c.Members = len(ids), ids
			var at []string
			for _, h := range members {
				at = append(at, "@"+h)
			}
			c.Purpose = purpose("Group messaging with: " + strings.Join(at, " "))
		default:
			return fmt.Errorf("channels.json %s: kind %q", s.ID, s.Kind)
		}
		b.ws.Convs = append(b.ws.Convs, c)
		b.ws.Unread[s.ID] = Unread{Mentions: s.Mentions, LastRead: strconv.Itoa(s.Unread)} // the count, resolved in timestamps
	}
	return nil
}

// codeProps is the code channel's properties, after fakeslack's
// CodeChannelFixtures "working" channel: an agent session that's
// processing, the PR in the context bar, a summary and a PR view, the
// diff and Inkwell's plan canvas as tabs.
func (b *builder) codeProps() json.RawMessage {
	cc := map[string]any{"repo": DemoRepo, "branch": DemoBranch, "pr_status": "open", "ci_state": "pending",
		"pr_title": DemoPRTitle,
		"context_bar_items": []map[string]any{
			{"key": "pr", "label": "PR #217", "icon": "branch", "url": DemoPRURL},
			{"key": "preview", "label": "Preview deploy", "icon": "globe", "url": "https://order-portal-git-" + DemoBranch + ".quillfeather.example"},
			{"key": "rerun", "item_type": "action", "label": "Re-run checks", "icon": "search"},
		},
		"agent_session_views": []map[string]any{
			{"view_id": CodeSummaryView, "type": "block_kit", "name": "Summary", "content_version": 1},
			{"view_id": CodePRView, "type": "pull_request", "pr_url": DemoPRURL, "name": "#217"},
		},
		"diff_file_id": CodeDiffFileID, "diff_file_version": 2,
		"plan_canvases": []map[string]any{{"canvas_id": PlanCanvasID, "date_tabbed": b.now.Add(-20 * time.Hour).Unix()}},
	}
	props := map[string]any{
		"record_channel": map[string]any{"record_id": "AC:4815162342001:900100200300:" + CodeID, "record_type": "agent_channel",
			"record_label": "Code Channel", "record_label_plural": "Code Channels"},
		"code_channel": cc,
		"agent_session": map[string]any{"status": "processing", "title": DemoPRTitle, "agent_bot_user_ids": []any{},
			"encoded_agent_bot_user_ids": []string{InkwellID}, "date_status_processing_expire": b.now.Add(time.Hour).Unix()},
		"tabz": []map[string]any{
			{"id": "code", "type": "code", "label": "Code"},
			{"id": "Ct" + PlanCanvasID, "type": "canvas", "label": "", "data": map[string]any{"file_id": PlanCanvasID}},
		},
	}
	out, _ := json.Marshal(props)
	return out
}

// ---- times ----

var (
	agoRE     = regexp.MustCompile(`^(\d+h)?(\d+m)?$`)
	workdayRE = regexp.MustCompile(`^-(\d+) (\d{1,2}):(\d{2})$`)
	afterRE   = regexp.MustCompile(`^\+((?:\d+h)?(?:\d+m)?)$`)
)

// workday is n workdays before (n<0: after) the branch's today.
func (b *builder) workday(n int) time.Time {
	d := b.now.In(location(branchTZ))
	step := -1
	if n < 0 {
		step, n = 1, -n
	}
	for i := 0; i < n; {
		d = d.AddDate(0, 0, step)
		if wd := d.Weekday(); wd != time.Saturday && wd != time.Sunday {
			i++
		}
	}
	return d
}

// at resolves a time spec for a message by handle; parent is the thread
// parent's time for "+…".
func (b *builder) at(spec, by string, parent time.Time) (time.Time, error) {
	if m := workdayRE.FindStringSubmatch(spec); m != nil {
		n, _ := strconv.Atoi(m[1])
		hh, _ := strconv.Atoi(m[2])
		mm, _ := strconv.Atoi(m[3])
		if n < 1 || hh > 23 || mm > 59 {
			return time.Time{}, fmt.Errorf("bad time %q", spec)
		}
		d := b.workday(n)
		loc := b.locs[by]
		if loc == nil {
			loc = location(branchTZ)
		}
		return time.Date(d.Year(), d.Month(), d.Day(), hh, mm, 0, 0, loc), nil
	}
	if m := afterRE.FindStringSubmatch(spec); m != nil && m[1] != "" {
		if parent.IsZero() {
			return time.Time{}, fmt.Errorf("%q outside a thread", spec)
		}
		d, err := time.ParseDuration(m[1])
		if err != nil {
			return time.Time{}, err
		}
		return parent.Add(d), nil
	}
	if spec != "" && agoRE.MatchString(spec) {
		d, err := time.ParseDuration(spec)
		if err != nil {
			return time.Time{}, err
		}
		return b.now.Add(-d), nil
	}
	return time.Time{}, fmt.Errorf("bad time %q", spec)
}

// jitter is a stable few seconds so minutes don't all start at :00.
func jitter(s string, n int) time.Duration {
	h := fnv.New32a()
	h.Write([]byte(s))
	return time.Duration(h.Sum32()%uint32(n)) * time.Second
}

// ---- history ----

var (
	mentionRE = regexp.MustCompile(`(^|[^\w<@])@([a-z][a-z0-9_-]*)`)
	chanRE    = regexp.MustCompile(`(^|[\s(])#([a-z0-9][a-z0-9_-]*[a-z0-9])`)
)

// resolveText turns "@handle", "@channel", "@here" and "#channel" into
// Slack's escapes; unknown names stay as written.
func (b *builder) resolveText(s string) string {
	s = mentionRE.ReplaceAllStringFunc(s, func(m string) string {
		sm := mentionRE.FindStringSubmatch(m)
		switch name := sm[2]; name {
		case "channel", "here", "everyone":
			return sm[1] + "<!" + name + ">"
		default:
			if p, ok := b.byHandle[name]; ok {
				return sm[1] + "<@" + p.ID + ">"
			}
		}
		return m
	})
	return chanRE.ReplaceAllStringFunc(s, func(m string) string {
		sm := chanRE.FindStringSubmatch(m)
		if id, ok := b.convID[sm[2]]; ok && !strings.HasPrefix(id, "D") && !strings.HasPrefix(id, "G") {
			return sm[1] + "<#" + id + "|" + sm[2] + ">"
		}
		return m
	})
}

// historyFile is a conversation's history file name.
func historyFile(c slack.Conversation, users map[string]string) string {
	switch {
	case c.IsIM:
		return "dm-" + users[c.User]
	case c.IsMPIM:
		return "group"
	}
	return c.Name
}

func (b *builder) histories() error {
	handles := map[string]string{}
	for _, p := range b.ws.People {
		handles[p.ID] = p.Handle
	}
	seq := 0
	for _, c := range b.ws.Convs {
		name := "history/" + historyFile(c, handles) + ".json"
		var specs []msgSpec
		if err := readJSON(name, &specs); err != nil {
			return err
		}
		for _, s := range specs {
			parent, err := b.message(c.ID, s, nil, &seq)
			if err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
			for _, r := range s.Replies {
				if len(r.Replies) > 0 {
					return fmt.Errorf("%s: nested replies", name)
				}
				if _, err := b.message(c.ID, r, parent, &seq); err != nil {
					return fmt.Errorf("%s: %w", name, err)
				}
			}
		}
	}
	return nil
}

func (b *builder) message(ch string, s msgSpec, parent *pendingMsg, seq *int) (*pendingMsg, error) {
	p, ok := b.byHandle[s.By]
	if !ok {
		return nil, fmt.Errorf("unknown handle %q", s.By)
	}
	var pt time.Time
	if parent != nil {
		pt = parent.t
	}
	t, err := b.at(s.At, s.By, pt)
	if err != nil {
		return nil, err
	}
	t = t.Add(jitter(s.Text+s.At, 50))
	*seq++
	m := &slack.Message{Type: "message", User: p.ID, Text: b.resolveText(s.Text), Subtype: s.Subtype}
	if p.Bot {
		m.BotID = b.bot(p).BotID
		m.BotProfile = b.botProfile(p)
	} else {
		h := fnv.New64a()
		h.Write([]byte(fmt.Sprint(ch, *seq, s.Text)))
		x := h.Sum64()
		m.ClientMsgID = fmt.Sprintf("%08x-%04x-4%03x-a%03x-%012x", uint32(x>>32), uint16(x>>16), uint16(x)&0xfff, uint16(x>>4)&0xfff, x&0xffffffffffff)
	}
	if len(s.Blocks) > 0 {
		if !json.Valid(s.Blocks) {
			return nil, errors.New("invalid blocks")
		}
		m.Blocks = s.Blocks
	}
	for _, r := range s.Reactions {
		f := strings.Fields(r)
		if len(f) < 2 {
			return nil, fmt.Errorf("reaction %q has nobody", r)
		}
		rc := slack.Reaction{Name: strings.Trim(f[0], ":")}
		for _, h := range f[1:] {
			id, err := b.id(h)
			if err != nil {
				return nil, err
			}
			rc.Users = append(rc.Users, id)
		}
		rc.Count = len(rc.Users)
		m.Reactions = append(m.Reactions, rc)
	}
	for _, fs := range s.Files {
		f, err := b.file(fs, p.ID, t)
		if err != nil {
			return nil, err
		}
		m.Files = append(m.Files, f)
	}
	if len(m.Files) > 0 && m.Subtype == "" && !p.Bot {
		m.Upload = true
	}
	pm := &pendingMsg{ch: ch, t: t, seq: *seq, msg: m, parent: parent}
	if s.Edited != "" {
		d, err := time.ParseDuration(strings.TrimPrefix(s.Edited, "+"))
		if err != nil {
			return nil, fmt.Errorf("edited %q: %w", s.Edited, err)
		}
		pm.edited = d
	}
	if s.Pinned != "" {
		if _, err := b.id(s.Pinned); err != nil {
			return nil, err
		}
		pm.pinner = s.Pinned
	}
	b.pending = append(b.pending, pm)
	return pm, nil
}

// file is a message's file object.
func (b *builder) file(fs fileSpec, user string, t time.Time) (slack.File, error) {
	b.nfile++
	switch {
	case fs.Image != "":
		sz, ok := fileSizes[fs.Image]
		if !ok {
			return slack.File{}, fmt.Errorf("no picture %q", fs.Image)
		}
		id := fmt.Sprintf("F0DEMOIMG%02d", b.nfile)
		name := dflt(fs.Name, fs.Image+".png")
		f := b.imageFile(id, fs.Image, name, sz[0], sz[1])
		f.Title, f.User, f.Created = dflt(fs.Title, name), user, t.Unix()
		return f, nil
	case fs.PDF != "":
		sz, ok := fileSizes[fs.PDF]
		if !ok {
			return slack.File{}, fmt.Errorf("no picture %q", fs.PDF)
		}
		id := fmt.Sprintf("F0DEMOPDF%02d", b.nfile)
		name := dflt(fs.Name, fs.PDF+".pdf")
		return slack.File{ID: id, Name: name, Title: dflt(fs.Title, name), Mimetype: "application/pdf", Filetype: "pdf",
			PrettyType: "PDF", Size: int64(max(1, fs.Pages)) * 61 << 10, Mode: "hosted", User: user, Created: t.Unix(),
			ThumbPDF: b.fileURL(fs.PDF, sz[0], sz[1]), ThumbPDFW: sz[0], ThumbPDFH: sz[1],
			URLPrivate: b.base + "files-pri/" + TeamID + "-" + id + "/" + name, URLPrivateDownload: b.base + "files-pri/" + TeamID + "-" + id + "/download/" + name,
			Permalink: b.base + "files/" + id + "/" + name}, nil
	case fs.Canvas != "":
		// the shape fakeslack's canvasFile gives a shared canvas
		return slack.File{ID: fs.Canvas, Mode: "quip", Filetype: "quip", Mimetype: "application/vnd.slack-docs", PrettyType: "Canvas",
			User: user, Created: t.Unix(),
			URLPrivate:         b.base + "files-pri/" + TeamID + "-" + fs.Canvas + "/canvas",
			URLPrivateDownload: b.base + "files-pri/" + TeamID + "-" + fs.Canvas + "/download/canvas",
			Permalink:          b.base + "docs/" + TeamID + "/" + fs.Canvas}, nil // title filled in by canvases()
	case fs.List != "":
		return slack.File{ID: fs.List, Mode: "list", Filetype: "list", Mimetype: "application/vnd.slack-list", PrettyType: "List",
			User: user, Created: t.Unix(), Permalink: b.base + "lists/" + TeamID + "/" + fs.List}, nil // title filled in by lists()
	}
	return slack.File{}, errors.New("a file needs image, pdf, canvas or list")
}

func (b *builder) fileURL(key string, w, h int) string {
	return b.base + "img/" + imgFile + key + "/" + strconv.Itoa(w) + "x" + strconv.Itoa(h) + ".png"
}

// imageFile is an image file with Slack's thumbs, like fakeslack's
// fakeImage, drawn by art_files.go.
func (b *builder) imageFile(id, key, name string, w, h int) slack.File {
	fit := func(side int) (int, int) {
		if w >= h {
			return side, max(1, side*h/w)
		}
		return max(1, side*w/h), side
	}
	u := b.fileURL(key, w, h)
	f := slack.File{ID: id, Name: name, Mimetype: "image/png", Filetype: "png", PrettyType: "PNG",
		Size: int64(w * h / 3), Mode: "hosted", URLPrivate: u, URLPrivateDownload: u,
		Permalink: b.base + "files/" + id + "/" + name, OriginalW: w, OriginalH: h}
	f.Thumb360W, f.Thumb360H = fit(360)
	f.Thumb360 = b.fileURL(key, f.Thumb360W, f.Thumb360H)
	f.Thumb480W, f.Thumb480H = fit(480)
	f.Thumb480 = b.fileURL(key, f.Thumb480W, f.Thumb480H)
	f.Thumb720W, f.Thumb720H = fit(720)
	f.Thumb720 = b.fileURL(key, f.Thumb720W, f.Thumb720H)
	if max(w, h) > 720 {
		f.Thumb1024W, f.Thumb1024H = fit(min(1024, max(w, h)))
		f.Thumb1024 = b.fileURL(key, f.Thumb1024W, f.Thumb1024H)
	}
	return f
}

func dflt(s, def string) string {
	if s != "" {
		return s
	}
	return def
}

// timestamps gives every message its ts: in time order, all channels
// together, with a serial in the microseconds so no two are equal. It
// then files them into History (newest first) and Replies (oldest
// first), and fills in threads, edits, pins and read states.
func (b *builder) timestamps() error {
	latest := b.now.Add(-5 * time.Second)
	for _, pm := range b.pending {
		if pm.parent != nil && !pm.t.After(pm.parent.t) {
			pm.t = pm.parent.t.Add(30 * time.Second)
		}
		if pm.t.After(latest) {
			pm.t = latest // never in the future, whatever the data says
		}
	}
	all := append([]*pendingMsg(nil), b.pending...)
	sort.SliceStable(all, func(i, j int) bool {
		if !all[i].t.Equal(all[j].t) {
			return all[i].t.Before(all[j].t)
		}
		// a parent before its replies, then file order
		return all[i].seq < all[j].seq
	})
	for i, pm := range all {
		pm.msg.TS = fmt.Sprintf("%d.%06d", pm.t.Unix(), 100+i)
	}
	for _, pm := range all {
		if pm.parent != nil && tsLess(pm.msg.TS, pm.parent.msg.TS) {
			return fmt.Errorf("%s: a reply sorts before its parent", pm.ch)
		}
	}
	for _, pm := range all {
		m := pm.msg
		if pm.edited > 0 {
			et := pm.t.Add(pm.edited)
			if et.After(latest) {
				et = latest
			}
			m.Edited = &slack.Edited{User: m.User, TS: fmt.Sprintf("%d.000000", et.Unix())}
		}
		if pm.pinner != "" {
			who, _ := b.id(pm.pinner)
			m.PinnedTo = []string{pm.ch}
			m.PinnedInfo = &slack.PinnedInfo{Channel: pm.ch, PinnedBy: who, PinnedTS: pm.t.Add(10 * time.Minute).Unix()}
			b.ws.Pins[pm.ch] = append(b.ws.Pins[pm.ch], m.TS)
		}
	}
	// threads: replies oldest first; the parent learns its replies
	for _, pm := range all {
		if pm.parent == nil {
			continue
		}
		par := pm.parent.msg
		pm.msg.ThreadTS, pm.msg.ParentUser = par.TS, par.User
		par.ThreadTS = par.TS
		par.ReplyCount++
		par.LatestReply = pm.msg.TS
		if !contains(par.ReplyUsers, pm.msg.User) {
			par.ReplyUsers = append(par.ReplyUsers, pm.msg.User)
		}
		if pm.msg.User == SelfID || par.User == SelfID || strings.Contains(par.Text, "<@"+SelfID+">") {
			par.Subscribed = true
		}
		if pm.msg.User == SelfID {
			par.Subscribed = true
		}
	}
	for _, pm := range all {
		if pm.parent == nil {
			continue
		}
		if b.ws.Replies[pm.ch] == nil {
			b.ws.Replies[pm.ch] = map[string][]slack.Message{}
		}
		b.ws.Replies[pm.ch][pm.msg.ThreadTS] = append(b.ws.Replies[pm.ch][pm.msg.ThreadTS], *pm.msg)
	}
	for i := len(all) - 1; i >= 0; i-- {
		if pm := all[i]; pm.parent == nil {
			b.ws.History[pm.ch] = append(b.ws.History[pm.ch], *pm.msg)
		}
	}
	// read states: the newest n top-level messages are unread
	for _, c := range b.ws.Convs {
		u := b.ws.Unread[c.ID]
		n, _ := strconv.Atoi(u.LastRead)
		h := b.ws.History[c.ID]
		switch {
		case len(h) == 0:
			u.LastRead = ""
		case n >= len(h):
			u.LastRead = "0000000000.000000"
		default:
			u.LastRead = h[n].TS
		}
		b.ws.Unread[c.ID] = u
	}
	return nil
}

// tsLess compares Slack timestamps (same-width fractions).
func tsLess(a, b string) bool {
	as, af, _ := strings.Cut(a, ".")
	bs, bf, _ := strings.Cut(b, ".")
	if len(as) != len(bs) {
		return len(as) < len(bs)
	}
	if as != bs {
		return as < bs
	}
	return af < bf
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

// ---- sections ----

func (b *builder) sections() error {
	var specs []sectionSpec
	if err := readJSON("sections.json", &specs); err != nil {
		return err
	}
	for _, s := range specs {
		sec := Section{ID: s.ID, Name: s.Name, Emoji: s.Emoji, Type: s.Type}
		for _, n := range s.Channels {
			id, ok := b.convID[n]
			if !ok {
				return fmt.Errorf("sections.json %s: no conversation %q", s.ID, n)
			}
			sec.Channels = append(sec.Channels, id)
		}
		b.ws.Sections = append(b.ws.Sections, sec)
	}
	return nil
}

// ---- canvases, lists, scheduled messages ----

// UserName and ChannelName let canvas markdown resolve mentions.
type names struct{ b *builder }

func (n names) UserName(id string) string {
	for _, p := range n.b.ws.People {
		if p.ID == id {
			return p.Name
		}
	}
	return ""
}
func (n names) ChannelName(id string) string { return n.b.convName[id] }

// fillShared gives shared canvas and list files their titles.
func (b *builder) fillShared(id, title string) {
	for ch, h := range b.ws.History {
		for i := range h {
			for j := range h[i].Files {
				if f := &b.ws.History[ch][i].Files[j]; f.ID == id {
					f.Name, f.Title = title, title
				}
			}
		}
	}
}

func (b *builder) canvases() error {
	var specs []canvasSpec
	if err := readJSON("canvases.json", &specs); err != nil {
		return err
	}
	for _, s := range specs {
		md, err := content.ReadFile(path.Join("data/canvases", s.File))
		if err != nil {
			return err
		}
		creator, err := b.id(s.Creator)
		if err != nil {
			return fmt.Errorf("canvases.json %s: %w", s.ID, err)
		}
		updated := b.now.Add(-time.Hour)
		if s.Updated != "" {
			if updated, err = b.at(s.Updated, s.Creator, time.Time{}); err != nil {
				return fmt.Errorf("canvases.json %s: %w", s.ID, err)
			}
		}
		b.ws.Canvases = append(b.ws.Canvases, fakeslack.FakeCanvas{ID: s.ID, Title: s.Title, Creator: creator,
			Created: b.now.Add(-time.Duration(s.AgeDays) * 24 * time.Hour).Unix(), Updated: updated.Unix(),
			Access: map[string]string{}, Doc: canvas.Doc{Blocks: canvas.ParseMarkdown(string(md), names{b})}})
		b.fillShared(s.ID, s.Title)
	}
	// Inkwell's plan for the code channel (a canvas tab there)
	b.ws.Canvases = append(b.ws.Canvases, fakeslack.FakeCanvas{ID: PlanCanvasID, Title: "Plan: fix the bulk discount rounding",
		Creator: InkwellID, Created: b.now.Add(-20 * time.Hour).Unix(), Updated: b.now.Add(-25 * time.Minute).Unix(),
		Access: map[string]string{}, Doc: canvas.Doc{Blocks: canvas.ParseMarkdown(planMarkdown, names{b})}})
	return nil
}

const planMarkdown = "# Plan: fix the bulk discount rounding\n\n" +
	"Orders of 10+ reams get the 7.5% bulk discount, but `applyBulkDiscount` works in floating-point dollars, " +
	"so some totals come out a cent high (12 reams of Ultra Bright 24 lb: $99.35 instead of $99.34).\n\n" +
	"- [x] Reproduce with Nadia's order from yesterday\n" +
	"- [x] Find the rounding: `Math.round` on dollars, not cents\n" +
	"- [x] Do the money in integer cents\n" +
	"- [ ] Add tests for 10, 11, 25, 49, 50 and 120 reams\n" +
	"- [ ] Open PR #217 and get Theo's review\n\n" +
	"> Out of scope: the 50+ tier's wording on the cart page (Nadia has opinions; separate ticket).\n"

func (b *builder) lists() error {
	var specs []listSpec
	if err := readJSON("lists.json", &specs); err != nil {
		return err
	}
	for _, s := range specs {
		creator, err := b.id(s.Creator)
		if err != nil {
			return fmt.Errorf("lists.json %s: %w", s.ID, err)
		}
		updated := b.now.Add(-time.Hour)
		if s.Updated != "" {
			if updated, err = b.at(s.Updated, s.Creator, time.Time{}); err != nil {
				return fmt.Errorf("lists.json %s: %w", s.ID, err)
			}
		}
		created := b.now.Add(-time.Duration(s.AgeDays) * 24 * time.Hour)
		l := fakeslack.FakeList{ID: s.ID, Title: s.Title, Channel: b.convID[s.Channel], Creator: creator,
			Created: created.Unix(), Updated: updated.Unix(), Meta: todoMeta(s.ID, s.Icon)}
		today := b.now.In(location(branchTZ))
		for i, it := range s.Items {
			who, err := b.id(it.Who)
			if err != nil {
				return fmt.Errorf("lists.json %s: %w", s.ID, err)
			}
			text := it.Task
			done := it.Done
			r := lists.Record{ID: fmt.Sprintf("Rec0DEMO%03d", i+1), ListID: s.ID, DateCreated: created.Add(time.Duration(i) * time.Hour).Unix(),
				CreatedBy: creator, Position: fmt.Sprintf("%04d", i+1), ViewPositions: map[string]string{},
				Fields: []lists.Field{
					{Key: "name", ColumnID: "Col0TDNAME1", Text: &text, RichText: textBlocks(text)},
					{Key: "todo_completed", ColumnID: "Col0TDDONE1", Checkbox: &done},
					{Key: "todo_assignee", ColumnID: "Col0TDWHO01", User: []string{who}},
					{Key: "todo_due_date", ColumnID: "Col0TDDUE01", Date: []string{today.AddDate(0, 0, it.Due).Format("2006-01-02")}},
				}}
			l.Records = append(l.Records, r)
		}
		b.ws.Lists = append(b.ws.Lists, l)
		b.fillShared(s.ID, s.Title)
	}
	return nil
}

func textBlocks(s string) json.RawMessage {
	out, _ := json.Marshal(lists.TextBlocks(s))
	return out
}

// todoMeta is a to-do list's schema and view (fakeslack's todoList).
func todoMeta(id, icon string) lists.Meta {
	yes := true
	schema := []lists.Column{
		{ID: "Col0TDNAME1", Key: "name", Name: "Task", Type: lists.Text, IsPrimary: true, Options: lists.Options{Format: "text"}},
		{ID: "Col0TDDONE1", Key: "todo_completed", Name: "Completed", Type: lists.TodoCompleted},
		{ID: "Col0TDWHO01", Key: "todo_assignee", Name: "Assignee", Type: lists.TodoAssignee, Options: lists.Options{Format: "single_entity", ShowMemberName: &yes}},
		{ID: "Col0TDDUE01", Key: "todo_due_date", Name: "Due date", Type: lists.TodoDueDate, Options: lists.Options{Format: "single", DateFormat: "default"}},
	}
	var vcols []lists.ViewColumn
	for i, c := range schema {
		vcols = append(vcols, lists.ViewColumn{ID: c.ID, Key: c.Key, Visible: true, Position: fmt.Sprintf("%04d", i)})
	}
	view := "View0" + strings.TrimPrefix(id, "F0")
	return lists.Meta{Schema: schema, TodoMode: true, DefaultView: view, Icon: icon,
		Views: []lists.View{{ID: view, Name: "All items", Type: lists.ViewTable, Position: "0001", Columns: vcols, IsAllItems: true,
			ShowCompleted: &yes, Sorts: []lists.Sort{}, Filters: []lists.Filter{}}}}
}

func (b *builder) scheduled() error {
	var specs []schedSpec
	if err := readJSON("scheduled.json", &specs); err != nil {
		return err
	}
	for _, s := range specs {
		ch, ok := b.convID[s.Channel]
		if !ok {
			return fmt.Errorf("scheduled.json: no conversation %q", s.Channel)
		}
		day, clock, _ := strings.Cut(s.At, " ")
		var hh, mm int
		if _, err := fmt.Sscanf(clock, "%d:%d", &hh, &mm); err != nil {
			return fmt.Errorf("scheduled.json: at %q", s.At)
		}
		d := b.now.In(location(branchTZ))
		switch {
		case day == "tomorrow":
			d = d.AddDate(0, 0, 1)
		case strings.HasPrefix(day, "+"):
			n, err := strconv.Atoi(day[1:])
			if err != nil {
				return fmt.Errorf("scheduled.json: at %q", s.At)
			}
			d = b.workday(-n)
		default:
			return fmt.Errorf("scheduled.json: at %q", s.At)
		}
		at := time.Date(d.Year(), d.Month(), d.Day(), hh, mm, 0, 0, d.Location())
		created, err := b.at(dflt(s.Created, "1h"), "jordan", time.Time{})
		if err != nil {
			return fmt.Errorf("scheduled.json: %w", err)
		}
		b.ws.Scheduled = append(b.ws.Scheduled, Scheduled{Channel: ch, ThreadTS: s.Thread, At: at, Created: created,
			Blocks: b.richText(s.Text)})
	}
	return nil
}

// richText is text as one rich_text block, with @handle and #channel as
// user and channel elements (what the composer sends).
func (b *builder) richText(s string) json.RawMessage {
	var els []map[string]any
	resolved := b.resolveText(s)
	ref := regexp.MustCompile(`<(@|#)([A-Z0-9]+)(?:\|[^>]*)?>`)
	last := 0
	for _, m := range ref.FindAllStringSubmatchIndex(resolved, -1) {
		if m[0] > last {
			els = append(els, map[string]any{"type": "text", "text": resolved[last:m[0]]})
		}
		id := resolved[m[4]:m[5]]
		if resolved[m[2]:m[3]] == "@" {
			els = append(els, map[string]any{"type": "user", "user_id": id})
		} else {
			els = append(els, map[string]any{"type": "channel", "channel_id": id})
		}
		last = m[1]
	}
	if last < len(resolved) {
		els = append(els, map[string]any{"type": "text", "text": resolved[last:]})
	}
	out, _ := json.Marshal([]any{map[string]any{"type": "rich_text", "elements": []any{
		map[string]any{"type": "rich_text_section", "elements": els}}}})
	return out
}
