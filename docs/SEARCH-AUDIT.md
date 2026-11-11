# Search audit: Clack vs Slack (2026-11-10, branch `audit-search` from `ui`@b508b79)

The top-bar search box, the search popover (Slack's omniswitcher), the results page and search in channel, checked against Slack's web client, element by element. docs/SEARCH.md has the API calls each surface makes.

How it was checked:

- **Slack, source:** the public CDN build `407aff01646b` (slack-datamine-plus `meta.json`, captured 2026-11-10). These chunks were fetched without a cookie from `a.slack-edge.com/bv1-13-br/`: `gantry-v2-async-client-boot-shared` (omniswitcher: `SearchAutocomplete`, `SearchInputBox`, `Footer`, `SuggestionItem`, `CombinedSearchFeedbackModal` q9NO, `FeedbackLink` RhHp, the filter auto-slug Zf2V), `gantry-v2-async-client-boot-render` (`BaseSearch`, the top-bar box), `client-boot` (the search thunks in rZ6M: `searchFeedbackApi`, `searchFeedbackDialog`; the modifier vocabulary fsda; per-tab filters `getDefaultFiltersByEntity` / `_p`), `…search-view_tsx` (`SearchView` → `CombinedView`, `UnifiedSearchRefinement`, `CombinedViewFeedbackLink`), `…search-filters_tsx` (More menu, pills), `…search-filter-sort…` (sort selects, `GenericSearchEmptyState`) and `client-search-in-channel-view`. `modern.vendor.99445d7c5cf41877` (cached by an earlier audit on 2026-11-08) inlines the method names: `search.feedback`, and `feedback.submitEnterpriseSearchFeedback` behind the generated fetcher (`createApiMethodFetcher`: camelCase args snake-cased, `reason` sent as `_x_reason`). Styles come from Slack's own CSS, which earlier audits cached from the same CDN on 2026-11-08 (all.css) and 2026-11-09 (the per-chunk CSS files). Module names are cited below.
- **Slack, live:** not used this round. Nothing was opened in Slack, and no search, feedback or click was sent.
- **Clack:** internal/fakeslack. The new tests are TestSearchPopoverKeys, TestSearchPopoverGhostText, TestSearchFeedbackFromPopover, TestSearchFeedbackFromResults, TestSearchInChannelFeedbackAndSort, TestSearchOpenFile, TestSearchMoreMenuDetails, TestSearchDateLabel and TestSearchParseSlackModifiers (popups), plus TestSearchFeedbackCalls (core). Feedback only ever went to the fake. The `search` workflow (workflow_dispatch) saves light and dark shots as the `search-shots` artifact, including the new picked row, the feedback dialog, its error and confirmation, and search in channel with no results.

Status: **matched** (no change needed), **fixed** (on this branch), **open** (still different; reason given).

## Top-bar search box

| Feature | Slack (evidence) | Clack before | Status |
|---|---|---|---|
| Placeholder | `BaseSearch.getSearchText`: "Search {teamName}"; on Grid the org name (`getSelfEnterpriseOrgNameWithFallback`); "(GovSlack)" variant | "Search " + team name | **matched** |
| With a query | "Search: {plainTextQuery}" | the bare query | **open**: the pill is drawn in internal/ui/chrome.go and bbm.go, which aren't search code (ui root) |
| Clear button | `p-top_nav__search__clear`: a 16px close icon after the text when there's a query, tooltip "Clear search" (300 ms), runs `clearSearchInput` | none | **open**: same files |
| Opening | `onMouseDown` (left button, no Ctrl) and Enter/Space open the omniswitcher | opens on click (press + release) | **open**: same files |
| Size, icon, text | measured in docs/FIDELITY.md row 10 (width `min(1000, ⅔·…)`, search icon 16 at +10, 13px text at +34) | same | **matched** |
| ⌘K / Ctrl+K | the same omniswitcher, empty | `NewQuickSwitcher` | **matched** |

## Search popover (omniswitcher)

| Feature | Slack (evidence) | Clack before | Status |
|---|---|---|---|
| Frame, input row, placeholder | `SearchInputBox`: "Search across people, channels, files, workflows, and more"; geometry measured earlier (649 wide over the pill, input 48 + 1px line) | same | **matched** |
| Placeholder per mode | "Search messages at {teamName}" (and channels, files, people, DMs, workflows) in a searchy mode; "Ask Slackbot…"; "Search for anything" | default text only | **open**: Clack has no searchy modes or Slackbot mode |
| Clear, Filter, Recent searches buttons | Clear when there's text, the filter button opens the results page, the clock toggles history | same | **matched** |
| Blank slate | `renderBlankSlateSuggestions`: Unreads & drafts (five, "Show N more" / "Show fewer", 24 at most), a divider, Recent places | same | **matched** |
| "Show N more" states | `.c-search_autocomplete__see_more` 13px link colour; `:hover` underlined; `--selected` base-ter and underlined | grey whenever it was the selection row, never underlined | **fixed** |
| Recent searches | history mode lists them, "Your recent searches will show up here." when empty; Slack loads them from the server (`searchAutocomplete` fetcher: query, limit, use_hash, type, query_suggestion_limit) | this session's searches, kept in memory | **open**: the method name is a constant module the build doesn't inline, so it wasn't confirmed |
| Typing | top hit, "Show results for:", then people and channels (edge `users/search`, `channels/search`), Recent messages (`search.inline`), files and canvases (`search.autocomplete.files`) | same | **matched** |
| Selection at first | `selectedIndex` null: nothing is selected; suggestion 0 is `--pseudo-selected` (base-ter #eaeaea) while the input has focus, with the Enter key in `suggestion_item_shortcut` (16px from the content) | the first row selected and drawn grey with the Enter chip | **fixed**: same look, now a pseudo-selection |
| Picked row | `.c-search_autocomplete__suggestion_item--selected`: #1264a3, white text, icons and secondary text; no badge (`!r && S`), no Enter key | grey like the first row; badge and Enter chip drawn | **fixed** |
| Mouse | `onMouseMove` picks the row; `onMouseLeaveList` sets null again | hover moved the grey selection, which stayed after leaving | **fixed** (`gesture.Hover` on the list) |
| ↓ / ↑ | `incrementSelectedIndex`: from null ↓ picks 0, ↑ the last; past either end the selection goes back to null (`restoreUserQuery`) | wrapped row to row | **fixed** |
| ←, →, Home, End, Backspace, ⌘A | reset the selection to null | no effect | **fixed** (the editor's caret moves) |
| Enter | accepts the selected row, else the pseudo-selected top suggestion | same | **matched** |
| Escape | closes | same | **matched** |
| Tab | focuses the selected row's action toolbar (`getSuggestionSecondaryActions`: View profile, Search in channel, Open in new window…) | nothing | **open**: Clack has no suggestion toolbar |
| Text while navigating | `setText: true` shows a picked query suggestion in the box | the typed text stays | **open**: swapping the box's text without starting a new search isn't built |
| Ghost text | `getGhostTextForSelectedSuggestion`: for the selected (else first) suggestion, the rest of its name after the typed text, then " — Open"; " — Search" for a recent search or a query row; with an empty box only a picked row ("name — Open"); typing a space hides it (`hasTypedSpace`) | "— Open" after any query on conversation, person or file rows, prefix or not | **fixed** |
| Footer keys | `Footer`: "▲ ▼ Select" while nothing is selected; with a selection "Enter Open" and "Tab Actions" | always "▲▼ Select" | **fixed**. "Tab Actions" is left out because Clack has no toolbar (above). |
| Offline notice | "No connection. Search is not available while offline, and navigating to conversations may be limited." | none | **open**: the popover doesn't track the connection |
| Give feedback | `FeedbackLink` (`experimentOmniPolish`: the link alone) opens `CombinedSearchFeedbackModal` with feedbackType "omniswitcher", the query and the suggestions | toast "Feedback isn't supported yet" | **fixed** (see Feedback) |
| from:@ / in:# completion | the input is a Texty editor (`completeOnMembers`, `completeOnChannels`, `completeOnEmoji`); `useSearchFilterAutoSlug` turns "from:@name " into a filter token | a plain text field | **open**: no Texty input in the popover |
| Modifier vocabulary | fsda `wd`: has, type, from, to, in, is, before, during, on, after, hasmy, with, creator, threads, dm, each also with "-" | no hasmy:, threads:, dm: | **fixed** (`srModKeys`) |
| Date values | `buildDateCapturePattern`: today, yesterday, week, month, year, month names, numeric dates | passed through as typed | **matched** (Slack's server reads them) |
| Message cards | "Recent messages" cards measured earlier (border, avatar 36, name 15/900, highlights) | same | **matched** |

## Results page

| Feature | Slack (evidence) | Clack before | Status |
|---|---|---|---|
| Page | `SearchView` always renders `CombinedView` (`UnifiedView` with `renderFeedbackLink`) | one results page | **matched** |
| Title, pills row, "Sorted by", result count | measured on app.slack.com in the 2026-11-08 search audit (docs/SEARCH.md) | same | **matched** |
| Sources | `UnifiedSearchRefinement`: Messages (Slack icon), then DMs, Files, People, Channels, Canvases as sub-items, each with its count; no Lists source (lists are File type → Lists) | same six with counts | **matched** |
| Messages source icon | `SlackIcon`, the four-colour logo image | monochrome slack-logo glyph | **open**: icons are owned elsewhere |
| "Display options: Show as a left sidebar" | source menu footer | none | **open**: not built (see SEARCH.md) |
| Messages pills | `_p` messages: From, In, With ("Includes me"), Has a link, Threads only, Last 7 days, then More | same | **matched** |
| Pills that don't fit | `useSearchFiltersResponsive` measures each `${filter}-button` and moves the rest into More | a fixed set | **open**: not built; the fixed set is what fits at 1440 px |
| Files pills | From, In, File type, Date, Only my channels (With in More) | the source pill only | **open**: not built; modifiers typed into the query do work |
| Canvases pills | Creator, From, In, Date (With in More) | the source pill only | **open**: not built |
| Channels pills | All channels, Channel type (Any / Public / Private / Archived / External), Workspace, Org | the source pill only | **open**: core has no channel_type / exclude_my_channels options |
| People pills | Title, Location, Workspace, Department (Account type, Org, Celebration, Division in More) | the source pill only | **open**: core has no profile_* options |
| DMs pills | With | the source pill only | **open**: not built |
| More: rows | Saved preferences (Exclude bot messages, Only my channels), Results (Exact matches only), With people, Message has, Message is, Date, File type, Reactions | same rows | **matched** |
| More: Exclude archived | under Results, only when the menu's `e` prop is set and the tab isn't excluded | none | **open**: the gate wasn't resolved |
| More: width | `e5`: width 240 | 280 | **fixed** |
| More: icons | Message has `message`, Reactions `emoji` (16px) | `attach`, `add-reaction` | **fixed** |
| More: Date description | `description: /upd S({after, before})`: "After Nov 3, 2026", "Before …", "Any time", "Today", "Yesterday", "On …", a preset's name, "Nov 3 - Nov 9" | none | **fixed** (`srDateLabel`) |
| More: Date rows | `type: radio` | a check at the right | **fixed** (radio rows) |
| More: File type icons | `labelIcon` from `$.t9` (canvas, lists, document, email, image, pdf-file, presentation, text-snippet, spreadsheet, sound, video), 8px before the label | none | **fixed** where Clack has the glyph. pdf-file, presentation and text-snippet are **open**: no glyphs |
| More: counts | blue `badgeCount` on submenus with selections; `getMoreFiltersCount` on the More pill | none (the pill only turns active) | **open**: the slot is there now (popups `MenuItem.BadgeCount`, `BadgeStyle: "blue"`, `BadgeHighlighted` for `.p-more_filters_dropdown__menu`, whose badges stay on the highlighted row; branch exp-menus); the More menu still has to fill it, and the pill's count is search code |
| Sort, messages | `MessageEntity`: Most relevant (default), Newest, Oldest, divider, Edit default; "(default)" dropped with stick_user_sort | same | **matched** |
| Sort, other tabs | Files: Most relevant, Oldest, Newest, A to Z, Z to A; Canvases adds Last updated; Channels: Most relevant, Newest, Oldest, Most members, Fewest members, A to Z, Z to A; People: Most relevant, A to Z, Z to A; each with Edit default | none | **open**: core's files/channels/people calls have no sort option, and the A to Z values weren't traced |
| Give feedback | `CombinedViewFeedbackLink` (`short`): CombinedSearchFeedbackModal for the selected module; hidden when `has_hipaa_compliance`, `ml_opt_out` or `search_feedback_opt_out` is on | toast "Feedback isn't supported yet" | **fixed**. The opt-out flags aren't read: **open** |
| Message result cards, hover bar, More actions | measured in the 2026-11-08 audit | same | **matched** |
| Highlights | U+E000/E001 markers stripped; terms highlighted | same | **matched** |
| Pagination | numbered `c-pagination` (`getCurrentPageRange`) | same | **matched** |
| No results: text | `GenericSearchEmptyState`: "Nothing turned up", "You may want to try using different keywords, checking for typos, or adjusting filters." | "…check for typos, or adjust filters." (the Slackbot variant's wording) | **fixed** |
| No results: feedback line | "Not the results you expected? Give feedback." on its own line first; the link opens the combined modal for the module | none | **fixed** |
| No results: Search without filters | `RelaxationSuggestion`: an outline button when the response suggests dropping a filter (messages) | none | **open**: core doesn't read the relaxation data |
| No results: Slackbot | Ask Slackbot / Broaden Search variants for teams with AI search | none | **open**: AI search isn't on the account |
| Opening a canvas | `openCanvas` | `OpenCanvasFull`, else toast "Opening canvases isn't supported yet" | **fixed**: the canvas view (`docs/<id>`), no toast |
| Opening a list | list view | handled as a file | **fixed**: `lists/<id>` |
| Opening an image | `previewFile` → media viewer | lightbox | **matched** |
| Opening other files | `previewFile`: the file details flexpane | the first channel it was shared in, else toast "Opening this file isn't supported yet" | **fixed**: no toast; it opens like a message's file card, at the permalink. The flexpane itself is **open**: Clack has none |
| Files, channel and people rows | not compared this round | unchanged | **open**: not re-measured |

## Search in channel

| Feature | Slack (evidence) | Clack before | Status |
|---|---|---|---|
| Header | `SearchInChannelView`: "Search in #channel", 'Search "kw" in #channel'; "Search in channel" without a conversation | same text | **matched** |
| Header for a DM | the avatar (xx_small) before the name | text only | **open**: the pane's header draws text only |
| Before a search | "Not searching in this channel?" and "Go to full search (⌘G / CTRL G)" on one line (`p-explorer_section--align`: 13px, regular, #1d1c1d at 70%, link 4px after) | bold title, link on the next line | **fixed** |
| "Nothing to show yet." | the empty state with "Type a keyword or **from: @name** …" and the empty-search illustration | plain text, no illustration | **open**: the image asset isn't drawn, and "from: @name" isn't bold (no rich text in that paragraph) |
| Request | `search.modules.messages`: "in:<#C> q", count 10, search_context search_in_channel, search_exclude_bots false, sort score/desc, timestamp/desc or timestamp/asc | same, without Oldest | **fixed** (Oldest) |
| `disable_sort_override` | sent when Most relevant is picked | not sent | **open**: core's SearchOpts has no field for it, and it pairs with the response's `sort_forced_to_recent`, which core doesn't read |
| Loading | seven skeleton cards | nothing | **open**: no skeleton component |
| Results label | "**N** results found", "View full search" 6px after it, no separator | "N results found · View full search" | **fixed** |
| Sticky header and footer | `stickyResultHeader`, `stickyResultFooter` | scrolled with the rows | **fixed** |
| Sort | `MessageEntity` in channel: a filter select with the sort icon and its label (label only when the column is ≥ 350 wide); options Most relevant (default), Newest, Oldest, divider, Edit default; with stick_user_sort the choice is saved | link text "Most relevant ▾"; Most relevant / Newest only | **fixed** |
| Rows | message cards (`variant: "card"`), "View all N results" past ten | same | **matched** |
| Footer | `p-explorer_sections--footer`: 40px, ctr-sec, the unstyled "Give feedback" (content-ter) at the right | centred link-blue text | **fixed** |
| Give feedback | `searchFeedbackDialog({module: messages, feedbackLabelOverride: "inChannel"})` → `search.feedback` | toast "Feedback isn't supported yet" | **fixed** (see Feedback) |
| No results after a search | `GenericSearchEmptyState` (module messages, feedbackLabelOverride "inChannel"); the "Not searching…" line goes | "Nothing turned up" with the old text, under the "Not searching" line | **fixed** |
| ⌘G / Ctrl+G | go to full search | Mod+G opens search | **matched** |

## Feedback

| Feature | Slack (evidence) | Clack before | Status |
|---|---|---|---|
| Dialog | `CombinedSearchFeedbackModal`: "Share your feedback on Search"; select "How would you describe the search results you’re seeing?" (The results are helpful, Something is missing, I’m seeing confusing or surprising results, The results are out of order, It took too long to see results, Search is missing a feature, Search is broken, Something else; "Select an option"); "Tell us details about your feedback" (textarea, 1,000 characters, "What would you like us to know?"); the note with "contact our Support team" (`/help/requests/new`) and feedback@slack.com; "I’m okay with being contacted by Slack about my feedback" (unchecked); footer "Privacy Policy", Cancel, Submit | none: all three links toasted "Feedback isn't supported yet" | **fixed** (popups/search_feedback.go). Fields follow Slack's CSS: label 15/700 + 8, select 36, textarea 104 with 8/12/4 padding. Nothing is focused on open (Slack's Modal focuses the dialog); Tab reaches the select, the textarea and the checkbox |
| Submit rule | disabled until a category is chosen and the text is 1 to 1,000 characters, not only spaces | none | **fixed** |
| Submit call | `combinedSearchFeedbackSubmit` → `feedback.submitEnterpriseSearchFeedback` {type "feedback", feedback_type "omniswitcher" or "search", text, contact, category, query, payload, is_test false} | none | **fixed**. Tested against fakeslack only |
| Payload, popover | {search_session_id, query, client_version, suggestions} | none | **fixed**. client_version is Slack's own fallback "noversion" (Clack isn't a web build). Slack's suggestions are its full suggestion objects; Clack sends each row's id, name and `is_type_*` flag |
| Payload, results page | {search_session_id, query_session_id, query, module (or the label override), active_sort, active_sort_dir, active_filters, client_version, module_counts} | none | **fixed**. active_filters holds exclude_bots and only_my_channels; the response's own `filters` object isn't kept by core: **open** |
| Search in channel | `searchFeedbackApi`: `search.feedback` {client_token (`generateAndEnqueue`), type "search", payload without client_version, module_counts {}}; Slack opens its dialog for that token | none | **fixed**. The dialog arrives on the socket (dialog_opened / view_opened), which Interactive shows when realtime is on. Not tried live: it would send feedback |
| Failure | an error Alert in the dialog: "Something went wrong submitting your feedback. Please try again." | none | **fixed** (`.c-alert--level_error`) |
| Sending state | Submit shows a spinner (`loading`) | none | **open**: Submit is disabled while sending instead (no loading button in popups) |
| Character counter | Slack's textarea `maxCharacterLimit` UI | none | **open**: not traced; Submit just turns off past 1,000 |
| Confirmation | `CombinedSearchFeedbackConfirmationModal`: the celebration illustration (`celebration-5ae8885.png`, 231×199) on the theme colour (padding 12), "Thank you! You’re helping us make Slack better.", the note, OK; no close button; 525 wide | none | **fixed**. The image loads from Slack's public CDN like default avatars do. The title wraps to two lines like Slack's title bar (Clack's modal title is one line, so it's drawn in the body) |
| Hidden for opted-out teams | `getShouldShowFeedbackLink`: not with has_hipaa_compliance, ml_opt_out or search_feedback_opt_out | none | **open**: the flags aren't read |

## Counts

Rows: 91. **matched** 24, **fixed** 37, **open** 30. Four fixed rows keep an open remainder (File type icons, the results page's feedback opt-out flags, the file details flexpane, active_filters); they're counted as fixed.

## Not done

- Everything marked open above. The largest pieces are the other tabs' filters and sorts, the Texty input with from:/in: completion and filter tokens, the suggestion action toolbar (Tab), and the file details flexpane.
- The top-bar box's "Search: query" text and clear button need internal/ui/chrome.go and bbm.go, which belong to the ui root.
- The popover's selection still reaches Slick plugins as before: `selectedIndex` is the pseudo-selected row when nothing is picked (slick_search.go isn't search_*.go).
