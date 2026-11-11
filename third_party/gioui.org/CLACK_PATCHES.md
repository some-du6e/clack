# Clack's patches to Gio v0.10.3

This directory is `gioui.org@v0.10.3` from the Go module cache (byte for
byte, LICENSE included) plus the changes below. The root `go.mod` points at
it with `replace gioui.org => ./third_party/gioui.org`. When an upstream
release carries the fix, drop the replace and delete this directory.

To see the whole diff against the release:

    go mod download gioui.org@v0.10.3
    diff -ru "$(go env GOMODCACHE)/gioui.org@v0.10.3" third_party/gioui.org

It should show only `gpu/gpu.go`, `widget/index.go`, `widget/editor.go`,
`widget/text.go`, `widget/clack_spans.go`, `text/shaper.go`,
`text/gotext.go`, `op/op.go`, `internal/ops/ops.go`, `app/os_x11.go`,
`app/clack_gpu.go`, `app/clack_panel.go`, `app/os.go`, `app/os_macos.go`,
`app/os_macos.m`, `app/os_windows.go`, `app/internal/windows/windows.go`
and these two files (`CLACK_PATCHES.md`, `UPSTREAM.patch`).

## 1. gpu: drop opacity layers covered by a full-window opaque paint

`gpu/gpu.go`, `drawOps.collect` and `renderer.drawLayers`.

When a paint is a uniform opaque colour over the whole viewport and no
opacity layer is open, `collect` throws away the image ops recorded so far
(`d.imageOps = d.imageOps[:0]`) and turns the paint into the clear colour.
It kept `d.layers`, though. Opacity layers pushed and popped earlier in the
frame still held `opStart`/`opEnd` indices into the longer, discarded
`imageOps`, and `drawLayers` then indexed past the end:

    panic: runtime error: index out of range [609] with length 135
        panic: active render pass   (Metal)
    gioui.org/gpu.(*renderer).drawLayers  gpu.go:872

Karim hit it in Clack alpha.18 on macOS. In Clack the trigger is any
`paint.PushOpacity` (a dimmed pending message, a fade) followed later in
the same frame by a full-window opaque fill.

Fix: also truncate `d.layers` when the image ops are scrapped. The opacity
stack is empty there, so every recorded layer has already ended and is
fully covered by the opaque paint. As a second guard, `drawLayers` skips any
layer whose op range is empty or out of bounds instead of panicking with a
render pass open.

Tests: `internal/ui/theme/pixeltest` (`TestOpaqueFillAfterOpacityLayer`,
raw Gio ops) and `internal/ui/richview` (`TestDimmedViewUnderOpaqueFill`).
CI runs both against this copy (they must pass) and against stock
v0.10.3 (they must panic).

`UPSTREAM.patch` is the same change as a `git am` patch for
git.sr.ht/~eliasnaur/gio, made on upstream main 2919cda (whose
`gpu/gpu.go` has the same bug). It adds a `gpu/internal/rendertest` case and
its reference image; that test panics on main without the fix and
passes with it. It has not been sent. Gio wants a `Signed-off-by` line
(DCO), so whoever sends it adds their own (`git am -s`).

## 2. widget: caret positions inside multi-glyph clusters

`widget/index.go`, `glyphIndex.Glyph`.

When a cluster ends, the index spreads its runes' caret positions over the
cluster's width, starting from the X of the glyph that ends the cluster.
For a one-glyph cluster that is the cluster's start. For a cluster of
several glyphs it isn't: a base plus a zero-width mark (an emoji and its
skin tone modifier or VS16, combining accents) ends with the mark, which
sits at the cluster's right edge, so every position in the cluster, and
`Editor.Regions` for it, came out one cluster-width too far right. In
Clack the emoji images drawn over `widget.Editor` text (search box,
`plain_text_input`, theme.Editor) landed on the text after 👋🏾.

Fix: remember the X of the cluster's first glyph and spread the positions
from there (left-to-right runs; right-to-left keeps upstream's arithmetic).

Test: `internal/ui/theme` `TestThemeEditorEmojiRegions`. Not yet in
`UPSTREAM.patch`.

## 3. op: Checkpoint and Rollback

`op/op.go` (`Ops.Checkpoint`, `Ops.Rollback`) and `internal/ops/ops.go`
(`SaveCheckpoint`, `Rollback`). An addition, not a fix: no upstream API
changes behaviour.

A widget that panics half way through its layout leaves the Ops with
transforms and clips pushed and macros still recording. Recovering from
the panic isn't enough: the caller's own deferred `Pop`s then panic with
"stack push and pop must not cross macro boundary", because the macro
stack still has the abandoned macro on top. Clack alpha.24 crashed on
macOS this way when a canvas's block list changed during its layout
(internal/ui/canvasview, `layoutDoc`).

`Rollback` truncates the op data and references to the checkpoint and
restores the macro and op stacks' current ids (their counters keep going,
so a stale handle never matches). canvasview's `View.Layout` takes a
checkpoint, recovers a panic, rolls back and draws the canvas error state
in place.

