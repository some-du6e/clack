# The inspect tool

Clack's inspect tool is a developer shortcut for UI bug reports. It is not a Slack feature, so nothing about it shows until it runs: no menu item, no Preferences row, no button.

## Turning it on

Type the Konami code anywhere in the main window: ↑ ↑ ↓ ↓ ← → ← → B A. The tool is on at once, with no relaunch, and a toast says so. Clack keeps it on (`"inspect_tool": true` in `ui.json`). The code again turns it off.

The keys still do what they always do wherever you type them: the arrows move the caret, B and A type, and in an empty composer ↑ opens your last message for editing (Esc leaves it as it was). Clack only watches them go by (Gio patch 28, `third_party/gioui.org/CLACK_PATCHES.md`).

Two other ways, both read at launch only:

- the environment variable `CLACK_INSPECT=1`. A Clack opened from the Dock or Finder doesn't get a shell's variables, and a second launch hands over to the running Clack, so on macOS quit Clack (⌘Q; closing the window only hides it) and start `/Applications/Clack.app/Contents/MacOS/clack` from a shell that has it.
- `"inspect_tool": true` in `ui.json` in Clack's config folder: `~/Library/Application Support/clack` on macOS, `~/.config/clack` on Linux, `%AppData%\clack` on Windows. Edit it with Clack quit: Clack writes `ui.json` from what it read at launch whenever a setting changes, the window moves or you switch conversations, so an edit made while it runs is lost.

Then Hyper+D starts a session: ⌃⌥⇧⌘D on macOS, Ctrl+Alt+Shift+D on Linux and Windows (where Super may be held too). The shortcut works over modals too, so dialogs can be inspected.

Until Gio patch 29 the shortcut, then ⌥⇧⌘D, did nothing on macOS. AppKit sends no command for a ⌘ press with ⌥ held (`interpretKeyEvents:` calls neither `doCommandBySelector:` nor `insertText:`), and Gio dropped every ⌘ press that came without one, so no key filter ever saw it; Hyper+D has ⌥ too. `TestMacInspectKeys` on ci's macos job posts the keys through AppKit as a keyboard makes them.

## A session

A banner at the top says the session is on: "Inspect: click or drag to blur, then copy · shift-click copies at once · ↑ ↓ parent · F freezes · Esc to stop". The Slack component under the pointer gets a pink outline and a label with its name, root element and size; one of the chrome's elements (below) gets one with its selector. ↑ widens the outline to the enclosing element or component and ↓ comes back. F freezes the window on the frame on screen (below). Esc or Hyper+D again ends the session, though in a frozen window the first Esc only unfreezes it.

The window keeps working while the session runs, minus clicks and keys. Hover states still show (a message's toolbar, tooltips), lists scroll, and carets, focus rings and animations stay as they are. No button, editor or shortcut reacts. The tool alone reads the clicks and keys. Gio patch 21 (`Source.HoverOnly`, `third_party/gioui.org/CLACK_PATCHES.md`) makes that possible. The window also drops its system action areas for the session (the top nav's drag area on macOS, Linux and Windows, the Windows caption buttons): the system acts on a press there before Clack sees it, so it would move the window instead of picking or dragging.

There are three ways to capture:

- **Click** an element: the outlined element is captured and the review below opens first, so parts of it can be blurred out before anything is copied. A press that moves less than 6 dp before it comes up is still a click.
- **Shift+click** an element: the same capture, copied and saved at once with no review, and the session ends. Shift counts if it is held at the press or at the release. (Until 2026-11-17 these two were the other way round; Karim asked for the review on every plain pick.)
- **Drag** a rectangle, for what the picker can't outline: once the pointer has moved 6 dp pressed, the window dims around the rectangle being dragged out and its size shows beside it. Releasing captures that region as it is and opens the review. Esc with the button down drops only the press or the drag, and the session goes on. A region thinner than 8 dp grabs nothing.

Every way, Clack renders the capture from the release's frame, or in a frozen window from the frozen frame, again through a headless GPU context (the window's own GPU can't read frames back). It draws the crop at 4 px per dp whatever the display, under a transform over the frame's ops (Gio patch 31, `Ops.Whole`). Paths and glyphs are vectors, so the element comes out as sharp as a window four times as dense, and the image stays small enough that Slack shows it near its own size. A crop whose image would pass 3200 px is drawn less dense, never below the window's own px. For a click or a Shift+click the crop is the element and 24 dp around it. That margin gets blurred past reading (a 5 dp box blur, three passes) so the neighbouring messages, channels and names are smears, and the element is outlined. For a drag the crop is the region itself, with no margin, no blur and no outline.

### Freezing the window

Some things show only while the pointer is on them: a message's hover toolbar, a tooltip, a hover card, a button's hover colour. Picking one usually means moving the pointer off it, and then it's gone. F holds the window on the frame on screen instead. Hover the thing, press F, and the window stays exactly as it was drawn, hover states, tooltips, the caret's blink and the animated emoji included. The banner turns to "Frozen: click or drag to blur, then copy · shift-click copies at once · ↑ ↓ parent · F or Esc unfreezes".

