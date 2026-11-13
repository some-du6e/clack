package ui

// Plugin credits ("by Izie, Jeremy") draw as Slack user mentions: the
// @mention pill, which opens the profile, as Taut's authorsMrkdwn gives
// them inside the Hack Club Slack (app/settings/common.tsx:158, `<@id>`
// for an author with a slackId). Karim 2026-11-12: never links.
//
// Each credited person resolves to a member of the workspace Clack is
// signed in to:
//
//  1. a Slack id the data carries: a Taut author's slackId, a Slick
//     author's `<@U…>`, the id in a /team/U… URL, or the slackId of the
//     Taut author (shared/authors.ts) with that name or page. Those ids
//     are Hack Club Slack members, so the id is used in the Hack Club
//     Slack, and anywhere else only when this workspace has that member.
//  2. for a person the data names without any Slack id (Slick's and the
//     library's upstream contributors, unknown names): the one live,
//     human member whose display name, real name or username is the
//     name. Someone the data ties to a Hack Club id isn't name-matched
//     outside the Hack Club Slack: a namesake there is somebody else.
//
// Anyone unresolved is their name as plain text (no link, no pill).

import (
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/pluginlib"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/taut"
)

// creditPerson is one name a plugin credits.
type creditPerson struct {
	Name    string
	URL     string
	SlackID string // a Hack Club Slack member id from the plugin's data
}

// creditResolver turns credited people into mrkdwn for one workspace.
type creditResolver struct {
	s  *store.Store // nil: nobody resolves by name or membership
	hc bool         // signed in to the Hack Club Slack
}

// credits is the resolver for the workspace a is signed in to.
func (a *App) credits() creditResolver {
	r := creditResolver{}
	if a == nil {
		return r
	}
	if c := a.Core(); c != nil {
		r.s = c.S
	}
	if t := a.Taut(); t != nil {
		r.hc = t.IsHackClub()
	} else if r.s != nil {
		r.s.RLock()
		r.hc = taut.HackClubIDs[r.s.TeamID] || taut.HackClubIDs[r.s.Self.TeamID]
		r.s.RUnlock()
	}
	return r
}

var creditSepRE = regexp.MustCompile(`\s*,\s*|\s+and\s+`)

var teamURLRE = regexp.MustCompile(`^https://([a-z0-9-]+)\.slack\.com/team/([UW][A-Z0-9]{2,})/?(?:[?#].*)?$`)

// knownCreditAuthor is the Taut author (shared/authors.ts) with that name
// or page.
func knownCreditAuthor(p creditPerson) (taut.Author, bool) {
	for _, a := range taut.KnownAuthors() {
		if a.SlackID == "" {
			continue
		}
		if p.URL != "" && a.URL == p.URL || p.URL == "" && strings.EqualFold(a.Name, strings.TrimSpace(p.Name)) {
			return a, true
		}
	}
	return taut.Author{}, false
}

// member reports whether this workspace's store has the live member id.
func (r creditResolver) member(id string) bool {
	if r.s == nil {
		return false
	}
	u, ok := r.s.User(id)
	return ok && u != nil && !u.Deleted
}

// storeName is the loaded member's name, or "".
func (r creditResolver) storeName(id string) string {
	if r.s == nil {
		return ""
	}
	return r.s.UserName(id)
}

// id is p's member id here, or "".
func (r creditResolver) id(p creditPerson) string {
	var hcIDs []string // ids that name a Hack Club Slack member
	if p.SlackID != "" {
		hcIDs = append(hcIDs, p.SlackID)
	}
	if m := teamURLRE.FindStringSubmatch(p.URL); m != nil {
		if m[1] == "hackclub" {
			hcIDs = append(hcIDs, m[2])
		} else if r.member(m[2]) { // another workspace's profile page: only its own member
			return m[2]
		}
	}
	if a, ok := knownCreditAuthor(p); ok {
		hcIDs = append(hcIDs, a.SlackID)
	}
	for _, id := range hcIDs {
		if r.hc || r.member(id) {
			return id
		}
	}
	if len(hcIDs) > 0 {
		return ""
	}
	return r.byName(p.Name)
}

// mention is p as a mention, else their name as plain text.
func (r creditResolver) mention(p creditPerson) string {
	name := strings.TrimSpace(p.Name)
	if id := r.id(p); id != "" {
		if name == "" || name == id {
			name = r.storeName(id)
		}
		if name == "" {
			if a, ok := tautAuthorBySlackID(id); ok {
				name = a.Name
			}
		}
		if name == "" || name == id {
			return "<@" + id + ">"
		}
		// labelled, in case the member isn't loaded yet (the store's name wins)
		return "<@" + id + "|" + mrkdwnEscape(strings.NewReplacer("|", " ").Replace(name)) + ">"
	}
	return mrkdwnEscape(name)
}

