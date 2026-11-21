package views

// Activity's order, as Slack's activity inbox store keeps it (client-boot
// tsND, the activityIA4 reducer, build 35b42de8b870):
//
//   - Every list (rootList, filteredList, peekViewList) is sorted by um/L
//     Y: the newer feedTs first, then the key (localeCompare). That holds
//     for every entry type: a mention's feed_ts is its message, a
//     reaction's the reaction, a thread_v2 bundle's the reply that last
//     notified you (the server sends the bundle again, same key, with
//     each new reply's ts).
//   - A realtime entry (activityUpdatedV2 → PY/r pushActivityItemToStore →
//     tsND N upsertActivityRtmItem) goes through
//     getUpsertedActivityRtmItems: [entry, ...list] deduplicated
//     (getDedupedUniqueActivityItems: 1Cyf v, then uniqBy key, the entry
//     first so it wins), and the entry put back at its sorted place by
//     binary search. A thread that gets a reply leaves its old place for
//     the top.
//   - A feed page (eP2o / Ueic fetchActivityFeed → tsND O
//     upsertActivityItems) goes through getUpsertedActivityItems: [page,
//     ...existing] deduplicated the same way (the page's copy wins) and
//     sorted. A fetch from the top (fetchNew) on a view with filters
//     (9OWn Wk, `$`: types, Unreads, VIP, Cleared, automations, In) is a
//     refresh (PY/r refreshItems: T && !!b): the existing rows are only
//     getItemsRetainedOnRefresh's, the ones the list didn't hold when the
//     request went out (keysBeforeRefresh: they came in on the socket
//     since), the ones marked unread here and still unread
//     (pendingUnreadMarkKeys, Unreads only) and the ones newer than the
//     page's newest. The cursor is the page's. On the plain list every row
//     stays in Slack's (actPlainReload says where Clack's differs).
//   - The VIP sort draws the stored order split in two (9OWn Y,
//     actVIPOrder): the unread VIP rows, then the rest, each by Y.

import (
	"sort"
	"sync"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"

	"github.com/some-du6e/clack/internal/core"
)

// actKeyCollator is String.prototype.localeCompare's default collator
// (en-US, no options), which um/L Y orders equal feedTs by; a collator
// isn't safe for concurrent use, and both the UI goroutine (realtime
// entries) and the load's (pages) sort.
var actKeyCollator struct {
	sync.Mutex
	c *collate.Collator
}

func actKeyCompare(a, b string) int {
	if a == b {
		return 0
	}
	actKeyCollator.Lock()
	defer actKeyCollator.Unlock()
	if actKeyCollator.c == nil {
		actKeyCollator.c = collate.New(language.AmericanEnglish)
	}
	return actKeyCollator.c.CompareString(a, b)
}

// actFeedCompare is um/L Y: the newer feedTs first (parseFloat(feedTs ||
// "0"), so an entry without one is the oldest), then the key ascending
// (the entry's own key, "" without one).
func actFeedCompare(a, b *core.ActivityItem) int {
	at, bt := a.FeedTS, b.FeedTS
	if at == "" {
		at = "0"
	}
	if bt == "" {
		bt = "0"
	}
	switch {
	case actTSLess(bt, at):
		return -1
	case actTSLess(at, bt):
		return 1
	}
	return actKeyCompare(a.Key, b.Key)
}

// actSortFeed sorts items by um/L Y.
func actSortFeed(items []core.ActivityItem) {
	sort.SliceStable(items, func(i, j int) bool { return actFeedCompare(&items[i], &items[j]) < 0 })
}

// actFeedSorted reports whether items are in um/L Y's order.
func actFeedSorted(items []core.ActivityItem) bool {
	for i := 1; i < len(items); i++ {
		if actFeedCompare(&items[i-1], &items[i]) > 0 {
			return false
		}
	}
	return true
}

// actMentionStands is 1Cyf v: over a combined list, a thread_v2 entry
// whose feedTs is a mention's (Lo+X L: at_user, at_user_group,
// at_channel, at_everyone, keyword), and a dm or channel entry whose
// latestTs is one, is left out: the mention stands for the reply or the
// message. It reports which entries stay.
func actMentionStands(items []core.ActivityItem) []bool {
	mentions := map[string]bool{}
	for i := range items {
		if it := &items[i]; actIsMention(it.Type) && it.FeedTS != "" {
			mentions[it.FeedTS] = true
		}
	}
	keep := make([]bool, len(items))
	for i := range items {
		it := &items[i]
		switch it.Type {
		case "thread_v2":
			keep[i] = it.FeedTS == "" || !mentions[it.FeedTS]
		case "dm", "channel":
			keep[i] = it.LatestTS == "" || !mentions[it.LatestTS]
		default:
			keep[i] = true
		}
	}
	return keep
}

// actDeduped is getDedupedUniqueActivityItems over [first..., rest...]:
// 1Cyf v, then one entry per key, the first one met (uniqBy).
func actDeduped(first, rest []core.ActivityItem) []core.ActivityItem {
	all := make([]core.ActivityItem, 0, len(first)+len(rest))
	all = append(append(all, first...), rest...)
	return actUniq(all, actMentionStands(all))
}

