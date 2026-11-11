# Composer audit: Clack vs Slack, row by row

How this was done (2026-11-10, branch `audit-composer` from `ui`@b508b79):

- **Slack, source:** the web client's public CDN chunks, fetched without a cookie (the copies the channels audit used): `gantry-v2-async-client-boot-shared` 4f8632cd9602d1fa, `gantry-v2-async-client-boot-render` 777e711847af270a, `client-boot` bac7c893b49156a7, `gantry-v2-vendors` 6085a6e7dffbbb12 (Quill's defaults) and `client-boot-styles` cf726c4f2eaecc74 (CSS). Rows cite the rspack module ids. The main ones: MessageInput (rs9k), MessagePaneInput (cRQG), InputContainer (63o2), TextyInput and its Quill modules (fffk: SlackKeyboard, SlackMarkdown, SlackLinkModule), TextyButtons (Ozua), WysiwygContainer and SendButton (ABuu), the formatting bar (ZGif, nFtI, bpQI, NMbH, SzyL), the schedule menu (BYKP, BQDv, Mluw), the custom time dialog (z1vG), the edit composer (cjbg), the + menu (boot-render 26v2 useComposerIAFormatter), the clip gates (Jg9Y), the character count (9ysi, Orr3, xjGd) and the markdown patterns (XmNU, OF9+).
- **Slack, live:** not used. Comparing the composer live means typing in it, and Slack saves what you type as a draft (`drafts.create`), so the audit stayed with the source and the CSS. Experiment groups (`composer_control_simplifications_*`, `updated_scheduled_sends_desktop`, `rich_text_headers_and_dividers`) weren't read for the account; where they change things the row says which side Clack takes.
- **Clack:** the synthetic workspace and `internal/fakeslack`, in `internal/ui/composer/audit_test.go`. The `composer-audit-shots` workflow (push on `internal/ui/composer/**`, and `workflow_dispatch`) runs those tests and draws the composer headlessly in light and dark (typed text, empty, a heading line, 1,234 characters over the limit) as the `composer-audit-shots` artifact.
- No real Slack message text was saved, and nothing was posted, reacted to or changed in Slack.

Status: **matched** (no change needed), **fixed** (on this branch), **open** (still different; reason given).

## Formatting toolbar and show/hide

| Feature | Slack (evidence) | Clack before | Status |
|---|---|---|---|
| Buttons and groups | nFtI getButtonsAndFormats (portable content): [Bold, Italic, Underline, Strikethrough, ¶ menu] [Link] [Ordered list, Bulleted list] [Blockquote] [Code, Code block, Divider]; bpQI buildComposer puts a separator only after a group of more than one, so B I U S ¶ │ Link Ordered Bulleted │ Quote Code Code-block Divider | same | **matched** |
| Tooltips and key chips | NMbH ToolbarButtonTooltip + SzyL labels: Bold ⌘B, Italic ⌘I, Underline ⌘U, Strikethrough ⌘⇧X, Code ⌘⇧C, Link ⌘⇧U, Bulleted list ⌘⇧8, Ordered list ⌘⇧7, Blockquote ⌘⇧9, Code block ⌘⌥⇧C, Divider (no key); Ctrl on Windows/Linux | same | **matched** |
| Narrow bar | bpQI getAvailableButtonSlots: floor((width − trailing)/36) slots, else slots−1 buttons and "More options" (ellipsis) holding the rest | same (TestChromeBarWidth) | **matched** |
| ¶ trigger icon | ComposerMenuTrigger draws the active item's IconForFormat: heading-1/2/3 in a heading, paragraph otherwise | always ¶ | **fixed** (`crParaIcon`) |
| ¶ menu | radio items with icons: Paragraph (paragraph), Big heading (heading-1), Medium heading (heading-2), Small heading (heading-3); 248 wide, top-left | check-marked items, no icons | **fixed** (`MenuItem.Radio` + `Icon`) |
| Buttons off in a code block | bpQI shouldDisableButton: everything but the lists, quote, code block and overflow, the ¶ "menu" included | ¶ stayed on | **fixed** |
| Buttons off in a heading | bold always (shouldDisableButton); italic, underline, strike and code too unless `rich_text_headers_and_dividers` (ZGif disabledFormats) | nothing turned off | **fixed** (`HeadingFormats`) |
| Aa (Show formatting) | ComposerButton: "Show formatting" / "Hide formatting", pressed while shown; the state is the account pref `msg_input_sticky_composer` (Ex/u kx) and the click writes it (WysiwygContainer, setUserPrefByApi) | a process variable, never read from or written to Slack | **fixed** (`syncShowFormatting`, `toggleShowFormatting`; TestFormattingTogglePref) |
| No toolbar under "Format messages with markup" | Texty isn't WYSIWYG with `plain_text_mode` | same | **matched** |
| Floating bar | TextyFloatingFormattingBar (ZGif): with the toolbar hidden, selecting text shows the same buttons above the selection (shouldToolbarShow, cj0/ calculateToolbarPosition) | none | **fixed** (exp-composer, floatbar.go: the toolbar's buttons in Slack's dark floating bar 5px above the selection, placed by calculateToolbarPosition; Escape hides it until the selection changes) |
| Simplified composer | the `composer_control_simplifications_*` experiments (47WU) fold Aa into an expanding strip, change the send button, the + and @ menus | not built | **open**: experiment-only, groups not known for the account |

