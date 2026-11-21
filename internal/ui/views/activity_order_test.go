package views

// The order Slack's activityIA4 store keeps (activity_order.go) against
// Slack's own code: every expected list here is what client-boot tsND's
// getUpsertedActivityRtmItems / getUpsertedActivityItems /
// getItemsRetainedOnRefresh, 1Cyf v and um/L Y (build 35b42de8b870,
// copied verbatim, lodash's compact and uniqBy stubbed, en-US ICU) answer
// for the same steps (the oracle in
// /root/clack-ops/scratch/activity-order/oracle). Synthetic data.

import (
	"slices"
	"strings"
	"testing"

	"github.com/some-du6e/clack/internal/core"
)

// actOrderStep is a realtime entry (rtm) or a feed page; a page with
// refresh joins as a refresh of a view with filters, before being the
// list's keys when it went out (nil: the list's keys at the step) and
// marks the rows marked unread here.
type actOrderStep struct {
	rtm     *core.ActivityItem
	page    []core.ActivityItem
	refresh bool
	before  []string
	marks   []string
	want    string // key@feedTs, ...
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
		// the plain list's reload: every row stays, the page's copies win
		{"root-refresh-keeps-every-row", []core.ActivityItem{
			actOI("n1", "at_user", "90.000000"), actOI("gone", "at_user", "80.000000"), actOI("n2", "at_user", "70.000000"),
			actOI("p2a", "at_user", "40.000000"), actOI("p2b", "at_user", "30.000000"),
		}, []actOrderStep{
			{page: []core.ActivityItem{actOI("new", "at_user", "95.000000"), actOI("n2", "at_user", "92.000000"), actOI("n1", "at_user", "90.000000")},
				want: "new@95.000000,n2@92.000000,n1@90.000000,gone@80.000000,p2a@40.000000,p2b@30.000000"},
		}},
		// a filtered view's reload: getItemsRetainedOnRefresh
		{"filtered-refresh-retains", []core.ActivityItem{
			actOI("newer", "at_user", "1700000009.000100"), actOI("kept-unread", "at_user", "1700000005.000100"),
			actOI("read-since", "at_user", "1700000004.000100"), actOI("marked-unread", "at_user", "1700000003.000100"),
			actOI("socket", "at_user", "1700000001.000100"), actOI("older-unread", "at_user", "1700000002.000100"),
		}, []actOrderStep{
			{refresh: true, before: []string{"newer", "kept-unread", "read-since", "marked-unread", "older-unread"}, marks: []string{"marked-unread"},
				page: []core.ActivityItem{actOI("new-unread", "at_user", "1700000006.000100"), actOI("kept-unread", "at_user", "1700000005.000100")},
				want: "newer@1700000009.000100,new-unread@1700000006.000100,kept-unread@1700000005.000100,marked-unread@1700000003.000100,socket@1700000001.000100"},
			{refresh: true, before: []string{"newer", "new-unread", "kept-unread", "marked-unread", "socket"}, marks: []string{"marked-unread"},
				page: []core.ActivityItem{}, want: "marked-unread@1700000003.000100"},
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
				o := actUpsertOpts{refresh: s.refresh}
				if s.refresh {
					o.before, o.unreadMarks = map[string]bool{}, map[string]bool{}
					for _, k := range s.before {
						o.before[k] = true
					}
					for _, k := range s.marks {
						o.unreadMarks[k] = true
					}
				}
				got := actUpsertPage(core.ActivityPage{Items: s.page, Next: "page-cursor"}, core.ActivityPage{Items: list, Next: "old-cursor"}, o)
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
