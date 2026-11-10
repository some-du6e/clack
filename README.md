<img src="assets/icon.svg" width="64" height="64" alt="">

# Clack

Clack (formerly yoloslack) is a fast Slack client for the desktop, written in Go with a native, GPU-rendered window ([Gio](https://gioui.org)). There is no browser, no JavaScript and no Electron: one small binary draws every pixel itself. It copies the 2026 Slack desktop client and is built for the Hack Club Slack (Enterprise Grid). Other workspaces work too.

## Install

Download the file for your system from the latest [GitHub Release](../../releases/latest). `<ver>` below is the version without the `v`, e.g. `0.3.1`.

| system | file |
|---|---|
| macOS 11+ (Apple Silicon and Intel) | `Clack-<ver>-macos-universal.dmg` (or `.zip`) |
| Windows 10/11 x64, installer | `Clack-<ver>-windows-amd64-setup.exe` |
| Windows 10/11, portable | `Clack-<ver>-windows-amd64.zip` (`-arm64.zip` for ARM) |
| Linux, any distro | `Clack-<ver>-x86_64.AppImage` (`-aarch64` for ARM) |
| Debian 12+, Ubuntu 22.04+ | `clack_<ver>_amd64.deb` (`_arm64`) |
| Fedora | `clack-<ver>.x86_64.rpm` (`.aarch64`) |

None of the builds are signed by Apple or Microsoft, so both systems warn the first time you open Clack. The steps below get past that.

### macOS

Open the `.dmg` and drag **Clack** onto **Applications**. With the `.zip`, unzip it and move `Clack.app` to `/Applications`.

The app has only an ad-hoc signature and isn't notarized, so the first launch says Apple "could not verify" it. Do one of these once:

- Right-click (or Control-click) Clack in Applications, choose **Open**, then **Open** again. (macOS 15 and later don't offer this.)
- Try to open Clack, then go to **System Settings → Privacy & Security**, scroll down and click **Open Anyway**.
- In Terminal: `xattr -dr com.apple.quarantine /Applications/Clack.app`

The app declares the `slack://` scheme, so macOS can send Slack links to it. If the official Slack app is installed too, macOS picks one of the two.

### Windows

**Installer:** run `Clack-<ver>-windows-amd64-setup.exe`. It installs for your user only (no admin prompt) into `%LocalAppData%\Programs\Clack`, adds a Start Menu entry and, if you tick it, makes Clack open `slack://` links (off by default: the Slack desktop app claims those too, and you don't need it to sign in). A desktop shortcut is optional. Uninstall from **Settings → Apps**. Running a newer installer upgrades in place.

**Portable:** unzip `Clack-<ver>-windows-amd64.zip` anywhere and run `Clack.exe`. It doesn't touch the registry. To have it open `slack://` links, run `Clack.exe -register-url-handler` once from that folder (`-unregister-url-handler` undoes it; both only affect your user).

The files aren't code-signed, so SmartScreen may say "Windows protected your PC". Click **More info → Run anyway**.

### Linux

**AppImage** (no install):

```sh
chmod +x Clack-<ver>-x86_64.AppImage
./Clack-<ver>-x86_64.AppImage
```

The AppImage mounts itself with FUSE (`fusermount3` from the `fuse3` package, which most desktops already have). If FUSE isn't available, run it with `--appimage-extract-and-run`.

**Debian/Ubuntu:**

```sh
sudo apt install ./clack_<ver>_amd64.deb
```

**Fedora:**

```sh
sudo dnf install ./clack-<ver>.x86_64.rpm
```

The packages put `clack` in `/usr/bin`, add Clack to the application menu and declare it as a `slack://` handler. If another app already handles `slack://` links, make Clack the default with `xdg-mime default clack.desktop x-scheme-handler/slack`.

Clack needs a Wayland or X11 desktop with OpenGL ES/EGL (Mesa is fine). The packages pull in the libraries; for the AppImage on a minimal system, see [packaging/linux/README.md](packaging/linux/README.md) for the package names on Debian/Ubuntu, Fedora and Arch. Desktop notifications use the standard `org.freedesktop.Notifications` D-Bus service that every desktop provides.

### Check the download

Each release has a `SHA256SUMS` file. Put it next to the files you downloaded and run:

```sh
sha256sum -c --ignore-missing SHA256SUMS        # Linux
shasum -a 256 -c --ignore-missing SHA256SUMS    # macOS
```

On Windows, compare `Get-FileHash Clack-<ver>-windows-amd64-setup.exe` (PowerShell) with the line in `SHA256SUMS`.

### Development builds

Every commit is also built on the [Actions](../../actions/workflows/ci.yml) page: open a run and download an artifact from the bottom of the page (needs a GitHub login). These are bare binaries (`clack-linux-amd64`, `clack-windows-amd64.exe`, `clack-darwin-arm64`) with no installer. Run them the same way; on macOS, clear the quarantine flag first with `xattr -d com.apple.quarantine clack-darwin-arm64`.

## What it looks like

There are no screenshots in this repo on purpose: any real screenshot would show other people's messages, and the repo is public. In words:

- **The window** is Slack's 2026 layout on Slack's aubergine background (the same radial-plus-conic gradient). Along the top is a 40 px bar with back/forward/history buttons, a wide "Search Hack Club" pill and a help button.
- **The left rail** (70 px) has the workspace icon, then Home, DMs, Activity, Files, To-dos and Agents & tools with unread badges. At the bottom sit a + button, the light/dark toggle and your avatar with a presence dot.
- **The sidebar** (resizable, 232 px by default) starts with the team name in heavy type, settings and compose buttons, and a "Find a conversation…" box. Below that come Threads, Huddles, Drafts & sent, Saved and Directories, then your sections (Starred, Channels, Direct messages, Apps, custom ones). Unread conversations are bold white, mentions get lilac badges, and DMs show avatars, presence and status emoji.
- **The main panel** is white (or #1a1d21 in dark mode) with rounded corners. Its header has the star, channel name, topic, member facepile with count, a Huddle split button, notifications, search and a kebab menu. Under it are the channel's tabs (Messages plus canvases, lists and bookmarks), the message list (grouped rows, reactions, threads, unfurls, files and images, day dividers, hover action bar) and the rich composer (formatting toolbar, @ # : / autocomplete, attachments, "X is typing…").
- **Threads and channel details** open in a pane on the right. Ctrl+K, search, the emoji picker, profile cards, menus, the lightbox and Preferences open as popups over the window.
- **Dark mode** follows Slack's dark theme, including the darker aubergine chrome and the 1 px outline around the workspace.

## Try the demo

No Slack account needed:

```sh
clack demo              # open Clack on a built-in fake workspace
clack demo --speed 3    # the bots talk three times as often (0.5 is half)
clack demo --dark       # start in dark mode
clack demo --quiet      # no ambient chatter: people only answer you
clack demo --reset      # forget the demo's saved settings and start fresh
clack demo --help
```

`clack --demo` works too. The demo opens the normal Clack window signed in to **Quillfeather Paper Co.**, the Wexley Falls branch of a small paper distributor. You're Jordan, the new hire in week 2. Everyone and everything in it is made up:

- **A dozen coworkers** with drawn avatars, titles, pronouns and statuses, among them a branch manager who invents awards, a top seller and her honey-selling rival, a weary one-person IT department with a moody printer, and an intern with a new startup idea every day.
- **Channels** #general, #random, #sales-leaderboard, #party-planning-crew, #it-help, #warehouse, #lunch-orders, #kudos, a private #secret-santa-2026 and a code channel. There are also DMs, a group DM, and custom sidebar sections.
- **A few days of backstory** with threads, reactions, custom emoji, generated images and a PDF, a canvas, a list, pinned messages, a scheduled message and an @channel or two.
- **Scripted bots**, all local with no AI service involved:
  - People post on a timer, with banter that runs across channels. They reply in threads when you post or mention them, react to your messages, show "is typing…" and change their statuses.
  - **Inkwell**, an AI coding agent app, streams its "Thinking…" plan into #inkwell-order-portal, a code channel with a diff and a pull request view.
  - **Pulse** posts polls whose buttons work.
  - Now and then someone rings you for a huddle. The demo has no audio, so joining doesn't connect.
  - Your Activity feed fills up as it all happens.

The demo is offline and can't reach Slack. It runs its own fake Slack server (internal/fakeslack) inside the process on 127.0.0.1. Before anything else starts, the process makes itself loopback-only: every connection to a host other than localhost is refused and proxies are ignored. It keeps its settings in its own folder (`clack-demo` next to Clack's config folder, or `CLACK_DEMO_HOME`), so it never reads or writes your credentials, cache or preferences. A **Demo workspace** banner stays above the workspace, and the window title says (Demo). The bots' scripts and the workspace live in `internal/demo/data`.

## Sign in

Clack has two ways in. Use the first.

### Sign in with a Slack link (Clack's own session)

This is how Slack's desktop app signs in. You sign in in your browser, Slack's page hands "the Slack app" a one-time `slack://…/magic-login/…` link, and Clack trades that link for a session Slack issues to Clack (`auth.loginMagicBulk?ssb=1`). Nothing is copied from your browser, so Clack and the browser are two separate sessions, the way the browser and Slack desktop are.

1. Start Clack. The sign-in screen opens on **Sign in with a Slack link**.
2. Click **Open sign-in page** (or open `https://slack.com/ssb/add?s=1&v=4.53.276` yourself) and sign in to your workspace in the browser.
3. When Slack offers to open the Slack app, click it and let the browser open Clack. On macOS that works out of the box. On Windows, tick "Open slack:// links with Clack" in the installer (or run `Clack.exe -register-url-handler` once); on Linux, run `clack --register-url-handler` once. If you can see the `slack://` link itself, you can paste it into **Sign-in link** instead. Slack's page usually doesn't show it, though.

The link works once, for a few minutes. If it has expired, sign in on the page again. Treat it like a password until then. Workspaces with single sign-on may send you back to the browser to finish.

Letting the browser open Clack needs Clack to handle `slack://` links. On macOS the app declares the scheme, so it works as soon as Clack is installed. Elsewhere Clack never takes them by itself. Run `clack --register-url-handler` once (on Linux it runs `xdg-mime default clack.desktop x-scheme-handler/slack`; on Windows it writes the per-user `slack` URL keys). The Slack desktop app claims `slack://` too and re-claims it whenever it starts, so with both installed whichever registered last wins, and a link may open the wrong app. A link that reaches Clack while it's already signed in does nothing but show a note: any web page can make your browser open a `slack://` link, and one for someone else's account must not swap yours. Sign out first.

From a terminal: `clack login 'slack://T…/magic-login/…'`, or `clack login` and paste the link at the prompt. A running Clack also accepts `clack 'slack://T…/magic-login/…'`.

After that, Clack sends every cookie Slack set for the session (`d`, `lc`, `d-s`, …), takes in each new value Slack sends back, and saves them to `creds.json` (soon after `d` changes, within 30 s for the rest, and at exit). If the link asked for DPoP (`dpop=1`), Clack keeps a P-256 key for that domain in `~/.config/clack/dpop-keys/` and signs a proof for each API and socket request. If Slack rejects a proof, or the key is gone, Clack stops talking to Slack and asks you to sign in again.

What isn't verified yet: whether this session survives you signing out in the browser (it should, it's a separate session), how Enterprise Grid treats DPoP, and whether Slack's desktop boot page always has the token Clack reads from it. See [docs/DESKTOP-SIGNIN.md](docs/DESKTOP-SIGNIN.md).

### Advanced: paste a token

> [!NOTE]
> **This way Clack shares your browser's Slack session. It is not a separate sign-in.** Earlier versions got sessions signed out **everywhere** (the browser, the desktop app and Clack) on Hack Club's Enterprise Grid, a few seconds after signing in. The cause was found: Grid reacts to Go's TLS handshake. This release connects to Slack with OpenSSL's handshake instead, which Grid leaves alone ([docs/SIGNOUT-AUDIT.md](docs/SIGNOUT-AUDIT.md)). Slack can still end a shared session for its own reasons, so use an account you can sign back in to.

Click **Advanced: paste a token instead** on the sign-in screen (or press Enter at `clack login`'s link prompt). Fill in your workspace, the `xoxc-` **token**, the **`d` cookie** (`xoxd-…`) and, optionally, the org token (Enterprise Grid workspaces such as Hack Club). Don't paste `d-s`: Clack never sends it on a pasted session. Clack only checks them with `auth.test`: it never loads Slack web pages with your cookie and never calls a sign-out API, so your browser session stays signed in.

The quickest way to get them: open Slack in the browser you'll copy from, open DevTools (F12) on that tab, and run this in the Console. It copies the tokens Slack's own page keeps in `localStorage`. The sign-in screen shows the same snippet with a Copy button:

```js
copy(JSON.stringify({teams: JSON.parse(localStorage.localConfig_v2).teams}))
```

Paste that JSON into the Token field. Clack picks out the workspace token, the org token and the workspace URL. (JSON from the older snippet, with a `"ua"` in it, still works; the user agent is ignored.) `d` is HttpOnly, so copy it from DevTools > Application > Cookies > `https://app.slack.com`. Leave `d-s` alone: Slack resets it to the current time on every response, so a copy is stale at once, and sending an older one than the browser's looks like a cloned cookie.

### Either way

Every connection Clack makes to Slack uses the TLS handshake of OpenSSL 3.0.13 (the one Python sends) over HTTP/1.1, not Go's, which Enterprise Grid signed sessions out for. Every request says what it is: `User-Agent: Clack/<version> (+https://github.com/some-du6e/clack)`. It sends no browser or Slack desktop User-Agent, no `sec-ch-ua` client hints, no `Sec-Fetch-*` and no `Origin` or `Referer`. Clack doesn't ask for your browser's user agent, and an old `creds.json` that has one is read and the value ignored.

Credentials are saved to `~/.config/clack/creds.json`, readable only by you (mode 600 in a 700 directory; an owner-only ACL on Windows). They only ever go to https slack.com hosts. "Sign out on this device" deletes that file and nothing else.

**Live updates are off until you turn them on.** On first run a banner asks. You can also switch them in Preferences > Advanced > Live updates. While they're off, Clack never opens Slack's realtime websocket. It refreshes unread counts and the open conversation and threads over the Web API every 20 seconds instead. You can read and send, but there's no typing indicator, presence, instant delivery, desktop notifications, huddle events or slash-command modals. `clack -no-realtime` (or `CLACK_NO_REALTIME=1`) keeps the socket closed whatever the setting says.

Clack never calls Slack's sign-out or token-revoke methods. "Sign out on this device" only deletes `creds.json`; your Slack session in the browser and other apps is not affected.

Treat `creds.json` like a password: it is a full session for your account. See [docs/SECURITY.md](docs/SECURITY.md) for how Clack protects it, what plugins can see, and how Clack follows the Hack Club scraping policy.

## Run

```sh
clack            # open the window
clack -dark      # start in dark mode (the saved preference wins)
clack -version   # print the version
clack 'slack://channel?team=T0266FRGM&id=C0266FRGV'   # open a Slack link
```

| flag | default | |
|---|---|---|
| `-creds <file>` | `~/.config/clack/creds.json` | credentials file |
| `-dark` | off | start in dark mode |
| `-register-url-handler` | | make Clack this user's `slack://` handler and exit (Windows registry; `xdg-mime` on Linux). Clack never does this on its own; the Slack desktop app claims the same links |
| `-unregister-url-handler` | | remove Clack's `slack://` registration (Windows) and exit |

Only one Clack runs per user. Starting it again, or opening a `slack://` link, brings the running window to the front and opens the link there (`slack://channel?id=C…[&thread_ts=…]`, `slack://user?id=U…`, `slack://open`). A `slack://…/magic-login/…` or `slack://login-v2/…` link signs in instead, from the sign-in screen only. Instances with different `CLACK_HOME`s are separate. On Windows, Clack also puts an icon in the notification area: click it to bring the window back, or right-click it to quit.

Release builds check GitHub Releases for a newer version 15 s after start and then hourly by the wall clock, sleep included (pre-release builds follow pre-releases). **Preferences → Advanced → Check now** checks right away and shows the answer in place; on macOS it is also **Clack → Check for Updates…**, and on Linux and Windows **Check for Updates…** in the workspace menu. If one is out, Clack downloads the package that matches your install in the background and checks it against the release's `SHA256SUMS`, the way Slack desktop does: the menu item reads **Downloading Update**, then **Restart to Apply Update**, and the Help button in the top bar gets a red dot. Help opens a **Take action** card that asks "Restart and update Clack?" with **Not Now** and **Restart Clack**. Nothing installs until you choose to restart. Turn the download off in **Preferences → Advanced → Download updates automatically**, and the check off with **Check for updates**. Releases aren't code-signed yet, so the checksum catches a broken download but not a compromised release (see docs/SECURITY.md D-3).

Config lives in `~/.config/clack` (`%AppData%\clack` on Windows, `~/Library/Application Support/clack` on macOS). Set `CLACK_HOME` to keep it elsewhere. Config from the old name (`yoloslack`) is copied over on first start. Set `CLACK_TIMING=1` to log startup timings, `CLACK_FRAMELOG=1` to log every frame, and `CLACK_NOEMBOLDEN=1` to skip the text-weight correction (faster on machines without a GPU).

### If Clack freezes

If the window stops responding for more than 2 seconds (a beachball on macOS), Clack writes a freeze report and keeps running. Reports go to `freeze-reports/` in the config directory:

- macOS: `~/Library/Application Support/clack/freeze-reports/`
- Linux: `~/.config/clack/freeze-reports/`
- Windows: `%AppData%\clack\freeze-reports\`

Each `freeze-<date>-<time>.txt` holds the Clack version, what the window was doing (layout, posted work or the GPU submit), your `ui.json` settings, and the stack of every goroutine, plus how long the freeze lasted once it ended. It has no message text, and tokens and cookies are redacted. On macOS, **Help → Show Freeze Reports** opens the folder in Finder (on every OS: **Preferences → Advanced → Show freeze reports**). To report a freeze, attach the newest file to a GitHub issue. The log also prints the report's path. Clack keeps the 10 most recent reports.

### Logs

Clack writes its log to stderr and to a file, even when you start it from the Dock or the Start menu:

- macOS: `~/Library/Logs/Clack/clack.log`
- Linux: `$XDG_STATE_HOME/clack/clack.log` (`~/.local/state/clack/clack.log`)
- Windows: `%LOCALAPPDATA%\Clack\logs\clack.log`

At 5 MB the file becomes `clack.log.1`, and the one before that becomes `clack.log.2`. Older files are deleted. Set `CLACK_LOG_DIR` to keep the logs somewhere else. The file has no message text or search queries. Tokens, cookies and token-carrying URL parameters are redacted, and so is everything after `?` in a `slack://` link. If the folder can't be written, Clack prints one warning and logs to stderr only.

**Preferences → Advanced → Troubleshooting** has **Open logs folder** and **Copy diagnostics**. On macOS they are also in the Help menu. Copy diagnostics puts a short summary on the clipboard for a bug report: version, OS, plugin ids, which components plugins patch, and the last 200 log lines.

### If Clack won't open

A Clack that freezes or crashes at launch can still update itself from a terminal:

```sh
clack --update            # find the newest release, ask, download, verify, install
clack --update --check    # only say whether there is a newer one
clack --update --yes      # don't ask
```

The binary to run depends on how you installed Clack:

| install | command |
|---|---|
| macOS | `/Applications/Clack.app/Contents/MacOS/clack --update` |
| Windows installer | `Start-Process -Wait -NoNewWindow "$env:LOCALAPPDATA\Programs\Clack\Clack.exe" -ArgumentList '--update'` (PowerShell) |
| Windows portable | the same, with the path of your `Clack.exe` |
| AppImage | `./Clack-<ver>-x86_64.AppImage --update` |
| .deb / .rpm | `clack --update` |

It opens no window and doesn't read your settings (`ui.json`), your sign-in or your workspace theme, so whatever is freezing the app stays out of it, and it sends nothing to Slack. It picks the release the same way the in-app check does. It downloads the package for your install from GitHub only, checks it against the release's `SHA256SUMS` and prints each step. Then it installs the package the same way **Install and restart** does: it replaces `Clack.app` or the AppImage in place, runs the Windows installer silently, swaps the portable `Clack.exe`, or runs `apt`/`dnf` behind a password prompt. A Windows install finishes a moment after the command exits, because Windows can't replace a running `Clack.exe`. Start Clack yourself afterwards. The exit code is 0 when Clack was updated or was already up to date, and 1 with an `update failed:` message otherwise (for example a checksum mismatch, no network, or an install type it can't update).

## Build

Every push runs gofmt, vet and the tests in GitHub Actions, then builds Linux (cgo; Gio needs Wayland/X11/GL headers), Windows (cross-compiled, no cgo) and macOS binaries. A `v*` tag runs `.github/workflows/release.yml`, which builds the packages listed under [Install](#install) (the macOS app as a universal arm64 + x86-64 binary, the Windows installer with Inno Setup, the `.deb`/`.rpm` with nfpm, the AppImage with appimagetool), installs and checks them on the CI runners, and publishes them with `SHA256SUMS` as a GitHub Release. Tags with a `-` (`v0.4.0-rc.1`) become pre-releases. The packaging files are in `packaging/`.

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

- **One cache directory per account, and nothing outside it.** Like Slack's desktop client, which keeps its store in IndexedDB so it can start before the network answers, Clack keeps a boot snapshot in the OS's cache folder (`~/Library/Caches/Clack`, `~/.cache/clack`, `%LocalAppData%\Clack`), one directory per workspace and user (`v1/<team>-<user>/`). It holds the sidebar (conversations, sections, unread counts), your prefs and theme, the user directory and custom emoji, avatar, emoji and message-thumbnail images (at most 384 MB), and the newest 50 messages of the conversation you had open. Every file is readable by you only (0600). **Signing out deletes the directory.**
- Every other message, thread, draft, file and rendered document lives only in memory for the lifetime of the process. Logs carry method names, ids, counts and timings only.
- Full files from `files.slack.com` are fetched into a memory-only cache and never saved; only Slack's thumbnails (`files-tmb`) reach the account's image cache.
- Clack makes the same kind of requests the official client makes: on demand, rate-limited, no crawling.

## License

MIT. See [LICENSE](LICENSE). Third-party components and their licenses are listed in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