## Live formatting (markdown shortcuts), links, lists, code, quotes

| Feature | Slack (evidence) | Clack before | Status |
|---|---|---|---|
| `*bold*` `_italic_` `~strike~` `` `code` `` | SlackMarkdown buildLocalMarkdownChangeDelta with OF9+ generateMarkdownRegex: opening marker at the line start or after whitespace, punctuation (`\p{P}`), `+`, `\|` or `=`; inner text without the marker, not starting or ending in whitespace; closing marker at the line end or before whitespace, punctuation, `+`, `\|`, `=` (`` ` `` also before a word character); the pair is the nearest marker before the typed one, or after it when there is none before | opening marker after any non-word character; nothing checked after the closing marker; only a closing marker formatted | **fixed** (`afPair`, `afBoundary`) |
| Full-width `＊` `＿` `｀` | OF9+ has CJK-specific patterns for them (H_, QX, j5) | none | **open**: those patterns (letter/number classes around the marker) aren't ported |
| Typing after the closing marker | the format is turned off for what follows; Backspace right after brings it back (formatsToRestoreOnBackspace) | pending style cleared; Backspace leaves the caret after styled text, which continues it | **matched** (same result) |
| `>` quote | buildLocalBlockQuoteMarkdownChangeDelta: `>` or `＞` typed at a line start turns the line into a quote at once and the marker goes | needed `> ` | **fixed** (`autoformatQuote`) |
| Lists | keyboard module "list autofill": space after `^\s*?(1(\.\|\))\|-\|\*\|•)$` (`[ ]`, `[x]` are canvas-only) | any `N. ` (so `12. `), no `1) `, no leading spaces | **fixed** (`afBlockPrefix`) |
| Lists inside a quote | list autofill allows it and keeps the quote (`border` line format) | lists only from paragraphs | **open**: Clack's Doc has no quoted list lines |
| ```` ``` ```` | buildLocalCodeBlockMarkdownChangeDelta: at a line start the line becomes code; after text the line splits and the rest becomes code | same | **matched** |
| ```` ``` ```` at the end of a code line | the fence goes and a plain line is added after the block ("remove") | nothing | **open**: small; not ported |
| `#`, `##`, `###` headings | SlackMarkdown formatHeader (convertHeaders with maxHeaderLevel 3, on with portable content) | none | **fixed** (`autoformatHeading`) |
| `---` divider | formatHorizontalRule (convertHorizontalRule) | only the Divider button | **fixed** |
| URLs as you type | SlackLinkModule.maybeLinkify on space and on an Enter that adds a line | linked only when sending | **fixed** (`autoLinkBefore`, with the send path's URL finder: http(s):// and www.) |
| URL pasted over a selection | links the selection | same | **matched** |
| Link dialog | ComposerLinkModal (3D1w): "Add link", or "Edit link" when it opens on a link (initialUrl); Text and Link fields; Cancel / Save | always "Add link" | **fixed** |
| Link dialog on rich text | when the selection holds mentions or emoji (textIsEditable false) the Text field is replaced by the selection drawn at half opacity | an editable text field of the plain text | **open**: rich preview not drawn |

## @, #, : and / autocompletes (look; ranking is internal/acrank)

| Feature | Slack (evidence) | Clack before | Status |
|---|---|---|---|
| Container | `.c-texty_autocomplete`: radius 6, ring rgba(29,28,29,.13) + 0 4px 12px rgba(0,0,0,.08), #f8f8f8, 420 to 540 wide, 220 high (370 for `/`) | same constants (autocomplete_view.go) | **matched** |
| Rows | 32px (`/` rows 60px with 36px icons, radius 4), padding 0 16px; selected white on #1264a3; emoji match highlight #0b4c8c on #f2c744 | same | **matched** |
| Help bar | 32px, 12px #616061 on rgba(29,28,29,.04): "↑ ↓ to navigate", "↵ to select", "esc to dismiss" | same | **matched** |
| Offline help | "No connection. Some results may not be available." with cloud-offline | not drawn | **open**: small; the popup doesn't follow the connection state |
| Section headers | only with mention sections (`composer_control_simplifications_mention_menu`) | flat lists | **matched** (default); **open** under that experiment |

## Footer buttons

| Feature | Slack (evidence) | Clack before | Status |
|---|---|---|---|
| Order | the + in WysiwygContainer's prefix, then TextyButtons (Ozua): Aa, Emoji, Mention, │, clips, │, Run shortcut; the send group last; without clips one divider before Run shortcut | same, clips included | **matched** |
| Clip buttons | Video and audio need window.MediaRecorder (Jg9Y yD / hP: isStoriesButtonEnabled, isAudioButtonEnabled); without a recorder MessagePaneInput passes them off and the clips group and its divider go | shown; a click toasted "Recording clips isn't available in yoloslack yet" | **fixed** (`crNoRecorder`, the toast is gone) |
| Recording clips | AudioPopover (waveform, timer, "Stop recording", attaches audio_message.m4a, subtype slack_audio) and the video recorder | none | **open**: Clack has huddle mic capture but no clip recorder, encoder or clip upload |
| Run shortcut | SlashCommandsButton "Run shortcut", disabled unless the input is empty (`.c-wysiwyg_container__button--workflows.--disabled`: opacity .3, tooltip still shows); a click puts "/" into the empty input | always enabled; mid-text it added " /" | **fixed** |
| Mention | MentionButton "Mention someone"; maybeTriggerMentionAutocomplete replaces the selection with "@", " @" after any character but a space (a newline counts), a "@" before the caret is replaced, not doubled | " @" after anything but a space or newline | **fixed** (`mentionTrigger`) |
| Emoji | EmojiButton "Emoji" ("Add emoji and GIFs" with the GIF picker) | "Emoji" | **matched**; the GIF picker is **open** (Clack has none) |
| + button | ShortcutsMenuTriggerButton, round 28px | same | **matched** |
| + menu rows | useComposerIAFormatter (boot-render zuQH, the ShortcutsMenu over it): the file rows first (unified_file_picker on, the account's group: one "File, canvas, or list" row with canvas-browser opening the UnifiedFilePicker; off: Canvas, List, Recent file), then New Google Calendar meeting, GIF, Enable GIFs, Salesforce record, Text snippet (text-snippet, ⌘⇧Enter / Ctrl+Shift+Enter), the clip rows, Workflow, Email, Poll, Upload from your computer (laptop, ⌘O / Ctrl+O) | Upload from your computer first (upload icon), "Paste image from clipboard" (not Slack's), disabled Text snippet and Canvas, then shortcuts | **fixed** (exp-composer): Text snippet (the snippet dialog) then Upload from your computer, Slack's icons and keys, then the app shortcuts; Paste image from clipboard is gone. **open**: the file rows (Clack has no UnifiedFilePicker, canvas picker, composer list insert or recent-files menu), GIF (Clack's GIF picker sends at once; Slack's adds the GIF to the draft) and Workflow (no workflow picker); Calendar, Salesforce, Email and Poll are off for the account |
| Ctrl+O | keyboard module "file upload" (isDesktopApp): onPlusMenuShortcut opens the + menu | nothing | **fixed** (`EvPlusMenu`) |
| Send button | SendButton: send-filled 16 + caret-down split; tooltips "Send now" with Return (CmdOrCtrl+Return when Enter adds a line) and "Schedule for later" | no tooltips | **fixed** |
| Narrow footer | TextyButtonOverflow folds into "More options" when the button row's own width is under 220 + (children − 5) × 36 = 580 (15 children, counting the empty slots) | folds below a 600px box (live earlier: folded at 582 and 383, not at 998) | **open**: the crossover depends on the row's measured width inside the footer grid; not measured |
| Edit mode | Cancel / Save | same | **matched** |

## Drafts, scheduling, editing, paste

| Feature | Slack (evidence) | Clack before | Status |
|---|---|---|---|
| Draft per conversation | MessageInput setDraft / drafts.create, update | in memory plus drafts sync (drafts_sync.go) | **matched** |
| Draft indicator | sidebar pencil; ✎ count on Drafts & sent | drawn by the sidebar | **matched** |
| Manage drafts in the composer | DraftManagementButton ("Manage drafts", "Save as draft") behind experimentDraftManagementInComposerEnabled | none | **open**: experiment-only |
| Schedule menu rows | BYKP + BQDv getScheduledSendMenuOptions picks by time block. Tuesday to Friday before 8:00 it offers Later today at 9:00 AM, from 8:00 to noon Later today at 1:00 PM, and Tuesday to Thursday afternoons Tomorrow at 9:00 AM, each followed by Monday at 9:00 AM; Friday afternoon to Sunday: Monday at 9:00 AM only; Mondays: the same with "Next Monday at 9:00 AM"; then a separator and "Custom time" | always Tomorrow / Monday / "Custom time…" | **fixed** (schedule_menu.go) |
| 24-hour labels | `time24`: "Later today at 09:00", "Tomorrow  at 09:00" (Slack's double space), "Monday at 09:00" | 12-hour only | **fixed** |
| A DM in another time zone | each row says "3:00 AM for Alice" (Mluw); at night for them (22:00 to 08:00, cCED) a row for their 9:00 leads (MemberSpecificSchedulingTime) | none | **fixed**; Slack also uses their Do Not Disturb, which Clack can't read for others: **open** |
| `updated_scheduled_sends_desktop` | "Tomorrow at 9:00 AM" always offered; the last custom time comes back while ahead | none | **fixed** |
| Custom time dialog | ScheduleMessageDialog (z1vG): "Schedule message" (18/24) over "Time zone: …" (getTimezoneLocationLabel of the detected zone), a DateTimePicker (SzUY: the date field 60%, "Today" / "Tomorrow" / "November 12th, 2026", days outside the range off; the time select 40% after 12px, 15-minute options within the range), "{memberTime} for {memberName}" in a DM across time zones, 5 minutes to 120 days ahead, default the next hour; Cancel / "Schedule Message" | calendar and steppers, next half hour, any future time, "Schedule" | **fixed** (default, limits, button; on exp-composer the DateTimePicker with popups' calendar and time list, the time zone line and the member line) |
| After scheduling | no toast; the context bar says "Your message will be sent tomorrow at 9:00 AM. See all scheduled messages" (ContextBarScheduledSendContent, 07q3; useContextBarOverride shows it while there are more scheduled messages than when the conversation opened, or the soonest is under 5 s old; "{N} messages scheduled to be sent." for several) | toast "Message scheduled for …" | **fixed** (toast gone; on exp-composer the bar, from core's new `ScheduledFor`; the link opens Drafts & sent's Scheduled tab) |
| Scheduling fails | a local ephemeral: "We’re not sure what happened, but your message couldn’t be scheduled. It’s been saved in your Drafts so you can try again." | toast "Couldn't schedule: …" | **fixed** |
| Edit last message (↑) | shouldUpArrowTriggerEdit / shouldUpArrowMoveFocusToList with the Accessibility choice; the Up binding skips code blocks; "empty" is texty.isEmpty | same rules, but spaces counted as empty and code blocks not skipped | **fixed** |
| Saving an emptied edit | cjbg maybeSave → startDelete: the delete-message dialog (else unbroadcast, else the original struck through); a message with files (fileIdsToSave) saves the empty comment instead | toast "A message can't be empty" | **fixed** (`popups.ConfirmDelete`, files excepted); the other two branches are for messages you can't delete, which Clack doesn't edit |
| Unsend (Mod+Z) | onCmdZWhenEmpty within 15 s of sending | same, spaces counted as empty | **fixed** (empty rule) |
| Rich paste | Texty clipboard: HTML to delta (cleanStyleFromHtmlDelta) | paste_html.go | **matched** (earlier work, not re-audited) |
| Pasting files and images | onFilesSelected, pasted image | attach.go | **matched** (attach agent's) |
| Edit file details (a pending file) | EditFileDialog (boot-shared e4hN): "Edit file details" ("Edit alt text" in the alt-text variant), the image preview, "File name" (required, 255) and for images "Description" (alt text, 10,000), Done / Next, "Would you like to save your work?"; reached from the pending image's file viewer (openPendingFileViewer) and the alt-text reminder speed bump; editFileApi sends files.edit {file, title, alt_txt, is_canvas_template} | none | **open**: Clack has neither the pending-file viewer nor the alt-text speed bump that open it; core now sends `alt_txt` with files.getUploadURL when a file has alt text, ready for it |
| Long paste | onPasteLimit: more than 5e4 refused; MessageInput's renderPasteLimitDialog "That’s a bit too long to paste" with "If you’d like you could **create a snippet** instead.", "Create a Snippet" (the snippet dialog with the paste) and OK | pasted anything (rich pastes over 40,000 went in as plain text) | **fixed**: refused, with Slack's dialog; on exp-composer its text and "Create a Snippet" too |
| Markdown paste toasts | MarkdownPasteToast / MarkdownModePasteToast | none | **open**: not compared |

## Character limit

| Feature | Slack (evidence) | Clack before | Status |
|---|---|---|---|
| Limit | `maxLength: 12e3` on MessagePaneInput, InputContainer and the edit composer | `maxLength` 40,000 handed to plugins, nothing enforced | **fixed** (`composerMaxLength`) |
| Counting | getMessageLengthForDelta (9ysi): JS string length; a link whose text isn't its URL counts both; an emoji its `:name:`; a mention its id; a broadcast its label; the last newline not; skipped under half the limit | none | **fixed** (`slackLength`) |
| Warning | TextyLengthWarning (Orr3): "-1,234" in 12px/18px, padding 0 4px, radius 2px, `--dt_color-base-inv-imp` (#e01e5a, dark #d3305d), at bottom 44px right 4px of the box; tooltip "Your message is N characters too long" | none | **fixed** |
| Sending over | xjGd isOverMaxLength: send button disabled, Enter does nothing | sent | **fixed** |
| Editing over | cjbg: can't save; "text is too long!" under the editor (content-imp, .8rem, right) | saved | **fixed** |

## Keyboard shortcuts in the composer

| Shortcut | Slack (evidence: fffk SlackKeyboard and the formatting bindings) | Clack before | Status |
|---|---|---|---|
| Enter / Shift+Enter | Quill's "list empty enter" first (an empty list item leaves the list, indent and all), then on_enter_onEnter: a newline only when shouldInsertNewline (xrjr: Shift or Alt; Ctrl on macOS; anything under "Start a new line"; in code under enter_is_special_in_tbt all but Shift), else MessageInput.onEnter sends: Enter in a list item with text and on an empty quote line sends; Shift+Enter adds the next item or quote/code line, two empty quote or code lines in a row end the block | lists and an empty quote line added lines | **fixed** (Karim 2026-11-10: "making new lines instead of sending") |
| Ctrl+Shift+Enter (⌘⇧Enter) | MessageInput.onEnter (enableSnippetCreation) opens the snippet dialog (wc+S SnippetDialog, lYxy editor) with the message and empties the composer | a new line | **fixed** (exp-composer: the dialog with Title (optional), Type, Content with line numbers, Wrap, Share this file and the conversation select, the message, Create Snippet; uploads with snippet_type) |
| Mod+B, I, U | bold, italic, underline | same | **matched** |
| Mod+Shift+X, C, U | strikethrough, code, link dialog | same | **matched** |
| Mod+Shift+7, 8, 9 | ordered list, bulleted list, quote | same | **matched** |
| Mod+Shift+. | "blockquote-alt" | nothing | **fixed** |
| Mod+Alt+Shift+C | code block | same | **matched** |
| Mod+Shift+F | "convert markdown": typed markdown in the message (or selection) becomes formatting (nTS+ buildMarkdownChangeDelta: lists, then code blocks, then quotes and inline marks, three Quill delta passes over OF9+/XmNU patterns) | nothing | **open**: Slack's converter not ported (it is three delta passes; Clack's mrkdwn round trip would lose list structure) |
| Mod+O | the + menu (desktop) | nothing | **fixed** |
| Mod+Z, Mod+Shift+Z | undo, redo; in an empty composer Mod+Z unsends | same | **matched** |
| Mod+A | select all | same | **matched** |
| Mod+K with a selection | onCmdKWhenSelected: a coachmark "Trying to add a link? Use ⌘ + V instead" while the jumper opens | the jumper only | **open**: coachmark not drawn |
| Mod+Left / Mod+Right, empty | onCmdLeftWhenEmpty / onCmdRightWhenEmpty: back / forward in history | word or line motion | **fixed** (`historyNavigator`) |
| Up / Mod+Up | edit last message or focus the list (above) | same | **matched** |
| Escape | on an empty quote line, or a code line that is empty or has the caret at its start, removes the block format; otherwise it goes on (the shell's close / mark read) | always went on | **fixed** (claims Escape with `shortcut.OwnEscape` only then) |
| Tab / Shift+Tab | indent / outdent a list item with a selection or the caret at its start; otherwise Slack's tab binding passes it to the browser | indented with the caret anywhere in an item | **fixed**; **open**: elsewhere Slack's Tab moves focus on (and doesn't type in code blocks), Clack keeps it in the composer |
| Backspace at a line start | "outdent backspace" outdents a nested item first, then drops the list; empty code, header and quote lines lose the format; a code line with text joins the line above | dropped the whole format at once, a code line included | **fixed** |
| Home / End | onHome / onEnd: on macOS the start / end of the whole message | line start / end everywhere | **fixed** (`edMacHomeEnd`) |
| Right at the end of inline code | "inline code right arrow": types a plain space and steps past it | moved nowhere | **fixed** |
| Down at the end of a trailing code block | "block code down arrow": adds a plain line below | nothing | **fixed** |
| Up at the start of a leading code block | "up arrow out of code block": adds a line above | nothing | **open**: small; not ported |
| PageUp / PageDown, Down when empty | handled only when the view passes onPageUp / onPageDown / onDownArrowWhenEmpty | caret moves by a page | **open**: not compared |

## Counts

87 rows: 27 matched, 45 fixed, 20 open (after exp-composer). Five rows are partly matched or fixed and partly open; they count under both.

## Effects outside the composer

- Slick's CharacterCounter reads TextyInput's `maxLength`, so it now shows Slack's "N/12,000" (the slick test expects that); docs/PLUGINS-live-slick.md still records the "43/40,000" seen live earlier.
- The composer no longer draws the video and audio clip buttons, so footer screenshots elsewhere lose them and the divider between them.
