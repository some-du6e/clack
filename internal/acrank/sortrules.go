package acrank

// Slack's sort rules (client-boot y+lP applySortRules, the rules in J3WV;
// CDN build 4d0c78085232): reorderings the Sorter applies to its sorted
// results before boostExactMatches, for the sources they list. The only
// rules this account's experiments turn on are
// search_autocomplete_promote_exact_name_matches':
//
//   - promote_exact_name_matches_in_context_v1 (treatment_3, treatment_5):
//     up to two exact name matches who are members of the current
//     conversation (or thread) go to the first member slots;
//   - promote_exact_name_matches_top3_v1 (treatment_3) / _top5_v1
//     (treatment_5): up to two other exact name matches, minus the ones
//     already lifted, are lifted to member slot 2 (or 4) plus those.
//
// Both only move member results, within the positions members hold
// (reorderWithinSlots). An exact name match (isEligibleExactMatch) is a
// member who is an active human of the org (not deleted, forgotten,
// suspended, invited, profile-only, external, a bot or app, or Slackbot)
// whose real or display name, normalized, is the normalized query. In a
// treatment group up to five of them are also kept in the model's input
// (exposeAndRetainExactNameMatches + includeRetainedResultsInModelInput),
// for texty-autocomplete and the switcher's searcher.

// ExpExactNames is the experiment the rules belong to.
const ExpExactNames = "search_autocomplete_promote_exact_name_matches"

// Rule ids.
const (
	RuleExactInContext = "promote_exact_name_matches_in_context_v1"
	RuleExactTop3      = "promote_exact_name_matches_top3_v1"
	RuleExactTop5      = "promote_exact_name_matches_top5_v1"
)

// ExactNameSources are the sort sources the rules run for; RetainSources
// the ones whose exact matches are kept in the model's input.
var (
	ExactNameSources = []string{SourceTexty, "omniswitcher:suggestions-from-searcher", "omniswitcher"}
	RetainSources    = []string{SourceTexty, "omniswitcher:suggestions-from-searcher"}
)

// ExactNameRules returns the rule ids a group turns on, in Slack's order.
func ExactNameRules(group string) []string {
	switch group {
	case "treatment_3":
		return []string{RuleExactInContext, RuleExactTop3}
	case "treatment_5":
		return []string{RuleExactInContext, RuleExactTop5}
	}
	return nil
}

// ExactFacts are the rules' predicates for one result.
type ExactFacts struct {
	User      bool // isUser: a member result
	Eligible  bool // isEligibleExactMatch
	InContext bool // … and in the current conversation or thread
}

// StablePartition is stablePartition: up to max entries pred accepts
// first, stably.
func StablePartition[T any](e []T, pred func(T) bool, max int) []T {
	var a, rest []T
	for _, x := range e {
		if len(a) < max && pred(x) {
			a = append(a, x)
		} else {
			rest = append(rest, x)
		}
	}
	return append(a, rest...)
}

// StableLift is stableLiftToPosition: the first head entries stay, then
// up to max of the rest that pred accepts.
func StableLift[T any](e []T, head int, pred func(T) bool, max int) []T {
	if head > len(e) {
		head = len(e)
	}
	out := append([]T(nil), e[:head]...)
	return append(out, StablePartition(e[head:], pred, max)...)
}

// ReorderWithinSlots is reorderWithinSlots: the entries slot accepts are
// reordered by f in the positions they hold.
func ReorderWithinSlots[T any](e []T, slot func(T) bool, f func([]T) []T) []T {
	var pos []int
	var sub []T
	for i, x := range e {
		if slot(x) {
			pos = append(pos, i)
			sub = append(sub, x)
		}
	}
	r := f(sub)
	out := append([]T(nil), e...)
	for k, i := range pos {
		out[i] = r[k]
	}
	return out
}

// ApplyExactNameRules runs the group's rules over a sorted list.
func ApplyExactNameRules[T any](list []T, group string, facts func(T) ExactFacts) []T {
	isUser := func(x T) bool { return facts(x).User }
	inCtx := func(x T) bool { f := facts(x); return f.Eligible && f.InContext }
	for _, rule := range ExactNameRules(group) {
		switch rule {
		case RuleExactInContext:
			list = ReorderWithinSlots(list, isUser, func(us []T) []T { return StableLift(us, 0, inCtx, 2) })
		case RuleExactTop3, RuleExactTop5:
			head := 2
			if rule == RuleExactTop5 {
				head = 4
			}
			list = ReorderWithinSlots(list, isUser, func(us []T) []T {
				n := 0 // inContextLifted
				for i := 0; i < len(us) && i < 2; i++ {
					if inCtx(us[i]) {
						n++
					}
				}
				return StableLift(us, head+n, func(x T) bool { return facts(x).Eligible }, 2-n)
			})
		}
	}
	return list
}

// RetainExactNames is exposeAndRetainExactNameMatches with
// includeRetainedResultsInModelInput: in a treatment group the first five
// exact name matches of pool (in its order) are kept in kept, each one
// missing taking the place of the last entry that isn't one (or appended
// while kept is shorter than limit). same reports whether two entries are
// one result.
func RetainExactNames[T any](kept, pool []T, group string, limit int, facts func(T) ExactFacts, same func(a, b T) bool) []T {
	if group != "treatment_3" && group != "treatment_5" {
		return kept
	}
	var retain []T
	for _, x := range pool {
		if f := facts(x); f.User && f.Eligible {
			retain = append(retain, x)
			if len(retain) == 5 {
				break
			}
		}
	}
	isRetained := func(x T) bool {
		for _, r := range retain {
			if same(r, x) {
				return true
			}
		}
		return false
	}
	out := append([]T(nil), kept...)
	for _, r := range retain {
		present := false
		for _, x := range out {
			if same(x, r) {
				present = true
				break
			}
		}
		if present {
			continue
		}
		if len(out) < limit {
			out = append(out, r)
			continue
		}
		at := -1
		for i := len(out) - 1; i >= 0; i-- {
			if !isRetained(out[i]) {
				at = i
				break
			}
		}
		if at < 0 {
			break
		}
		out[at] = r
	}
	return out
}
