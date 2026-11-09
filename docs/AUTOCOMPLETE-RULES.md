# Composer autocomplete: Slack's rules

How Slack's web client builds and orders the composer's `@`, `#`, `:` and `/`
lists, and where Clack ports each rule. Evidence comes from the public CDN
bundles of build `71420901d111` (2026-11-09: `client-boot`,
`gantry-v2-async-client-boot-shared`, `modern.vendor`, `application`), cited
by module id, plus one read-only look at app.slack.com in channel
C0B4DEFKFCJ (the remote model and experiment values below). Ranking code is
in `internal/acrank`; the frecency store is `internal/frecency` and
`internal/core/frecency.go` (from the emoji-picker branch); the composer
side is `internal/ui/composer/autocomplete_rank.go`.

## One pipeline for every list

All four lists go through the same `Searcher` (QcwJ). The thunks differ only
in options:

| list | thunk (module) | entities | source |
| --- | --- | --- | --- |
| `@` | autocompleteMembers (sYpo) | members, userGroups, broadcastKeywords | texty-autocomplete |
| `#` | autocompleteChannels (BUcX) | channels (public, private, other workspaces, archived, previous names) | texty-autocomplete |
| `:` | autocompleteEmoji (3K8G) | emoji (`matchKeywords`) | texty-autocomplete |
| `/` | autocompleteCommands (application.js `_5`) | slash commands, global, channel and call actions, workflow triggers | slash-command-menu |

Defaults merged in (L03J, `getAllOptions` in nIFn): limit 25 (the thunks pass
25), `fuzzyLimit` 3, `fuzzyJumpLimit` 20, `fuzzyCJK`, and sort
`{preferExactMatch, frecency, preferChannelsUserBelongsTo, model:
"autocompleteFallback"}`. `@` adds `preferChannelMembers` and the current
channel and thread users.

### Sources and timing

- **No debounce.** `TextyAutocomplete.search` (modern.vendor) runs on every
  keystroke and aborts the previous search through an `AbortController`.
  Clack's `acDebounce` is 0 and cancels the previous request.
- **Tiered.** With `tiered: true` the local result shows at once. The remote
  fetch upserts into the store, then the whole local set is matched and
  sorted again and replaces the rows. A loading row appears only when there
  are no local rows after 300 ms, and the search times out at 10 s.
- **Empty query.** `allowEmptyQuery` gives every candidate score 0. Desktop
  skips the server for an empty query.
- **Local candidates.** `@` uses every member in the store
  (`getAllMembers`), so Clack now feeds all cached users
  (`core.KnownUsers`), not just sidebar people. `#` uses every known
  channel. `:` uses every emoji, after a character-mask prefilter.
- **Server calls** (`getRemote*` in QcwJ):
  - members: edge `users/search` `{query, count: 25, fuzz: 1,
    enable_workspace_ranking: true, include_profile_only_users: true,
    current_channel, default_workspace}`. There's no `NOT deactivated` and no
    `search_email` for the composer. See `core.AutocompleteUsers`.
  - channels: edge `channels/search` `{query, count: 25, fuzz: 1, filter:
    "xws", include_record_channels}`. See `core.AutocompleteChannels`.
  - usergroups: a flannel search per query. Clack loads all usergroups up
    front, so the local match covers it.
  - Each call waits at most 2 s.

## Matching (SearcherMatcher, 1uar; FuzzyMatcher, +EXg)

Text is normalized by `normalizeStringForMatching` (pRCT): trim, lowercase,
deburr (tsv0, Cyrillic transliterated), collapse spaces.

The fuzzy score counts word-boundary jumps. The query must start at a word
start; a separator (`- _ ' . ’ ( ) /` or space) in the query matches a
separator in the text. Score 0 is a prefix; each jump to another word start
adds 1. Only the first 20 word starts can be jumped to. The search is
depth-first and returns the first path it finds, not the cheapest. Anything
above 3 is dropped. Substrings inside a word never match: "ker" doesn't find
"Karim Hacker". `ml_fuzzy_match_beyond_word_boundaries_v2` would let 4+
letter substrings score 2, but it is off for Hack Club.

- **Members**: the best of `_real_name_normalized_lc` and
  `_display_name_normalized_lc`. The username isn't matched, and the old
  Clack handle tier is gone. "me" or "you" match the viewer at 0.
  Deactivated people stay in (`includeDeleted`). Deactivated bots are
  dropped (`excludeDeletedBots`).