Test: `internal/ui/canvasview` `TestLayoutPanicRecovered` (a panic with
pushed transforms, clips and an open macro inside a recording parent; the
parent then pops and stops cleanly).

## 4. app: IgnoreGPUBlocklist

`app/os_x11.go` (`x11Window.NewContext`) and `app/clack_gpu.go`. An
addition: nothing changes unless Clack calls it.

Gio never uses Vulkan on X11 (`vulkanBuggy`: it doesn't work reliably on
NVIDIA systems, gioui.org/issue/347). That is Gio's one GPU blocklist, the
counterpart of Chromium's: Slick's Snappy plugin appends Chromium's
`ignore-gpu-blocklist` switch when "Ignore GPU blocklist" is on, and Clack
maps the switch to `app.IgnoreGPUBlocklist()` at the next start
(internal/slick/switches.go, cmd/clack). An X11 window then tries Vulkan
first and falls back to OpenGL ES, as before, when Vulkan fails. Wayland,
macOS and Windows have no blocklist; it changes nothing there.

## 5. widget: Mod+Backspace and Mod+Delete in Editor

`widget/editor.go`, `Editor.processKey`'s delete filters and
`Editor.command` (plus `deleteToLineEdge`).

`command` checks `ModShortcut` before the delete keys. Outside macOS
`ModShortcut` and `ModShortcutAlt` are both Ctrl, so Ctrl+Backspace and
Ctrl+Delete landed in the shortcut branch, which has no case for them,
and the word deletion meant for them never ran. On macOS the delete
filters didn't list `ModShortcut` (Cmd), so Cmd+Backspace never reached
the editor and AppKit beeped. Karim hit it in a Block Kit modal's
`plain_text_input` on 2026-11-10 ("u cant delete the modal editable
field?"); every `widget.Editor` in Clack had it (search fields, dialogs,
Block Kit inputs).

Fix: with `ModShortcutAlt` held (Ctrl elsewhere, Option on macOS) the
delete keys remove a word; with Cmd alone on macOS they delete to the
start or end of the line, or the selection, and at the line's edge one
grapheme cluster, which joins the lines. That is what browser text fields
do, so Slack's inputs behave the same.

Test: `internal/ui/blockkit` `TestModalTextInputsEdit` (every Block Kit
text input in a modal; ci runs it on Linux and macOS). Not in
`UPSTREAM.patch`.

## 6. widget: styled spans in an Editor's text

`widget/clack_spans.go` (new: `TextSpan`, `Editor.SetSpans`,
`Editor.Spans`, `Editor.SpanBounds`, `textView.layoutSpans`) and
`widget/text.go` (the
`textView.spans` field and a hook at the top of `layoutText`). An
addition: an Editor without spans shapes its text as before.

Slack's search box draws its filter tokens as pills inside the text:
`from:@Karim` with 23px of padding before it for the modifier's icon,
4px after it, and the value after the first `:` in bold
(`.c-member_slug--search-filter`, docs/SEARCH-AUDIT.md). A
`widget.Editor` shapes its whole text in one font with no room for
either, so the caret, selections and clicks would not line up with
pills drawn on top.

