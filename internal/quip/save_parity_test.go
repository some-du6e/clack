package quip

import (
	"encoding/hex"
	"errors"
	"reflect"
	"sort"
	"strconv"
	"testing"

	"github.com/some-du6e/clack/internal/paritytest"
)

type saveSectionIn struct {
	Server map[string]any `json:"server"`
	Edit   struct {
		Kind    string `json:"kind"`
		RTML    string `json:"rtml"`
		Checked *bool  `json:"checked"`
		Style   *int32 `json:"style"`
		Indent  *int32 `json:"indent"`
		Dirty   []int  `json:"dirty"` // the oracle's: Slack's setters' dirty fields
	} `json:"edit"`
	LocalSeq    int64  `json:"localSeq"`
	Session     string `json:"session"`
	SaveSeq     int64  `json:"saveSeq"`
	Team        string `json:"team"`
	ParentStyle *int32 `json:"parentStyle"`
}

type saveSectionOut struct {
	Hex      string `json:"hex"`
	LocalSeq int64  `json:"localSeq"`
}

// TestQuipSaveSectionLikeSlack builds each case's bundle section from the
// server's section and the edit, against what Slack's own section entity
// (EKJU over ZRIn) put in its bundle for the same state.
func TestQuipSaveSectionLikeSlack(t *testing.T) {
	s := paritytest.Load[saveSectionIn, saveSectionOut](t, "testdata/parity/quip-save-section.json")
	s.Run(t, paritytest.Check[saveSectionIn, saveSectionOut]{Clack: func(t *testing.T, c saveSectionIn) saveSectionOut {
		var server Section
		build(t, reflect.ValueOf(&server).Elem(), c.Server)
		e := Edit{SectionID: *server.ID, RTML: c.Edit.RTML}
		if c.Edit.Checked != nil {
			e.Checked = *c.Edit.Checked
		}
		switch c.Edit.Kind {
		case "text":
			e.Kind = EditText
		case "check":
			e.Kind = EditCheck
		case "delete":
			e.Kind = EditDelete
		case "block":
			e.Kind = EditBlock
		default:
			t.Fatalf("edit kind %q", c.Edit.Kind)
		}
		if c.Edit.Style != nil {
			e.Restyle, e.Style = true, *c.Edit.Style
		}
		if c.Edit.Indent != nil {
			e.Reindent, e.Indent = true, *c.Edit.Indent
		}
		e.Recheck = c.Edit.Checked != nil && e.Kind != EditCheck
		trace := c.Session + "-" + strconv.FormatInt(c.SaveSeq, 10)
		u, err := UpdateSection(&server, e, c.ParentStyle, trace, c.LocalSeq+1)
		if err != nil {
			t.Fatalf("UpdateSection: %v", err)
		}
		return saveSectionOut{Hex: hex.EncodeToString(Marshal(u)), LocalSeq: c.LocalSeq + 1}
	}})
}

type saveTitleOut struct {
	Title     string   `json:"title"`
	RichText  string   `json:"richText"`
	EmojiInfo []string `json:"emojiInfo"`
}

// titleCtl is a case's control, as the oracle's stand-in takes it: its
// kind, Slack id, name, emoji id, the emoji store's model of it (Info,
// absent while not loaded), a channel's privacy and a broadcast's range.
type titleCtl struct {
	Kind      string `json:"kind"`
	ID        string `json:"id"`
	Private   bool   `json:"private"`
	Name      string `json:"name"`
	EmojiID   string `json:"slackEmojiId"`
	CustomURL string `json:"customUrl"`
	Info      *struct {
		TeamID      string `json:"teamId"`
		DisplayName string `json:"displayName"`
		Unicode     string `json:"unicode"`
		DisplayURL  string `json:"displayUrl"`
	} `json:"info"`
	Range string `json:"range"`
	Ms    int64  `json:"ms"`
}

// titleControlsOf is what the case's document holds: each control's
// section, the emoji info Slack's last save left on the title (the loaded
// ones among its first ten emoji, in the RTML's order: emojiInfoOf over
// the store's model, its url the model's or else the stored
// custom_emoji_url), the channels' privacy.
func titleControlsOf(rtml string, ctls map[string]titleCtl) TitleControls {
	secs := map[string]*Section{}
	private := map[string]bool{}
	var info []EmojiInfo
	ids := make([]string, 0, len(ctls))
	for id := range ctls {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	opt := func(s string) *string {
		if s == "" {
			return nil
		}
		return Ptr(s)
	}
	for _, id := range ids {
		k := ctls[id]
		c := &Content{}
		switch k.Kind {
		case "user", "usergroup":
			c.SlackUser = &SlackUser{SlackUserID: Ptr(UserID(k.ID))}
		case "channel":
			c.SlackChannel = &SlackChannel{SlackChannelID: Ptr(ChannelID(k.ID))}
			private[k.ID] = k.Private
		case "emoji":
			c.SlackEmoji = &SlackEmoji{EmojiName: opt(k.Name), SlackEmojiID: opt(k.EmojiID), CustomEmojiURL: opt(k.CustomURL)}
		case "broadcast":
			c.SlackBroadcast = &SlackBroadcast{Range: opt(k.Range)}
		case "date":
			c.SlackDate = &SlackDate{UTCDateMs: Ptr(k.Ms)}
		}
		secs[id] = &Section{ID: Ptr(id), SectionClass: Ptr(ClassControl), Content: c}
	}
	emoji := 0
	for _, id := range ControlIDs(rtml) {
		k, ok := ctls[id]
		if !ok || k.Kind != "emoji" {
			continue
		}
		if emoji++; emoji > titleEmojiMax {
			break
		}
		if k.Info != nil && k.Info.DisplayName != "" {
			u := k.Info.DisplayURL
			if u == "" {
				u = k.CustomURL
			}
			info = append(info, EmojiInfo{TeamID: Ptr(k.Info.TeamID), DisplayName: Ptr(k.Info.DisplayName),
				Unicode: opt(k.Info.Unicode), DisplayURL: opt(u)})
		}
	}
	return TitleControls{EmojiInfo: info,
		Section: func(id string) *Section { return secs[id] },
		ChannelPrivate: func(id string) (bool, bool) {
			p, ok := private[id]
			return p, ok
		}}
}

// TestQuipSaveTitleLikeSlack is the title metadata every save sends.
func TestQuipSaveTitleLikeSlack(t *testing.T) {
	type in struct {
		RTML     string              `json:"rtml"`
		Controls map[string]titleCtl `json:"controls"`
	}
	s := paritytest.Load[in, saveTitleOut](t, "testdata/parity/quip-save-title.json")
	s.Run(t, paritytest.Check[in, saveTitleOut]{Clack: func(t *testing.T, c in) saveTitleOut {
		m, err := TitleMetadata(c.RTML, titleControlsOf(c.RTML, c.Controls))
		if errors.Is(err, ErrUnsupported) {
			return saveTitleOut{Title: "(refused)"} // a known divergence names the case
		}
		if err != nil {
			t.Fatalf("TitleMetadata: %v", err)
		}
		out := saveTitleOut{Title: m.Title, RichText: hex.EncodeToString(Marshal(m.RichText)), EmojiInfo: []string{}}
		for i := range m.EmojiInfo {
			out.EmojiInfo = append(out.EmojiInfo, hex.EncodeToString(Marshal(&m.EmojiInfo[i])))
		}
		return out
	}})
}
