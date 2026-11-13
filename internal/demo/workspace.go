package demo

// The contract between the demo's content (build.go, art*.go, data/) and
// its live side (world*.go, bots*.go): Build turns the data files into a
// Workspace of ready-to-serve Slack objects; the world serves it through
// fakeslack's hooks and the bots change it over time.

import (
	"encoding/json"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
)

// Fixed ids. The viewer is fakeslack.SelfID so fakeslack's own handlers
// (auth.test, drafts, canvases) agree with the demo about who "you" are.
const (
	TeamID     = fakeslack.TeamID
	SelfID     = fakeslack.SelfID
	TeamName   = "Quillfeather Paper Co."
	TeamDomain = "quillfeather"

	GeneralID = "C0DEMOGENL" // #general
	RandomID  = "C0DEMORAND" // #random
	SalesID   = "C0DEMOSALE" // #sales-leaderboard
	PartyID   = "C0DEMOPART" // #party-planning-crew
	ITHelpID  = "C0DEMOITHP" // #it-help
	WareID    = "C0DEMOWARE" // #warehouse
	LunchID   = "C0DEMOLUNC" // #lunch-orders (Pulse's polls)
	KudosID   = "C0DEMOKUDO" // #kudos
	SantaID   = "C0DEMOSNTA" // #secret-santa-2026 (private)
	CodeID    = "C0DEMOCODE" // #inkwell-order-portal (Inkwell's code channel)
	GroupDMID = "G0DEMOMPIM" // priya, dale, kip and you
	ConnectID = "C0DEMOCONN" // #papertrail-freight (Slack Connect with Papertrail Freight)

	// Papertrail Freight: the carrier the branch shares a Slack Connect
	// channel with (people.json "team": "papertrail")
	PartnerTeamID     = "T0DEMOPTRL"
	PartnerTeamName   = "Papertrail Freight"
	PartnerTeamDomain = "papertrail-freight"
	partnerTeamKey    = "papertrail"

	InkwellID    = "U0DEMOINKW" // the AI coding agent (an app's bot user)
	InkwellBotID = "B0DEMOINKW"
	InkwellAppID = "A0DEMOINKW"
	PulseID      = "U0DEMOPULS" // polls and stand-ups (an app's bot user)
	PulseBotID   = "B0DEMOPULS"
	PulseAppID   = "A0DEMOPULS"
)

// Workspace is the whole fake workspace, resolved: handles are user ids,
// relative times are real timestamps, image specs are URLs on the fake.
type Workspace struct {
	Now  time.Time // what the relative times were resolved against
	Base string    // the fake's URL (http://127.0.0.1:port/)

	Self   slack.User
	Users  []slack.User // everyone, the viewer and the bot users included
	People []Person     // the cast as written (persona, status rotation)
	Bots   []Bot

	// Convs are the viewer's conversations in sidebar order: channels,
	// the private channel, the code channel, DMs and the group DM.
	Convs []slack.Conversation
	// History is each conversation's top-level messages, newest first
	// (conversations.history's order); a thread parent carries
	// ReplyCount, ReplyUsers, LatestReply.
	History map[string][]slack.Message
	// Replies is each thread's replies, oldest first, keyed by channel
	// then parent ts (the parent is not included).
	Replies map[string]map[string][]slack.Message
	// Pins is each channel's pinned message timestamps.
	Pins map[string][]string
	// Unread is each conversation's read state at start.
	Unread map[string]Unread

	Sections []Section
	// Emoji is emoji.list's map: custom emoji name -> image URL on the
	// fake (or "alias:<name>").
	Emoji map[string]string

	Canvases  []fakeslack.FakeCanvas // fakeslack.AddCanvas'd at start
	Lists     []fakeslack.FakeList   // fakeslack.AddList'd at start
	Scheduled []Scheduled            // fakeslack.AddDraft'd at start
	// Usergroups are the org's user groups (fakeslack.SetUsergroups)
	Usergroups []fakeslack.UsergroupFixture
}

// Person is one cast member as data/people.json writes them.
type Person struct {
	ID       string `json:"id"`
	Handle   string `json:"handle"`
	Name     string `json:"name"`
	Title    string `json:"title"`
	Pronouns string `json:"pronouns"`
	TZ       string `json:"tz"` // IANA name; "" is America/New_York
	// Persona is a line about who they are (authoring notes; the bots'
	// voices live in their scripts).
	Persona string `json:"persona"`
	// Status is the status at start; Statuses is a rotation the bots set
	// over time.
	Status   Status   `json:"status"`
	Statuses []Status `json:"statuses"`
	// Team is "papertrail" for Papertrail Freight's people (a Slack
	// Connect org); "" is Quillfeather.
	Team   string `json:"team,omitempty"`
	Self   bool   `json:"self,omitempty"`
	Bot    bool   `json:"bot,omitempty"`
	Admin  bool   `json:"admin,omitempty"`
	Avatar Avatar `json:"avatar"`
}

// Status is a custom status.
type Status struct {
	Emoji string `json:"emoji"` // ":coffee:"
	Text  string `json:"text"`
}

// Avatar describes a drawn avatar (art.go): no image comes from anywhere.
type Avatar struct {
	Bg     string `json:"bg"`     // background, "#rrggbb"
	Skin   string `json:"skin"`   // face
	Hair   string `json:"hair"`   // hair colour
	Style  string `json:"style"`  // hair style: short, long, bun, bald, curly, mohawk…
	Extra  string `json:"extra"`  // glasses, beard, tie, headset, hardhat, bow…
	Glyph  string `json:"glyph"`  // bots: a symbol drawn instead of a face (quill, pulse)
	Accent string `json:"accent"` // a second colour for extras
}

// Bot is an app's bot user: what bots.info and apps.profile.get answer.
type Bot struct {
	UserID, BotID, AppID string
	Name, Desc           string
	Color                string // app_card_color, "rrggbb"
	Icons                map[string]string
}

// Section is a sidebar section (users.channelSections.list). Type is
// "standard" (custom), "stars", "channels", "direct_messages".
type Section struct {
	ID, Name, Emoji, Type string
	Channels              []string // standard and stars only
}

// Unread is a conversation's read state at start (client.counts).
type Unread struct {
	LastRead string // ts; "" reads everything
	Mentions int
}

// Scheduled is a message scheduled to send later (drafts.list).
type Scheduled struct {
	Channel, ThreadTS string
	Blocks            json.RawMessage
	At                time.Time
	Created           time.Time
}

// UserByID finds a user (the viewer and bots included).
func (w *Workspace) UserByID(id string) (slack.User, bool) {
	for _, u := range w.Users {
		if u.ID == id {
			return u, true
		}
	}
	return slack.User{}, false
}

// PersonByHandle finds a cast member by handle.
func (w *Workspace) PersonByHandle(h string) (Person, bool) {
	for _, p := range w.People {
		if p.Handle == h {
			return p, true
		}
	}
	return Person{}, false
}

// Conv finds a conversation by id.
func (w *Workspace) Conv(id string) (slack.Conversation, bool) {
	for _, c := range w.Convs {
		if c.ID == id {
			return c, true
		}
	}
	return slack.Conversation{}, false
}
