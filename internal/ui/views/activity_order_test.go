package views

// The order Slack's activityIA4 store keeps (activity_order.go) against
// Slack's own code: every expected list here is what client-boot tsND's
// getUpsertedActivityRtmItems / getUpsertedActivityItems, 1Cyf v and
// um/L Y (build 35b42de8b870, copied verbatim, lodash's compact and
// uniqBy stubbed, en-US ICU) answer for the same steps (the oracle in
// /root/clack-ops/scratch/activity-order/oracle). Reloads keep ui's rows
// instead of Slack's (activity_order_reload_test.go). Synthetic data.

import (
	"slices"
	"strings"
	"testing"

	"github.com/some-du6e/clack/internal/core"
)

// actOrderStep is a realtime entry (rtm) or a feed page that joins the
// list (a first page or the next one).
type actOrderStep struct {
	rtm  *core.ActivityItem
	page []core.ActivityItem
	want string // key@feedTs, ...
}

func actOI(key, typ, feed string) core.ActivityItem {
	return core.ActivityItem{Key: key, Type: typ, FeedTS: feed}
}

func actOIL(key, typ, feed, latest string) core.ActivityItem {
	it := actOI(key, typ, feed)
	it.LatestTS = latest
	return it
}

func actOrderKeys(items []core.ActivityItem) string {
	var out []string
	for i := range items {
		ts := items[i].FeedTS
		if ts == "" {
			ts = "undefined"
		}
		out = append(out, items[i].Key+"@"+ts)
	}
	return strings.Join(out, ",")
}

