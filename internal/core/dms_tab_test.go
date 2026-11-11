package core

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/some-du6e/clack/internal/slack"
)

// The DMs tab lists what Slack's does (gantry dms-list-view uV3r
// useChannelIds, upCu useSortedChannels): no bot or Slackbot DMs (Karim,
// 2026-11-10: "bots show up in the DM list when real Slack doesn't list
// them"), no deleted people, frozen IMs or system users, and no DM that
// never had a message except the self-DM.
func TestDMsTabFilters(t *testing.T) {
	a, f := testApp(t)
	a.S.PutUsers([]slack.User{{ID: "UDEL", Name: "gone", Deleted: true}, {ID: "UAPP", Name: "app", IsAppUser: true}})
	msg := func(ts, user string) string {
		return `{"type":"message","ts":"` + ts + `","user":"` + user + `","text":"synthetic"}`
	}
	im := func(id, user, ts string, extra string) string {
		m := ""
		if ts != "" {
			m = `"message":` + msg(ts, user) + `,`
		}
		return `{"id":"` + id + `",` + m + `"channel":{"id":"` + id + `","is_im":true,"user":"` + user + `"` + extra + `}}`
	}
	f.reply["client.dms"] = `{"ok":true,"ims":[` + strings.Join([]string{
		im("DALICE", "U1", "1700000005.000100", ""),
		im("DBOT", "UB", "1700000004.000100", ""),
		im("DSLACKBOT", "USLACKBOT", "1700000003.000100", ""),
		im("DGONE", "UDEL", "1700000002.000100", ""),
		im("DQUIET", "U2", "", ""),
		im("DSELF", "USELF", "", ""),
		im("DFROZEN", "U2", "1700000001.000100", `,"is_frozen":true`),
		im("DSEARCH", "USEARCH", "1700000001.000200", ""),
		im("DAPP", "UAPP", "1700000001.000300", ""), // is_app_user alone isn't a bot (isBotUser)
	}, ",") + `],"mpims":[{"id":"GGROUP","message":` + msg("1700000006.000100", "U2") +
		`,"channel":{"id":"GGROUP","is_mpim":true,"members":["USELF","U1","U2"]}}]}`
	p, err := a.DMs(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, d := range p.DMs {
		got = append(got, d.Channel)
	}
	sort.Strings(got)
	if want := "DALICE,DAPP,DSELF,GGROUP"; strings.Join(got, ",") != want {
		t.Errorf("DMs tab: %s, want %s", strings.Join(got, ","), want)
	}
	q := f.last("client.dms")
	if q.Get("count") != "250" || q.Get("include_closed") != "true" || q.Get("include_channel") != "true" {
		t.Errorf("client.dms params %v", q)
	}
	if q.Get("exclude_bots") != "true" { // agents_in_dm_sidebar off
		t.Errorf("exclude_bots %q, want true without agents_in_dm_sidebar", q.Get("exclude_bots"))
	}
}