// actUniq is uniqBy key over the entries of all that keep holds (all's
// backing array is reused).
func actUniq(all []core.ActivityItem, keep []bool) []core.ActivityItem {
	seen := make(map[string]bool, len(all))
	out := all[:0]
	for i := range all {
		if !keep[i] {
			continue
		}
		k := actKey(&all[i])
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, all[i])
	}
	return out
}

// actUpsertRtm is getUpsertedActivityRtmItems: the realtime entry in
// items, replacing the copy with its key, at its sorted place (a binary
// search, as Slack's: the list is in order). An entry 1Cyf v leaves out
// (a mention stands for it) changes nothing but the list's own
// deduplication: an older copy with its key stays where it was.
func actUpsertRtm(items []core.ActivityItem, in core.ActivityItem) []core.ActivityItem {
	all := make([]core.ActivityItem, 0, len(items)+1)
	all = append(append(all, in), items...)
	keep := actMentionStands(all)
	took := keep[0] // Slack's a[0] === e: the entry is the first one kept
	a := actUniq(all, keep)
	r := a
	if took {
		r = a[1:]
	}
	if !actFeedSorted(r) {
		actSortFeed(a)
		return a
	}
	if !took {
		return a
	}
	lo, hi := 0, len(r)
	for lo < hi {
		mid := (lo + hi) / 2
		if actFeedCompare(&in, &r[mid]) <= 0 {
			hi = mid
		} else {
			lo = mid + 1
		}
	}
	out := make([]core.ActivityItem, 0, len(a))
	out = append(out, r[:lo]...)
	out = append(out, in)
	return append(out, r[lo:]...)
}

// actUpsertOpts is how a page joins the list (tsND O's payload).
type actUpsertOpts struct {
	// refresh is refreshItems: a fetch from the top on a view with
	// filters; the list keeps only getItemsRetainedOnRefresh's rows
	refresh bool
	// before is keysBeforeRefresh: the list's keys when the request went
	// out
	before map[string]bool
	// unreadMarks is pendingUnreadMarkKeys (Unreads only): the rows
	// marked unread here, kept while they are
	unreadMarks map[string]bool
	// plain is a reload of the plain list in time order (activityReload):
	// the retained rows, and the rows past the page (later pages loaded)
	plain bool
}

// actRetainedOnRefresh is getItemsRetainedOnRefresh: of the rows the list
// holds, the ones it didn't hold when the request went out, the ones
// marked unread here (still unread: unreadMarks holds the rows whose
// local state is unread) and the ones newer than the page's newest.
func actRetainedOnRefresh(cur, page []core.ActivityItem, o actUpsertOpts) []core.ActivityItem {
	newest, some := "", len(page) > 0
	for i := range page {
		if ts := page[i].FeedTS; newest == "" || actTSLess(newest, ts) {
			newest = ts
		}
	}
	if newest == "" {
		newest = "0"
	}
	var out []core.ActivityItem
	for i := range cur {
		it := &cur[i]
		k := actKey(it)
		ts := it.FeedTS
		if ts == "" {
			ts = "0"
		}
		if !o.before[k] || o.unreadMarks[k] || some && actTSLess(newest, ts) {
			out = append(out, *it)
		}
	}
	return out
}

// actUpsertPage is upsertActivityItems: the page joined to the list cur
// holds (the rows retained on a refresh), deduplicated with the page's
// copies first, sorted, and the page's cursor.
func actUpsertPage(page, cur core.ActivityPage, o actUpsertOpts) core.ActivityPage {
	existing, next := cur.Items, page.Next
	switch {
	case o.refresh:
		existing = actRetainedOnRefresh(cur.Items, page.Items, o)
	case o.plain:
		existing, next = actPlainReload(cur, page, o)
	}
	items := actDeduped(page.Items, existing)
	actSortFeed(items)
	return core.ActivityPage{Items: items, Next: next, Convs: actRefs(cur.Convs, page.Convs)}
}

// actPlainReload is the rows a reload of the plain list in time order
// keeps besides the page, and its cursor. Slack's keeps every row the
// list holds (refreshItems is off there): its reducers take out a row
// cleared, ignored or accepted here, where Clack's Clear all before
// this, invitation and restore flows read the reload instead. So a row
// within the page's span that the page no longer lists is gone, and the
// rest is getItemsRetainedOnRefresh's (what came in on the socket
// meanwhile, newer than the page) and, when the page reaches into the
// rows loaded before and isn't the feed's last, the later pages loaded
// past it, with their cursor. A page that doesn't reach them (more new
// rows than a page) keeps none, its cursor continuing right after it.
func actPlainReload(cur, page core.ActivityPage, o actUpsertOpts) ([]core.ActivityItem, string) {
	kept := actRetainedOnRefresh(cur.Items, page.Items, o)
	if len(page.Items) == 0 || page.Next == "" {
		return kept, page.Next
	}
	last := &page.Items[0]
	for i := range page.Items {
		if actFeedCompare(&page.Items[i], last) > 0 {
			last = &page.Items[i]
		}
	}
	if !o.before[actKey(last)] {
		return kept, page.Next
	}
	in := make(map[string]bool, len(kept))
	for i := range kept {
		in[actKey(&kept[i])] = true
	}
	later := false
	for i := range cur.Items {
		it := &cur.Items[i]
		if k := actKey(it); !in[k] && actFeedCompare(last, it) < 0 {
			kept = append(kept, *it)
			in[k], later = true, true
		}
	}
	if later {
		return kept, cur.Next
	}
	return kept, page.Next
}