- **Usergroups**: the name, lowercased, and the handle.
- **Keywords**: `here` `channel` `everyone`, and `@channel` also answers
  "group". Which ones are offered follows `getBroadcastKeywordsForUser`
  (4e6T, Clack's `core.BroadcastKeywords`).
- **Channels**: the normalized name. A previous name can match, which marks
  the row `bestPreviousNameMatch`. Typing a channel id finds it.
- **Commands**: the name (the leading "/" costs one jump that is forgiven),
  the description, the app name and aliases. A first word that names the
  command exactly is a command match.
- **Emoji**: `scoreEmojiNameAgainstQuery` over the name and aliases. The
  query's letters must appear in order from the first place the query, or
  its first letter, occurs. A run of skipped letters is a combo break: at
  most 2, at most 1 for 3 or 4 letter queries, none under 3. A gap may be at
  most 5 letters, 1 for 3 letters, 0 under 3. Score is
  `1 − len(query)/len(name) + breaks`. A table of blocked pairs is copied
  as is ("hi" never offers `shit`).
- **Remote passthrough.** The experiment
  `autocomplete_flannel_fuzzy_match_passthrough` is `treatment` for Karim.
  A server result that fails the local match still shows at score 2, and
  `autocomplete_demote_flannel_passthrough_rltv_exact_match` (on) moves
  those after the last row whose name contains the query.

A node harness ran Slack's own +EXg, emoji scorer and j8FV
code. `acrank_test.go` pins its values (12 matcher cases, 13 emoji cases, 8
similarity cases). They agree to 9 decimals.

## Ordering

1. **Group sort** (SearcherSorter `sortAsync`, j8FV). Members, member
   channels, emoji, usergroups and keywords come first, then non-member
   channels and commands, then archived channels. Within a group the order
   is score ascending, then name by `Intl.Collator("en-US", {numeric:
   true})` (QJub). The member name is the display name, or the real name
   under `display_real_names`.
2. **Exact matches first** (`preferExactMatch`). Members never count as
   exact here, because `makeResultsMatcher` forces
   `avoidExactMatchForMembers`.
3. **Frecency** (RI+6) from the `frecency` user pref. Keys are item ids
   (`U…`, `C…`, `E1f44d`, `/remind`, `BKhere`) or normalized query texts.
   For every key that contains the query or sits inside it, the best record
   per id counts. A record's score is the mean of its last 10 visits'
   points (100 under 4 h, 80 under 1 d, 60 under 3 d, 40 under 1 w, 20 under
   30 d, 10 under 90 d) times its count. It's doubled when stored under
   exactly this query, decays 0.9755 a day and is normalized to 0–100 over
   the list. With fewer than 75 candidates it only scores; with more it
   also reorders before the cut.
4. **Bonus points** (`getFrecencyBonusPointsFunction`). Exact +100, and an
   exact emoji +300 since `emoji_exact_match_boost` is on. Otherwise a
   continuous fuzzy bonus, n-gram cosine × 50 (×150 for emoji). Also: only
   a previous name −50, a channel member with presence +25, starred +10,
   archived −50, a public non-member channel −25, deactivated −50,
   usergroups and keywords −25, affinity +50.
5. **The model** (Sorter, y+lP; LinearModel, 94aK) scores the top 75 by a
   weighted sum of features (gSR8). The common features go to every kind,
   then member, channel, emoji and keyword features. Usergroups and the `/`
   menu get only the common ones. Weights come from
   `search.autocomplete.model` (sPSO), refreshed after 28 h; the bundled
   `autocompleteFallback` (X46X) applies until the call answers.
   `USER_IS_EXTERNAL` carries an extra −1. Hack Club's live model
   (`internal/acrank/testdata/remote_model_2026-11-09.json`, 75 weights)
   gives the biggest weights to `USER_ENTITY_TYPE_CTR_30DD` 7.9 (a per-type
   constant), `IS_BROADCAST_KEYWORD` 4.0, `USER_IS_IN_CURRENT_THREAD` 3.8,
   `MEMBER_OF_CURRENT_CHANNEL` 3.0, `CURRENT_USER_TEXTY` −2.7,
   `ONLY_MATCHES_PREVIOUS_NAME` −3.2 and `FUZZY_SCORE` −1.7 per jump. The
   frecency features add up to about +5.7 for an item picked a few times
   today under this exact text. That's why a frecent person can outrank a
   channel member.
6. **Post-processing.** Keep the first two rows, then move exact matches
   up (`boostExactMatches(…, 2)`), then demote passthrough rows, then cut
   to 25.

`search.autocomplete.offlineFeatures` came back empty for Karim, so Clack
uses Slack's default per-type CTRs (member .132, channel .148, emoji .094,
other .179).

## Recording

On accept, `TextyAutocomplete.handleSelect` calls `recordFrecencyEvent({id:
completer.getResultFrecencyId(row), text: matchText})` for every list.
Emoji go through `recordEmojiFrecencyEvent`, with id `"E"+unicode` or
`"E"+name`. The pref is saved with `users.prefs.set` through a 60 s leading
and trailing throttle (1TmA, `createThrottledThunk(…, 6e4)`). Clack:
`Autocomplete.recordPick`, then `core.RecordFrecency` and
`core.RecordEmojiFrecency`.

## Shape and keys

Unchanged by this port, and measured earlier (`autocomplete_view.go`): rows
are a `MediumMemberEntity` with a "Not in channel" context label
(TextyAutocompleteMemberResult, EPrK). Usergroup and keyword rows have their
own components. Row 0 starts pseudo-selected. Tab and Enter insert, arrows
wrap, Esc closes.

## Known gaps

- **Emoji keywords** (`matchKeywords`). Clack has no keyword table, so
  ":happy" doesn't find `smile`.
- **The exact-name lift rules** (J3WV,
  `search_autocomplete_promote_exact_name_matches` treatment_3 or
  treatment_5). Not ported. I didn't read the experiment's value for Karim.
- **Features from data Clack doesn't keep**: mpims, Salesforce, offline
  CTRs per entity, DM metadata the server sends. These are 0 here, which
  is also what Slack computes when that data is missing.
- **The `/` menu.** Only slash commands; Slack's global actions, channel
  actions and workflow triggers aren't in Clack's command list.
