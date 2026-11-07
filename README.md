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

**Installer:** run `Clack-<ver>-windows-amd64-setup.exe`. It installs for your user only (no admin prompt) into `%LocalAppData%\Programs\Clack`, adds a Start Menu entry and, unless you untick it, makes Clack open `slack://` links. A desktop shortcut is optional. Uninstall from **Settings → Apps**. Running a newer installer upgrades in place.

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

## Sign in

Clack signs in with the token and cookies of your own signed-in Slack session, the same values other third-party Slack clients use. Start Clack (or run `clack login`) and fill in your workspace, the `xoxc-` **token**, the **`d` cookie** (`xoxd-…`) and, optionally, the org token (Enterprise Grid workspaces such as Hack Club) and the `d-s` cookie. Clack only checks them with `auth.test`: it never loads Slack web pages with your cookie and never calls a sign-out API, so your browser session stays signed in. They're saved to `~/.config/clack/creds.json` (mode 600). "Sign out on this device" deletes that file and nothing else.

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

- **Message contents are never written to disk.** Messages, threads, drafts, files and rendered documents live only in memory for the lifetime of the process, as in any Slack client. Logs carry method names, ids, counts and timings only.
- Only metadata is cached on disk: the user directory, the custom emoji map, usergroups and your prefs (`~/.config/clack/cache/meta.json`), plus avatar and emoji images.
- Files and thumbnails from `files.slack.com` are fetched into a memory-only cache and never saved.
- Clack makes the same kind of requests the official client makes: on demand, rate-limited, no crawling.
