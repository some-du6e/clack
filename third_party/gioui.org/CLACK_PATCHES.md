# Clack's patches to Gio v0.10.3

This directory is `gioui.org@v0.10.3` from the Go module cache (byte for
byte, LICENSE included) plus the changes below. The root `go.mod` points at
it with `replace gioui.org => ./third_party/gioui.org`. When an upstream
release carries the fix, drop the replace and delete this directory.

To see the whole diff against the release:

    go mod download gioui.org@v0.10.3
    diff -ru "$(go env GOMODCACHE)/gioui.org@v0.10.3" third_party/gioui.org

It should show only `gpu/gpu.go`, `widget/index.go`, `widget/editor.go`,
`widget/text.go`, `widget/label.go`, `widget/clack_spans.go`,
`text/shaper.go`, `text/gotext.go`, `text/lru.go`, `op/op.go`, `internal/ops/ops.go`, `app/os_x11.go`,
`app/clack_gpu.go`, `app/clack_panel.go`, `app/os.go`, `app/os_macos.go`,
`app/os_macos.m`, `app/os_windows.go`, `app/internal/windows/windows.go`,
`app/os_wayland.go`, `io/pointer/pointer.go`, `gesture/gesture.go`,
`gesture/clack_wheel.go`, `io/input/router.go`, `app/window.go`,
`app/clack_drop*.go`, `app/clack_hide*.go`, `app/clack_pancursor_windows*.go`,
`app/clack_titlebar*.go`, `app/internal/windows/clack_titlebar.go`,
`app/clack_cursors/`, `app/os_unix.go`, `app/os_darwin.go`, `app/os_android.go`,
`app/os_js.go`, `gesture/clack_autoscroll.go`, `io/input/clack_values.go`,
`internal/ops/clack_size.go`, `op/clack_size.go`, `op/clip/clack_size.go`,
`io/input/clack_hover.go`, `io/input/clack_hover_test.go`,
`font/opentype/opentype.go`, `io/input/pointer.go`, `io/input/key.go`,
`io/input/clack_release_test.go`, `gpu/clack_release_test.go`,
`text/clack_release_test.go` and these two files (`CLACK_PATCHES.md`,
`UPSTREAM.patch`).

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

## 9. Animated mouse-wheel scrolling

`io/pointer/pointer.go` (`Event.Notched`), `gesture/clack_wheel.go` (new:
`WheelCurve`, `Wheel`, `WheelDuration`, `SetWheelAnimation`),
`gesture/gesture.go` (`Scroll`), `app/os_windows.go`
(`scrollEvent`, `wheelPixelsPerDelta`) with `SystemParametersUint` and the
`SPI_GETWHEELSCROLL*` constants in `app/internal/windows/windows.go`,
`app/os_x11.go` (buttons 4 to 7) and `app/os_wayland.go` (`flushScroll`).
An event without `Notched` scrolls as upstream does.

Karim on Windows 11 (alpha.37): "scrolling feels choppy". Gio applied
each wheel notch in the frame it arrived, and as raw `WHEEL_DELTA`
units: 120 pixels at once, whatever the DPI or the "lines per notch"
setting. Slack's Chromium animates a notch: cc's
`ScrollOffsetAnimationCurve` for `ScrollType::kMouseWheel` is an
ease-in-out (`kEaseInOutControlPoints`, cubic-bezier(0.42, 0, 0.58, 1))
whose duration falls with the distance (`DurationBehavior::kInverseDelta`:
12 frames of 60 Hz, 200 ms, up to 120px; 6 frames, 100 ms, from 480px;
linear between). A notch that arrives while it runs moves the target on
and `UpdateTarget` rebuilds the curve from where it is, scaling y1 so the
velocity carries over, with the duration bounded by
`VelocityBasedDurationBound` (2.5 times the time to the target at the
current speed). The target is clamped to the scroller's range. Read in
Chromium main df2e801: `cc/animation/scroll_offset_animation_curve.cc`,
`scroll_offset_animation_curve_factory.cc`, `scroll_offset_animations_impl.cc`.

- `Event.Notched` marks a Scroll from a notched wheel whose `Scroll` is
  already the notches' distance in pixels. Only the OS backends set it.
- `gesture.Scroll` (so every `layout.List` and `widget.List`) feeds a
  notch into a `WheelCurve` and reports the curve's progress frame by
  frame, asking for the next frame with `op.InvalidateCmd` while it moves,
  as its fling does. A precise delta applies at once and stops a running
  animation (Blink cancels a wheel animation for a precise-pixel scroll).
  `Stop` stops it too.
- `gesture.Wheel` is the same for widgets that read `pointer.Scroll`
  themselves (`internal/ui/listview`).
- `SetWheelAnimation(false)` makes notches instant. Clack sets it from
  the OS's reduced-motion setting each frame, as Chromium's
  `ScrollAnimationsEnabledBySystem` does (`internal/ui/osmotion.go`).

