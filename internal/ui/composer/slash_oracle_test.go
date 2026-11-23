package composer

import (
	"encoding/json"
	"testing"

	"github.com/some-du6e/clack/internal/core"
)

// TestSlashCommandsLikeSlack runs Slack's slash-command path on Slack's
// own deltas and compares with what Slack's modules gave (the oracle
// table): the processed delta (linkify) passes the slash test or not, the
// command word lowercased (’ as ', a full-width slash kept), the tokens,
// and executeCommand's blocks for deltaWithoutCmd, whitespace, headings,
// dividers, colours, lists, quotes, code, links and slugs included.
func TestSlashCommandsLikeSlack(t *testing.T) {
	for _, c := range slashOracleCases {
		t.Run(c.name, func(t *testing.T) {
			q := linkifyQ(qFromSlackDelta(oracleDelta(t, c.ops)))
			if got := looksLikeSlashCommand(q); got != c.slash {
				t.Fatalf("slash test %v, Slack's %v", got, c.slash)
			}
			if !c.slash {
				return
			}
			a := parseSlashArgs(q)
			if a.Cmd != c.cmd || a.Disp != c.disp {
				t.Errorf("cmd %q disp %q, Slack's %q %q", a.Cmd, a.Disp, c.cmd, c.disp)
			}
			type param struct {
				Type  string `json:"type"`
				Value string `json:"value"`
			}
			ps := []param{}
			for _, p := range a.Params {
				ps = append(ps, param{p.Type, p.Value})
			}
			pj, _ := json.Marshal(ps)
			if ok, got, want := sameJSON(t, pj, []byte(c.params)); !ok {
				t.Errorf("userParams\n got %s\nwant %s", got, want)
			}
			if ok, got, want := sameJSON(t, commandBlocks(a.Rest), []byte(c.blocks)); !ok {
				t.Errorf("blocks\n got %s\nwant %s", got, want)
			}
		})
	}
}

// oracleHits reads a case's searchFunc results as RZMQ sees them.
func oracleHits(t *testing.T, raw string) map[string][]slugHit {
	t.Helper()
	var m map[string][]struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		RealName string `json:"real_name"`
		Keyword  bool   `json:"is_broadcast_keyword"`
		Group    bool   `json:"is_usergroup"`
		Handle   string `json:"handle"`
		Profile  struct {
			DisplayName string `json:"display_name"`
			RealName    string `json:"real_name"`
		} `json:"profile"`
	}
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatal(err)
	}
	out := map[string][]slugHit{}
	for q, rs := range m {
		for _, r := range rs {
			switch {
			case r.Keyword:
				out[q] = append(out[q], slugHit{id: r.ID, keyword: true, name: r.Name})
			case r.Group:
				out[q] = append(out[q], slugHit{id: r.ID, group: true, name: r.Handle})
			default:
				real := r.Profile.RealName
				if real == "" {
					real = r.RealName
				}
				name := r.Profile.DisplayName
				if name == "" {
					name = real
				}
				out[q] = append(out[q], slugHit{id: r.ID, name: name, display: r.Profile.DisplayName, real: real})
			}
		}
	}
	return out
}

// TestAutoSlugLikeSlack: the typed-name mentions Slack's
// replacePotentialMentionsSynchronously makes over the same search
// results: unique names, spelled-out names, user groups, keywords,
// channels (archived ones last), and the text left alone (code, links,
// a chip already there, no match).
func TestAutoSlugLikeSlack(t *testing.T) {
	for _, c := range mentionOracleCases {
		t.Run(c.name, func(t *testing.T) {
			hits := oracleHits(t, c.members)
			var chans []struct {
				ID       string `json:"id"`
				Name     string `json:"name"`
				Archived bool   `json:"is_archived"`
			}
			if err := json.Unmarshal([]byte(c.channels), &chans); err != nil {
				t.Fatal(err)
			}
			st := slugStore{find: func(q string) []slugHit { return hits[q] }}
			for _, ch := range chans {
				st.channels = append(st.channels, core.Conv{ID: ch.ID, Name: ch.Name, Kind: "channel", IsArchived: ch.Archived})
			}
			out, _ := autoSlug(qFromSlackDelta(oracleDelta(t, c.ops)), st)
			var got []map[string]any
			for _, op := range out.ops {
				m := map[string]any{"insert": string(op.text)}
				if id, ok := qMentionID(op); ok {
					m["id"] = id
				} else if len(op.attrs) > 0 {
					m["attributes"] = map[string]any(op.attrs)
				}
				got = append(got, m)
			}
			gj, _ := json.Marshal(got)
			if ok, g, w := sameJSON(t, gj, []byte(c.want)); !ok {
				t.Errorf("ops\n got %s\nwant %s", g, w)
			}
		})
	}
}
