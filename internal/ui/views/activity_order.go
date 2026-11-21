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
//     sorted, with the page's cursor. A fetch from the top on a view with
//     filters (9OWn Wk) is a refresh there (getItemsRetainedOnRefresh
//     keeps the rows in on the socket meanwhile, marked unread here or
//     newer than the page); on the plain list every row stays. Slack
//     takes rows out locally (removeBulkItems, the invites' tombstones,
//     wJvP's restore) where Clack reloads, so Clack's reloads keep ui's
//     rows instead (actReloadKeeps): what changed on the socket while the
//     request was out, and the later pages loaded, while the page's span
//     is the page's.
//   - The VIP sort draws the stored order split in two (9OWn Y,
//     actVIPOrder): the unread VIP rows, then the rest, each by Y.

import (
	"slices"
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

// actUpsertOpts is how a page joins the list (tsND O's payload, and
// Clack's reload rule).
type actUpsertOpts struct {
	// reload is a fetch from the top (actRefresh): the list keeps only
	// actReloadKeeps' rows besides the page
	reload bool
	// before is the list's keys when the request went out
	// (keysBeforeRefresh)
	before map[string]bool
	// touched is the keys a realtime entry upserted while the request was
	// out: their copies are newer than the request
	touched map[string]bool
	// unreadMarks is pendingUnreadMarkKeys (Unreads only): the rows
	// marked unread here, kept while they are
	unreadMarks map[string]bool
	// sticky is Unreads: a reload keeps no later pages (ui's actRetained,
	// Slack's filtered refresh)
	sticky bool
	// vip is the VIP sort: activity.feed answers priority_unreads_first_v1
	vip bool
}

// actServerCompare is the order activity.feed answers in: chrono_v1's um/L
// Y, or under the VIP sort (priority_unreads_first_v1) the unread VIP
// entries first, each part by Y.
func actServerCompare(a, b *core.ActivityItem, vip bool) int {
	if vip {
		if ga, gb := a.Priority && a.Unread, b.Priority && b.Unread; ga != gb {
			if ga {
				return -1
			}
			return 1
		}
	}
	return actFeedCompare(a, b)
}

// actReloadKeeps is the rows a reload keeps besides its page, and its
// cursor: ui's rule (actMerge, actRetained) over the list in Slack's
// order. Kept are the rows the list didn't hold when the request went out
// and the ones a realtime entry changed meanwhile (both newer than the
// page's read), the ones marked unread here (Unreads), and, but under
// Unreads, the later pages loaded past the page with their cursor: when
// the page reaches the rows loaded before (its last row the list can
// hold, 1Cyf v over the page and the list, was one of them) and isn't the
// feed's last. Every other row in the page's span is the page's to list:
// one it doesn't list is gone (an ignored or accepted invitation, a
// cleared or deleted entry). Under the VIP sort the pages aren't in time
// order and a read row moves down: when a row loaded before the page's
// last, in the feed's order, isn't on the page, the later pages go too
// and paging continues from the page.
func actReloadKeeps(cur, page core.ActivityPage, o actUpsertOpts) ([]core.ActivityItem, string) {
	var kept []core.ActivityItem
	in := map[string]bool{}
	for i := range cur.Items {
		it := &cur.Items[i]
		if k := actKey(it); !o.before[k] || o.touched[k] || o.unreadMarks[k] {
			kept = append(kept, *it)
			in[k] = true
		}
	}
	if o.sticky || len(page.Items) == 0 || page.Next == "" {
		return kept, page.Next
	}
	all := make([]core.ActivityItem, 0, len(page.Items)+len(cur.Items))
	all = append(append(all, page.Items...), cur.Items...)
	stands := actMentionStands(all)
	var last *core.ActivityItem
	for i := len(page.Items) - 1; i >= 0; i-- {
		if stands[i] {
			last = &page.Items[i]
			break
		}
	}
	if last == nil || !o.before[actKey(last)] {
		return kept, page.Next // more new rows than a page: continue right after it
	}
	listed := make(map[string]bool, len(page.Items))
	for i := range page.Items {
		listed[actKey(&page.Items[i])] = true
	}
	if o.vip {
		for i := range cur.Items {
			it := &cur.Items[i]
			if k := actKey(it); !in[k] && !listed[k] && actServerCompare(it, last, true) < 0 {
				return kept, page.Next // moved down (or gone): page again from the page
			}
		}
	}
	later := false
	for i := range cur.Items {
		it := &cur.Items[i]
		if k := actKey(it); !in[k] && !listed[k] && actServerCompare(last, it, o.vip) < 0 {
			kept = append(kept, *it)
			in[k], later = true, true
		}
	}
	if later {
		return kept, cur.Next
	}
	return kept, page.Next
}

// actUpsertPage is upsertActivityItems: the page joined to the list cur
// holds (on a reload, the rows actReloadKeeps keeps), deduplicated with
// the page's copies first, sorted, and the page's cursor (a reload that
// keeps the later pages, theirs). A row a realtime entry changed while the
// request was out keeps its copy over the page's when the copies' read
// state differs or its feed_ts is newer (WWrp applyNewerActivityRtmItems
// does the first: the page was read before it).
func actUpsertPage(page, cur core.ActivityPage, o actUpsertOpts) core.ActivityPage {
	existing, next := cur.Items, page.Next
	if o.reload {
		existing, next = actReloadKeeps(cur, page, o)
	}
	items := page.Items
	if len(o.touched) > 0 {
		at := make(map[string]int, len(o.touched))
		for i := range cur.Items {
			if k := actKey(&cur.Items[i]); o.touched[k] {
				at[k] = i
			}
		}
		copied := false
		for i := range items {
			j, ok := at[actKey(&items[i])]
			if !ok {
				continue
			}
			if c := &cur.Items[j]; c.Unread != items[i].Unread || actTSLess(items[i].FeedTS, c.FeedTS) {
				if !copied {
					items, copied = slices.Clone(items), true
				}
				items[i] = *c
			}
		}
	}
	all := actDeduped(items, existing)
	actSortFeed(all)
	return core.ActivityPage{Items: all, Next: next, Convs: actRefs(cur.Convs, page.Convs)}
}
