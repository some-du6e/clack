package fakeslack

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/some-du6e/clack/internal/slack"
)

// Synthetic members. Hack Club's directory has far more people posting
// than any client holds, so a socket message's sender, a typer or a
// mentioned person is often someone the client never fetched (core asks
// users/info for them). Ids with SynthMemberPrefix are members the fake
// makes up on demand, numbered by what follows the prefix: users/info
// answers any of them, so a test has an endless supply of people the
// client doesn't know yet (internal/ui's TestMessageLatency).
const SynthMemberPrefix = "U0SYNTH"

// SynthMemberID is the n-th synthetic member's id.
func SynthMemberID(n int) string { return fmt.Sprintf("%s%06d", SynthMemberPrefix, n) }

// synthMember is the member a synthetic id names (ok false for any other
// id).
func (s *Server) synthMember(id string) (slack.User, bool) {
	rest, ok := strings.CutPrefix(id, SynthMemberPrefix)
	if !ok || rest == "" {
		return slack.User{}, false
	}
	n, err := strconv.Atoi(rest)
	if err != nil || n < 0 {
		return slack.User{}, false
	}
	name := fmt.Sprintf("synth%06d", n)
	img := func(px int) string {
		return s.URL + "img/" + strconv.Itoa(px) + "x" + strconv.Itoa(px) + "/" + name + ".png"
	}
	real := fmt.Sprintf("Synthetic Member %d", n)
	return slack.User{ID: id, TeamID: TeamID, Name: name, RealName: real, Presence: "active",
		Profile: slack.Profile{DisplayName: name, RealName: real, Team: TeamID, Fields: json.RawMessage(`{}`),
			Image24: img(24), Image32: img(32), Image48: img(48), Image72: img(72), Image192: img(192), Image512: img(512)}}, true
}