The pointer then moves over the frozen picture. The outline follows what the frozen frame has under the pointer, ↑ and ↓ work as before, and a click, a Shift+click or a drag captures from the frozen frame. To get a toolbar, freeze over its message, move off the message, then onto the toolbar where the frozen frame still draws it. In a live window the toolbar would be gone by then.

F again or Esc unfreezes, and the session goes on; Esc again ends it. Holding either key down counts once. A capture that ends the session ends the freeze too, and so does a new window size. While frozen the window lays nothing out, so nothing in it changes. A message that lands meanwhile reaches the client's data and shows when the freeze ends, and a report captured from the frozen frame describes that frame: its page, pane, overlays and theme are the ones from when the freeze began. Keys and clicks go to the tool alone, and the composer keeps the focus; nothing typed while frozen reaches it. On Windows an input method that is composing (Japanese or Chinese, say) keeps F to itself, so switch it to direct input first.

A session ends by itself when the sign-in screen comes up (the Slack session ended, Sign Out, another account) and when the window closes to the tray, frozen or not. Files dropped on the window during a session are ignored: nothing is attached or shared.

How it works. The tool keeps each session frame's ops as one call before it draws over them (Gio patch 31's `Ops.Whole`). The tool reads a frame's input before the window resets its ops, so when F comes the frame on screen is still whole. From then on every frame draws that call again with the tool's outline, label and banner on top, in place of a layout. The frozen frame's own inspect areas stay in the ops with their tags, so Gio's hit test still decides what the pointer is over, and slackc's `InspectFrame.Hold` keeps each area as its events left it. The image cache holds every animated image on the frame it showed (`imgcache.HoldLive`), the huddle window's too, which draws from the same cache. When the freeze ends, Gio patch 49 (`Source.ResetPointers`) catches every widget up before the window lays out again. No widget saw the pointer move while the window was frozen, so the reset sends what the router would have sent: a pointer Cancel to every widget, a Leave to each one that last heard the pointer come in and isn't under it now, and an Enter and a Move to each one under it. Every event carries the mouse's own pointer id, which matters on Windows, where the mouse is pointer 1 and a hover ignores another pointer's events. Without the Leave, the hovers that clear only on a Leave would stay: a message body's link and emoji tooltip, a plugin tree's :hover, a sidebar heading's ⋯ and +. The freeze holds one frame's ops, which stay in the window's own buffer, and lets them go when it ends. A window with no session pays nothing.

### Blurring before it's copied

After a click or a drag nothing is copied yet. The shot is shown still over the dimmed window, exactly as the copy will look, under the banner "Blur: drag over anything private · ⌫ undo · Enter copy · Esc cancel":

- drag rectangles over anything that shouldn't be shared (other people's messages, names, avatars). Each one is blurred out of the shot. Drag as many as you need, and the banner counts them.
- ⌫, Delete or Mod+Z takes the last rectangle back. A right click takes back the newest one under the pointer.
- Enter (or Mod+C) copies and saves the report with the rectangles baked into the image.
- Esc or Hyper+D drops the report: nothing is copied or saved.

A blurred rectangle holds nothing of what was under it. It is filled from the nearest pixels outside every rectangle (on each side, level with the pixel, the nearer side weighing more), then smoothed 6 dp. It is not a blur of its own pixels, so no deblurring or sharpening can bring a name or a face back. The rectangles are painted over the shot before the margin is blurred, so the margin's smear carries nothing of them either. `TestComposeRedactKeepsNothingInside` composes two shots that differ only under a rectangle and requires the two images to be identical. The report's text says how many rectangles the reporter blurred.

Then (at once for a Shift+click, on Enter for the review) Clack:

1. draws the report as a panel under the shot, on the CPU with the Go fonts. The image is as wide as the shot, or as the panel's 600 dp minimum for a small one (the shot sits centred above the panel at its own size),
2. copies that one image to the clipboard, so a paste in #clack carries both,
3. saves the image and the report's text as `inspect-reports/inspect-<date>-<time>.png` and `.txt` in the config folder, readable by the owner only.

Where the system can't copy images (Linux without wl-copy or xclip) the report's text goes on the clipboard instead, and the image is still saved. Where there is no shot at all (the headless capture failed) the report's text is copied at once, with no review.

## What the report says

| row | what |
|---|---|
| capture | `dragged region` for a drag; absent for a click |
| component | the innermost Slack component boundary under the pointer (`slackc.Render`/`Component`), and whether a plugin patches it. A dragged region has none |
| element | its root element as a selector, Slack's classes and `data-qa` included; for a chrome element picked itself, that element |
| within | the enclosing components, innermost first |
| classes | the elements around it (`div.c-message_kit__message < …`) |
| tree, style | inside a plugin's host tree: the boxes under the pointer and the innermost one's computed colours and font |
| rect, pointer | in dp, window coordinates |
| region, page, overlays | the shell column (for a dragged region, every column it takes in), the redacted route, the popups open |
| blurred | how many rectangles the reporter blurred out of the image |
| ids, kinds | tags for the ids the components were drawn for, and what kind of conversation a tagged channel is |
| colors | the element's background and ink, and the pixel under the pointer, read from the screenshot |
| window | light or dark, the account theme, the UI font, size, scale and zoom |
| experiments | the experiments the element's code read while it drew, then the ones read around it |
| patches | the components plugins patch |