func TestActivityOrderMatchesSlacksReducer(t *testing.T) {
	rtm := func(it core.ActivityItem) *core.ActivityItem { return &it }
	cases := []struct {
		name  string
		list  []core.ActivityItem
		steps []actOrderStep
	}{
		// Karim's: a mention, a reaction, a reply in a thread listed
		// below them, a reply in one listed lower still, then the feed
		{"karim", []core.ActivityItem{
			actOIL("thread_v2-C1-1799999000.000100", "thread_v2", "1799998800.000100", "1799998800.000100"),
			actOIL("thread_v2-C2-1799980000.000100", "thread_v2", "1799989200.000100", "1799989200.000100"),
			actOI("fa-old", "at_channel", "1799913600.000100"),
		}, []actOrderStep{
			{rtm: rtm(actOI("at_user-C1-1799999460.000100", "at_user", "1799999460.000100")),
				want: "at_user-C1-1799999460.000100@1799999460.000100,thread_v2-C1-1799999000.000100@1799998800.000100,thread_v2-C2-1799980000.000100@1799989200.000100,fa-old@1799913600.000100"},
			{rtm: rtm(actOI("message_reaction-C1-1799999520.000100", "message_reaction", "1799999520.000100")),
				want: "message_reaction-C1-1799999520.000100@1799999520.000100,at_user-C1-1799999460.000100@1799999460.000100,thread_v2-C1-1799999000.000100@1799998800.000100,thread_v2-C2-1799980000.000100@1799989200.000100,fa-old@1799913600.000100"},
			{rtm: rtm(actOIL("thread_v2-C1-1799999000.000100", "thread_v2", "1799999580.000100", "1799999580.000100")),
				want: "thread_v2-C1-1799999000.000100@1799999580.000100,message_reaction-C1-1799999520.000100@1799999520.000100,at_user-C1-1799999460.000100@1799999460.000100,thread_v2-C2-1799980000.000100@1799989200.000100,fa-old@1799913600.000100"},
			{rtm: rtm(actOIL("thread_v2-C2-1799980000.000100", "thread_v2", "1799999990.000100", "1799999990.000100")),
				want: "thread_v2-C2-1799980000.000100@1799999990.000100,thread_v2-C1-1799999000.000100@1799999580.000100,message_reaction-C1-1799999520.000100@1799999520.000100,at_user-C1-1799999460.000100@1799999460.000100,fa-old@1799913600.000100"},
			{page: []core.ActivityItem{
				actOIL("thread_v2-C2-1799980000.000100", "thread_v2", "1799999990.000100", "1799999990.000100"),
				actOIL("thread_v2-C1-1799999000.000100", "thread_v2", "1799999580.000100", "1799999580.000100"),
				actOI("message_reaction-C1-1799999520.000100", "message_reaction", "1799999520.000100"),
			}, want: "thread_v2-C2-1799980000.000100@1799999990.000100,thread_v2-C1-1799999000.000100@1799999580.000100,message_reaction-C1-1799999520.000100@1799999520.000100,at_user-C1-1799999460.000100@1799999460.000100,fa-old@1799913600.000100"},
		}},
		// 1Cyf v: a mention stands for the thread reply, DM message or
		// channel post it shares a ts with
		{"mention-stands-for-thread", []core.ActivityItem{
			actOIL("T", "thread_v2", "100.000000", "100.000000"),
			actOI("old", "keyword", "50.000000"),
		}, []actOrderStep{
			{rtm: rtm(actOI("M", "at_user", "200.000000")), want: "M@200.000000,T@100.000000,old@50.000000"},
			{rtm: rtm(actOIL("T", "thread_v2", "200.000000", "200.000000")), want: "M@200.000000,T@100.000000,old@50.000000"},
			{rtm: rtm(actOIL("T", "thread_v2", "300.000000", "300.000000")), want: "T@300.000000,M@200.000000,old@50.000000"},
			{rtm: rtm(actOIL("D", "dm", "400.000000", "200.000000")), want: "T@300.000000,M@200.000000,old@50.000000"},
			{rtm: rtm(actOIL("C", "channel", "500.000000", "250.000000")), want: "C@500.000000,T@300.000000,M@200.000000,old@50.000000"},
			{page: []core.ActivityItem{
				actOIL("T2", "thread_v2", "200.000000", "200.000000"),
				actOI("M2", "keyword", "250.000000"),
			}, want: "T@300.000000,M2@250.000000,M@200.000000,old@50.000000"},
		}},
		// equal feedTs: the keys by localeCompare (en-US); no feedTs is 0
		{"ties-by-key", nil, []actOrderStep{
			{page: []core.ActivityItem{
				actOI("b", "at_user", "10.000000"), actOI("B", "at_user", "10.000000"), actOI("a-x", "at_user", "10.000000"),
				actOI("a_x", "at_user", "10.000000"), actOI("ax", "at_user", "10.000000"), actOI("a", "at_user", "10.000000"),
				actOI("A1", "at_user", "10.000000"), actOI("a10", "at_user", "10.000000"), actOI("a9", "at_user", "10.000000"),
				actOI("nofeed", "at_user", ""), actOI("z", "at_user", "11"),
			}, want: "z@11,a@10.000000,a_x@10.000000,a-x@10.000000,A1@10.000000,a10@10.000000,a9@10.000000,ax@10.000000,b@10.000000,B@10.000000,nofeed@undefined"},
			{rtm: rtm(actOI("aa", "keyword", "10.000000")),
				want: "z@11,a@10.000000,a_x@10.000000,a-x@10.000000,A1@10.000000,a10@10.000000,a9@10.000000,aa@10.000000,ax@10.000000,b@10.000000,B@10.000000,nofeed@undefined"},
		}},
		// a page joins every row the list holds, the page's copies winning
		{"a-page-keeps-every-row", []core.ActivityItem{
			actOI("n1", "at_user", "90.000000"), actOI("gone", "at_user", "80.000000"), actOI("n2", "at_user", "70.000000"),
			actOI("p2a", "at_user", "40.000000"), actOI("p2b", "at_user", "30.000000"),
		}, []actOrderStep{
			{page: []core.ActivityItem{actOI("new", "at_user", "95.000000"), actOI("n2", "at_user", "92.000000"), actOI("n1", "at_user", "90.000000")},
				want: "new@95.000000,n2@92.000000,n1@90.000000,gone@80.000000,p2a@40.000000,p2b@30.000000"},
		}},
		// a page out of time order (the VIP sort's) is stored by feedTs
		{"unsorted-page", nil, []actOrderStep{
			{page: []core.ActivityItem{actOI("vip-old", "at_user", "10.000000"), actOI("vip-new", "at_user", "50.000000"),
				actOI("rest-a", "at_user", "60.000000"), actOI("rest-b", "at_user", "20.000000")},
				want: "rest-a@60.000000,vip-new@50.000000,rest-b@20.000000,vip-old@10.000000"},
			{page: []core.ActivityItem{actOI("p2", "at_user", "15.000000"), actOI("rest-a", "at_user", "61.000000")},
				want: "rest-a@61.000000,vip-new@50.000000,rest-b@20.000000,p2@15.000000,vip-old@10.000000"},
		}},
	}
	for _, c := range cases {
		list := slices.Clone(c.list)
		for i, s := range c.steps {
			if s.rtm != nil {
				list = actUpsertRtm(list, *s.rtm)
			} else {
				got := actUpsertPage(core.ActivityPage{Items: s.page, Next: "page-cursor"}, core.ActivityPage{Items: list, Next: "old-cursor"}, actUpsertOpts{})
				if got.Next != "page-cursor" {
					t.Errorf("%s step %d: cursor %q, want the page's", c.name, i, got.Next)
				}
				list = got.Items
			}
			if got := actOrderKeys(list); got != s.want {
				t.Errorf("%s step %d:\n got %s\nwant %s", c.name, i, got, s.want)
			}
		}
	}
}

