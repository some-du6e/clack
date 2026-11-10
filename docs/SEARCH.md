# Search and the quick switcher

How Clack's search matches Slack's web client. Sources: the public CDN
chunks (build a84662d2d135: `gantry-v2-async-client-boot-shared` for the
omniswitcher, `…components_search_search-filters_tsx` for the filters,
`…search-view_tsx` for the results page, `client-search-in-channel-view`
for ⌘F, `modern.vendor` for the searchModules fetcher), and app.slack.com
read-only on 2026-11-08: the requests each surface sends, captured in the
page with the token stripped. Key names and types come from read-only
calls; no content was kept.

## What Slack calls

| Surface | Method | Args (besides the token) |
|---|---|---|
| Popover: people | edge `users/search` | query, count 30, fuzz 1, enable_workspace_ranking, search_email, include_profile_only_users, top_users (frecency), default_workspace |
| Popover: channels | edge `channels/search` | query, count 30, fuzz 1, filter `xws`, include_record_channels, check_membership, top_channels, default_workspace |
| Popover: Recent messages | `search.inline` (enterprise host) | search_session_id, client_req_id, max_ts (now), count 3, page 1, query, thread_replies "", extract_len 110, recent_channels, from_me, with_me |
| Popover: files and canvases | `search.autocomplete.files` | query, include_shares |
| Submitting a search | `search.save` | terms, type `default` |
| Results: Messages | `search.modules.messages` | the base args\* + search_exclude_bots and search_only_my_channels (the user prefs), spell_correction FUZZY_MATCH, search_only_team "", facets_result_count 5, query_refinement_suggestions_version 1, recent_channels, sort score/timestamp, sort_dir desc/asc, max_filter_suggestions 10, request_context `{"active_cid","recent_filter_in","recent_filter_from"}`, search_tab_filter, search_tab_sort; Exact matches only adds search_exact_matches_only |
| Results: DMs | `search.modules.dms` | the base args, count 40 |
| Results: Files / Canvases | `search.modules.files` | the base args (count 0 when only counting), the two prefs, search_only_team, facets_result_count, sort timestamp desc, max_filter_suggestions, request_context, search_tab_*; Canvases queries `type:quip <q>` |
| Results: Channels | `search.modules.channels` | the base args, count 20, search_only_my_channels, channel_type all, external_shared_team_ids, exclude_my_channels 0, request_context, spell_correction, sort relevant desc, search_only_team |
| Results: People | `search.modules.people` | the base args, count 20, tz, external_shared_team_ids, custom_fields `{}`, hide_deactivated_users 0, request_context, spell_correction, sort relevant desc, search_only_team, profile_* "" |
| ⌘F search in channel | `search.modules.messages` | query `in:<#C> <q>`, count 10, search_context search_in_channel, search_exclude_bots false, sort score/desc, timestamp/desc or timestamp/asc |
| Give feedback (popover, results page, no-results state) | `feedback.submitEnterpriseSearchFeedback` | type `feedback`, feedback_type `omniswitcher` or `search`, text, contact, category, query, payload (JSON), is_test false |
| Give feedback (search in channel) | `search.feedback` | client_token, type `search`, payload (JSON); Slack opens its dialog for the token |

\* module, query, page, client_req_id, search_session_id, extracts 1,
highlight 1, max_extract_len 200, extra_message_data 1, no_user_profile 1,
count, file_title_only false, query_rewrite_disabled false,
include_files_shares 1, search_context (`desktop_<module>_tab`). One
session covers every module of a query. Results carry highlight markers
(U+E000/U+E001), which core strips.

## Filters → modifiers

What each control adds to the query (clicked on app.slack.com):

| Control | Modifier |
|---|---|
| Includes me | `with:<@self>` |
| Has a link / Threads only | `has:link` / `is:thread` |
| Last 7 days, Date presets | `after:<day before> before:<day after>` (Today: yesterday…tomorrow; Yesterday: −2…today; Last 7/30 days, 12 months, 3 years) |
| Message has: File, Link, Action | `has:file`, `has:link`, `has:action` |
| Message is: A direct message, In a thread, Saved, To-dos, Pinned, In Activity | `is:dm`, `is:thread`, `is:saved`, `is:todos`, `has:pin`, `in:activity` |
| With people | `with:<@U>` |
| Reactions | `has::name:` |
| File type | `type:huddle_notes`, `lists`, `canvases`, `documents`, `emails`, `images`, `pdfs`, `presentations`, `snippets`, `spreadsheets`, `audio`, `videos` |
| Exclude bot messages / Only my channels | the `search_exclude_bots` / `search_only_my_channels` prefs |
| Exact matches only | `search_exact_matches_only=true` |

The parser (`search_rank.go`) keeps terms first and modifiers after, in
order, the way Slack serializes them. It handles "quoted" words and
phrases, `-word`, `-"phrase"` and `-from:` style exclusions, and
in/from/to/with/before/after/on/during/has/is/type/creator.

Build 407aff01646b (2026-11-10) added `hasmy:`, `threads:` and `dm:` to
the modifiers the parser knows (fsda's list).

## Feedback

The popover's footer, the results page and the no-results state open
Slack's "Share your feedback on Search" dialog
(`CombinedSearchFeedbackModal`, popups/search_feedback.go). Its payload is
JSON: the popover sends search_session_id, query, client_version
("noversion", Slack's own fallback) and the suggestions on screen; the
results page sends search_session_id, query_session_id, query, module,
active_sort, active_sort_dir, active_filters, client_version and
module_counts. Search in channel calls `search.feedback`, and Slack answers
by opening its own dialog for the client_token. Tests send feedback to
internal/fakeslack only.

## Not done

docs/SEARCH-AUDIT.md lists every element with its status. The open rows
include:

- The source menu's "Google Drive" row (connected external sources) and
  "Display options: Show as a left sidebar".
- Slackbot / AI answers on the results page and in the popover.
- Help Center rows in the blank slate, and the setup tip card.
- The Messages pill's four-colour Slack logo (icons are owned elsewhere;
  it uses the monochrome slack-logo).
- Filters and sorts on the Files, Canvases, Channels, People and DMs tabs.
- The Texty input (from:@ / in:# completion and filter tokens) and the
  suggestion action toolbar (Tab).