`SetSpans` takes rune ranges, each with an optional font and empty space
(pixels) before and after it. For a single-line, unmasked editor whose
text has no line break, `layoutText` then shapes the text in segments
split at the spans' edges, each in its span's font, and moves each
segment's glyphs right by the width of everything before it (earlier
segments plus the spans' space). Only the last glyph keeps
`FlagLineBreak` and only the first `FlagParagraphStart`, so the index
sees one line; the text's bounds include the space at either end. The
glyph index builds the caret positions from those glyphs, so `Regions`,
the caret, selection painting and pointer hits all follow the styled
layout. At a span's edge the index keeps two caret positions for one
rune (before and after the space), and `Regions` starts from the first,
so a pill can't be placed from `Regions`: `SpanBounds` gives each span's
text as laid out (from its first glyph to the end of its last, in
`Regions`' coordinates). Spans are ignored (the text is shaped as usual)
for multi-line or masked editors.

Test: `internal/ui/popups` `TestSearchTokenPills` (a filter token's
modifier starts 24px after the text before it, its value is as wide as
the bold measurement, the text after it starts 4px after the value).
Not in `UPSTREAM.patch`.

## 7. text: tab stops

`text/shaper.go` (`Parameters.TabStop`), `text/gotext.go`
(`shapeAndWrapText`, `tabStops`), `widget/text.go` and `widget/editor.go`
(`textView.TabStop`, `Editor.TabStop`). An addition: zero, the default,
shapes as upstream does.

Gio's shaper has no tab stops: a tab is shaped with its font's glyph,
usually .notdef, so it draws as a box one glyph wide. Clack's snippet
dialog edits code in a `widget.Editor`, and Slack's CodeMirror draws a tab
to the next multiple of `tabSize` (4) columns from the line's start, so
the content lines up.

With `TabStop` set, each one-rune tab cluster's advance becomes
`(TabStop - col % TabStop)` space advances of its run's face, where `col`
counts the paragraph's runes before it, a tab counting the columns it
takes (CodeMirror's count). Its glyph becomes the face's space, so
nothing is drawn, and the run's advance follows. This happens before line
wrapping, so carets, selections, `Regions` and pointer hits use the
widened tabs.

Test: `internal/ui/composer` `TestSnippetTabStops`. Not in
`UPSTREAM.patch`.

## 8. app: Panel windows, and move areas on X11

`app/clack_panel.go` (new: `Panel`), `app/os.go` (`Config.panel`),
`app/os_macos.go` and `app/os_macos.m` (`GioPanelWindow`,
`GioView.acceptsFirstMouse:`, `gio_createWindow`'s `panel`, the first
show), `app/os_windows.go` (`newWindow`, `Configure`) with
`SW_SHOWNOACTIVATE` in `app/internal/windows/windows.go`, and
`app/os_x11.go` (`newX11Window`, the `ButtonPress` case,
`moveByPointer`). Additions: a window without `Panel` is created, shown
and focused as before, and an X11 window without an `ActionMove` area
gets every press as before.

Slack's presenter panel while you share your screen in a huddle
(HuddleMiniPanelMounter, internal/ui/huddleui/minipanel.go) is an
Electron BrowserWindow opened hidden, with `type: "panel"` on macOS and
`acceptFirstMouse: true`, then shown with `showInactive()`. Gio has no
such window: every new window takes the focus (`makeKeyAndOrderFront`,
`SetForegroundWindow` and `SetFocus`, a plain `XMapWindow`), a click on
one activates the app on macOS, and the click that makes a macOS window
key never reaches Gio. So a presenter clicking Mute in Clack's panel
while showing another app pulled the focus away from it, and on macOS
the first click only focused the panel.

`app.Panel(true)` does what Electron and Chromium do for those options:

- First show without the focus. macOS: `orderFrontRegardless` instead
  of `makeKeyAndOrderFront` (`NativeWindowMac::ShowInactive`). Windows:
  `SWP_NOACTIVATE` in `Configure`'s `SetWindowPos`, `SW_SHOWNOACTIVATE`
  instead of `SW_SHOWNORMAL`, and no `SetForegroundWindow`/`SetFocus`
  after creation (Chromium's `HWNDMessageHandler::Show` for
  `kInactive`). X11: `_NET_WM_USER_TIME` set to `CurrentTime` (0)
  before the map (`X11Window::Map(inactive)`), which tells the window
  manager not to focus it. Wayland: unchanged; the compositor decides,
  as it does for Chromium.
- macOS only: the window is a `GioPanelWindow`, Electron's
  `ElectronNSPanel`: an `NSWindow` whose `styleMask` always includes
  `NSWindowStyleMaskNonactivatingPanel` (the mask is reserved for
  `NSPanel`; reporting it is Electron's workaround) and whose
  `setCollectionBehavior:` always adds `CanJoinAllSpaces` and
  `FullScreenAuxiliary`, created at `NSFloatingWindowLevel`. Its
  `GioView` returns YES from `acceptsFirstMouse:`, so the click that
  makes the panel key also reaches the widgets.

The window then behaves like any other: a later click on it focuses it
on Windows and X11, as Slack's panel (focusable) does.

Move areas on X11: `system.ActionInputOp(system.ActionMove)` already
moves the window on macOS (`performWindowDragWithEvent`), Windows
(`HTCAPTION`) and Wayland (`xdg_toplevel.move`); X11 ignored it. A
primary press in an `ActionMove` area now hands the window to the window
manager the way Chromium does for a drag region (`DoWMMoveResize`):
`XUngrabPointer`, then a `_NET_WM_MOVERESIZE` client message to the root
with the press's root position and `_NET_WM_MOVERESIZE_MOVE`, and the
press isn't delivered, as on the other platforms. The panel's
background is such an area (`.p-huddle_mini_panel` is
`-webkit-app-region: drag`, its buttons `no-drag`).

Tests: `internal/ui/huddleui` `TestMiniPanelShowsInactive` (the panel's
real X11 window under Xvfb has `_NET_WM_USER_TIME` 0; huddles-audit-shots)
and `TestShareFlow` (the panel's background is an `ActionMove` area and
its buttons aren't). The macOS and Windows parts are built by ci's macos
and windows jobs; whether a window takes the focus can't be observed on
the CI runners. Not in `UPSTREAM.patch`.
