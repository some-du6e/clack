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
`app/clack_drop*.go`, `app/clack_hide*.go`, `app/clack_wakeup_test.go`, `app/clack_pancursor_windows*.go`,
`app/clack_titlebar*.go`, `app/internal/windows/clack_titlebar.go`,
`app/clack_bounds*.go`, `app/internal/windows/clack_bounds.go`,
`app/cocoatest/`, `app/clack_cursors/`, `app/os_unix.go`, `app/os_darwin.go`, `app/os_android.go`,
`app/os_js.go`, `gesture/clack_autoscroll.go`, `io/input/clack_values.go`,
`internal/ops/clack_size.go`, `op/clack_size.go`, `op/clip/clack_size.go`, `op/clack_whole.go`,
`io/input/clack_hover.go`, `io/input/clack_hover_test.go`,
`font/opentype/opentype.go`, `io/input/pointer.go`, `io/input/key.go`,
`io/input/clack_release_test.go`, `gpu/clack_release_test.go`,
`text/clack_release_test.go`, `io/input/clack_keylog.go`,
`io/input/clack_keylog_test.go`, `app/clack_keys_darwin_test.go`,
`app/metal_darwin.go`, `app/clack_wake_darwin_test.go`,
`gpu/clack_stats.go`, `gpu/caches.go`, `gpu/path.go`,
`gpu/headless/clack_stats.go`, `gpu/headless/clack_cost_test.go`,
`gpu/internal/driver/clack_calls.go`, `gpu/internal/metal/metal_darwin.go`,
`gpu/internal/d3d11/d3d11_windows.go`, `gpu/internal/opengl/opengl.go`,
`internal/gl/gl_unix.go`, `internal/gl/gl_windows.go`,
`internal/gl/gl_js.go`, `op/clip/clip.go`, `op/clip/shapes.go`,
`op/clip/clack_interior_test.go`, `op/clip/clack_hole_test.go`,
`gpu/clack_regions_test.go`, `gpu/clack_split_test.go`,
`gpu/headless/clack_identity_test.go`, `gpu/headless/clack_helpers_test.go`,
`gpu/internal/opengl/clack_attrib_test.go`,
`app/metal_macos.go`,
`app/app.go`, `app/clack_asked_test.go`, `app/clack_asked_x11_test.go`, `op/paint/clack_live.go`,
`io/input/clack_replay.go`, `io/input/clack_replay_test.go`,
`op/paint/clack_live_test.go`, `gpu/clack_live_test.go`,
`gpu/headless/clack_live_test.go`, `layout/list.go`,
`layout/clack_list_test.go`, `gpu/clack_partial.go`, `gpu/clack_sticky.go`,
`gpu/clack_floor.go`,
`gpu/pack.go`, `gpu/clack_partial_test.go`, `gpu/headless/clack_partial_test.go`,
`gpu/internal/driver/clack_partial.go`, `internal/d3d11/d3d11_windows.go`,
`internal/gl/gl.go`, `app/clack_caret_macos.go`, `app/clack_caret_macos.m`,
`app/clack_caret_darwin_test.go`, `app/clack_xsettings_x11.go`,
`app/clack_xsettings_nox11.go`, `app/clack_xsettings_x11_test.go`,
`app/clack_unmap_x11_test.go`, `app/clack_keytext_windows_test.go`,
`gpu/clack_opcache_test.go`, `io/input/clack_focus.go`,
`io/input/clack_focus_test.go`, `io/input/clack_inputidle.go`,
`io/input/clack_inputidle_test.go`,
`io/input/clack_reset.go`, `io/input/clack_reset_test.go`,
`gesture/clack_reset_test.go` and
these two files (`CLACK_PATCHES.md`, `UPSTREAM.patch`).

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
  overrides it since patch 13 and macOS's insertion point period since
  patch 47; on Linux Clack sets GTK's `gtk-cursor-blink-time` / 2, as
  Chromium does (`internal/ui/caretblink_linux.go`).
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
`Hide`, and outside Windows the blink stays 500 ms (since patch 47,
macOS reads its own; Clack reads GTK's on Linux).

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
  itself. macOS hides since §24 (`orderOut:`). Other platforms return false
  and do nothing (Clack then lets the close go on). It may be called while
  handling a `ClosingEvent`: the
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
  position itself. The legacy `WM_NCLBUTTONDOWN`, `WM_NCLBUTTONDBLCLK` and
  `WM_NCLBUTTONUP` for a button are the program's as well: touch comes that
  way (`DefWindowProc` turns it into mouse input), and so does a click while
  Windows 11's Snap Layouts flyout is open. Given one, `DefWindowProc`'s
  caption-button loop waits for a `WM_LBUTTONUP` that never comes with
  mouse-in-pointer on (the release is a pointer message): Clack's window
  hung in it on the CI runner (ci 43578875698) after a click on maximize
  with the flyout up. Moves over the caption and
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
caption is the program's, a click on maximize with the Snap Layouts flyout
open is the widget's and leaves the window running, a double-click on the
caption maximizes; a failure prints the window messages and Gio events
around it, from `clackMsgTrace`, a hook only the tests set).
`internal/ui` `TestTitleBarWindow` is Clack's window on the same job. Not
in `UPSTREAM.patch`.

## 23. app: StartHidden, a window created without being shown (Windows)

`app/clack_hide.go` (new: `StartHidden`), `app/os.go`
(`Config.startHidden`) and `app/os_windows.go` (`window.startedHidden`;
`Configure`'s first run; `newWindow`'s focus). An addition: a window
without the option shows as before.

Slack's desktop window starts hidden when Launch on Login started the app
(`--startup`, `environment.invokedOnStartup`) and the tray's Launch Control
says Keep in Background (`hideOnStartup`, true by default): `sf`
(`main.bundle.cjs` 4.53.276 win32 @929699) keeps the BrowserWindow, created
with `show: false`, unshown until the tray icon brings the app forward.
Gio's first `Configure` ends in `ShowWindow`, and `newWindow` then takes
the foreground and the focus, so a window could only be hidden after it
had flashed up and taken the focus.

