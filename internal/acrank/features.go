package acrank

import (
	"math"
	"strings"
)

// Feature extraction (gSR8) for the kinds the composer completes. Each
// kind gets the common list (I) plus its own (C users, E channels, L
// emoji, P keywords); usergroups and the slash-command menu get only the
// common list. A feature Clack can't know (offline CTRs, Salesforce,
// mpims) is left out, which is a 0 as in Slack when the data is missing.

// expAgeDecay is min(1, exp(ln(.5)/halfLife × age)); 0 without a ts.
func expAgeDecay(now, ts, half float64) float64 {
	if ts == 0 || math.IsNaN(ts) {
		return 0
	}
	a := math.Max(0, now-ts)
	return math.Min(1, math.Exp(math.Log(.5)/half*a))
}

// olderOrMissing is ageLatestOlderOrMissing.
func olderOrMissing(now, ts, limit float64) bool {
	if ts == 0 {
		return true
	}
	a := math.Max(0, now-ts)
	return a == 0 || a > limit
}

func b2f(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// prefixNorm is isNormalizedPrefixMatch.
func prefixNorm(text, nq string) bool {
	n := dropSigil(Normalize(text))
	return n != "" && nq != "" && strings.HasPrefix(n, nq)
}

// exactNorm is isNormalizedExactMatch.
func exactNorm(text, nq string) bool {
	return dropSigil(Normalize(text)) == nq
}

func modelScore(e *entry, rawQ, nq string, numResults int, now float64, o Options) float64 {
	c := e.c
	w := o.Weights
	src := o.Source
	if src == "" {
		src = SourceTexty
	}
	total := 0.0
	add := func(name string, v float64) {
		if v != 0 && !math.IsNaN(v) && !math.IsInf(v, 0) {
			total += w.weight(name, src) * v
		}
	}
	// the query the text features compare against
	q := nq
	if c.Kind == Command && e.firstWord != "" {
		q, rawQ = e.firstWord, e.firstWord
	}
	name := featName(e)

	// ---- common (I) ----
	bonus := 0.0
	for _, v := range e.fs.bonus {
		bonus += v
	}
	add("FRECENCY_BONUS", bonus)
	if c.Kind == Member || c.Kind == Channel || c.Kind == UserGroup {
		add("FRECENCY", e.fs.score) // getFrecencyScoreForSuggestion: navigation types only
	}
	add("RAW_FRECENCY", e.fs.raw)
	add("FRECENCY_LOG_HIT_COUNT_SCALED", e.fs.logHitScaled)
	add("FRECENCY_LOG_ITEM_COUNT_SCALED", e.fs.logItemSca)
	add("FRECENCY_WEIGHT_HIT_VISITS", e.fs.weightHit)
	add("FRECENCY_WEIGHT_ITEM_VISITS", e.fs.weightItem)
	add("FRECENCY_HIT_COUNT", e.fs.hitCount)
	add("FRECENCY_ITEM_COUNT", e.fs.itemCount)
	add("FRECENCY_LOG_HIT_COUNT", math.Log1p(e.fs.hitCount))
	add("FRECENCY_LOG_ITEM_COUNT", math.Log1p(e.fs.itemCount))
	add("FUZZY_SCORE", e.score)
	fz, hasFz := e.fs.bonus["fuzzyScoreBonus"]
	add("FUZZY_SCORE_50", b2f(hasFz && fz == 50))
	add("FUZZY_SCORE_25", b2f(hasFz && fz == 25))
	add("FUZZY_SCORE_ALL", b2f(hasFz && fz != 0 && fz < 25))
	if c.Kind != Member && name != "" {
		add("NAV_NORMAL_PREFIX_MATCH_NO_USER", b2f(prefixNorm(name, q)))
		add("NAV_NORMAL_MATCH_NO_USER", b2f(exactNorm(name, q)))
	}
	if name != "" && c.Kind != Keyword { // keywords aren't navigational
		add("NAV_EXACT_MATCH", b2f(name == rawQ))
		add("NAV_NORMAL_MATCH", b2f(exactNorm(name, q)))
		add("NAV_NORMAL_PREFIX_MATCH", b2f(prefixNorm(name, q)))
	}
	add("IS_FLANNEL_FUZZY_PASSTHROUGH", b2f(e.passthrough))
	add("NUM_RESULTS", float64(numResults))
	ctr := otherCtr
	if v, ok := entityCtr[c.Kind]; ok {
		ctr = v
	}
	add("USER_ENTITY_TYPE_CTR_30DD", ctr)
	if src == SourceSlash {
		return total
	}

	switch c.Kind {
	case Member:
		add("TYPE_NAVIGATION_USER", 1)
		if c.IM {
			add("USER_PRIORITY", c.IMPriority)
		}
		add("USER_IS_ACTIVE", b2f(c.Active))
		add("USER_IS_DEACTIVATED", b2f(!c.Active))
		add("USER_IS_EXTERNAL", b2f(c.External))
		add("USER_IS_CURRENT_USER", b2f(c.Self))
		if o.HasChannel && !o.ChannelIsIM {
			in := c.InChannel
			add("MEMBER_OF_CURRENT_CHANNEL", b2f(in))
			add("NOT_MEMBER_OF_CURRENT_CHANNEL", b2f(!in))
		}
		add("CURRENT_USER_TEXTY", b2f(src == SourceTexty && c.Self))
		add("USER_SAME_TEAM", b2f(c.SameTeam))
		add("USER_IS_BOT", b2f(c.Bot))
		add("USER_IS_IN_CURRENT_THREAD", b2f(c.InThread))
		if c.IM {
			add("USER_DM_LATEST_MSG_AGE_DECAY_7D_V2", expAgeDecay(now, c.IMLatest, 604800))
			add("USER_DM_LATEST_MSG_AGE_DECAY_2H_V2", expAgeDecay(now, c.IMLatest, 7200))
			add("USER_DM_EMPTY_V2", b2f(c.IMLatest == 0))
			add("USER_DM_IS_STARRED", b2f(c.IMStarred))
		}
		add("USER_DM_LATEST_OLDER_30D_V2", b2f(olderOrMissing(now, c.IMLatest, 2592e3)))
		add("USER_DM_LATEST_OLDER_90D_V2", b2f(olderOrMissing(now, c.IMLatest, 7776e3)))
		add("USER_DM_NULL", b2f(!c.IM))
		lr := 0.0
		if c.IM {
			lr = c.IMLastRead
		}
		add("USER_HAS_DMED", b2f(lr != 0))
		add("USER_DM_READ_AGE_DECAY_2H_V3", expAgeDecay(now, lr, 7200))
		add("USER_DM_READ_AGE_DECAY_7D_V3", expAgeDecay(now, lr, 604800))
		add("USER_DM_READ_OLDER_7D", b2f(olderOrMissing(now, lr, 604800)))
		add("USER_DM_READ_OLDER_30D", b2f(olderOrMissing(now, lr, 2592e3)))
		add("USER_DM_READ_OLDER_90D", b2f(olderOrMissing(now, lr, 7776e3)))
		if name != "" {
			add("NAV_NORMAL_PREFIX_MATCH_USER", b2f(prefixNorm(name, q) && !prefixNorm(c.FirstName, q)))
			add("NAV_NORMAL_MATCH_USER", b2f(exactNorm(name, q) && !prefixNorm(c.FirstName, q)))
		}
		if c.FirstName != "" {
			add("USER_FIRST_NAME_NORMAL_MATCH", b2f(exactNorm(c.FirstName, q)))
			add("USER_FIRST_NAME_NORMAL_PREFIX_MATCH", b2f(prefixNorm(c.FirstName, q)))
			add("USER_FIRST_NAME_EXACT_MATCH", b2f(c.FirstName == rawQ))
		}
		if c.LastName != "" {
			add("USER_LAST_NAME_NORMAL_MATCH", b2f(exactNorm(c.LastName, q)))
			add("USER_LAST_NAME_NORMAL_PREFIX_MATCH", b2f(prefixNorm(c.LastName, q)))
			add("USER_LAST_NAME_EXACT_MATCH", b2f(c.LastName == rawQ))
		}
		add("QUERY_IS_SELF", b2f(c.Self && (q == "me" || q == "you")))
		add("HISTORY_AGE_DECAY_2H", expAgeDecay(now, c.History, 7200))
		add("HISTORY_AGE_DECAY_7D", expAgeDecay(now, c.History, 604800))
		if o.EnterpriseID != "" && o.EnterpriseID == c.EnterpriseID {
			n := 0
			def := false
			for _, t := range c.Teams {
				for _, s := range o.SelfTeams {
					if t == s {
						n++
						if t == o.DefaultWorkspace {
							def = true
						}
					}
				}
			}
			add("MEMBER_OF_SEARCHER_DEFAULT_TEAM", b2f(def))
			add("HAS_WORKSPACE_INTERSECTION_WITH_SEARCHER", b2f(n > 0))
			add("NUM_WORKSPACES_INTERSECTING_WITH_SEARCHER", float64(n))
		}
		add("USER_AFFINITY_SCORE", c.Affinity)
	case Channel:
		add("TYPE_NAVIGATION_CHANNEL", 1)
		add("CHANNEL_IS_MEMBER", b2f(c.IsMember))
		add("CHANNEL_IS_NOT_MEMBER", b2f(!c.IsMember))
		add("CHANNEL_IS_STARRED", b2f(c.Starred))
		add("CHANNEL_IS_ARCHIVED", b2f(c.Archived))
		add("CHANNEL_IS_OTHER_TEAM", b2f(c.OtherTeam))
		if name != "" {
			add("NAV_NORMAL_MATCH_CHANNEL_LOG_RESULTS", b2f(exactNorm(name, q))*math.Log1p(float64(numResults)))
			add("CHANNEL_QUERY_RATIO_LENGTH_STRING", ratioLen(name, rawQ))
		}
		add("CHANNEL_IS_UNREAD", b2f(c.Unread))
		add("CHANNEL_READ_AGE_DECAY_2H", expAgeDecay(now, c.LastRead, 7200))
		add("CHANNEL_READ_AGE_DECAY_7D", expAgeDecay(now, c.LastRead, 604800))
		add("CHANNEL_LATEST_MSG_AGE_DECAY_2H", expAgeDecay(now, c.Latest, 7200))
		add("CHANNEL_LATEST_MSG_AGE_DECAY_7D", expAgeDecay(now, c.Latest, 604800))
		add("ONLY_MATCHES_PREVIOUS_NAME", b2f(e.prevOnly))
		add("HISTORY_AGE_DECAY_2H", expAgeDecay(now, c.History, 7200))
		add("HISTORY_AGE_DECAY_7D", expAgeDecay(now, c.History, 604800))
		add("CHANNEL_PRIORITY_90D_SCORE", c.Priority90D)
	case Emoji:
		add("TYPE_EMOJI", 1)
		if e.exact && e.emojiExact {
			add("EMOJI_EXACT_MATCH", 1)
		}
		add("EMOJI_QUERY_LENGTH_RATIO", e.emojiRatio)
		if e.emojiCombo >= 0 {
			add("EMOJI_TOTAL_COMBO_BREAKS", float64(e.emojiCombo))
			for i, n := range []string{"EMOJI_COMBO_BREAK_0", "EMOJI_COMBO_BREAK_1", "EMOJI_COMBO_BREAK_2", "EMOJI_COMBO_BREAK_3"} {
				add(n, b2f(e.emojiCombo == i))
			}
		}
		ec := c.EmojiCtr
		if ec == 0 {
			ec = entityCtr[Emoji]
		}
		add("EMOJI_USER_CTR_30DD", ec)
		add("EMOJI_TEAM_CTR_90DD", entityCtr[Emoji])
	case Keyword:
		add("IS_BROADCAST_KEYWORD", 1)
		add("HERE_KEYWORD", b2f(c.Name == "here"))
		add("HERE_KEYWORD_EXACT_MATCH", b2f(c.Name == "here" && rawQ == "here"))
		add("BROADCAST_KEYWORD_NORMAL_MATCH", b2f(exactNorm(c.Name, q)))
	}
	return total
}

// dropSigil is .replace(/^[@#]/g, ""): one leading @ or #.
func dropSigil(s string) string {
	if s != "" && (s[0] == '@' || s[0] == '#') {
		return s[1:]
	}
	return s
}

// ratioLen is queryRatioLengthValueFromSuggestion.
func ratioLen(text, q string) float64 {
	if len(text) == 0 {
		return 0
	}
	return float64(len(q)) / float64(len(text))
}
