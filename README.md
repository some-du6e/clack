<img src="assets/icon.svg" width="64" height="64" alt="">

# Clack

Clack (formerly yoloslack) is a fast Slack client for the desktop, written in Go with a native, GPU-rendered window ([Gio](https://gioui.org)). There is no browser, no JavaScript and no Electron: one small binary draws every pixel itself. It copies the 2026 Slack desktop client and is built for the Hack Club Slack (Enterprise Grid). Other workspaces work too.

## What it looks like

There are no screenshots in this repo on purpose: any real screenshot would show other people's messages, and the repo is public. In words:

- **The window** is Slack's 2026 layout on Slack's aubergine background (the same radial-plus-conic gradient). Along the top is a 40 px bar with back/forward/history buttons, a wide "Search Hack Club" pill and a help button.
- **The left rail** (70 px) has the workspace icon, then Home, DMs, Activity, Files, To-dos and Agents & tools with unread badges. At the bottom sit a + button, the light/dark toggle and your avatar with a presence dot.
- **The sidebar** (resizable, 232 px by default) starts with the team name in heavy type, settings and compose buttons, and a "Find a conversation…" box. Below that come Threads, Huddles, Drafts & sent, Saved and Directories, then your sections (Starred, Channels, Direct messages, Apps, custom ones). Unread conversations are bold white, mentions get lilac badges, and DMs show avatars, presence and status emoji.
- **The main panel** is white (or #1a1d21 in dark mode) with rounded corners. Its header has the star, channel name, topic, member facepile with count, a Huddle split button, notifications, search and a kebab menu. Under it are the channel's tabs (Messages plus canvases, lists and bookmarks), the message list (grouped rows, reactions, threads, unfurls, files and images, day dividers, hover action bar) and the rich composer (formatting toolbar, @ # : / autocomplete, attachments, "X is typing…").
- **Threads and channel details** open in a pane on the right. Ctrl+K, search, the emoji picker, profile cards, menus, the lightbox and Preferences open as popups over the window.
- **Dark mode** follows Slack's dark theme, including the darker aubergine chrome and the 1 px outline around the workspace.

## Download

Release binaries are attached to each [GitHub Release](../../releases) (`clack-linux-amd64`, `clack-windows-amd64.exe`, `clack-darwin-arm64`, plus `SHA256SUMS`). Rename the file to `clack` if you like; the examples below use that name. Builds of every commit are also on the [Actions](../../actions/workflows/ci.yml) page: open a run and download an artifact from the bottom of the page (needs a GitHub login).

### Linux (x86-64)

```sh
chmod +x clack-linux-amd64
./clack-linux-amd64
```

It needs a Wayland or X11 desktop with OpenGL ES/EGL (Mesa is fine). On a minimal install, add the runtime libraries, e.g. on Debian/Ubuntu:

```sh
sudo apt install libwayland-client0 libwayland-egl1 libx11-6 libx11-xcb1 libxkbcommon-x11-0 libxcursor1 libegl1 libgles2 libvulkan1
```

Desktop notifications use the standard `org.freedesktop.Notifications` D-Bus service that every desktop provides.

### macOS (Apple Silicon)

```sh
chmod +x clack-darwin-arm64
xattr -d com.apple.quarantine clack-darwin-arm64   # the binary isn't notarized
./clack-darwin-arm64
```

### Windows (x86-64)

Download `clack-windows-amd64.exe` and double-click it. The binary isn't signed, so SmartScreen may warn: choose **More info → Run anyway**.

## Sign in

**Signing in from Clack is temporarily unavailable.** The old cookie sign-in loaded a Slack page with your `d` cookie to find your tokens, and that could sign you out of Slack in your browser, so it was removed. A safer sign-in is being worked on. An existing `~/.config/clack/creds.json` keeps working.

Clack never calls Slack's sign-out or token-revoke methods. "Sign out on this device" only deletes `creds.json`; your Slack session in the browser and other apps is not affected.

Treat `creds.json` like a password: it is a full session for your account.

## Run

```sh
clack            # open the window
clack -dark      # start in dark mode (the saved preference wins)
clack -version   # print the version
```

| flag | default | |
|---|---|---|
| `-creds <file>` | `~/.config/clack/creds.json` | credentials file |
| `-dark` | off | start in dark mode |

Config lives in `~/.config/clack` (`%AppData%\clack` on Windows, `~/Library/Application Support/clack` on macOS). Set `CLACK_HOME` to keep it elsewhere. Config from the old name (`yoloslack`) is copied over on first start. Set `CLACK_TIMING=1` to log startup timings, `CLACK_FRAMELOG=1` to log every frame, and `CLACK_NOEMBOLDEN=1` to skip the text-weight correction (faster on machines without a GPU).

## Build

Every push runs gofmt, vet and the tests in GitHub Actions, then builds Linux (cgo; Gio needs Wayland/X11/GL headers), Windows (cross-compiled, no cgo) and macOS binaries. A `v*` tag runs `.github/workflows/release.yml`, which publishes the three binaries and their checksums as a GitHub Release.

To build yourself you need Go ≥ 1.24. On Linux also install `libwayland-dev libx11-dev libx11-xcb-dev libxkbcommon-x11-dev libgles2-mesa-dev libegl1-mesa-dev libffi-dev libxcursor-dev libvulkan-dev`, then:

```sh
go build -o clack ./cmd/clack
```

## How it is put together

- `internal/slack`: Slack's web-client API (team and enterprise hosts, edge API) and the realtime gateway.
- `internal/store`: in-memory state plus the metadata disk cache.
- `internal/core`: the in-process service the UI calls. It handles boot, history, search, sending with optimistic rows, uploads, drafts and scheduling, activity, saved items, and typed realtime events.
- `internal/richtext`: turns Slack messages into documents for native layout.
- `internal/ui/...` and `internal/imgcache`: the Gio app.

See `docs/NATIVE.md` for the contracts between these packages.

## Speed

Measured on the Hack Club Slack:

- The window paints the sidebar from a small metadata cache in about 0.4–0.5 s, and core is ready (conversations, counts, sections) about 0.6 s after start. Conversations, counts, sections and team info load in parallel. The realtime socket connects about 1 s after start.
- Switching to a cached channel takes 1–3 ms of layout. An uncached channel costs one `conversations.history` call, about 0.15–0.3 s. Unread channels are prefetched in the background.
- When idle, the window draws no frames. On a machine without a GPU (software OpenGL), each frame takes a few hundred milliseconds to rasterize; with a GPU it is a few milliseconds.

## Data & scraping policy

Clack follows the [Hack Club scraping policy](https://news.hackclub.com/news/scraping-use-policy/):

- **Message contents are never written to disk.** Messages, threads, drafts, files and rendered documents live only in memory for the lifetime of the process, as in any Slack client. Logs carry method names, ids, counts and timings only.
- Only metadata is cached on disk: the user directory, the custom emoji map, usergroups and your prefs (`~/.config/clack/cache/meta.json`), plus avatar and emoji images.
- Files and thumbnails from `files.slack.com` are fetched into a memory-only cache and never saved.
- Clack makes the same kind of requests the official client makes: on demand, rate-limited, no crawling.