// A realtime entry makes a new list (a load in flight may still read the
// old one), and leaves the old one as it was.
func TestActivityRtmUpsertNewSlice(t *testing.T) {
	old := []core.ActivityItem{actOI("a", "at_user", "3.0"), actOI("b", "at_user", "2.0"), actOI("c", "at_user", "1.0")}
	keep := slices.Clone(old)
	got := actUpsertRtm(old, actOI("c", "at_user", "4.0"))
	if actOrderKeys(got) != "c@4.0,a@3.0,b@2.0" {
		t.Fatalf("got %s", actOrderKeys(got))
	}
	if actOrderKeys(old) != actOrderKeys(keep) {
		t.Errorf("the old list changed: %s", actOrderKeys(old))
	}
	if &got[0] == &old[0] {
		t.Error("the upsert reused the old list's array")
	}
}

// A reload of the plain list in time order (actReloadKeeps): a row within
// the page's span the page no longer lists is gone, what came in on the
// socket meanwhile stays, the later pages loaded stay with their cursor
// when the page reaches them; a page that doesn't (more new rows than a
// page) or that ends the feed keeps none.
func TestActivityPlainReload(t *testing.T) {
	cur := core.ActivityPage{Next: "old-cursor", Items: []core.ActivityItem{
		actOI("sock", "at_user", "105.0"), // in on the socket while the request was out
		actOI("a", "at_user", "100.0"), actOI("gone", "at_user", "90.0"), actOI("b", "at_user", "80.0"),
		actOI("c", "at_user", "70.0"), actOI("d", "at_user", "60.0"), actOI("e", "at_user", "50.0"),
	}}
	view := actReloadCase{before: []string{"a", "gone", "b", "c", "d", "e"}}
	for _, c := range []struct {
		name       string
		page       []core.ActivityItem
		next       string
		want, cur2 string
	}{
		{"reaches the later pages", []core.ActivityItem{actOI("new", "at_user", "110.0"), actOI("a", "at_user", "100.0"),
			actOI("b", "at_user", "80.0"), actOI("c", "at_user", "70.0")}, "page-2",
			"new@110.0,sock@105.0,a@100.0,b@80.0,c@70.0,d@60.0,e@50.0", "old-cursor"},
		{"more new rows than a page", []core.ActivityItem{actOI("n1", "at_user", "200.0"), actOI("n2", "at_user", "190.0"),
			actOI("n3", "at_user", "180.0")}, "page-2",
			"n1@200.0,n2@190.0,n3@180.0,sock@105.0", "page-2"},
		{"the feed's last page", []core.ActivityItem{actOI("a", "at_user", "100.0"), actOI("b", "at_user", "80.0")}, "",
			"sock@105.0,a@100.0,b@80.0", ""},
		{"nothing left", nil, "", "sock@105.0", ""},
	} {
		got := actReloadTest(core.ActivityPage{Items: c.page, Next: c.next}, cur, view)
		if k := actOrderKeys(got.Items); k != c.want || got.Next != c.cur2 {
			t.Errorf("%s:\n got %s (cursor %q)\nwant %s (cursor %q)", c.name, k, got.Next, c.want, c.cur2)
		}
	}
}