## Redaction

The report is meant for #clack, so it holds no content. It never collects message text, names, topics, emails, search queries or tokens. It is built from component names, classes, sizes, colours and tags.

An id becomes a tag: its kind and the first 6 hex digits of SHA-256 over a salt and the id (`ch:3fa2b1`, `user:77ab02`, `ts:9c01de`). Each report gets a new random salt, which is never written anywhere. Within one report the same channel always has the same tag, so "the same message" and "another one" stay apparent. Across reports, and for anyone without the salt, a tag says nothing. Routes keep their page (`c/`, `dms`) and tag every other segment. A run-time attribute value (a channel id in `data-qa-channel-sidebar-channel-id`) becomes a tag unless it is an enumeration such as `data-qa=add_reaction`.

The image shows the picked element as the user saw it, and of the rest of the window only the blurred margin around it, or a dragged region as it was. Whatever the element or region holds (a picked message's text) is in it, unless the user blurs it out in the review. A click (or a drag over it) opens the review before anything is copied or saved, so the user decides what to share; a Shift+click copies the element as it is, for when nothing in it is private.

## Coverage

Coverage is best inside Slack component boundaries: messages and their parts (sender, avatar, text, reactions, the reply bar, attachments), the sidebar's items, the composer, search, activity rows, menus and more, about 90 names in all. The chrome Clack draws without a component around it is marked for the tool alone with Slack's classes and `data-qa` (`slackc` `ClassDef.Inspect` and `InspectArea`: recorded for the picker, never on the element stack, so no selector matches them and nothing draws differently), as a browser's inspector reaches any element: the channel header (`.p-view_header`, its title button, the channel's name `span.p-view_header__channel_title`, the icon, the topic container, the actions), the tab bar (`c-tabs__tab_menu`, each `c-tabs__tab` and its title `c-tabs__tab_content`), the top nav (its containers, the history buttons, the search box and its text), the tab rail (`.p-tab_rail`, each `p-tab_rail__button` with Slack's `data-qa`, `tab_rail_activity_button`, its icon and label, the user button), and the Activity view's saved-view tabs and their titles. Plugin host trees go down to their boxes, through slackc's own hit test; of two trees under the pointer the one with its own element there wins, so an element a plugin puts among a native's children (UserPronouns' span in BaseBroadcastPreamble) is picked, not the native's box in the tree around it.

Closed since α.88:

- the Activity filter bar (Karim, α.85: the Unreads filter button couldn't be picked). The bar and its parts are marked with Slack's component names (`slackc.InspectAs`: a name for the picker only, which no plugin can patch; the names come from `docs/PLUGINS-props-live.md`): `ActivityFilterBar`, `ActivityFilterBulkAction` (the select group), `ActivityFilterUnreadsButton`, `ActivityFilterByMenu`, `ActivityFilterBySpecific` (the In pill), `ActivityFilterSearch` and `ActivityFilterLayoutToggle`.
- the message hover toolbar's buttons (`button.c-button-unstyled.c-icon_button… .c-message_actions__button` with their `data-qa`). They were entered with no box, so the picker landed on the whole `MessageActions` card.
- the top nav's bare parts on macOS, Linux and Windows, and the Windows caption buttons. Their system action areas took the press before the tool saw it, so a click there started moving the window (or minimized or closed it). A session now lays out none.

Still not pickable as themselves, found by a static pass over the source (files with no component boundary or inspect mark inside them) rather than live. Each is reported as the nearest component around it, or as the shell column (sidebar, primary, secondary, top nav, tab rail) when nothing encloses it:

- the message list itself, the toolbar's quick-reaction emoji, and the tooltips;
- the inside of channel details (`details.go`, `details_more.go`, `details_agents.go`, `details_posting.go`), Later, Drafts, Files and channel files, Unreads, Huddles and their history, folder tabs, the file picker and canvas comments;
- Preferences pages, the sign-in card and the introduction screens, the workflow pages and the drop overlay.

The drag is the way to capture any of these. Naming more of them means a boundary or an `InspectAs` mark with Slack's component name where Slack has one.

How it finds what is under the pointer: while a session records a frame, slackc puts a pass-through pointer area over every component it draws and every chrome element marked with a size, with a new tag each frame. Gio sends an Enter to each new area under the pointer, so Gio's own hit test (clips, and whatever is drawn over them) decides what counts. `internal/ui/slackc/inspect.go` has the details.

## Later

- Coverage of the plain Gio widgets listed above, by naming more boundaries where Slack has a component.
- Encrypting the report to a key only the maintainer holds, if one-way tags aren't enough. That would let a report carry a few chosen values (a channel name, say) that only the maintainer can read.
