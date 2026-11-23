package ui

// A mention pill's name is MemberSlug's: rich text's UserElement passes
// shouldDisplayRealNamesInMentions as forceRealNames (under the
// mentions_full_names_only_pref experiment: the viewer's
// mentions_real_names_override, 1 on and -1 off, else the team's
// display_real_names_in_mentions), and the pill then reads the member's
// real name, else their mention name. Clack's pills read the display name
// whatever the pref (toasts, search and message slugs already followed
// it). Synthetic member on the in-repo fake.

import (
	"context"
	"testing"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/richview"
)

func TestMentionPillRealNamesLikeSlack(t *testing.T) {
	const id = "U0REALNM01"
	for _, c := range []struct {
		name       string
		prefs      map[string]any
		experiment string
		want       string
	}{
		{"real names in mentions", map[string]any{"mentions_real_names_override": 1}, "on", "@Zed Real"},
		{"display names in mentions", map[string]any{"mentions_real_names_override": -1}, "on", "@zz.disp"},
		{"out of the experiment", map[string]any{"mentions_real_names_override": 1}, "off", "@zz.disp"},
	} {
		t.Run(c.name, func(t *testing.T) {
			fs, err := fakeslack.Start(fakeslack.Small)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { fs.Close() })
			ca := core.New(slack.New(fs.Creds()), store.New(t.TempDir()))
			t.Cleanup(ca.Shutdown)
			fs.SetPrefs(c.prefs)
			if _, err := ca.RefreshPrefs(context.Background()); err != nil {
				t.Fatal(err)
			}
			ca.SetExperimentGroup("mentions_full_names_only_pref", c.experiment)
			ca.S.PutUsers([]slack.User{namedUser(id, "zz.handle", "zz.disp", "Zed Real")})
			installMemberRead(ca)
			t.Cleanup(func() { uninstallMemberRead(ca) })
			sp := richtext.Span{Kind: richtext.User, ID: id}
			if got, ok := richview.MentionText(sp); !ok || got != c.want {
				t.Errorf("the mention pill reads %q (%v), Slack's MemberSlug %q", got, ok, c.want)
			}
			if got, ok := richview.MentionTextPeek(sp); !ok || got != c.want {
				t.Errorf("the pill's peek reads %q (%v), want %q", got, ok, c.want)
			}
		})
	}
}