Where notches come from, and their distance (Chromium's numbers):

- Windows: a `WM_MOUSEWHEEL` delta that is a whole multiple of
  `WHEEL_DELTA`. `SPI_GETWHEELSCROLLLINES` lines (3 when unreadable) of
  `kScrollbarPixelsPerLine` (100/3 dp) per notch, 100 dp by default;
  `WM_MOUSEHWHEEL` `SPI_GETWHEELSCROLLCHARS` (1) of the same width;
  `WHEEL_PAGESCROLL` 87.5% of the window's height
  (`kMinFractionToStepWhenPaging`, `cc/input/scroll_utils.h`; Blink takes the scroller's). Read on
  every event, as `WebMouseWheelEventBuilder::Build`
  (`ui/events/blink/web_input_event_builders_win.cc`) does. Smaller
  deltas (precision touchpads, free-spinning wheels) are not marked
  and are converted at the same rate. Chromium gets precision touchpads
  through DirectManipulation and can't tell the rest apart
  (crbug.com/545234). Gio has no DirectManipulation.
- X11: buttons 4 to 7, 120 dp each (`MouseWheelEvent::kWheelDelta` on
  Linux, `ui/events/event.cc`; Gio moved 10 pixels, 20 horizontally).
- Wayland: an axis with discrete steps, 120 dp per 10 of axis value
  (`kAxisValueScale`, `ui/ozone/platform/wayland/host/wayland_pointer.cc`;
  Gio multiplied the value by 10). Continuous axes (touchpads) are
  unchanged.
- macOS: none. Chromium animates the wheel there only when the
  `NSScrollAnimationEnabled` default is set (`animation_mac.mm`), which
  it isn't out of the box, and Gio passes AppKit's deltas through
  unchanged.

Tests: `internal/ui/wheel` (the curve against ease-in-out, durations,
retargets keeping speed, reversal, clamping, touchpad passthrough,
reduced motion, and a `layout.List` animating through a real
`input.Router`). ci's windows-applog job runs
`internal/ui` `TestWheelFrameProbe` (frame times while notches scroll a
long channel). Not in `UPSTREAM.patch`.

## 10. Idle frames: input.ExecuteHook, and the caret in an inactive window

`io/input/router.go` (`Source.Execute`), `widget/editor.go`
(`blinkInterval`, `Editor.inactive`, `Editor.Layout`).

Karim runs Clack all day on a Windows laptop; Slack's Chromium draws
nothing while nothing changes.

- `input.ExecuteHook`: a package variable that sees every command a
  `Source` executes, on the caller's stack, before the router does. Nil
  by default (one load per command). Clack's idle-frame probe
  (`internal/ui/idleprobe`, `internal/ui` `TestIdleFrames`) sets it to
  record who asked for each `op.InvalidateCmd`; the router keeps only the
  earliest wake-up time.
- The `Editor` caret blinks like Blink's `FrameCaret`
  (`third_party/blink/renderer/core/editing/frame_caret.cc`): a
  repeating timer every `LayoutTheme::CaretBlinkInterval`, with no
  timeout, while the frame is focused and active. Upstream Gio blinked
  for 10 s and then showed a solid caret. The interval is Chromium's
  default, `OsSettingsProvider::kDefaultCaretBlinkInterval` (500 ms,
  `ui/native_theme/os_settings_provider.h`). Windows' `GetCaretBlinkTime`
  overrides it since patch 13; GTK's `gtk-cursor-blink-time` / 2 does
  in Chromium, not here yet.
  When the window loses the focus Gio keeps the key focus and sends the
  editor `FocusEvent{Focus: false}`; upstream restarted the blink on it,
  so a background window kept drawing for 10 s. Chromium disables the
  caret in an inactive window
  (`FrameSelection::FocusedOrActiveStateChanged` →
  `FrameCaret::SetCaretEnabled(false)`, `frame_selection.cc`): now the
  editor draws no caret and asks for no frames until a
  `FocusEvent{Focus: true}`. A `ReadOnly` editor, whose caret is never
  painted, no longer asks for blink frames either (Blink disables
  blinking in read-only text controls).

Read in Chromium main f6df131 (2026-11-12). Clack's own composer editor
(`internal/ui/composer/editor.go`) does the same. Not in `UPSTREAM.patch`.

## 11. OS file drops: app.FileDropEvent

`app/clack_drop.go` (the event, the queue, text/uri-list),
`app/window.go` (`eventSummary.drops`, `nextEvent`, `processEvent`),
`app/clack_drop_windows*.go` with hooks in `app/os_windows.go`,
`app/os_macos.go` and `app/os_macos.m`, `app/clack_drop_x11.go` with hooks
in `app/os_x11.go`, and `app/os_wayland.go`.

Gio v0.10.3 has no way to take files dragged from Explorer, Finder or a
file manager. Slack's FileDragDrop (boot-shared `oCSS`) shows a drop
overlay over the pane under the pointer and attaches what's dropped, and
Karim drags screenshots onto Clack on Windows every day.

`Window.Event` now returns `FileDropEvent{Kind, Position, Paths,
Modifiers}`: one `FileDragEnter`, `FileDragOver` as the pointer moves,
then `FileDragLeave` (the pointer left, or Esc) or `FileDrop`. They come
before the next `FrameEvent`, in order; consecutive `FileDragOver`s fold
into the last one. `Position` is in the FrameEvent's coordinates (Gio's
own decorations subtracted, like the router's pointer events). Only drags
that carry files send anything. The window always answers the OS with a
copy. Gio doesn't redraw for these: the app asks for a frame when it
draws something.

- Windows: an OLE drop target (`RegisterDragDrop` after `OleInitialize`
  on the window's thread), the IDropTarget vtable as
  `syscall.NewCallback`s, no cgo. CF_HDROP from the data object's
  `GetData`, `DragQueryFileW` for the paths, `ScreenToClient` for the
  point; MK_SHIFT/MK_CONTROL/MK_ALT for the modifiers. The POINTL argument
  is passed by value: one register on amd64 and arm64
  (`clack_drop_windows_64.go`), two words on 386
  (`clack_drop_windows_32.go`). Each method also calls
  IDropTargetHelper (`CLSID_DragDropHelper`), as Chromium's
  `DropTargetWin` does, so the shell's drag image keeps drawing over the
  window. When OLE can't start there (another apartment model on the
  thread), the window falls back to `DragAcceptFiles`; `WM_DROPFILES` is
  handled either way as one `FileDrop` at `DragQueryPoint`. The target is
  revoked on `WM_DESTROY`.
- macOS: GioView registers for `NSPasteboardTypeFileURL` and implements
  NSDraggingDestination (`draggingEntered:`, `draggingUpdated:`,
  `draggingExited:`, `prepareForDragOperation:`,
  `performDragOperation:`), reading file URLs with
  `NSPasteboardURLReadingFileURLsOnlyKey`. The point is converted like
  mouse events (flipped, times the backing scale).
- X11: XDND version 5 (`XdndAware`). A drag whose types (the message's
  three, or `XdndTypeList`) include `text/uri-list` sends `FileDragEnter`
  on its first `XdndPosition` and `FileDragOver` on the rest, answering
  `XdndStatus` with `XdndActionCopy`; `XdndDrop` converts `XdndSelection`
  to `text/uri-list`, and the `SelectionNotify` brings the paths for
  `FileDrop` before `XdndFinished`. X11 only learns the paths on the drop,
  so `FileDragEnter` has none there. Modifiers come from `XQueryPointer`.
- Wayland: `wl_data_device` enter/motion/leave/drop for an offer with
  `text/uri-list`: `wl_data_offer_accept` and `set_actions(copy, copy)`
  on enter, then on drop `wl_data_offer_receive` into a pipe (read with a
  2 s deadline), `wl_data_offer_finish` when the compositor chose copy,
  and the offer destroyed. Upstream's enter destroyed every offer but the
  clipboard's; `flushOffers` now keeps the drag's.

Tests: `app/clack_drop_test.go` (order, folding, the decoration offset,
drops before a ConfigEvent, URI lists; ci's linux job), and on ci's
windows-applog job `app/clack_drop_windows_test.go`: PowerShell C#
drags like Explorer from another process (the left button down on its
own window, then `DoDragDrop` of a WinForms `DataObject` with CF_HDROP,
the cursor moved onto a real Gio window and on by 10, 5 px, then a drop
or a cancel), and a `WM_DROPFILES` with an HDROP built in-process; on
ci's linux job `app/clack_drop_x11_test.go` under Xvfb: python-xlib as
the XDND source (XdndEnter, two XdndPositions, XdndDrop answering the
SelectionRequest, or XdndLeave).
Not in `UPSTREAM.patch`.

## 12. text: letter spacing (CSS letter-spacing)

`text/shaper.go` (`Parameters.LetterSpacing`, `Parameters.PxPerSp`,
`Shaper.SetTracking`, `Shaper.SetTrackingScale`), `text/lru.go` (the
layout cache key), `text/gotext.go` (`shapeText`'s features,
`letterSpacing`), `widget/label.go` and `widget/text.go` (`PxPerSp` from
the context). An addition: with no tracking function and a zero
`LetterSpacing`, text shapes as upstream does.

Slack's Theme component (boot-shared `oWug`) sets
`--custom-font-tracking-*` for San Francisco Pro and Segoe UI (System),
Roboto Mono, Noto Sans and Meiryo, and Slack's CSS pairs each type size
with its variable as `letter-spacing` (`body` 15px, `h1`–`h3`, and about
2,000 rules in client-boot-styles). Gio's shaper had no letter spacing.

A `LetterSpacing` (or, when it's zero, the shaper's tracking function at
the text's size in sp, `PxPerEm / PxPerSp`) is applied the way Blink's
`ShapeResult::ApplySpacingOrExpansion` applies letter-spacing: the last
glyph of every cluster gets that much more advance, except clusters that
start with a rune Blink treats as a zero-width space (form feed, CR,
U+FFFC, ZWNJ, ZWJ, default ignorables) and non-space clusters of cursive
scripts (Arabic, Hanifi Rohingya, Mandaic, Mongolian, N'Ko, Phags-pa,
Syriac; CSS Text 4 §8.2.1). Like Blink's `FontFeatures`, spaced text is
shaped with `liga`, `clig` and `calt` off. The truncator gets the same.
This happens before line wrapping, so wrapping, carets, selections,
`Regions` and pointer hits use the spaced advances. The spacing is part
of the layout cache key.

`PxPerSp` is set by `widget.Label` and `widget.Editor`/`Selectable` from
the context; callers that build `Parameters` themselves either set it or
rely on `SetTrackingScale` (the window's metric). `Untracked` opts out of
the tracking function for callers that space text themselves.

Tests: `internal/ui/theme` `TestLetterSpacingShaper` and
`TestTrackingForSize`. Not in `UPSTREAM.patch`.

## 13. app: Window.Hide, showing a hidden window, and the caret blink time from Windows

`app/clack_hide.go` (new: `Window.Hide`), `app/os_windows.go`
(`window.hide`, `runLoop`, `Perform`'s `ActionRaise`,
`setCaretBlinkFromOS` in `newWindow`), `app/internal/windows/windows.go`
(`IsWindowVisible`, `GetCaretBlinkTime`, `SWP_HIDEWINDOW`) and
`widget/editor.go` (`CaretBlinkInterval`, `SetCaretBlinkInterval`, the
caret in `Editor.Layout`). Additions: nothing hides unless the app calls
`Hide`, and outside Windows the blink stays 500 ms.

Slack's desktop keeps running in the notification area when its window is
closed (the `runFromTray` setting, on by default; `main.bundle.cjs`
4.53.276 @1104712). Its MAIN_WINDOW_CLOSE handler (@1137077) calls
`preventDefault` on the close and `BrowserWindow.hide()`, and the tray
icon's click shows the window again (`FOREGROUND_APP` → `FOCUS_WINDOW`:
`show()`, `focus()`). Gio could abort a close (`ClosingEvent.Abort`) but
not hide a window.

- `Window.Hide()` runs on the window's thread and reports whether the
  window was hidden. Windows: Chromium's `HWNDMessageHandler::Hide`
  (Electron's `hide()`), `SetWindowPos` with `SWP_HIDEWINDOW |
  SWP_NOACTIVATE | SWP_NOMOVE | SWP_NOREPOSITION | SWP_NOSIZE |
  SWP_NOZORDER`, not `ShowWindow(SW_HIDE)`, which activates another window
  itself. Other platforms return false and do nothing (Clack then lets the
  close go on). It may be called while handling a `ClosingEvent`: the
  window thread runs `Window.Run` functions while it waits for the app to
  take the next event.
- A hidden window draws no frames, as a minimized one doesn't: `runLoop`
  skips its animation frames while `IsWindowVisible` is false. Gio still
  delivers its wake-up event to `Window.Event` after every `Invalidate`,
  so an app can run posted work then (Clack's `drainIdle`).
- `Perform(system.ActionRaise)` on a hidden window shows it first the way
  Chromium's `HWNDMessageHandler::Show(kNormal)` does: `SW_SHOW` when it
  was maximized (it stays maximized), `SW_SHOWNORMAL` otherwise, then
  raises it as before. The `WM_PAINT` that follows draws it.
- The caret blink time: Chromium on Windows asks
  `OsSettingsProviderWin::CaretBlinkInterval`
  (`ui/native_theme/os_settings_provider_win.cc`): `GetCaretBlinkTime`,
  the 500 ms default when that returns 0, and no blinking for `INFINITE`
  (the Control Panel's "Cursor blink rate: None"). Windows sends no
  notification when it changes, so Chromium reads it when it builds a
  renderer's preferences. Gio reads it as each window opens
  (`setCaretBlinkFromOS`) into `widget.SetCaretBlinkInterval`.
  `widget.CaretBlinkInterval()` is what `Editor` blinks at; zero keeps the
  caret drawn and asks for no frames (Blink's `FrameCaret` with a zero
  `LayoutTheme::CaretBlinkInterval`). Windows' default is 530 ms. Clack's
  composer editor reads the same function.

Read in Chromium main 64e5bbc6 (`ui/views/win/hwnd_message_handler.cc`
`Show`, `Hide`, `Activate`) and Electron main 295785f5. Tests on ci's
windows-applog job (`CLACK_WINDOWS_TRAY_TESTS`):
`app/clack_hide_windows_test.go` (`TestHideAndRaise`: hide from inside an
aborted `ClosingEvent`, no frames but wake-ups while hidden, `ActionRaise`
shows it, maximized stays maximized; `TestCaretBlinkFromOS`: the runner's
blink time set to 800 ms, 530 ms and `INFINITE` with `SetCaretBlinkTime`,
then put back), and `internal/ui` `TestCloseToTrayWindow` (Clack's own
window). Not in `UPSTREAM.patch`.

## 14. app: nothing delivered after DestroyEvent

`app/os.go` (`eventLoop.destroyed`, `FlushEvents`). While
`deliverEvent` waits to hand the app its `DestroyEvent`, its `select` may
run a pending invalidate first (Go picks among ready cases at random):
`callbacks.Invalidate` queues a wakeup after `nextEvent` cleared the
queue for the `DestroyEvent`. The app returns at the `DestroyEvent`, and
`FlushEvents` went on to deliver that wakeup to nobody, forever. On
Windows that is inside `WM_DESTROY`: `DestroyWindow` never returns and the
HWND is never freed. `FlushEvents` now delivers nothing once a
`DestroyEvent` went out. Tested by `internal/ui` `TestCloseToTrayWindow`
(Quit while the window is hidden; it hung in 7 of 32 runs on 2026-11-12).
Not in `UPSTREAM.patch`.

## 15. Middle-click autoscroll

`gesture/clack_autoscroll.go` (new: `Autoscroller`, `Autoscroll`,
`AutoscrollSpeed`, `SetAutoscroll`, `AutoscrollVeto`, `AutoscrollPress`),
`gesture/gesture.go` (`Scroll`), `io/input/clack_values.go` (new:
`Source.Value`, `Source.SetValue`) with a `values` field in
`io/input/router.go`, `io/pointer/pointer.go` (eleven `Cursor…Panning`
cursors), `app/clack_pancursor_windows.go` (new) with
`app/clack_cursors/*.cur` and their `LICENSE`, `loadCursor` and the
cursor table in `app/os_windows.go`, `CreateIconFromResourceEx` in
`app/internal/windows/windows.go`, and the cursor tables in
`app/os_unix.go`, `app/os_darwin.go`, `app/os_android.go` and
`app/os_js.go`. Nothing changes unless an app runs an `Autoscroller`.

Karim, Windows 11, 2026-11-12: "middle wheel holding no workie pls make
equal to chromeium". Slack's Chromium on Windows scrolls when you press
the middle button over something that scrolls. Read in Chromium main
5ed4bb5 (2026-11-13):

- When: `RuntimeEnabledFeatures::MiddleClickAutoscroll`, set on in
  Windows by `WebViewImpl::ApplyWebPreferences`
  (`third_party/blink/renderer/core/exported/web_view_impl.cc`); on Linux
  only with `features::kMiddleClickAutoscroll` (chrome://flags, off,
  because a middle click pastes there); never on macOS.
  `gesture.SetAutoscroll` has the same defaults. Chromium doesn't look at
  reduced motion for it (no check in `AutoscrollController` or
  `FixedVelocityCurve`), so Clack doesn't either.
- Start: `Node::DefaultEventHandler` (`core/dom/node.cc`) on a middle
  mousedown: nothing in a link; otherwise the nearest box whose content
  overflows either way (`LayoutBox::IsUserScrollable`,
  `layout_box_hot.cc`). `AutoscrollController::StartMiddleClickAutoscroll`
  (`core/page/autoscroll_controller.cc`) then walks up from it for the
  first box that can scroll vertically and the first that can scroll
  horizontally. Here every `Scroll` (so every `layout.List`, `widget.List`
  and `widget.Editor`) offers itself on a middle press with the axes its
  ranges allow; scrollers lay out outside in, so the last offer per axis
  is the innermost. Widgets that read `pointer.Scroll` themselves use an
  `Autoscroll` (`Offer` on the press, `Tick` each frame); a link calls
  `AutoscrollVeto`.
- Speed: `HandleMouseMoveForMiddleClickAutoscroll`, per axis: nothing
  within `kNoMiddleClickAutoscrollRadius` (15 DIPs) of the origin, else
  `pow(|d|, kExponent 2.2) × kMultiplier (-0.000008)`. The browser turns
  that into a fling that never decays (`RenderWidgetHostImpl::AutoscrollFling`,
  `ui/events/gestures/fixed_velocity_curve.cc`: offset = velocity × 5000
  × seconds), so the speed is `0.04 × |d|^2.2` DIPs a second, 1005 at
  100 DIPs. Distances are in dp of the frame's metric.
- Modes: `kMiddleClickInitial` until the speed first changes
  (`kMiddleClickHolding`); releasing the middle button while still
  initial makes it `kMiddleClickToggled`; any release while holding
  stops it (`HandleMouseReleaseForMiddleClickAutoscroll`). Any press
  while it runs stops it and is swallowed (`EventHandler::HandleMousePressEvent`,
  kHandledSuppressed); any key down stops it and every key event is
  swallowed while it runs (`KeyboardEventManager::KeyEvent`). A scroller
  that goes away is dropped, and with both gone it ends
  (`StopAutoscrollIfNeeded`).
- Cursor: Chromium paints nothing at the origin. The origin icon is the
  cursor: `MiddleClickAutoscrollCursor` gives `kMiddlePanning` (the round
  four-arrow `pan_middle.cur`, or its vertical or horizontal variant when
  only one axis scrolls) until the pointer leaves the dead zone, then the
  direction's panning cursor among the axes that scroll. On Windows those
  are Chromium's own images (`ui/resources/cursors/pan_*.cur`,
  `IDC_PAN_*` in `ui_unscaled_resources.grd`, loaded with `LoadCursor` by
  `ui/base/win/win_cursor_factory.cc`). Windows has no panning cursors, so
  Gio now ships those eleven files and makes them with
  `CreateIconFromResourceEx(LR_DEFAULTSIZE)`, which sizes them as
  `LoadCursor` does. Elsewhere the new cursors are the all-scroll and
  resize cursors Chromium uses on Linux (`CursorNamesFromType`,
  `ui/base/cursor/cursor_factory.cc`).

How it runs in Gio. The app owns one `Autoscroller` per window and calls
`Begin` before its layout and `End` after it. `Begin` stores it in the
window's router (`Source.SetValue`), where every `Scroll` finds it through
its `input.Source`, and reads the events of a window-wide input area that
`End` adds last. A middle press there is pending until `End`. If a
scroller offered itself and no link vetoed it, `End` starts it and
executes a `pointer.GrabCmd` for the area's tag: the other handlers get
`Cancel` (Chromium's `InvalidateClick`), and the held button's drags and
release come to the area wherever the pointer goes. While toggled the
area doesn't pass events through, so the click that stops it reaches
nothing else; otherwise it passes them. A wheel turned while toggled is
eaten by it, where Chromium would still scroll. The area carries the
cursor. Each target `Autoscroll.Tick` moves its scroller by speed × frame
time, within its ranges, and asks for the next frame with
`op.InvalidateCmd` only while that moves something. With no speed (the
dead zone) or the end in the way it asks for none, so an autoscroll left
on at the bottom of a list costs no frames.

Clack runs it in `internal/ui` `App.frame`: `Begin` first, before every
key and pointer reader; `End` just under the window-wide pointer tracker.
Its message lists and threads, the sidebar, views, Preferences and
editors are `layout.List`/`widget.List`/`Scroll`s; the Lists views'
grid, board and item page read `pointer.Scroll` and use `Autoscroll`
(`internal/ui/listview/wheel.go`); rich text links veto.

Tests: `internal/ui/autoscroll` (the speed against Chromium's arithmetic,
holding, the dead zone, click to toggle, any click and any key stopping
and being swallowed, a quick click, nothing to scroll, a link, off, the
end of the list asking for no frames, a scroller going away, dp, nested
scrollers per axis), `internal/ui` `TestAutoscrollInClack` (Clack's frame
over a fakeslack channel and the sidebar). On ci's windows-applog job
(`CLACK_WINDOWS_AUTOSCROLL_TESTS`): `app/clack_pancursor_windows_test.go`
(`TestPanCursors`: every cursor loads from its file, as a cursor, with its
hotspot) and `internal/ui` `TestAutoscrollWindow` (SendInput's middle
button held and clicked on Clack's own window). Not in `UPSTREAM.patch`.

## 16. op: the memory a recorded path holds

`internal/ops/clack_size.go` (`Ops.Size`), `op/clack_size.go`
(`CallOp.Size`) and `op/clip/clack_size.go` (`PathSpec.Size`). An
addition: nothing calls it but Clack.

`text.Shaper.Shape` records each outline into an `op.Ops` of its own, and
`internal/ui/richview` keeps every shaped word's outline in its word cache.
The cache was bounded by its count only (two generations of 8,000 words),
and at the Hack Club account's scale it held 120 MB of outlines, nearly all
of words no message on screen used (the memory workflow, perf.yml's scale
jobs, 2026-11-12). `PathSpec.Size` is roughly what an outline holds: its
`Ops` data and reference arrays' capacity. richview ends a generation once
its words hold 8 MB (`maxCachedWordBytes`).

`internal/ui/theme`'s emboldened-run cache (1,500 runs, which held 117 MB
on Linux, where dark text draws as emboldened outlines) ends at 16 MB the
same way (`maxRunBytes`).

richview and theme are also built against stock Gio v0.10.3
(`.github/scripts/gio-layer-fix.sh`), so they call this through
`theme.PathBytes` (`internal/ui/theme/gio_patch16.go`); on stock Gio
(`gio_patch16_stock.go`) an outline counts 1 KB a glyph. Tests:
`internal/ui/richview` `TestWordCacheByteBudget`, `internal/ui/theme`
`TestEmboldenCacheByteBudget`. Not in `UPSTREAM.patch`.

## 17. font/opentype: fonts parsed in place

`font/opentype/opentype.go` (`Parse`, `ParseCollection`).

`Parse` and `ParseCollection` read the file through go-text's
`opentype.NewBytesResource` (Clack's go-text patch 2,
`third_party/typesetting/CLACK_PATCHES.md`) instead of a `bytes.Reader`,
so the face's tables are slices of `src` rather than copies: Clack's
embedded Lato faces held 6 MB of copied tables. `src` must not change
after the call; Clack's fonts are embedded, or files read once and kept.
Not in `UPSTREAM.patch`.

## 18. Per-frame buffers let go of earlier frames' references

`io/input/router.go`, `io/input/pointer.go`, `io/input/key.go` and
`gpu/gpu.go` (`drawOps.reset`).

The router rebuilds its hit tree, pointer areas, focus order and key
filters every frame into the same arrays (`s = s[:0]`, then appends), and
trims its state history the same way; the renderer collects its image and
path ops so too. Truncating leaves the entries past the new length in place,
so after a frame with more handlers or paints than the next, the tail kept
the earlier frame's tags, and whatever owns them, reachable: a closed emoji
picker (its 10 MB index; Gio's tag is a field of the picker), the rows and
slackc boxes of a long channel scrolled away, and the pixels of images the
image cache had already dropped. Each buffer is now cleared before it is
truncated, as `ops.Reset` already does for an `Ops` list's references. Only
memory changes: every buffer is refilled with fresh values, and nothing reads
past its length.

Tests: `io/input` `TestRouterReleasesTagsOfEarlierFrames` (a focused, hovered
handler's owner is collectable three frames after it was last laid out),
`gpu` `TestDrawOpsReleaseImagesOfEarlierFrames` (an image a shorter frame no
longer paints is collectable), both on ci's linux job; `internal/ui`
`TestClosedEmojiPickerReleased` (the whole client: a closed picker is
collectable three frames later). Not in `UPSTREAM.patch`.

## 19. text: the Shaper lets go of the text it shaped

`text/shaper.go` (`Shaper.layoutText`).

`Shaper.Layout` reads its text through a `bufio.Reader` it keeps, and
`LayoutString` keeps the string it iterated; both stayed until the next call
of the same kind. Clack shares one Shaper per theme, and an editor shapes
through a reader over its own `textView`, so the last editor that shaped
stayed reachable with whatever owns it: a closed emoji picker's search field
kept the picker and its 10 MB index (found by `internal/ui`
`TestClosedEmojiPickerReleased`, which names the chain:
`App.th.Shaper.reader.rd`). The reader is reset to nil and the string
dropped once the text is read; the lines and glyphs `NextGlyph` reads are
the Shaper's own.

Test: `text` `TestShaperReleasesSource`, on ci's linux job. Not in
`UPSTREAM.patch`.

## 20. app: options keep a hidden or minimized window as it is (Windows)

`app/os_windows.go` (`window.Configure`, `window.configured`).

`Configure` ended every `Window.Option` with `ShowWindow` for the mode it
computed, and Gio reads a minimized window's mode as `Windowed`, so any
option showed a window hidden with `Window.Hide` (§13) and restored a
minimized one with `SW_SHOWNORMAL`. Clack's window title carries the unread
count ("… - 3 new items", Slack's DocumentTitle), so each new count brought
a window closed to the tray back on screen, or restored one the user had
minimized. Electron's `setTitle` is Chromium's `SetWindowText` and nothing
more; `setMinimumSize` only changes the size limits.

After the first `Configure` (`newWindow`'s, which shows the window), options
that leave the mode as it was skip the show on a hidden or minimized window:
the title and style still change, a hidden window still takes its new
size (`SetWindowPos` with `SWP_NOACTIVATE`, no show flag), and a minimized
one keeps its place, since `GetWindowRect` then gives the icon's
rectangle. An option that changes the mode (Maximized, Fullscreen, back
from Fullscreen) applies as before, showing the window as Electron's
`maximize()` does.

Found by `internal/ui` `TestTaskbarWindow` (flakes-1114, 2026-11-14): core's
count replaced the test's just after a close hid or minimized the window, in
2 of 30 runs. The test now changes the count on purpose after each close and
revive. Tests: that one, and `app/clack_hide_windows_test.go`
`TestOptionKeepsHiddenAndMinimized` (a title and a minimum size on a hidden
and on a minimized window; then a maximize still applies), on ci's
windows-shell and windows-applog jobs. Not in `UPSTREAM.patch`.

## 21. io/input: a Source that only hovers

`io/input/router.go` (the `Source.hoverOnly` field, `Source.Event`) and
`io/input/clack_hover.go` (`Source.HoverOnly`). An addition: a Source
nobody asks for one behaves as before.

Clack's inspect tool (internal/ui/inspect, a developer shortcut for bug
reports) points at widgets the way a browser's inspect-element mode does:
the widget under the pointer gets an outline, and a click picks it
instead of pressing it. While it runs, the window must look exactly as it
did (the hover states that show a message's toolbar or a tooltip, the
caret, the focus rings, animations) and keep scrolling, but no button,
editor or shortcut may react. `Source.Disabled()` can't do that: with it
`Focused` is always false (every caret and focus ring disappears, and
code that polls focus sees a blur), commands are dropped (lost focus
requests and invalidations), and the widgets declare no filters, so the
router's idea of what is hovered drifts from theirs and hover states
stick after the tool ends.

`HoverOnly` returns a Source whose `Event` hands out pointer Enter,
Leave, Move, Scroll and Cancel events and focus changes, and takes every
other event that matches (Press, Release, Drag, key and text input,
transfers) from the queue and drops it. Filters still reach the router,
`Execute` still runs commands and `Focused` still answers, so hover and
focus stay as the widgets believe them to be, before, during and after.

Tests: `io/input/clack_hover_test.go` (`TestHoverOnlyDropsClicks`: Enter
and Move, no Press or Release, nothing left for a later reader;
`TestHoverOnlyScrolls`; `TestHoverOnlyKeepsFocusDropsKeys`: still
focused, the typed key and text dropped, where a plain Source gets both),
on ci's linux-gio job. Not in `UPSTREAM.patch`.

## 22. app: HiddenTitleBar, a window without its title bar that keeps the frame (Windows)

`app/clack_titlebar.go` (new: `HiddenTitleBar`, `captionHit`),
`app/clack_titlebar_windows.go` (new), `app/internal/windows/clack_titlebar.go`
(new: the HT codes, `WM_NCPOINTER*`, `WM_POINTERLEAVE`, `WM_NCMOUSELEAVE`,
`SM_CXPADDEDBORDER`, `GetSystemMetricsForDpi`, `GetCursorPos`,
`WindowFromPoint`, `GetCapture`, `MonitorFromWindow`, `SHAppBarMessage`),
`app/os.go` (`Config.hiddenTitleBar`) and `app/os_windows.go` (`windowProc`'s
`WM_NCHITTEST`, `WM_NCCALCSIZE`, `WM_GETMINMAXINFO`, the non-client pointer
messages; `Configure`'s window size; `pointerUpdate` notes the mouse's
pointer ID). An addition: a window without the option behaves as before.

Slack's main window on Windows is frameless (win32 `main.bundle.cjs`
4.53.276 @1144516: `frame: false`, `titleBarStyle: "hidden"`,
`titleBarOverlay: {color: "#00000000", symbolColor: "#FFFFFF", height: 33}`).
Electron 45 (Slack's) keeps the standard window styles for an opaque
frameless window (its `fix_remove_caption-removing_style_call.patch` to
Chromium), takes the caption into the client area with `WM_NCCALCSIZE`
(`ElectronDesktopWindowTreeHostWin::GetClientAreaInsets`: restored, the
frame thickness on the left, right and bottom and nothing at the top;
maximized, the frame thickness all round, which is the work area) and draws
Windows' caption buttons over the page (`WinCaptionButtonContainer`). Gio's
`Decorated(false)` on Windows instead makes the whole window client area and
draws the resize borders inside it. A decorated Gio window keeps its OS
title bar, which Clack's top nav then sat under (a doubled title bar, gap
2).

`HiddenTitleBar(true)` on a decorated window:

- `WM_NCCALCSIZE`: restored, the client rectangle is the window's inset by
  the frame thickness (`SM_CXSIZEFRAME` + `SM_CXPADDEDBORDER` at the window's
  DPI, Chromium's `GetResizableFrameThicknessFromMonitorInPixels` with
  `WS_CAPTION`) on the left, right and bottom; maximized, the monitor's work
  area, 2 px off an edge with an auto-hide taskbar (Chromium's
  `kAutoHideTaskbarThicknessPx`; a top one moves the bottom up a pixel, as
  `OnNCCalcSize` does for a system-drawn frame), so Windows doesn't take the
  window for a full-screen one and keep the taskbar hidden.
  `SHAppBarMessage(ABM_GETAUTOHIDEBAREX)` calls into the shell, so it runs
  on a goroutine, cached per monitor and refreshed after a second, as
  Chromium's `GetAppbarAutohideEdges` does; a change posts the window a
  message that recomputes its frame. The window keeps `WS_OVERLAPPEDWINDOW`
  and the DWM frame isn't extended, so DWM keeps the shadow and the
  rounded corners, Snap works, and the system menu, Alt+Space and the
  minimize/maximize animations are Windows' own.
- `WM_NCHITTEST`, in `WinFrameView::NonClientHitTest`'s order: points
  outside the client area (the borders) are `DefWindowProc`'s; a full-screen
  window is all `HTCLIENT`; then the topmost `system.ActionInputOp` area:
  `ActionMinimize` is `HTMINBUTTON`, `ActionMaximize` or `ActionUnmaximize`
  `HTMAXBUTTON` (Windows 11 opens its Snap Layouts flyout over it),
  `ActionClose` `HTCLOSE`; then, restored and resizable, the top
  frame-thickness band is `HTTOP`, with `HTTOPLEFT`/`HTTOPRIGHT` within 16
  DIPs less the side border of the corners (`kResizeCornerWidth`); then
  `ActionMove` areas are `HTCAPTION`; the rest `HTCLIENT`.
  (`captionHit` is the platform-free part.)
- Pointer input over the caption buttons is non-client input
  (`WM_NCPOINTERUPDATE/DOWN/UP`, Gio has mouse-in-pointer on). The program's
  widgets get it as client pointer events, as Chromium routes the
  non-client mouse messages to its caption button views: a press over a
  button isn't passed to `DefWindowProc`, which would turn it into a
  `WM_NCLBUTTONDOWN` and run its classic caption-button loop (painting,
  capture, the button's own `WM_SYSCOMMAND`); the program performs the
  action (Clack posts `WM_SYSCOMMAND`). A release ends a press a button
  took wherever it lands. These messages don't carry the hit-test code
  their documentation promises: the high word of `wParam` is the
  pointer's flags, as in the client messages (`0x2016` for a primary
  button down, seen on the CI runner), so the patch hit-tests the
  position itself. Moves over the caption and
  the borders reach the program too, so hover ends there, and still go to
  `DefWindowProc`, as do presses on the caption and the borders (move,
  resize, double-click to maximize, the right-click system menu). Touch and
  pen go to `DefWindowProc`.
- `WM_POINTERLEAVE` and `WM_NCMOUSELEAVE` are a `pointer.Leave` once the
  cursor is off the window (`WindowFromPoint`, as Chromium's
  `HandleMouseEventInternal` checks): they also come as it crosses between
  the caption and the client area. Gio delivered no Leave on Windows
  before; a window without the option still doesn't.
- `WM_GETMINMAXINFO` adds the restored frame (twice the thickness wide,
  once tall) to `MinSize` and `MaxSize`, and `Configure` sizes a `Windowed`
  window the same way, instead of `AdjustWindowRectEx`'s caption.

Clack's main window uses it on Windows (`internal/ui/titlebar.go`, W34):
the top nav lays out `ActionMove` under its widgets and Electron's caption
buttons as `ActionMinimize`/`ActionMaximize`/`ActionClose` areas. Read in
Electron 45-x-y 6bfc23bf (`shell/browser/ui/views/win_frame_view.cc`,
`win_caption_button_container.cc`, `frameless_view.cc`,
`shell/browser/ui/win/electron_desktop_window_tree_host_win.cc`, its
`patches/chromium`) and Chromium 156.0.8078.100
(`ui/views/win/hwnd_message_handler.cc`, `ui/base/win/hwnd_metrics.cc`,
`ui/views/window/frame_view.cc`).

Tests: `app/clack_titlebar_test.go` (`TestCaptionHit`, the order of the
parts; `TestHiddenTitleBarOption`) on ci's linux-gio job, and on its
windows-applog job (`CLACK_WINDOWS_TITLEBAR_TESTS`)
`app/clack_titlebar_windows_test.go`: `TestHiddenTitleBarFrame` (the client
area starts at the window's top, the frame thickness on the other sides,
the client size asked for; a window without the option keeps its caption),
`TestHiddenTitleBarHitTest` (`WM_NCHITTEST` sent as Windows sends it over
the buttons, the caption, the top band and corner, the client and the
borders; maximized, no band and the restore button, the client area the
work area; a window without the option answers `HTCAPTION` for its own
caption and `HTCLIENT` over the action areas) and
`TestHiddenTitleBarButtonsInput` (`SendInput`: a hover of a button is its
widget's, the cursor leaving the window over the top ends it, a click is the
widget's click and Windows doesn't maximize, minimize or close the window
itself, a press dragged off a button clicks nothing, a button inside the
caption is the program's, a double-click on the caption maximizes).
`internal/ui` `TestTitleBarWindow` is Clack's window on the same job. Not
in `UPSTREAM.patch`.
