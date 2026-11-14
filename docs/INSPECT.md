# The inspect tool

Clack's inspect tool is a developer shortcut for UI bug reports. It is not a Slack feature, so nothing about it shows until it runs: no menu item, no Preferences row, no button.

## Turning it on

The shortcut works only when one of these is set:

- the environment variable `CLACK_INSPECT=1`
- `"inspect_tool": true` in `ui.json` in Clack's config folder (`~/.config/clack` on Linux), added by hand

Then ⌘⌥⇧D on macOS (Ctrl+Alt+Shift+D on Linux and Windows) starts a session. The shortcut works over modals too, so dialogs can be inspected.

## A session

A banner at the top says the session is on. The Slack component under the pointer gets a pink outline and a label with its name, root element and size. ↑ widens the outline to the enclosing component and ↓ comes back. Esc or the shortcut again ends the session.

The window keeps working while the session runs, minus clicks and keys. Hover states still show (a message's toolbar, tooltips), lists scroll, and carets, focus rings and animations stay as they are. No button, editor or shortcut reacts. The tool alone reads the clicks and keys. Gio patch 21 (`Source.HoverOnly`, `third_party/gioui.org/CLACK_PATCHES.md`) makes that possible.

A click picks the outlined element and ends the session. Clack then:

1. renders the click's frame again through a headless GPU context (the window's own GPU can't read frames back) and outlines the element on it,
2. draws the report as a panel under the screenshot, on the CPU with the Go fonts,
3. copies that one image to the clipboard, so a paste in #clack carries both,
4. saves the image and the report's text as `inspect-reports/inspect-<date>-<time>.png` and `.txt` in the config folder, readable by the owner only.

Where the system can't copy images (Linux without wl-copy or xclip) the report's text goes on the clipboard instead, and the image is still saved.

## What the report says

| row | what |
|---|---|
| component | the innermost Slack component boundary under the pointer (`slackc.Render`/`Component`), and whether a plugin patches it |
| element | its root element as a selector, Slack's classes and `data-qa` included |
| within | the enclosing components, innermost first |
| classes | the elements around it (`div.c-message_kit__message < …`) |
| tree, style | inside a plugin's host tree: the boxes under the pointer and the innermost one's computed colours and font |
| rect, pointer | in dp, window coordinates |
| region, page, overlays | the shell column, the redacted route, the popups open |
| ids, kinds | tags for the ids the components were drawn for, and what kind of conversation a tagged channel is |
| colors | the element's background and ink, and the pixel under the pointer, read from the screenshot |
| window | light or dark, the account theme, the UI font, size, scale and zoom |
| experiments | the experiments the element's code read while it drew, then the ones read around it |
| patches | the components plugins patch |

## Redaction

The report is meant for #clack, so it holds no content. It never collects message text, names, topics, emails, search queries or tokens. It is built from component names, classes, sizes, colours and tags.

An id becomes a tag: its kind and the first 6 hex digits of SHA-256 over a salt and the id (`ch:3fa2b1`, `user:77ab02`, `ts:9c01de`). Each report gets a new random salt, which is never written anywhere. Within one report the same channel always has the same tag, so "the same message" and "another one" stay apparent. Across reports, and for anyone without the salt, a tag says nothing. Routes keep their page (`c/`, `dms`) and tag every other segment. A run-time attribute value (a channel id in `data-qa-channel-sidebar-channel-id`) becomes a tag unless it is an enumeration such as `data-qa=add_reaction`.

The screenshot is the window as the user saw it. The user decides whether to paste it.

## Coverage

Coverage is best inside Slack component boundaries: messages and their parts (sender, avatar, text, reactions, the reply bar, attachments), the sidebar's items, the composer, search, activity rows, menus and more, about 90 names in all. Plain Gio widgets outside them (the message list itself, the toolbar's buttons) are reported as their enclosing component, or as the shell column (sidebar, primary, secondary, top nav, tab rail) when no component encloses them. Plugin host trees go down to their boxes, through slackc's own hit test.

How it finds what is under the pointer: while a session records a frame, slackc puts a pass-through pointer area over every component it draws, with a new tag each frame. Gio sends an Enter to each new area under the pointer, so Gio's own hit test (clips, and whatever is drawn over them) decides what counts. `internal/ui/slackc/inspect.go` has the details.

## Later

- Coverage of plain Gio widgets, by naming more boundaries where Slack has a component (the toolbar's buttons are `MessageActionButton…` in Slack).
- Encrypting the report to a key only the maintainer holds, if one-way tags aren't enough. That would let a report carry a few chosen values (a channel name, say) that only the maintainer can read.
- A way to inspect a frozen frame, for things that change under the pointer.