`StartHidden(true)` among a window's first options: that first `Configure`
places, sizes and styles the window (`SetWindowPos` with `SWP_NOACTIVATE`)
but leaves out the `ShowWindow`, and `newWindow` doesn't call
`SetForegroundWindow` or `SetFocus`. The window is then hidden as
`Window.Hide` leaves one (§13): it draws only the frames Gio's `update`
draws on its own `WM_WINDOWPOSCHANGED` and `WM_STYLECHANGED` (the
creation's two, and two for each option that changes it while hidden, as
for a window hidden to the tray; Electron also paints a `show: false`
window's first frame, `paintWhenInitiallyHidden`), `Invalidate` draws
nothing but still wakes the app, options that leave the mode alone keep it
hidden (§20), and `Perform(system.ActionRaise)` shows it (`SW_SHOWNORMAL`,
or `SW_SHOW` for a maximized mode), after which it paints. Elsewhere the
option does nothing.

Tests: `app/clack_hide_windows_test.go` `TestStartHidden` (not visible and
not the foreground window after its creation, no frames from `Invalidate`
but wake-ups, a title change keeps it hidden, `ActionRaise` shows and draws
it, the close works),
and `internal/ui` `TestTrayWindow` (Clack's own window launched on login),
on ci's windows-shell job (`CLACK_WINDOWS_TRAY_TESTS`). Not in
`UPSTREAM.patch`.

## 24. app: HiddenTitleBar and TrafficLightPosition on macOS, and Window.Hide there

`app/clack_titlebar.go` (`HiddenTitleBar`'s macOS part, new
`TrafficLightPosition`, `titlebarDoubleClickAction`), `app/os.go`
(`Config.trafficLight`), `app/os_macos.go` (`Configure`, `updateWindowMode`,
`gio_onMouse`'s click count and double-click, `hide`, `shown`, `draw`,
`SetAnimating`, `Perform`'s `ActionRaise`, `newWindow`,
`toggleFullScreenLater`), `app/os_macos.m` (`GioView`'s `titlebarHidden`,
traffic-light and `orderedOut` properties,
`_opaqueRectForWindowMoveWhenInTitlebar`, `viewDidChangeEffectiveAppearance`;
`GioWindowDelegate`'s `windowWillClose:` and its resize, key, main,
full-screen and occlusion methods; `gio_createWindow`'s `hiddenTitlebar`;
`redrawTrafficLights`), `app/clack_hide.go` (the doc) and `app/cocoatest/`
(new: the tests' AppKit helpers). Additions, but for two changes:
`Window.Hide` hides on macOS where it returned false, and every macOS
window now gets its `DestroyEvent` when it closes (below).

Slack's main window on macOS is frameless (mac `main.bundle.cjs` 4.53.276
@1145075: `frame: false`, `titleBarStyle: "hidden"`): the page runs under
the title bar, which shows no title, and the traffic lights stay, at
`{x, Math.floor(y / 2)}` of the `trafficLightPosition` setting (clamped
to 0..40 by `sg`, @974771). The setting is `{x: 14, y: 22}` (@1117788)
until the web client's top nav writes `{x: 12, y: 24}`
(`injectCustomTitlebarStyling`, boot-render), so the window opens with
the buttons at `{12, 12}` from then on. Closing it hides it
(`MAIN_WINDOW_CLOSE`, @1148845: `BrowserWindow.hide()`, after leaving full
screen). Gio's `Decorated(false)` on macOS hid the buttons with the title
bar, a decorated window kept its title bar, which Clack's top nav sat
under (gap 4), and nothing could hide a window, so a close quit Clack
(gap 3).

`HiddenTitleBar(true)` on a decorated macOS window is Electron's
`titleBarStyle: "hidden"` (`NativeWindowMac`'s constructor, Electron
45-x-y):

- The window is created with `NSWindowStyleMaskFullSizeContentView`, so its
  content is the size asked for and covers the title bar; the title bar is
  transparent with its title hidden, and window tabs are off (Electron's
  `NSWindowTabbingModeDisallowed` for a frameless window). The buttons
  stay. `Config.Decorated` stays true, so Gio draws no decorations of its
  own. In full screen the title bar AppKit reveals is an ordinary one with
  the title (`NotifyWindowEnterFullScreen`), hidden again as the window
  leaves it.
- `GioView` answers its bounds to AppKit's
  `_opaqueRectForWindowMoveWhenInTitlebar`, as Chromium's
  `BridgedContentView` does: under a transparent title bar the window
  server drags the window from whatever a view doesn't report as opaque,
  without asking the app. So every click in the title bar band reaches the
  view, and an `ActionMove` area drags the window as before
  (`performWindowDragWithEvent:`).
- The release of a double-click on an `ActionMove` area does what System
  Settings' "Double-click a window's title bar to" says, as Chromium's
  `NativeWidgetMacNSWindow` `sendEvent:` does for custom draggable areas:
  `AppleActionOnDoubleClick` unset or `Maximize` zooms (`performZoom:`),
  `Minimize` minimizes, `Fill` fills (`_zoomFill:`, macOS 15), anything
  else does nothing. No native title bar sees these clicks, since the view
  takes them all.

`TrafficLightPosition(x, y)` is Electron's `trafficLightPosition`, laid
out as its `WindowButtonsProxy` `redraw` does: the buttons' title bar
container becomes their height plus 2y tall at the window's top and the
buttons sit x from the left edge (from the right in a right-to-left title
bar), centred in it. AppKit lays the title bar out again after a resize, a
title or style change, the buttons' visibility (macOS 26), a key or main
window change and a theme change, so the window moves them back after
each, as Electron does; the container hides while the window leaves full
screen and comes back moved.

`Window.Hide` is `NativeWindowMac::Hide` (`orderOut:`). A full-screen
window first leaves full screen and is ordered out once
`windowDidExitFullScreen:` has returned, as Slack's `hx` awaits
`leave-full-screen` before it hides (the mode is `Windowed` from then on,
so an option given meanwhile doesn't send it back). The toggle waits for
the run loop's default mode, outside the close button's click and
`windowShouldClose:`, as Slack's handler leaves full screen after the close
event returned. A hidden window draws no frames (a frame would wait for a
drawable nothing shows; until §32 Gio's Metal layer waited without a
timeout) and keeps its display link stopped; the app still gets its
wake-ups. `ActionRaise` shows it, and
so does any other order-front (the occlusion change that follows): it
draws again.

A closing window lets go of its content view (`windowWillClose:`), for
every window. Gio's `DestroyEvent` comes from `GioView`'s `dealloc`, and the
view only went with its window: on the macOS 26 runner every closed window,
plain or with a hidden title bar, was still alive after its close
(`TestMacCloseDestroys` holds weak references), so no `DestroyEvent` came
within 10 s and Clack's Quit never ended `Run`. With the view let go, it
deallocs at once and Gio sends `DestroyEvent`, as Chromium's
`windowWillClose:` tears its widget down without waiting for AppKit to
free the window.

Clack's main window uses all three on macOS (`internal/ui/titlebar_darwin.go`,
W35). Read in Electron 45-x-y (`shell/browser/native_window_mac.mm`,
`shell/browser/ui/cocoa/window_buttons_proxy.mm`,
`electron_ns_window.mm`, `electron_ns_window_delegate.mm`) and Chromium
main (`components/remote_cocoa/app_shim/bridged_content_view.mm`,
`native_widget_mac_nswindow.mm`).

Tests: `app/clack_titlebar_test.go` (`TestHiddenTitleBarOption`'s
`TrafficLightPosition`, `TestTitlebarDoubleClickAction`) on ci's linux-gio
job, and on its macos job (`CLACK_MACOS_WINDOW_TESTS`: `TestMain` runs
`Main` on the main goroutine) `app/clack_titlebar_darwin_test.go`:
`TestHiddenTitleBarMac` (full-size content, the window the size asked
for, a transparent title bar with no title and no tabs, the buttons at
{12, 12} after a title change, a resize and a key change, `Decorated`
still true, `_opaqueRectForWindowMoveWhenInTitlebar` the whole view, where
AppKit sends clicks in the title bar band and on the close button, a click
on a widget in the band the widget's), `TestTitlebarDoubleClickMac`
(synthesized double-clicks with each `AppleActionOnDoubleClick`),
`TestHideMac` (the red button's aborted close hides, no frames but
wake-ups, `ActionRaise` and an order-front outside Gio show it, a close
destroys a hidden window), `TestMacCloseDestroys` (a close of a plain
window and of hidden-title-bar ones brings `DestroyEvent`, and logs whether
the view and the window are still alive) and `TestHideLeavesFullscreenMac`
(skipped on the runner: it enters full screen, but no `toggleFullScreen:`,
AppKit's own from the main queue included, ever takes the window out
again, so the full-screen hide is untested there). `internal/ui`
`TestMacWindowCloseHides` is Clack's window on the same job.
`cocoatest.PostKey` posts a key press as a keyboard makes one (each
modifier's `flagsChanged`, then the key's down and up with the characters
the current layout types, `UCKeyTranslate`) for `NSApp` to deliver, and
`TraceKeys` records what the view's key methods get (`internal/ui`
`TestMacInspectKeys`). Not in `UPSTREAM.patch`.

## 25. app: Bounds, Displays, and where a window is (Windows, macOS)

`app/clack_bounds.go` (new: `Bounds`, `Config.Bounds`, `Config.Display`,
`Display`, `Displays`), `app/clack_bounds_windows.go`,
`app/clack_bounds_macos.go` and `app/clack_bounds_other.go` (new: each
platform's `displays` and the window's place), `app/internal/windows/clack_bounds.go`
(new: `EnumDisplayMonitors`, a monitor's information and effective DPI, a
thread's per-monitor DPI awareness), `app/os.go` (`Config.bounds`,
`boundsSet`, `display`), `app/os_windows.go` (`newWindow` and `init`
create the window at the bounds; `update` reports them; `Configure`'s
`Windowed` frame; `window.hiddenShow` and `Perform`'s `ActionRaise`),
`app/os_macos.go` (`updateWindowMode` reports them; `Configure` places the
window before the mode, its size limits in every mode, a first full
screen once the window is on the screen; `newWindow` doesn't cascade a
placed window; `gio_onWindowMove`), `app/os_macos.m` (`windowDidMove:`;
`gioViewOf` in the delegate's other methods) and `app/cocoatest/` (`Window.ScreenFrame`, `Screens`, `SetTopLeft`,
`ZoomWindow`). Additions, but for the macOS size limits (below).

Slack reopens its main window where it was (gap 9). Its settings keep
`mainWindowSettings`: bounds, maximized, fullScreen (and on macOS the
window's Space, through its own native module). `MAIN_WINDOW_ACTIONS`
(win32 `main.bundle.cjs` 4.53.276 @1146194) saves them a second after the
window is maximized, unmaximized, restored, resized, moved, shown,
focused or blurred, a maximized window's bounds replaced by the default
bounds on its display (`rV(screen.getDisplayMatching(win.getBounds()))`).
The window is created at the bounds (`{...bounds, fullscreen}`, @1143722),
`validateBoundsForDisplay` (@1146420) keeps them on a display that is
still connected, and `INITIALIZE_MAIN_WINDOW` maximizes it (or makes it
full screen) before showing it. Gio had no way to place a window, to read
where it is, or to list the displays: every Clack launch opened at
1440×900 wherever the OS put it.

- `Bounds(r)` places a window: r is its frame, the OS frame included, in
  the OS's screen coordinates, which `Display` and `Config.Bounds` use
  too. On Windows, physical pixels of the virtual screen (the primary
  monitor's top left at the origin), the frame `GetWindowRect`'s with the
  invisible resize borders; given with the first options, the window is
  created there (`CreateWindowEx`'s position and size, where Gio passed
  `CW_USEDEFAULT`), so it starts with the DPI of the monitor it opens on: a
  `SetWindowPos` onto a monitor of another DPI afterwards would rescale it
  through `WM_DPICHANGED` (Electron calls `SetPosition` twice for that,
  its "dirty workaround" for electron#10862). On macOS, points from the
  primary screen's top left with y down, as Electron's `NativeWindowMac`
  `GetBounds` and `SetBounds` flip an `NSWindow` frame against
  `NSScreen.screens[0]`; the size is raised to the window's minimum, as
  `SetBounds` does (`setFrame:display:` doesn't check it), and a placed
  window isn't cascaded. Like `Size`, the option sets the mode to
  `Windowed`, so a `Maximized` or `Fullscreen` option after it maximizes
  or fills the display the window was placed on, and a maximized window
  restores to r (Windows' restored rectangle; AppKit's user frame). Later,
  it moves and sizes a windowed window. On Windows a visible maximized or
  minimized window is restored first (`SW_SHOWNOACTIVATE`), since the
  `SW_SHOWNORMAL` after it would put the restored rectangle back over the
  new one (Chromium's `SetBoundsInternal` takes `WS_MAXIMIZE` off for the
  same reason); on macOS a full-screen window ignores it, as Electron's
  `SetBounds` does.
- `Config.Bounds` is the window's frame in a `ConfigEvent` (Windows:
  `update`, on every `WM_WINDOWPOSCHANGED`; macOS: each draw, and a move,
  which draws nothing, through the delegate's `windowDidMove:`), maximized
  and full screen included. It is empty while the window isn't on the
  screen: minimized (its place is the taskbar's or the Dock's: Windows'
  `GetWindowRect` of an iconic window is the icon's), hidden by
  `Window.Hide`, or started hidden. `Config.Display` is the display the
  window is on, Electron's `getDisplayMatching(getBounds())`: Windows'
  `MonitorFromWindow(MONITOR_DEFAULTTONEAREST)` (Chromium's ScreenWin
  matches with `MonitorFromRect` and the same flag), macOS's
  `NSWindow.screen`. Reading it there keeps a save off AppKit's main
  thread, which the client's event handling holds.
- `Displays()` is Electron's `screen.getAllDisplays()`: each display's
  bounds, its work area (without the taskbar and app bars, or the menu
  bar and the Dock), its scale (screen units per DIP: the effective DPI
  over 96 on Windows, 1 on macOS) and whether it is the primary. Windows
  enumerates the monitors (`EnumDisplayMonitors`, `GetMonitorInfo`,
  `GetDpiForMonitor`) on a thread made per-monitor DPI aware for the call,
  as Gio's window threads are, so the rectangles are physical; macOS reads
  `NSScreen.screens` (frames and visible frames, flipped as Chromium's
  `ScreenMac` flips them, the primary first) on the main thread, waiting
  for it, so not from inside the window's event handling. Elsewhere it is
  nil: X11 and Wayland windows go where the window manager or the
  compositor puts them, so `Bounds` does nothing there and `Config.Bounds`
  stays empty.
- Windows, `StartHidden` (§23) with `Maximized` or `Fullscreen`: the
  first `Configure` leaves out the `ShowWindow(SW_SHOWMAXIMIZED)` it would
  have made and keeps it (`window.hiddenShow`) for the `ActionRaise` that
  shows the window, which comes out maximized or full screen. Before, it
  showed restored: a hidden window isn't maximized until it is shown, and
  `ActionRaise` read `Windowed` from it. (Slack's `maximize()` shows the
  window, then hides it again.) An option that changes the mode while it is
  hidden shows it as before, and the kept show goes.
- macOS, the size limits: `Configure` set `contentMinSize` and
  `contentMaxSize` only for `Windowed`, so a window opened maximized (now
  that one can open where it was) had no minimum until a later windowed
  option; they apply in every mode. A first `Fullscreen` option runs
  before `newWindow` orders the window front, and goes full screen once it
  is on the screen (`toggleFullScreen:` on the next turn of the run loop).
- macOS, a closed full-screen window: §24's `windowWillClose:` lets go of
  the content view, and AppKit then takes the closing window out of full
  screen; `windowWillExitFullScreen:` read the handle of the view that was
  gone (0) and Gio panicked ("runtime/cgo: misuse of an invalid Handle"),
  so Quit from full screen crashed Clack. The delegate's methods that call
  into Go now go through `gioViewOf`, which is nil once the view is gone,
  and do nothing then. Found by `TestBoundsMac` and
  `TestMacWindowBoundsFullScreen`, which close full-screen windows.

Clack's main window uses all of it (`internal/ui/winbounds.go`, W40):
Slack's default bounds (`rV`), `validateBoundsForDisplay`, the minimum
size and the save rule over `Displays`, `Config.Bounds` and
`Config.Display`, kept in ui.json's `main_window_settings`; Linux, where
Gio lists no displays (and doesn't see a window manager's maximize on X11),
keeps the window's size in dp and nothing else. Read in Electron 45-x-y
(`shell/browser/native_window_mac.mm`, `shell/browser/native_window.cc`)
and Chromium main (`ui/display/win/screen_win.cc`,
`ui/display/mac/screen_mac.mm`, `ui/display/display_finder.cc`,
`ui/views/win/hwnd_message_handler.cc`).

Tests: on ci's windows-applog job (`CLACK_WINDOWS_BOUNDS_TESTS`)
`app/clack_bounds_windows_test.go`: `TestDisplaysWindows` (the runner's
monitor as `GetSystemMetrics` and `SPI_GETWORKAREA` give it, primary, at
the system DPI) and `TestBoundsWindows` (created at the bounds, which the
`ConfigEvent` reports with the monitor; a move from another thread; none
while minimized and back after `ActionRaise`; a later `Bounds` on a
windowed and on a maximized window; created maximized, restoring to the
bounds; created full screen; started hidden and maximized: no bounds, then
shown maximized by `ActionRaise`, restoring to the bounds). On ci's macos
job (`CLACK_MACOS_WINDOW_TESTS`) `app/clack_bounds_darwin_test.go`:
`TestDisplaysMac` (`NSScreen.screens`, flipped, the primary at the
origin) and `TestBoundsMac` (opened at the bounds in points with the
screen it's on; `setFrameTopLeftPoint:`'s move reported; a later `Bounds`;
the minimum; opened maximized: zoomed on the screen, `zoom:` back to the
bounds; opened full screen, last, since the runner never leaves it).
`internal/ui` `TestWindowBoundsWindow`, `TestWindowBoundsOffScreenWindow`
and `TestWindowBoundsFullScreenWindow` (Windows) and `TestMacWindowBounds`,
`TestMacWindowBoundsOffScreen` and `TestMacWindowBoundsFullScreen` (macOS)
are Clack's window on the same jobs. Not in `UPSTREAM.patch`.

## 26. app: an Invalidate always reaches the client

`app/window.go` (`Window.Invalidate`, `Window.nextEvent`,
`Window.handOutWakeup`, the `invalidateMissed` field). A fix: the client
gets an event after every `Invalidate`, as `Window.Event`'s documentation
says.

`Invalidate` asks the driver only while `mayInvalidate` is set, and
`nextEvent` sets it again once the client has had every event. An
`Invalidate` in between (the driver still delivering the wake-up an
earlier one asked for) did nothing: on a window that draws, the frame the
first one scheduled covers it; a hidden (§13) or minimized window draws
none, and when the client had already done its work for that wake-up the
second `Invalidate` was lost until the next event of any kind. Clack runs
its posted work on those wake-ups (`internal/ui` `drainIdle`), and a
notification's taskbar flash, posted from `notificationShown`'s goroutine
while the notification's own wake-up was still being handled, waited
there: windowFlashBehavior "always" minimizes on close, and the flash
came at the next unrelated event, or never.

Now an `Invalidate` that finds `mayInvalidate` unset sets
`invalidateMissed`; handing the client a `FrameEvent` or a wake-up clears
it (that event covers every `Invalidate` before it), and `nextEvent`,
where it would have set `mayInvalidate`, gives a wake-up instead while it
is set. No frame is scheduled for it: a window that draws already has the
first one's. A destroyed window (no driver) takes none.

Found by `internal/ui` `TestTaskbarWindow` on ci's windows-shell job
(2026-11-14: the first attempt of four runs failed "always: no
HSHELL_FLASH … after a notification", and the log has no
`flashMainWindowFrame` line). Tests: `app/clack_wakeup_test.go`
`TestInvalidateWhileHandlingAWakeup` (every platform, on ci's linux-gio
job: the second `Invalidate` while the first wake-up is out gets its own,
one made before the wake-up went out doesn't, nor does a destroyed
window) and `app/clack_hide_windows_test.go`
`TestInvalidateWhileHandlingWakesHidden` (windows-shell: a hidden and a
minimized window, an `Invalidate` from another goroutine while the app
handles a wake-up, which waits for it, then another wake-up). Not in
`UPSTREAM.patch`.

## 27. app: the Windows loop never waits while a frame is due

`app/os_windows.go` (`window.runLoop`). A fix.

While animating, the loop drew when `PeekMessage(PM_NOREMOVE)` found the
queue empty and otherwise called `GetMessage`, expecting it to return the
message the peek had seen. Right after a pointer up on a caption button
that §22 handles itself (the press and release never reach
`DefWindowProc`), the peek reported a message `GetMessage` never
returned: the window thread waited in `GetMessage` with a frame due
(`animating` set, the next frame now) and drew nothing until the next
unrelated input. In Clack that froze the window after a click on its
caption buttons until the pointer moved again. While animating the loop
now takes the message with `PeekMessage(PM_REMOVE)` (dispatching it, or
ending at `WM_QUIT`) and draws when there is none; it calls `GetMessage`
only when no frame is due.

Found by `app/clack_titlebar_windows_test.go`
`TestHiddenTitleBarButtonsInput` on ci's windows-applog job (2026-11-14,
first attempts: close never hovered, maximize's click with the Snap
Layouts flyout open never arriving and no frames after it, a minimize
click seconds late), reproduced 3 times in 60 rounds by flake-check's
`flake-fix-gio-sequence` (the OS file drops, typed text and title bar
steps in ci's order) with the test's stall dump: the window thread in
`runLoop`'s `GetMessage`, `animating` true, the next frame due, `WM_NULL`
answered at once, after the trace's `WM_NCPOINTERDOWN`/`WM_NCPOINTERUP`.
Not in `UPSTREAM.patch`.

## 28. io/input: the key presses a router was given, whoever took them

`io/input/clack_keylog.go` (new: `Source.KeyPresses`, `Router.KeyLogged`),
`io/input/router.go` (`Queue` keeps each press; `Frame` and `execute`
queue replayed events through `requeue`) and `app/window.go`
(`processEvent` draws a frame for a kept press no filter matched). An
addition: a router no Source calls `KeyPresses` on behaves as before.

Gio gives a key event to one `Event` call, the first whose filters
match. Clack's inspect tool turns on with the Konami code, ↑ ↑ ↓ ↓ ← → ← →
B A typed in the main window, and that happens with the composer focused
more often than not: the composer takes the arrows (its caret moves; ↑ in
an empty one edits your last message) and the letters. A filter read
before the composer's would take the keys from it; one read after never
sees them.

`Source.KeyPresses` returns the key presses (not the modifier keys' own)
the router was given since the last call or the last frame, whatever
took them, and forgets them: the last 32 at most. The router keeps
presses only while a Source calls it every frame. A press the router
queues again for a state replay (a command executed with events pending;
deferred events at the next frame) isn't kept twice. Each kept press asks
the window for a frame (`Router.KeyLogged`, read by
`Window.processEvent`) but doesn't count as handled: Gio still moves the
focus for an unhandled Tab, and macOS still gets an unhandled key's
`doCommandBySelector:`.

Tests: `io/input/clack_keylog_test.go` (`TestKeyPressesSeesTakenKeys`: a
focused editor's ↑ and an unmatched B once each, no Shift;
`TestKeyPressesUnmatchedStaysUnhandled`; `TestKeyPressesOnlyWhileRead`;
`TestKeyPressesReplayedOnce`: the immediate replay and the deferred one;
`TestKeyPressesSourcesAndBound`) on ci's linux-gio job, `internal/ui`
`TestInspectKonami` (the code typed into the composer, which still gets
every key) and, through AppKit, `TestMacInspectKeys` on the macos job.
Not in `UPSTREAM.patch`.

## 29. app: ⌘ presses AppKit's text system sends no command for (macOS)

`app/os_macos.go` (`gio_onKeys`, `cmdKeys.pending`). A fix.

A key press on macOS goes through `interpretKeyEvents:`, and Gio
delivered a press with ⌘ only from the `doCommandBySelector:` it expected
that call to make (`gio_onCommandBySelector`); `gio_onKeys` returned
without it. AppKit makes one for ⌘ alone or with ⇧ (`noop:` for ⌘B and
⇧⌘D, the key binding's `moveToBeginningOfDocument:` for ⌘↑), but none
once ⌥ is held too: on macos-latest (com.apple.keylayout.US) ⌥⌘B's,
⌥⇧⌘D's and ⌃⌥⇧⌘D's `keyDown:` reach the view, and `interpretKeyEvents:`
calls neither `doCommandBySelector:` nor `insertText:`. So no filter ever
saw a ⌥⌘ press. Karim found it with Clack's inspect tool, whose shortcut
was ⌥⇧⌘D: it did nothing (2026-11-14).

`gio_onKeys` now marks the press pending before `interpretKeyEvents:`,
and `gio_onCommandBySelector`, which clears `cmdKeys`, unmarks it. A
press with ⌘ still pending afterwards is delivered by `gio_onKeys`, once.
A command key without ⌘ (an arrow, Return, Tab) that gets no command
still isn't delivered: an input method took it.

Found by `internal/ui` `TestMacInspectKeys` on ci's macos job (run
43661396830: `cocoatest.TraceKeys` logged ⌥⇧⌘D's and ⌃⌥⇧⌘D's `keyDown:`
with nothing after them, and no session started, posted through `NSApp`
or straight to the view). Tests: `app/clack_keys_darwin_test.go`
`TestMacCommandKeys` (macos job, `CLACK_MACOS_WINDOW_TESTS`: ⌘B, ⇧⌘D,
⌥⇧⌘D, ⌃⌥⇧⌘D, ⌥⌘B, ⌘↑, ↑, ⌥⇧D and A posted through `NSApp` reach a filter
once each; the log says which came through `doCommandBySelector:`, run
43663715941 the list above) and `internal/ui` `TestMacInspectKeys`. Not
in `UPSTREAM.patch`.
## 30. app: no frame for a display link tick after the animation stopped (macOS)

`app/os_macos.go` (`newOSWindow`'s display link callback). A fix.

The display link's callback runs on CoreVideo's thread: it puts a token
in `redraw` and asks the main thread for `setNeedsDisplay`. `draw` takes
the token and then waits, on the main thread, for the client's frame;
the frame's `updateAnimation` stops the animation when nothing is due
now (a caret blink's next wake-up is 500 ms away), but the display link
only stops 500 ms later, and `running` is still set until then. So a tick
that came during a frame slower than a vsync (16.7 ms at 60 Hz, 8.3 ms at
120 Hz) found the token free, and its `setNeedsDisplay` ran on the main
queue after the frame returned: AppKit drew once more, a `FrameEvent`
nobody asked for, after every such frame. Clack idle with its composer
focused draws a frame per caret blink; on Karim's Mac (2026-11-14, 8 GB,
swapping) every frame was slower than a vsync, so every blink drew twice.
On the runner it showed as frames whose causes `idleprobe.Recorder`
couldn't name, about 20 ms after three blinks out of four.

The main thread's block now draws only while the window still animates
(`w.anim`, which only the main thread writes); otherwise it gives the
token back and draws nothing. An animation that goes on keeps
`w.anim` set, so its next frame comes as before, and a later
`Invalidate` starts the animation again before its tick.

Test: `internal/ui` `TestMacIdleCPU` on ci's macos job
(`CLACK_MACOS_WINDOW_TESTS`): its "frames slower than a vsync" period
makes every frame take 30 ms (`OnWindowEvent`) and allows the caret's
blinks and `idleOtherFrames` more. Not in `UPSTREAM.patch`.

## 31. op: a finished Ops list as one CallOp

`op/clack_whole.go` (`Ops.Whole`). An addition, not a fix: no upstream
API changes behaviour.

Clack's inspect tool (`internal/ui/inspect`) copies an image of the
element a click picked. It used to read the frame back at the window's
size and paste the whole window: every channel and message around the
element came along, and Slack shrank the 2880 px wide paste (a Retina
window) to fit a message, so the element came out soft. The tool now
draws only the element and a margin, at about 4 px per dp whatever the
display: the frame's ops once more, through a headless GPU context,
under a transform that scales the crop up and moves it to the origin.
Gio's paths and glyph outlines are vectors (`drawOps.buildVerts`
transforms them), so the scaled frame is as sharp as one laid out at
that density.

That needs the frame's ops as something another `Ops` can call, and
upstream only gives one for a `Record`ed macro: the window's frame isn't
recorded, and recording it would cross the push/pop of every stack the
layout opens and closes around the tool's own calls. `Whole` is a
`CallOp` from the start of the list to its current end. The ops reader
already reads a call into any `Ops` from any start: macro definitions in
it are skipped as usual, and deferred macros are copied into the
reader's deferred list and run after the caller's ops, with the state
(transform included) saved when they were deferred, so the outer
transform applies to them too.

Test: `internal/ui` `TestInspectShots` (feel-shots, llvmpipe) renders the
pick through it. Not in `UPSTREAM.patch`.

## 32. app: no frames while a window is occluded or the screens sleep, and none waits long for a drawable (macOS)

`app/metal_darwin.go` (`setupLayer`'s `allowsNextDrawableTimeout`,
`RenderTarget`, `Present`, `slowWaits`), `app/os.go` (`errNoRenderTarget`),
`app/window.go` (`validateAndProcess`), `app/os_macos.go` (`draw`,
`SetAnimating`, `gio_onOcclusionChanged`, `drawPaused`, `resume`,
`gio_onPowerEvent`, `setScreensAsleep`, `screensOn`, `gio_onScreensChanged`,
`gio_onKeys`, `gio_onMouse`), `app/os_macos.m` (`occlusionChanged`,
`setFullScreenTransition`, the delegate's occlusion and full-screen
methods, `GioWorkspaceObserver`, `gio_eachView`, `gio_main`),
`app/cocoatest/` (`Window.OcclusionVisible`, `Cover`, `PostWorkspace`,
`DrawableTimeout`, `HoldDrawables`) and `app/clack_wake_darwin_test.go`.
A fix.

Karim's clack.log (2026-11-16): "frame submit (GPU, main thread)" freezes
were the first lines after 330 s and 403 s of silence, next to the
realtime socket's reset ("Socket URL has expired"): his Mac had just woken.
While the screen slept the same stage reached 40.3 s and 6 min 40.8 s.
That stage is the UI goroutine in `FrameEvent.Frame`, which on macOS waits
for the main thread's `validateAndProcess`, and there for `RenderTarget`'s
`-[CAMetalLayer nextDrawable]`. Gio set `allowsNextDrawableTimeout` to NO,
so the call waited for as long as the compositor kept the layer's
drawables: a sleeping display keeps them, a waking one hands them back
late. And nothing stopped a window from drawing then: the app's
Invalidates and animations (the caret's blink) drew covered windows and
windows on sleeping screens, 60 frames a second for an animating one.

What Chromium does (Slack's desktop app is Electron):

- Its browser thread never waits for a drawable. The GPU process draws
  into IOSurfaces, which the browser sets as CALayer contents; the only
  `nextDrawable` in chromium/chromium (`components/metal_util/hdr_copier_layer.mm`,
  HDR content) keeps CAMetalLayer's one-second timeout (no Chromium file
  sets `allowsNextDrawableTimeout`) and presents as Gio does:
  `presentsWithTransaction`, `commit`, `waitUntilScheduled`, `present`.
  Dawn's Metal swap chain (`src/dawn/native/metal/SwapChainMTL.mm`) turns a
  nil drawable into a `Timeout` status the caller skips; ANGLE's
  `SurfaceMtl.mm` retries one with the timeout off ("this may wait forever
  here"), but Chromium on macOS doesn't present through ANGLE window
  surfaces.
- `WindowOcclusionCheckerMac`
  (`components/remote_cocoa/app_shim/window_occlusion_checker_mac.mm`)
  counts a window occluded while its `occlusionState` lacks
  `NSWindowOcclusionStateVisible`, and every window occluded between
  `NSWorkspaceScreensDidSleepNotification` and
  `NSWorkspaceScreensDidWakeNotification` (`_displaysAreAsleep`, on every
  macOS but 13.0-13.2). It ignores occlusion changes during a full-screen
  transition (crbug.com/1081229) and updates once it's over, on the next
  turn of the run loop. `WebContentsViewCocoa`
  (`content/app_shim_remote_cocoa/web_contents_view_cocoa.mm`) marks the
  page occluded a second later and visible at once, and an occluded page's
  renderer draws nothing. Electron's `windowDidChangeOcclusionState:`
  (`shell/browser/ui/cocoa/electron_ns_window_delegate.mm`) also calls
  `WasOccluded` and `WasShown` at once (`electron_api_browser_window.cc`
  `OnWindowHide`, `OnWindowShow`); Slack's main bundle sets no
  `backgroundThrottling`, so it applies.
- The computer's own sleep and wake come from IOKit
  (`base/power_monitor/power_monitor_device_source_mac.mm`,
  `IORegisterForSystemPower`), and Slack logs them ("PowerMonitor: System
  suspending", "System resuming" with the sleep's length).

Now:

- `allowsNextDrawableTimeout` is YES, CAMetalLayer's default: `nextDrawable`
  waits a second at most. A nil drawable is `errNoRenderTarget`, which
  `validateAndProcess` doesn't treat as a failure (that destroyed the
  window): the frame's ops still go to the router (the client laid them
  out, its handlers take the input), the window asks for the next frame
  (`setNextFrame` now: the display link's next tick), and the client
  continues. The first skipped frame of a run logs "gio: no Metal drawable
  within 1s … frame skipped, the window tries again", the next drawable
  "gio: Metal drawables again after N frames skipped in …". A
  `nextDrawable` or a present (its `waitUntilScheduled`) of 250 ms or more
  logs how long it held the main thread, at most a line a minute for each.
- A window draws nothing while it's occluded. The delegate reports every
  occlusion change (`gio_onOcclusionChanged`); a window whose last change
  lacked `NSWindowOcclusionStateVisible`, and whose `occlusionState` still
  lacks it, skips `draw` and keeps its display link stopped. Both are
  needed, so a visible window never waits for a notification, and a new
  window draws before AppKit has said anything. When AppKit says it's
  visible it draws a frame and its animation goes on (`resume`), as
  WebContentsViewCocoa shows a page at once. A full-screen transition
  counts as visible, and its end reads the state again on the run loop's
  next turn, as WindowOcclusionCheckerMac does. Unlike Chromium there is
  no second's delay before a window stops: a paused Gio window throws
  nothing away, and a frame for a covered window could wait the second.
- The screens' sleep stops every window's frames until their wake, which
  draws each window again (`gio_eachView`), as `_displaysAreAsleep` does.
  The computer's own wake (`NSWorkspaceDidWakeNotification`) doesn't end it:
  its screens may still be off. Unlike Chromium, a click, a scroll or a key
  press in a Gio window ends it too (the screens are on), so a
  ScreensDidWake that never comes can't freeze a window someone is using.
- Each of `NSWorkspaceWillSleepNotification`, `NSWorkspaceDidWakeNotification`
  (with the sleep's length), the screens' sleep and their wake (with its
  length), and input that ended the screens' sleep, logs one line. The
  lengths are wall clock: the monotonic clock stands still while the Mac
  sleeps.
- The app still gets its wake-ups while its windows draw nothing (an
  Invalidate's, as a hidden window's, §24), so posted work and
  notifications go on.

Where Clack still differs: Gio draws on the main thread, so a frame that
finds no drawable (a window AppKit hasn't yet reported occluded, a display
waking up) holds it for the layer's second, where Chromium's browser thread
never waits; and Clack doesn't do Chromium's own occlusion check of windows
of the same size (macOS can miss that one covers another).

Tests, on ci's macos job (`CLACK_MACOS_WINDOW_TESTS`; flake-check's
`mac-wake` job runs each `$COUNT` times), `app/clack_wake_darwin_test.go`:
`TestOccludedWindowPausesMac` (an opaque window over every screen,
`cocoatest.Cover`, makes AppKit report the test window occluded within
about 30 ms: an animating window draws nothing for 1.5 s of Invalidates
and still gets its wake-ups, and draws again at once when uncovered; an
idle one draws nothing for an Invalidate until it's visible, then a frame
at once), `TestNoDrawableSkipsFrameMac` (the layer's
`allowsNextDrawableTimeout` is YES; `cocoatest.HoldDrawables` has
`nextDrawable` answer as CAMetalLayer documents for a pool with none free,
a second's wait and nil with the timeout, a wait until the release
without it: frames are skipped and tried again about once a second with
the main thread running other work in between, none is presented, the
window isn't destroyed, and once the drawables come back a frame is
presented, with one log line for the run and one for its end; a test
can't make the compositor keep drawables on a desktop that shows them,
and holding them doesn't empty the pool: on the runner a layer gave 8
unpresented drawables, their textures taken, without waiting, flake-check
run 43822438092) and `TestScreensSleepWakeMac` (`cocoatest.PostWorkspace`: the
computer's sleep and wake each log a line; between the screens' sleep and
wake an animating window draws nothing, the computer's wake included, and
its frames come back with the wake; a click while the screens are asleep
brings them back too, and a late ScreensDidWake logs nothing more). Before
the fix the occlusion test drew 90 frames while covered and the sleep test
found no log line (flake-check run 43821390827), and the no-drawable test
found the main thread running nothing for over 2.5 s, no frame skipped or
tried again (run 43823092739). With it each passed 3 runs of 3 (run
43823305380): the main thread's longest wait 0.9 s, 4 frames skipped in
4 s, frames back about 75 ms after the window was visible or the screens
woke. Not in `UPSTREAM.patch`.

## 33. io/input: when a router was last given input

`io/input/clack_inputidle.go` (new: `SinceInput`) and `io/input/router.go`
(`Queue` notes each event). An addition: nothing upstream reads the
clock.

Clack holds work that would stall the window for a pause in the person's
input: cmd/clack's forced collections (`debug.FreeOSMemory`), whose
stop-the-world phases and scavenge of all free memory freeze whatever the
UI goroutine is doing. A pause has to mean no input anywhere, and Clack
code only ever sees the events its own handlers are routed: a focused
widget's keys (the message list's arrow navigation, Block Kit's and
plugins' text fields, list search, the link dialog), a drag another
widget grabbed (the grab drops every other handler, the root's
included), a popover drawn with `op.Defer` above the window's root
handler, and the huddle windows' input never reach a handler at the
root. Frames are no sign of input either: a focused editor's caret
redraws the window twice a second for as long as it's focused.

`Queue` notes every pointer event (not the router's or window's own
`Cancel`), key event and `EditEvent` it's given, before routing, in a
process-wide clock on the monotonic time (a wall-clock step moves
nothing). `SinceInput` is how long ago the last one was (how long the
process has run when there was none). Focus changes, snippets and
selections aren't input. Clack reads it through `internal/ui/inputidle`,
whose stockgio build (`.github/scripts/gio-layer-fix.sh`) has no clock.

Tests: `io/input/clack_inputidle_test.go` (`TestSinceInput`: a cancel and
a focus change don't count; a key, an edit and a scroll do) on ci's
linux-gio job, `internal/ui` `TestInputIdleSeesFocusedWidgetKeys`,
`TestInputIdleSeesScroll` and `TestInputIdleSeesGrabbedDrag`, and
`cmd/clack` `TestWaitQuietOnInput`. Not in `UPSTREAM.patch`.

## 34. io/input: where the key focus is

`io/input/clack_focus.go` (new: `Source.FocusTag`, `Source.CanFocus`,
`Source.AskedFocus`, `Source.Within`). An addition: nothing upstream
behaves differently.

A Source answers only "is this tag focused?". Clack's overlay layers
(`internal/ui/slackc` layers.go: kit's Popover, Slack's modals, plugin
dialogs, the shell's menus) work the way the DOM does a modal. Escape
goes first to the focused element, then to the innermost layer whose
content holds the focus, then to the topmost one. A layer records the
element that had the focus when it opened (document.activeElement), and
gives it back when it closes if that element is still there and
focusable. That needs these answers about the last frame, and one about
the frame in progress:

- `FocusTag`: the focused tag, or nil.
- `CanFocus(tag)`: whether a `key.FocusCmd` for tag would keep the
  focus past the frame. In the last frame the tag was drawn (an
  `event.Op`) and asked for focus events (`key.FocusFilter`), which is
  what `keyQueue.Frame` keeps the focus on.
- `AskedFocus(tag)`: whether tag asked for focus events in the frame in
  progress (since the last `Frame`). A layer gives the focus back at the
  end of the frame in which it stopped drawing, before that frame's
  `Frame`; there `CanFocus` still passes a tag that went in this frame,
  and a widget drawn now has asked.
- `Within(tag, container)`: whether tag's `event.Op` was in container's
  clip area or one nested in it (the pointer queue's area tree). Deferred
  macros run from the root area, so a container whose content is
  deferred draws its own op inside the macro.

All four read the router's state and nothing else. A disabled Source
answers nil and false.

Test: `io/input/clack_focus_test.go` (`TestFocusTagCanFocusWithin`) on
ci's linux-gio job. Stock Gio builds of `internal/ui/slackc` (the
gio-layer-fix job's richview) use `gio_patch34_stock.go`. With no
answers there, a layer neither records an opener nor sees the focus
inside it. Not in `UPSTREAM.patch`.

## 35. op/paint, gpu, app, io/input: live images, what a frame was asked for, and frames drawn again

`op/paint/clack_live.go` (new: `LiveImage`, `NewLiveImageOp`),
`gpu/gpu.go` (`resolveLive`, called by `texHandle`), `app/app.go`
(`FrameEvent.Asked`, `FrameEvent.AskedAt`), `app/window.go` (`askFrame`,
the `asked` and `askedAt` fields, `processEvent`'s `frameEvent`),
`io/input/clack_replay.go` (new: `Source.Replay`) and `io/input/router.go`
(`Frame`). Additions: nothing upstream behaves differently unless asked.

Gio draws the whole window every frame, and Clack's layout of it takes
17–23 ms on Karim's Mac with his plugins (2026-11-16). An animated custom
emoji (`:explodes:`, 13 frames of 30–70 ms) asked for a frame every
frame of its own, so every one of them cost the whole layout: the
animation played slowly and the window lagged while it was on screen.
Chromium repaints only the image's layer. Clack now shows the last frame's
ops again when an animation is all that asked for the frame
(`internal/ui` quiet.go), which needs three things from Gio:

- A live image: `NewLiveImageOp(l)` is an `ImageOp` whose pixels are
  `l.Image()` as it is when the GPU draws the frame, not when the ops
  were written. Its handle is a `liveHandle`; `texHandle` asks it for the
  image to draw (`resolveLive`) and keys the texture by that image's own
  handle, so each of an animation's frames is its own texture, uploaded
  when shown. The ask happens in `uploadImages`, after `collect` dropped
  the paints outside the viewport and their clips: only the paints a frame
  shows ask, once each. The image's size must stay the size the live op
  was made with (the paint's bounds and texture coordinates come from it).
  `internal/imgcache` (`SetLiveOps`) hands out its playing animations this
  way: its `LiveImage` is the frame due at the GPU's ask, which also
  counts as a draw for the player (it stays on screen, its next frame is
  asked for).
- What a frame was asked for: `FrameEvent.Asked` reports whether the
  window's own frames asked for this one or a later one since the last
  FrameEvent, and `FrameEvent.AskedAt` the earliest such time (the zero
  time: at once). Every `setNextFrame` but one asks (`askFrame`, recorded
  in `asked` and `askedAt`): `updateState`'s wake-up from the router
  (`WakeupTime` after a frame: an `op.InvalidateCmd`, events pending),
  input or a focus change a handler took, a key press the key log kept
  (patch 28), `Option`, `ClickFocus`, and a frame that found nothing to
  draw into (patch 32). The one that doesn't is `callbacks.Invalidate`,
  where every `Window.Invalidate` comes back to the window from its
  driver (the macOS and Windows event loops' `Wakeup`, X11's and
  Wayland's dispatch): it sets `nextFrame` so the driver draws a frame,
  and asks nothing, so a frame only Invalidates brought has `Asked`
  false, and a client that knows why it invalidated can show its last
  frame's ops again. The first version of this patch read `nextFrame`
  itself, which every Invalidate sets: every frame said the window had
  asked for it, and no real window ever drew a quiet frame (only the
  tests that modelled one did). The frame takes the asks with it (they're
  cleared at every `frameEvent`, as `hasNextFrame` is), so a client that
  shows its last ops again executes `op.InvalidateCmd{At: AskedAt}` to
  keep a wake-up that isn't due yet; one that lays out has its widgets
  ask again. A wake-up's own timer brings its frame through `Invalidate`,
  and that frame is asked for at its time. A frame that replaces one the
  client wasn't handed yet (coalesced) keeps what that one was asked for.
- Frames drawn again: the router builds each handler's event filters
  from the `Event` calls of the frame's layout, and a frame that shows
  the last frame's ops again makes none, so its `Frame` swapped in empty
  filters. The focused editor then lost the focus (`keyQueue.Frame`
  drops a focus whose tag asked for no focus events) and got it back from
  Clack's KeepFocus at the next laid-out frame, a blur and a focus for the
  plugins each time (Taut's patched MessagePaneInput rendered again on
  every one: textyHasFocus false, true), and an event right after one had
  no filter to be routed by. The key log (patch 28) stopped too: it logs
  while a Source reads `KeyPresses` every frame, and a replayed frame
  reads none, so the press after one was lost (the inspect tool's Konami
  code couldn't be typed with an animation on screen). `Source.Replay()`
  says the frame in progress is the last one's ops again: its `Frame`
  keeps every handler's filters, the key filter and the key log as the
  last laid-out frame left them.

Tests, on ci's linux-gio job: `op/paint/clack_live_test.go`
(`TestLiveImageOp`); `gpu/clack_live_test.go` (`TestDrawOpsLiveImage`:
the renderer's own `uploadImages`/`texHandle` against a device that
records the pixels each upload sends, the same ops uploading the live
image's image at each frame, nothing when it's unchanged, and a paint
outside the viewport never asking); `app/clack_asked_test.go`
(`TestFrameEventAsked`: `Window.Invalidate` and `callbacks.Invalidate` as
the drivers deliver them ask nothing, a router wake-up, a handled press
and an `Option` do, an ask outlives an Invalidate but not its frame);
`io/input/clack_replay_test.go` (`TestReplayKeepsFilters`,
`TestReplayKeepsKeyLog`: a replayed frame keeps the focus, routes the
next press and keeps logging keys; the same frames without Replay lose
them); and under Xvfb, `app/clack_asked_x11_test.go`
(`TestFrameEventAskedX11`, `CLACK_X11_ASKED_TESTS`: on a real X11 window
an Invalidate's frames have `Asked` false, an `op.InvalidateCmd`'s next
frame, every frame before a later wake-up and that wake-up's own frame are
asked for, and so is the frame a real press (XTEST, python-xlib) brings)
and `gpu/headless/clack_live_test.go` (`TestLiveImageFrames`: a real GPU
context draws the same ops twice and shows each image). The client side
on App.Run's real window: `internal/ui` `TestRealWindowQuietFrames`
(linux-idle under Xvfb, macos on Metal, windows-idle on Direct3D).
flake-check's emoji-anim job runs them all 20 times. Not in
`UPSTREAM.patch`.

## 36. gpu: what a frame costs (frame stats)

`gpu/clack_stats.go` (new: `FrameStats`, the `ClackStats` interface),
`gpu/gpu.go` (counts in `frame`, `stencilClips`, `intersect`,
`drawLayers`, `drawOps`; `buildPaths` keeps quad bounds when asked),
`gpu/caches.go` (`opCacheValue.quads`),
`gpu/internal/driver/clack_calls.go` (new: `CallCounter`, `Calls`), the
Metal, Direct3D 11 and OpenGL backends (`ClackCalls`), `internal/gl`
(`Functions.Calls`, counted in every `gl_unix.go` call) and
`gpu/headless/clack_stats.go` (new: `Window.ClackFrameStats`,
`ClackStatsDetail`, `ClackSync`). Additions only.

Karim saw Clack at 10% GPU in Windows Task Manager with nothing moving
(2026-11-16), and his Mac's traces had ~11,400 Metal calls a frame.
`FrameStats` says what the renderer did for the last frame: the ops by
kind (blits, covers, intersections), the paths stenciled, draw calls,
render passes, the calls into the graphics API and the ones the backend's
state cache left out (patch 37), the paths whose vertices the frame
built and uploaded (`PathsBuilt`, `PathBytes`: the path cache's misses,
each a vertex buffer of its own; `buildPaths` returns them), and the
pixels: stencil fragments (from
each path's quads in its stencil region; `ClackStatsDetail` keeps the
quads' bounds on the CPU for it), covered, blitted, intersected, cleared,
the atlases' and the layer pages' size and the layer pages a frame drew
into (`LayerPx`, `LayerPages`). `ClackSync` reads one pixel back, so a
timed frame includes its rasterization. `ClackStateCaches` turns patch
37's caches and batching off and on, for tests.

Used by `gpu/headless`'s `TestClack*` (`clack_identity_test.go`: pixel
checks that fail; `clack_cost_test.go`'s `TestClackPlacementNoise`, a
probe that logs), `internal/ui/theme/framecost_test.go`
(`TestFrameCostPixels`), both in ci's linux-gio (llvmpipe), macos
(Metal) and windows-applog (Direct3D 11, WARP) jobs,
`internal/ui/framecost_test.go` (`TestFrameCostWindow`, flake-check's
gpu-cost job on all three), and the perf workflow's plugins job
(`perf_gpustats_test.go`: the gpu passes' draw and API calls). Not in
`UPSTREAM.patch`.

## 37. gpu/internal: Metal, Direct3D 11 and OpenGL leave out state the API already has

`gpu/internal/metal/metal_darwin.go`, `gpu/internal/d3d11/d3d11_windows.go`,
`gpu/internal/opengl/opengl.go`, `gpu/internal/driver/clack_calls.go`
(`ClackStateCache`). No change to what's drawn, and that is checked:
with `driver.ClackStateCache` false (`gpu.ClackStateCaches(false)`, which
also turns Metal's batching off) every backend makes every call Gio made,
the same draws in the same order, and `TestClackStateCacheIdentical` and
`TestFrameCostPixels` require the same pixels either way, on llvmpipe,
Metal and WARP in ci. The state is kept either way, so the switch can
change between any two frames.

Gio's renderer binds the pipeline, the quad's vertices, the textures, the
uniforms and the viewport again for every op. OpenGL's backend had a
state cache for most of it; Metal's and Direct3D's had none:

- Metal: one cgo call per command, each under its own autorelease pool.
  The backend now keeps the open render encoder's state (`encState`):
  the pipeline, the attribute buffer (a Metal buffer at an offset, or a
  Go buffer's bytes as of its upload, `Buffer.gen`), the bytes each
  stage was last sent at index 0, the fragment textures and samplers per
  unit and the viewport, and leaves out a bind of what the encoder has. A
  new encoder starts with nothing. `BindUniforms` sends a stage only the
  uniforms it reads: shaders reading none (the stencil and intersection
  fragment shaders, the texture materials') get no `setFragmentBytes`,
  and a stage whose bytes in its range (`Shader.uniformRange`, from the
  shader's reflection) are the ones it was last sent, at least as many
  as now, isn't sent them again (text runs of one colour in a row; Metal
  validation checks the bound length against the shader's whole argument
  struct, which can reach past the last member it reads). Sampler states are one per
  filter for all textures (`Backend.sampler`), so switching textures
  keeps the sampler bound; `Texture.Release` leaves it to the backend.
  A buffer bound again at another offset only gets
  `setVertexBufferOffset`. And the render commands left go into a Go
  buffer (`Backend.rec`: an opcode and 8-byte words, bytes copied in)
  that `renderEncReplay` runs in one cgo call under one autorelease pool
  when the pass ends (or at 1 MB): the same Metal calls in the same
  order, one cgo crossing a pass instead of one a call.
  `driver.ClackBatchCommands = false` calls them one at a time, for
  tests that compare.
- Direct3D 11: `prepareDraw` set the vertex and pixel shaders, input
  layout, blend state, vertex buffer and topology for every draw, and
  `BindUniforms`, `BindTexture` and `BindIndexBuffer` set theirs every
  time, each a COM call through syscall. `ctxState` keeps what the
  context was last set to; a set of the same is left out. The pixel
  shader's constant buffer is bound only for shaders that read it, and a
  constant buffer uploaded the bytes it has (`Buffer.last`) isn't
  updated. `BeginFrame` forgets everything (the swap chain code sets
  state too); `BeginRenderPass`, `BindImageTexture` and
  `BindStorageBuffer` forget the bound shader resources (Direct3D unbinds
  a resource from them when it's bound for output).
- OpenGL: `vertexAttribPointer` was called for every attribute of every
  draw, and every uniform was set before every draw. An attribute
  already pointing at the same buffer, offset and format is left alone
  (`glState.deleteBuffer` forgets attributes into a deleted buffer, whose
  name can come back), and a program's uniform set to the bits it has
  (`uniformLocation.last`) isn't set again. The skip trusts the buffer an
  attribute points into, so `queryState` (a shared context, a context the
  GPU is built again on) reads it from `VERTEX_ATTRIB_ARRAY_BUFFER_BINDING`
  (`queryVertAttrib`); Gio read `VERTEX_ATTRIB_ARRAY_ENABLED` there, so an
  enabled attribute seemed to point into buffer 1, the name the next
  `glGenBuffers` hands out, and its first bind would have been left out
  (`TestQueryVertAttribBinding`).

Measured on flake-check's gpu-cost job with Clack's whole window
(`TestFrameCostWindow`) and the scenes of `TestFrameCostPixels`, the
calls these leave out are `FrameStats.Skipped`, and `Crossings` the
calls from Go into C or the system they took: on a light window at 1x,
Metal 10,506 calls before, 6,056 after the state cache alone; Direct3D
11 (WARP) 22,929 and 5,559; OpenGL (llvmpipe) 15,157 and 10,154. Not in
`UPSTREAM.patch` yet (a candidate).

## 38. op/clip, gpu: a rounded clip is left off what's drawn inside its interior

`op/clip/shapes.go` (`RRect.Interior`, set on the `PathSpec`),
`op/clip/clip.go` (`PathSpec.interior`; `Op.add` writes it),
`internal/ops/ops.go` (new op `TypeClipInterior`, `DecodeClipInterior`)
and `gpu/gpu.go` (`pathOp.interior`, `pathOp.covers`, used by
`expandPathOp`, `packIntersections` and `intersectPath`;
`ClackLegacyClips`).

Clack's primary panel is a `clip.RRect` around everything in it, so every
text run, rounded image and icon inside was clipped by two paths: an
intersection, with its own pass over both paths' coverage into another
atlas (about 11 more GPU calls an op on Metal) and the panel's path
stenciled over the whole panel every frame. Gio's coverage is exactly 1
where a rectangle's straight edges are at least a pixel away: the
`TypeClipInterior` op before an outline's `TypeClip` gives that
rectangle (`RRect.Interior`: the columns between the corners' arcs, a
pixel in from each edge; under an arc the coverage is a sum of slivers
rounded to half floats). The renderer drops such a clip from the clip
stack of every op whose bounds lie in its interior, under an integer
offset only: the op is drawn as if the clip weren't there (same pixels:
the coverage it multiplied by is 1.0), and the clip isn't stenciled for
it. Strokes get no interior. The router ignores the new op. Patch 42
splits a big colour op along the interior, so its inside is drawn this
way.

`ClackLegacyClips(true)` turns this and patches 39 and 42 off, for tests
that compare the two ways (`ClackLegacyParts` each on its own). With every path stenciled at the same place, an op
clipped through the RRect or with the clip dropped inside its interior
is the same picture on all three backends (`TestClackPlacementNoise`,
2026-11-16, before patch 39 moved the clip's regions). Since 39 the
glyphs inside land elsewhere in the atlas, and their own coverage moves
by a rounding as it does when one more path is drawn first. Not in
`UPSTREAM.patch`.

## 39. gpu: a clip path is stenciled in regions near the ops it clips

`gpu/gpu.go` (`pathOp.more`, `pathRegion`, `addRegion`, `region`;
`expandPathOp`, `packStencils`, `stencilClips`, `packIntersections`,
`intersectPath`, `prepareIntersections`).

Gio stenciled a path once over the box around all the ops it clips. A
pixel's coverage only adds up the curves above it, so the same path can
as well be stenciled in several regions: a panel clipping a hovered row,
a divider, a scroll bar along its right edge and its corners isn't
stenciled over the whole panel. `addRegion` puts an op's rectangle in the
region it grows the least, unless that adds more than `regionSlack`
(32K) pixels no op needs; then it gets a region of its own, up to
`maxPathRegions` (8). Each region is packed and stenciled on its own,
from the same vertices; an op reads the coverage from a region holding
it. A path with one op, or ops close together, has one region as before.
Rect clips have none (they never had a use for theirs).

What can change: a path's coverage depends by a rounding on where its
stencil lands in the atlas (the GPU interpolates from the atlas origin),
so pixels on a region's edges can come out a level or two apart, as any
change earlier in a frame already does. `TestFrameCostPixels` measures
that floor in each scene and holds the new way to it. Not in
`UPSTREAM.patch`.

## 40. gpu: atlas pages shrink, idle ones go after a while, and layers of a depth share a page

`gpu/path.go` (`FBO.idle`, `FBO.unused`, `fboSet.resize`) and
`gpu/gpu.go` (`packLayers`, `ClackLegacyLayers`; `stencilClips`,
`intersect` and `drawLayers` size their set before returning early).

`fboSet.resize` meant to make a page's texture again when it wasted too
much, but its test (needed over current size) is at most 1 whenever the
page fits, so a coverage page kept its biggest size for good, and every
frame cleared and stored all of it (on llvmpipe a memset of the whole R16
texture; on Apple GPUs a store of every tile). A texture more than twice
the size its page needs for `fboIdleFrames` (60) frames in a row is made
again to size, and one the frame doesn't need at all goes after as many
frames instead of at once (a page count going down and up, as an opacity
layer fades in, made and released textures each time). `packLayers`
meant layers of one depth to share a page but never updated its depth,
so every nested opacity layer got a page, an FBO and a render pass of
its own. A set is sized, and so aged, in every frame, also one that needs
none of it (a frame with no opacity layer, intersection or path: the
last fade's window-sized layer page stayed for good). A page counts its
oversized frames and its unused ones, each rule waiting its 60 frames; since
patch 44 a frame that doesn't use the page counts toward both. `ClackLegacyLayers(true)` gives every nested layer a page again,
for tests: `TestClackLayerPages` draws the window's nested layers (a
fade holding three siblings, one with a layer of its own, over a rounded
clip) both ways and requires the same pixels in fewer pages;
`TestClackAtlasShrinks` checks a page keeps its size for 59 small frames,
is made to size on the 60th, grows back at once, and that a coverage
page no frame needs and a layer page no frame draws into go after 60
frames and not before. Not in `UPSTREAM.patch` yet (candidates).

## 41. app: the macOS display link and layer aren't set again for nothing

`app/os_darwin.go` (`displayLink.Start`), `app/metal_macos.go`
(`resizeDrawable`) and `app/os_macos.m` (`displayLayer:`).

A drawing window restarts its animation every frame (`draw` calls
`SetAnimating`), and `displayLink.Start` sent on an unbuffered channel to
the link's goroutine each time, which had to be scheduled to take it.
Start now returns at once when the last state the main thread sent was a
start (`displayLink.asked`; Start and Stop come from the main thread, and
the goroutine handles states in order). A first version read the
goroutine's `running` instead, which drops to 0 only after Stop has
returned: a Start right after a Stop was skipped and the window stopped
drawing (ci's macOS key tests, 2026-11-17). `resizeDrawable` set the layer's `drawableSize` every frame and
`displayLayer:` its `contentsScale`; each is set only when it changes.
The window's geometry is still read every frame (`updateWindowMode`):
caching it would have to catch every resize, move, screen and scale
change. Not in `UPSTREAM.patch`.

## 42. op/clip, gpu: a stroked rounded rectangle has a hole, and a big colour op is drawn in whole-pixel parts around it or a rounded clip's interior

`op/clip/shapes.go` (`RRect.StrokeHole`; `RRect.Path` records the
rectangle on the `PathSpec`), `op/clip/clip.go` (`PathSpec.rrect`;
`Op.add` writes a stroke's hole), `internal/ops/ops.go` (new op
`TypeClipHole`, `DecodeClipHole`) and `gpu/gpu.go` (`pathOp.hole`;
`splitOp`, `around`; `ClackLegacyParts`).

The workspace's shadow is concentric strokes of rounded rectangles the
size of the window, 8 at 1x and 16 at 2x in light mode, and its outline
one in dark mode. Gio stenciled a stroke over its whole box (each curve's
quad reaches down to the bottom of its contour) and covered the box
again. A stroke of width w covers nothing more than w/2 and a pixel in
from the straight sides, between the corners' arcs: `RRect.StrokeHole`,
written as `TypeClipHole` before the stroke's `TypeClip` (an RRect's
stroke only). Under an integer offset, the renderer draws a uniform
colour op through such a stroke in the parts of its bounds around the
hole (`around`: the rows above and below the width of the op, the
columns beside the hole between them), leaving the hole out of the
stencil and the cover; each part is an op of its own, and patch 39 gives
the parts' stencil regions. The same split draws a big colour op in a
rounded clip, as Clack's panel fill, in the edges around the clip's
interior, through the clip, and the interior, which patch 38 then draws
without it. A split must leave out or unclip `splitMin` (32K) pixels.

The parts are whole pixels in window space and tile the op, so each
pixel is drawn once with the coverage it had. Clack first split them in
the theme, as abutting clips; under any transform but an integer offset
(an inspect shot's scale, an animation) Gio makes each clip rectangle an
antialiased path, and two parts meeting at a fractional edge blended the
same colour twice, up to 25% of the background showing through the
panel and notches in the dark outline. Only at an integer offset does a
path have a hole or an interior, so under a transform every op is drawn
whole, as Gio did: `TestClackSplitsUnderTransform` (gpu/headless) and
`TestFrameCostPixels`' transformed scenes (internal/ui/theme) require the
old way's pixels there. Not in `UPSTREAM.patch`.

## 43. gpu: a path's sub-pixel offset is part of its cache key

`gpu/gpu.go` (`opKey.ox`, `opKey.oy`; `opKey.SetTransform`).

`transformOffset` takes an op's whole-pixel offset out of its transform
and leaves the fraction, which `buildVerts` builds into the path's
vertices, but the path cache's key kept only the linear part. A path
cached at the origin (Clack's theme.RRect, Circle, the rounded image
clip, borders, rings and icons) drawn at a new fraction of a pixel was
then drawn from the vertices of the first fraction it was built at: the
intro's sliding cards and the login card's shake moved in whole pixels
with their rounded rects off their clips by up to a pixel, and inspect
shots drew every same-sized avatar clip at the first one's fraction. The
fraction is in the key now: whole-pixel offsets still share one entry,
and a path at another fraction is built there (`TestCollectSubPixelKey`,
`TestClackSubPixelCache`). Not in `UPSTREAM.patch` yet (a candidate).

## 44. gpu: the path cache frees a slot without dropping another slot's entry

`gpu/caches.go` (`opCache.frame`).

`frame` frees every slot no frame used, and freed slots kept their key;
`put` hands freed slots out again. A key that left the screen for a frame
and came back into another slot then had its live index entry deleted by
its old slot at every frame's end, so `get` missed and the path was
tessellated, uploaded and released again every frame it stayed on screen
(open and close a menu, then scroll: the hover toolbar's icons, pills,
borders, avatar clips and text runs that come back). That gave back the
saving of patch 36's origin caching wherever paths come and go. `frame`
now deletes an entry only while it names the slot, and clears the slot
(`TestClackOpCacheReturningPathHits`). Upstream Gio v0.10.3's own bug: a
candidate for `UPSTREAM.patch`.

Patch 40's page aging, with it (`fboSet.resize`):
- a frame that doesn't use a page counts toward the page's oversized
  frames, so a page whose set only some frames use shrinks too
  (`TestClackAtlasShrinksUsedNowAndThen`); patch 40 reset the count in such
  frames, and a page used every other frame kept its biggest size for good;
- a grow keeps the dimension that still fits. Upstream makes the texture
  just the new need (padded 5%), so a page whose needs alternate between
  wide and tall was made again on every frame that used it, and the shrink
  above let pages used now and then fall into that too
  (`TestClackAtlasGrowKeepsSize`).

## 45. layout: a ScrollToEnd list can lay out short content from its start

`layout/list.go` (`List.StartAligned`, `List.layout`). A feature.

A `ScrollToEnd` list draws content shorter than itself at its end (the
end alignment in `layout`). Clack's thread pane is such a list: it keeps
to the newest reply as replies come in, but Slack top-aligns a short
thread under the pane's header, its reply box right under the last
reply. The pane made the list exactly as tall as the thread to get that,
from the height the last frame measured, so the frame in which a thread's
replies landed (or it grew, or shrank) drew it at the bottom of the pane
or cut, and the reply box was placed from the same stale height (dogfood,
α.104: the box missing under a thread that had just opened).

`StartAligned` leaves out the end alignment: content that doesn't fill the
list starts at its start, the position stays at the start (`Offset` 0,
`OffsetLast` the space after it), and the list still counts as at its end
(`BeforeEnd` false), so content that outgrows it is drawn from its end
on, as a `ScrollToEnd` list does. Clack's thread list sets it and lays
out in the height the reply box leaves; the ui root clips it to the
content it measured and puts the box right under it in the same frame
(`internal/ui` `layoutConvo`).

Tests: `layout/clack_list_test.go` `TestListStartAligned` (on ci's
linux-gio job: three items in a 100px list start at 0 and stay there at
the list's end, fifteen end at the bottom, and without the field three are
end aligned) and `internal/ui` `TestThreadReplyBoxWithFirstReplies`. Not
in `UPSTREAM.patch`.

## 46. gpu, app: partial frames (a frame drawn again only where it changed)

`gpu/clack_partial.go` (new: `ClackPartialFrames`, `ClackPartialTextures`,
`ClackFrameTexture`, `ClackPartialWhole`, `ClackPartialReset`, `sigOps`,
`damage`, the diff, the frame texture), `gpu/clack_sticky.go` (new: the
coverage atlas's sticky places), `gpu/gpu.go` (`frame`: everything the
passes draw from is decided before them; `packStencils`, `stencilClips`,
`intersect`, `drawLayers`, `drawOps` and `buildPaths` take the damage;
`pathOp.hash`, `material.texID`, `texture.id`; `collectOps` hashes each
clip path's geometry, and a path's tessellation waits for its upload:
`pathOp.raw`; `renderer.drawCopy`), `gpu/caches.go` (`opCacheValue.hash`,
`textureCache.nextID`), `gpu/pack.go` (`packer.work`), `gpu/clack_stats.go`
(`FrameStats.Partial`, `DamageRects`, `DamagePx`, `CopyPx`, `PackWork`,
`StickySlots`, `StickyHoles`, `PathsTessellated`),
`gpu/internal/driver/clack_partial.go` (new: `ClackPartial`), the OpenGL,
Direct3D 11 and Metal backends (`ClackScissor`, `ClackCopyToTarget`,
`ClackCopyDraws`, `ClackPartialOK`, `TextureFormatOutput` textures),
`internal/gl` (`SCISSOR_TEST`, `ClackBlitFramebufferLoaded`),
`internal/d3d11` (`RSSetScissorRects`, `RenderTargetView.GetResource`,
`Texture2D.GetDesc`, `RECT`), `app/clack_gpu.go` (`SetGPUFrameHook`,
`ClackGPUIdle`), `app/window.go`, and `app/os_windows.go`,
`app/os_macos.go`, `app/os_x11.go` (a hidden, minimized, occluded or
unmapped window lets the frame texture go).

Gio drew every frame whole. A caret's blink, a spinner's turn or an
animated emoji's next frame cost the stencil, cover and API calls of a
frame where everything changed: on llvmpipe, Clack's focused composer
cost 55% of a core for its blinks alone (α.105, 1440×900). Chromium
repaints only the damaged rectangle and composites cached layers. Now a
window's frame is compared with the last, op by op, and drawn again only
where they differ:

- Every op the frame draws gets a signature (`sigOps`): a 128-bit hash
  of everything that decides its pixels (material, colours, gradient and
  texture transforms, the texture's image, each clip path it's drawn
  through: the path's geometry as its vertices are built from it, its
  offset, its stencil region and place in the atlas and the page's size,
  an intersection's place, the opacity layers around it and their
  places), and its clip, the only pixels its quad touches. An opacity
  layer's composite is an entry of its own over the layer's bounds. A
  texture's image is named by an id the cache hands out, never by its
  address (a texture made again can come back at the same one).
- The last frame's signatures and this one's are diffed (Myers, past
  their common start and end, at most 128 edits). The ops one has and
  the other doesn't are the damage: everywhere else the same ops cover
  each pixel in the same order with the same inputs, so the pixel is
  what the last frame left.
- The damage, merged into at most 8 disjoint rectangles, is drawn again
  with every pass exactly as a whole frame draws it (the same atlas
  places, viewports and quads), each draw scissored to the damage. A
  scissor discards fragments and changes nothing in the ones it keeps,
  so the result is the whole frame's pixels, bit for bit. Only the atlas
  and layer texels a damaged pixel reads are cleared (a fill with blending
  off: 0 coverage, 1 for an intersection, transparent for a layer, exact
  in every format) and drawn; the pages are loaded, not cleared, and only
  the paths the damage stencils are built.
- Sticky places (`clack_sticky.go`): a path's coverage depends by a
  rounding on where its region lands in the atlas (§39), and the packer
  placed every region after the ones before it, so a path added or
  dropped (the typing indicator's text, a toolbar's icons) moved every
  later one and the frame was all damage. With partial frames the last
  frame's packing sequence is replayed (the packer places the same sizes
  in the same order the same way): a region that was there (the same
  geometry, region and offset) takes its own slot; a new region takes a
  slot nobody took of exactly its size, else the smallest that holds it,
  keeping the slot's size, so no other place moves; the regions no slot
  holds come after, and a slot nobody takes is a hole. A path that
  changes every frame (a working agent's spinner every 33 ms, the
  LoadingButton's every 16 ms, a huddle's speaking bars) so takes back the
  slots its last frames left: before, each frame left its regions' old
  slots as holes the replay packed ever after (the sequence grew from 702
  slots to 4,302 in 1,800 spinner frames, the packer's work 55 times over,
  the page wider, until the holes outgrew the regions minutes later).
  Everything is packed afresh in the frame's order (a frame drawn whole)
  when the holes outnumber the live slots or 256; when the slots, holes
  and the slack of bigger slots included, cover more than 1.5 times the
  regions' own area (after a lower scale or a smaller window every region
  fits an old, bigger slot, and the page stayed up to 4 times Gio's); or
  when the sequence needs more pages than the last fresh packing did
  (comparing with one page instead repacked 61-92% of a spinner's frames
  once the atlas needed two, at 4K). A frame with more paths of new
  geometry than a partial frame can add (a scroll's, a live resize's, a
  channel switch's) is whole however it's packed, so it's packed afresh
  at once, with no holes matched and no sequence replayed (a scroll had
  every moved region scan every hole: 2.2-2.6 times Gio's packing in the
  scan alone). `FrameStats.PackWork` counts the holes looked through, and
  `StickyRepacks` the fresh packings. A page's regions aren't together in
  the ops' order, so the stencil pass goes page by page.
- A window's frame draws into the frame texture (`TextureFormatOutput`,
  the swap chain's or the layer's own format), which keeps its pixels: a
  window's buffers come back undefined (DXGI's blit model discards them,
  Metal flips through several drawables, EGL's swap destroys them). It is
  copied whole into the target after the frame, byte for byte:
  `glBlitFramebuffer`, `CopySubresourceRegion` into the buffer behind the
  render target view (no further than that buffer goes), and on Metal a
  draw (one quad over the viewport sampling the texture at its texels'
  centres, blending off: exact for 8-bit sRGB), so the layer stays
  `framebufferOnly`, as Apple asks for the display's sake. A partial
  frame's damage starts from the clear colour, copied in from a small
  texture a clear of the same format made (made before the passes: without
  it the frame is whole), so no colour conversion can differ from the
  whole frame's load action.
- Whole frames in a row (a scroll, an animation over most of the window)
  draw straight into the target, without the frame texture or its copy,
  and leave the frame texture stale: the first small change after them
  (a still frame inside a scroll) draws straight in too, the second fills
  the frame texture again, and partial frames go on from the third. A
  frame at a new size (a live resize) draws straight in too, and the
  frame texture is made at the size the window stays at. A hidden,
  minimized or occluded window (Windows' minimize and hide, AppKit's
  occlusion and orderOut, X11's unmap) lets the frame texture and the
  clear texture go (`ClackGPUIdle`); its next frame is whole. Nothing
  makes them again while it stays so: Windows doesn't draw a hidden
  window's sync frame (the one Window.Hide's own WM_WINDOWPOSCHANGED
  queued, delivered once a close to the tray returned), and an unmapped
  X11 window draws no animation frames until it's mapped again.
  `ClackFrameTexturesHeld` counts the frame textures held, for tests.
  Windows has no occlusion tracker yet (Chromium's
  NativeWindowOcclusionTrackerWin): a window covered by others keeps its
  frame texture and draws what it's asked to.
- A path's vertices are built (tessellated on the CPU) when the frame
  uploads them: a partial frame uploads only the paths its damage
  stencils, and the rest keep their path data in the cache (`raw`), so
  the quiet frames after it (the same ops again) build nothing. Before,
  such a path's vertices were built again every frame until a whole
  frame, which after a cache restart could be every pen path on screen,
  for hours of idle.
- A frame is drawn whole when there's no last frame to start from (the
  first, a new frame texture or size, another target), the clear colour
  changed, the damage covers more than half the viewport or needs more
  than 128 edits, partial frames are off (`ClackPartialFrames(false)`;
  Clack's `CLACK_FULL_FRAMES=1`) or the device can't scissor and copy
  (Vulkan, which X11 tries first when Gio's GPU blocklist is ignored, as
  Slick's Snappy defaults do; an OpenGL context shared with others or
  without `glBlitFramebuffer`; the window logs it once). A frame drawn into a texture target (gpu/headless)
  is drawn whole, packed as before, unless a test asks
  (`ClackPartialTextures`: into the texture itself; `ClackFrameTexture`:
  through the frame texture and its copy, as a window's), so screenshot
  tests draw what they drew.

"The whole frame's pixels" is the frame the same GPU draws whole at that
point (`ClackPartialWhole`: partial frames' state goes on, every frame is
drawn whole). With sticky places, the frame Gio would draw afresh can
differ from it by the placement rounding (TestClackPlacementNoise's
floor), as any change earlier in a frame already made it.

What the frame still costs: the layout, collecting the ops and their
signatures, packing, loading the atlas pages and copying the frame
texture into the target (on a real GPU a copy of the window's pixels; on
llvmpipe a memcpy). `SetGPUFrameHook` reports every window frame's stats
(partial or whole, its damage, draws and API calls), for tests. A scroll
(whole frames in a row, drawn straight into the window) costs what it cost
without partial frames on llvmpipe and WARP (94-100% of the CPU a frame)
and about 0.7 ms more a frame on a Mac's GPU, the signatures and the diff
(`TestRealWindowCaretCost`).

Tests: `gpu/clack_partial_test.go` (ci's linux-gio Gio frame cost step:
`TestClackDamageEdits`, the edit script is valid and shortest against an
LCS on random sequences; `TestClackDamageRects`, disjoint and covering;
`TestClackDamage`; `TestClackStickyNoOverlap`, sticky places never
overlap or leave their page under random churn, and a region keeps its
place unless the sequence is packed afresh, with 30 fresh packings in 600
frames at most (8 in the port); `TestClackStickySpinnerBounded`, 3,000
frames of a spinner's two arcs among 700 regions, on one page and on two,
keep the sequence within 64 slots of the regions, the packer's work
within 3 times the first frame's, the pages and no fresh packing;
`TestClackStickyShrink`, the regions at half, 0.6 and 0.75 of their size
bring the pages back within 1.25 times Gio's own; `TestClackStickyScroll`,
a busy list's scroll packs at Gio's own cost and a sparse one's within
1.5 times, the pages within 1.5 times, and a busy list's end within 3 and
2.5),
`gpu/headless/clack_partial_test.go` on llvmpipe, Metal and WARP (ci's
linux-gio, macos and windows-applog jobs, and flake-check's gpu-cost and
gpu-cost-damage): `TestClackPartialFrames` draws 48 frames of changes a
window sees (a caret's blink, a spinner, an animated emoji, a ring's
colour, a fade, ops moved, added, dropped and swapped, a path growing
before every other, a new clear colour, a scale and a fraction of a pixel,
then a random walk) whole, partial into the target and partial through the
frame texture, and requires every frame's pixels to be the whole frame's
exactly (through the frame texture: its copy, Metal's draw included, bit
for bit the frame drawn straight into the target), the small changes to be
partial frames of small damage, the caret's and the emoji's to take a
tenth of the draws and a fifth of the API calls, whole frames in a row to
draw straight into the target, and the sticky whole frames to be within
the placement floor of Gio's own (packed afresh; the floor: the same
frames with a transparent path first or after the shadow rings, max(6,
floor+2) levels in twice the floor's pixels and 1,024 more);
`TestClackPartialSameOps` (the same ops again draw nothing; a live image's
next frame damages only it; the quiet frames after a partial frame of new
ops tessellate and build nothing); `TestClackPartialSpinner` (3,000
spinner frames: the sequence's holes, the packer's work, the frames
partial, the last the whole frame's pixels); `TestClackPartialDamageRects`.
In `app`: `TestUnmappedStopsFramesX11` (ci's linux-gio Xvfb step: an
animating window unmapped by another client draws no frames and holds no
frame texture until it's mapped again) and the Windows hide tests'
`TestHideAndRaise` (a window hidden from inside its ClosingEvent holds no
frame texture). In Clack: `internal/ui` `TestFrameCostDamage` (flake-check's
gpu-cost-damage: the caret's blink, the typing indicator, a spinner's turn
and an animated emoji's frame over Clack's window at 1440×900, Karim's
1918×1080 and 1280×800 at 2x, each whole and partial, pixels and costs, the
quiet frames after each partial frame, and the sticky whole frame within
TestFrameCostWindow's placement floor of Gio's own) and
`TestRealWindowCaretCost` (ci's linux-idle, macos, windows-idle and
flake-check's gpu-cost-damage, count=N runs: App.Run's real window, 6 s of
blinks partial and whole in turns of 3 s, their draws and the process's
CPU; a scroll in four turns of 1.5 s, partial frames on and off, what
its frames cost and copied and the coverage atlas, the median turn's CPU
a frame within 1.25 times CLACK_FULL_FRAMES' on every OS (the Mac's ran
110-129% a turn); on Xvfb the window's pixels read back with python3-xlib, the
caret's toggling between blinks and a partial frame's window bit for bit
a whole frame's; on macOS each blink's drawable read back after the frame
texture's draw into it, bit for bit the frame texture: `ClackVerifyCopy`,
`FrameStats.CopyChecked` and `CopyMismatchPx`, with the layer's drawables
made readable for that part alone by `app.ClackReadableDrawables`; the
blinks and the scroll are measured on the framebufferOnly drawables a
window has).

What a window's GPU holds, in bytes: Direct3D 11 counts the textures and
buffers it made and hasn't released (`driver.ClackResources`, the
backend's `ClackResourceBytes`: every mip level's texels at the format's
size, each buffer's size), and `FrameStats.DeviceTextureBytes` and
`DeviceBufferBytes` report them after each frame (-1 on the other
backends); `FrameStats.FrameTextureBytes` is the share partial frames
keep, the frame texture and the clear texture. internal/ui's
`TestRealWindowMemory` (ci's windows-idle, the runner's display at
1920x1080) measures Karim's window, a client area of 1918x1080 with a
thread open and the caret blinking, in three pairs of processes, partial
frames then `CLACK_FULL_FRAMES=1` (Gio's own whole frames and atlas
packing): each process's private working set and commit, its Go heap, and
what the device holds. The pairs' median difference in commit must be no
more than the frame and clear textures and 4 MB.

`gpu/clack_floor.go` has two switches for the pixel tests that hold a
change in packing to Gio's own placement rounding. `ClackPackOrder` packs
the coverage atlas in a seeded order instead of the ops' (every path's
coverage on other texels: the noise of any packing change, where a
transparent probe path moves only the paths after it), and
`ClackCanaryTexels` writes coverage one half into the centre texel of a
whole frame's largest stencil regions (pixels a renderer error gets
wrong). internal/ui's `TestFrameCostWindow` takes its new-against-old floor
from both kinds of move, and requires the bound to reject the new way with
16 such texels in every scene. Its karim 1x light thread scene had fit the
probes' floor with 2% to spare (5,260 px against 5,380, ui a3f8d0b0), and
went past it when ui 556c6251's theme colours made one probe move 18%
fewer pixels (5,282 against 5,116) with the difference itself unchanged.
Not in `UPSTREAM.patch`.

## 47. app: the caret's blink period from the OS (macOS's, X11's XSETTINGS)

`app/clack_caret_macos.go`, `app/clack_caret_macos.m` (new),
`app/os_macos.go` (`newWindow`) and `app/clack_xsettings_x11.go`,
`app/clack_xsettings_nox11.go` (new: `ClackXSettings`).

Gio's editor blinked at 500 ms on macOS (patch 13 read the OS's interval
on Windows only). Chromium's `OsSettingsProviderMac::CaretBlinkInterval`
(`ui/native_theme/os_settings_provider_mac.mm`, main 2026-11-17) doesn't
blink at all when the user asked for a non-blinking insertion point
(macOS 15's `AXPrefersNonBlinkingTextInsertionIndicator`), and otherwise
blinks at what AppKit's `NSTextView` does
(`ui/base/cocoa/defaults_utils.mm`): the average of the
`NSTextInsertionPointBlinkPeriodOn` and `...Off` user defaults when
either is set, never when the on period is over a minute, 500 ms when
neither is set or either is negative. It reads them again when the app
becomes active and when the accessibility preference changes. Gio now
does the same as a window opens (`setCaretBlinkFromOS`, into
`widget.SetCaretBlinkInterval`) and on those two notifications.

On Linux Chromium's caret blinks at half of GTK's gtk-cursor-blink-time,
which GTK's X11 backend takes from the X settings manager (XSETTINGS'
Net/CursorBlink and Net/CursorBlinkTime; gsd-xsettings, xfsettingsd).
`ClackXSettings` reads that manager's `_XSETTINGS_SETTINGS` property on a
display connection of its own (the server grabbed while it looks, as the
XSETTINGS spec has clients do: the manager's window can't go between
finding and reading it, which Xlib's default error handler would make the
process's end; Xlib's threads are made safe through Gio's own once);
Clack reads it once, as Chromium reads the interval (it never polls it),
parses it and does the rest (`internal/ui/caretblink_linux.go`).

Tests: `app/clack_caret_darwin_test.go` `TestCaretBlinkFromOSMac` (ci's
macOS window step: a window opens with the average of the two periods
set in the process's own defaults; the app becoming active reads them
again: one period alone, an on period over a minute, a negative one, both
0, neither); `app/clack_xsettings_x11_test.go` `TestClackXSettingsX11`
(ci's linux-gio Xvfb step: python-xlib plays a settings manager, and
`ClackXSettings` reads its property byte for byte, again after it changes,
and nothing once the manager is gone); `internal/ui` `TestXSettingsBlink`
and `TestCaretBlinkWatcherX11`. Not in `UPSTREAM.patch`.

## 49. io/input: every handler caught up with where the pointer is

`io/input/clack_reset.go` (new: `Source.ResetPointers`, its command,
`pointerQueue.resetPointers`, `noteMouse`, `notePointerRead`),
`io/input/pointer.go` (`pointerHandler.seen`/`seenID`, `pointerQueue.mouse`,
`Push` notes the mouse) and `io/input/router.go` (`Event` notes what a
handler read; `execute` runs the command at once, `executeCommand` runs
it). An addition: nothing upstream behaves differently unless asked, and
what's noted costs a field write per event read.

Clack's inspect tool (`internal/ui/inspect` freeze.go) freezes the window
on the frame on screen, so a hover state can be picked and captured
after the pointer leaves it: until the freeze ends the window draws that
frame's ops again and lays nothing out. Gio's router routes the pointer
over the frozen frame's areas, which are still in the ops, but no widget
asks for events in those frames (the frames' `Event` calls are the
tool's), so nothing is routed to the widgets and nothing reads what was.
The router's idea of what the pointer is over (`pointerInfo.entered`)
moves on with the pointer; the widgets' stays at the freeze. When the
freeze ended, a widget the pointer had left never got its Leave: the
router no longer counted it as entered.

The first version gave every handler a `pointer.Cancel` and the ones
under the pointer an Enter. That left two kinds of hover stuck (the
inspect-freeze review, 2026-11-18). Clack's message bodies (richview),
plugin trees (slackc), the message scrollbar, Lists' rows, the canvas
outline and the prefs reorder list clear their hover only on a Leave,
and a Cancel to them ends only a press: a message's emoji tooltip, a
link's underline, a plugin element's :hover stayed after the freeze. And
the Cancel carried pointer id 0. On Windows Gio numbers the mouse 1
(`EnableMouseInPointer`, GET_POINTERID_WPARAM), and `gesture.Hover`
drops a Cancel or a Leave of another pointer, so every Hover there (a
sidebar heading's ⋯ and +, the thread title's tooltip, the Activity,
Later, Threads and Unreads rows) stayed hovered.

`Source.ResetPointers()` now sends what the router would have sent had
the widgets been reading:

- every handler gets a Cancel, which ends a press, a drag or a grab;
- a handler that last read an Enter or a Move, with no Leave or Cancel
  read since (`notePointerRead` in `Router.Event`), and isn't under the
  mouse gets a Leave;
- every handler under the mouse gets an Enter and a Move there (the Move
  for trackers that ask only for Move, as Lists' rows do), whatever filter
  it asked for in the last frame.

Each event carries the mouse's own id (the id a handler's Enter carried,
for its Cancel and Leave), from the mouse's last event, which `Push`
keeps after the router stops tracking it (`noteMouse`). A mouse whose
last event was a Leave (it left the window) gets no Enter anywhere; Gio
sends that Leave only on Windows (patch 22), so elsewhere a mouse that
left is entered again where it was last seen in the window, as upstream
leaves that widget hovered. Touches end with the Cancel. The router
counts the handlers under the mouse as entered from there. It runs at
once, before the rest of the frame reads its events, even when a handler
already read its own in that frame (the tool read its input before the
frame was laid out). Upstream's `execute` would defer such a command to
the frame's end, and the frame would show the stale hovers once more.
The inspect tool calls it first in the frame that lays out again after a
freeze.

Tests: `io/input/clack_reset_test.go` (`TestResetPointers`): three
handlers and the tool's pass-through area over them; frames in which only
the tool reads let the pointer go from a to b and c unheard. Without the
reset a never gets its Leave, nor b its Enter. With it, for mouse ids 1
and 0, after the tool read its events in that frame: a reads a Cancel and
a Leave, b a Cancel and an Enter, c (Move and Leave only) a Move, the
tool a Cancel, an Enter and a Move, every one with the mouse's id; no
second Enter comes at the frame's end, and going back to a is a Leave and
an Enter as usual. A mouse pressed over b that left the window gets b no
Enter and a its Leave. `gesture/clack_reset_test.go`
(`TestHoverResetPointers`): the same freeze read through two real
`gesture.Hover`s, for mouse ids 1 and 0. In Clack: richview's
`TestResetPointersClearsBodyHover`, slackc's `TestResetPointersLeavesTree`
and `internal/ui`'s `TestInspectFreezeResetsHover`. On ci's linux-gio,
windows-applog and macos jobs. Not in `UPSTREAM.patch`.
