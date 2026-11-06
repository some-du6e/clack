<img src="assets/icon.svg" width="64" height="64" alt="">

# yoloslack

A fast 1:1 Slack client, written in Go with a native, GPU-rendered window ([Gio](https://gioui.org)). No browser, no JavaScript, no Electron. It copies the 2026 Slack desktop client and is built for the Hack Club Slack (Enterprise Grid). Other workspaces work too.

## Log in

yoloslack signs in with your Slack `d` cookie, the long-lived session cookie your browser already has.

1. Open your workspace in a browser (e.g. https://hackclub.slack.com) and sign in.
2. Open DevTools → Application → Cookies → `https://app.slack.com` and copy the value of the cookie named `d`. It starts with `xoxd-`.
3. Either paste it into the sign-in screen yoloslack shows on first start, or run:

   ```sh
   yoloslack login hackclub
   ```

   and paste the cookie when asked.

yoloslack loads the workspace once to find your session tokens (team and, on Enterprise Grid, org), checks them with `auth.test`, and saves them to `~/.config/yoloslack/creds.json` (mode 600). Set `YOLOSLACK_HOME` to keep config somewhere else. "Sign out" deletes the file.

Treat `creds.json` like a password: it is a full session for your account.

## Run

```sh
yoloslack           # open the window
yoloslack -dark     # start in dark mode (the saved preference wins)
```

| flag | default | |
|---|---|---|
| `-creds <file>` | `~/.config/yoloslack/creds.json` | credentials file |
| `-dark` | off | start in dark mode |

Set `YOLOSLACK_TIMING=1` to log startup timings.

Builds come from CI: every push runs gofmt, vet and the tests, then builds Linux (cgo, Gio needs Wayland/X11/GL headers), Windows (cross-compiled, no cgo) and macOS binaries. Download them from the run's artifacts. To build Linux yourself you need Go ≥ 1.24 and `libwayland-dev libx11-dev libx11-xcb-dev libxkbcommon-x11-dev libgles2-mesa-dev libegl1-mesa-dev libffi-dev libxcursor-dev libvulkan-dev`.

## How it is put together

- `internal/slack`: Slack's web-client API (team and enterprise hosts, edge API) and the realtime gateway.
- `internal/store`: in-memory state plus the metadata disk cache.
- `internal/core`: the in-process service the UI calls. It handles boot, history, search, sending with optimistic rows, uploads, drafts and scheduling, activity, saved items, and typed realtime events.
- `internal/richtext`: turns Slack messages into documents for native layout.
- `internal/ui/...` and `internal/imgcache`: the Gio app.

See `docs/NATIVE.md` for the contracts between these packages.

## Speed

Measured on the Hack Club Slack:

- Boot takes about 0.6 s with a warm metadata cache. Conversations, counts, sections and team info load in parallel, while emoji, users, usergroups and prefs come from the disk cache. The realtime socket is connected about 1 s after start.
- Opening a cached channel takes under 1 ms in core. Unread channels are prefetched in the background (at most 24, 3 at a time). An uncached channel costs one `conversations.history` call, about 0.15–0.3 s.
- The people and channel directory answer in about 90 ms (edge API). Message search takes about 0.7 s.

## Data & scraping policy

yoloslack follows the [Hack Club scraping policy](https://news.hackclub.com/news/scraping-use-policy/):

- **Message contents are never written to disk.** Messages, threads, drafts, files and rendered documents live only in memory for the lifetime of the process, as in any Slack client. Logs carry method names, ids, counts and timings only.
- Only metadata is cached on disk: the user directory, the custom emoji map, usergroups and your prefs (`~/.config/yoloslack/cache/meta.json`), plus avatar and emoji images.
- Files and thumbnails from `files.slack.com` are fetched into a memory-only cache and never saved.
- yoloslack makes the same kind of requests the official client makes: on demand, rate-limited, no crawling.