// list is people as mentions or names, joined with ", ".
func (r creditResolver) list(people []creditPerson) string {
	out := make([]string, 0, len(people))
	for _, p := range people {
		if strings.TrimSpace(p.Name) == "" && p.SlackID == "" {
			continue
		}
		out = append(out, r.mention(p))
	}
	return strings.Join(out, ", ")
}

// mrkdwnPeople splits a mrkdwn author list (Slick's `<@U…>, <@U…|name>`,
// Taut's older string form) into people.
func mrkdwnPeople(s string) []creditPerson {
	var out []creditPerson
	for _, part := range creditSepRE.Split(strings.TrimSpace(s), -1) {
		if part == "" {
			continue
		}
		if m := mrkdwnUserRE.FindStringSubmatch(part); m != nil && m[0] == part {
			out = append(out, creditPerson{Name: m[2], SlackID: m[1]})
			continue
		}
		out = append(out, creditPerson{Name: mrkdwnUnescape(part)})
	}
	return out
}

func mrkdwnUnescape(s string) string {
	return strings.NewReplacer("&lt;", "<", "&gt;", ">", "&amp;", "&").Replace(s)
}

// mrkdwn is a mrkdwn author list with every person resolved. An id
// nobody here has and no name labels is "a Hack Club Slack member".
func (r creditResolver) mrkdwn(s string) string {
	people := mrkdwnPeople(s)
	for i, p := range people {
		if p.SlackID != "" && p.Name == "" && r.id(p) == "" {
			if a, ok := tautAuthorBySlackID(p.SlackID); ok {
				people[i].Name = a.Name
			} else if n := r.storeName(p.SlackID); n != "" {
				people[i].Name = n
			} else {
				people[i].Name = "a Hack Club Slack member"
			}
		}
	}
	return r.list(people)
}

// libraryCredit is a library entry's authors as mentions or names, else
// its upstream contributors.
func (r creditResolver) libraryCredit(e pluginlib.Entry) string {
	var people []creditPerson
	for _, a := range e.Authors {
		if a.Name != "" || a.SlackID != "" {
			people = append(people, creditPerson{Name: a.Name, URL: a.URL, SlackID: a.SlackID})
		}
	}
	if len(people) == 0 {
		for _, c := range e.Contributors {
			people = append(people, creditPerson{Name: c})
		}
	}
	return r.list(people)
}

// tautAuthors is an installed Taut plugin's authors.
func (r creditResolver) tautAuthors(pl taut.PluginInfo) string {
	if len(pl.Authors) == 0 {
		return r.mrkdwn(pl.AuthorsMrkdwn)
	}
	people := make([]creditPerson, 0, len(pl.Authors))
	for _, a := range pl.Authors {
		people = append(people, creditPerson{Name: a.Name, URL: a.URL, SlackID: a.SlackID})
	}
	return r.list(people)
}

// ---- name lookups ----

// creditNameTTL is how long a name lookup stands before the store is
// asked again (members load as the session goes on).
const creditNameTTL = 15 * time.Second

type creditNameHit struct {
	id string // "" when nobody or several members have the name
	at time.Time
}

var creditNames struct {
	mu sync.Mutex
	by map[*store.Store]map[string]creditNameHit
}

// byName is the one member here called name, or "".
func (r creditResolver) byName(name string) string {
	f := store.FoldName(name)
	if r.s == nil || f == "" {
		return ""
	}
	now := time.Now()
	creditNames.mu.Lock()
	if creditNames.by == nil {
		creditNames.by = map[*store.Store]map[string]creditNameHit{}
	}
	m := creditNames.by[r.s]
	if m == nil {
		m = map[string]creditNameHit{}
		creditNames.by[r.s] = m
	}
	hit, ok := m[f]
	creditNames.mu.Unlock()
	if ok && now.Sub(hit.at) < creditNameTTL {
		return hit.id
	}
	ids := r.s.UsersNamed([]string{f})[f]
	hit = creditNameHit{at: now}
	if len(ids) == 1 {
		hit.id = ids[0]
	}
	creditNames.mu.Lock()
	m[f] = hit
	creditNames.mu.Unlock()
	return hit.id
}
