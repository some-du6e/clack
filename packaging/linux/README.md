# Linux packaging

`release.yml` builds Clack on Ubuntu 22.04 (glibc 2.35; the binary needs
glibc 2.34 or newer) for x86-64 and arm64 and packages it three ways:

- `clack_<ver>_<arch>.deb` and `clack-<ver>.<arch>.rpm`, made with
  [nfpm](https://nfpm.goreleaser.com) from `nfpm.yaml`
- `Clack-<ver>-<arch>.AppImage`, made with
  [appimagetool](https://github.com/AppImage/appimagetool) from an AppDir with
  `AppRun`, `clack.desktop` and the icons

Each package installs:

| file | |
|---|---|
| `/usr/bin/clack` | the binary |
| `/usr/share/applications/clack.desktop` | menu entry, `slack://` handler (`MimeType=x-scheme-handler/slack`) |
| `/usr/share/icons/hicolor/<N>x<N>/apps/clack.png` | 16, 24, 32, 48, 64, 128, 256, 512 |
| `/usr/share/icons/hicolor/scalable/apps/clack.svg` | |

The desktop file and icon are named `clack` because Gio's `app.ID` is
`clack`, which sets the X11 `WM_CLASS` and Wayland `app_id`. The post-install
and post-remove scripts refresh the desktop and icon caches. They don't make
Clack the system-wide `slack://` default. A user can do that with
`xdg-mime default clack.desktop x-scheme-handler/slack`.

CI installs the `.deb` on clean Debian 12, Ubuntu 22.04 and Ubuntu 24.04
containers and the `.rpm` on Fedora 41 and 42, then checks that
`clack -version` runs and every library below is present.

## Runtime dependencies

Clack links these libraries (`readelf -d`): libEGL, libwayland-client,
libwayland-cursor, libwayland-egl, libxkbcommon, libxkbcommon-x11, libX11,
libX11-xcb, libXcursor, libXfixes and libc. Gio loads libGLESv2 at runtime
with `dlopen`, and libvulkan when it's available. You also need a Mesa (or
vendor) EGL/GLES driver; desktop installs already have one.

| | Debian 12+ / Ubuntu 22.04+ | Fedora | Arch |
|---|---|---|---|
| Wayland | `libwayland-client0 libwayland-cursor0 libwayland-egl1` | `libwayland-client libwayland-cursor libwayland-egl` | `wayland` |
| keyboard | `libxkbcommon0 libxkbcommon-x11-0` | `libxkbcommon libxkbcommon-x11` | `libxkbcommon libxkbcommon-x11` |
| X11 | `libx11-6 libx11-xcb1 libxcursor1 libxfixes3` | `libX11 libX11-xcb libXcursor libXfixes` | `libx11 libxcursor libxfixes` |
| EGL + GLES | `libegl1 libgles2` | `libglvnd-egl libglvnd-gles` | `libglvnd mesa` |
| Vulkan (optional) | `libvulkan1` | `vulkan-loader` | `vulkan-icd-loader` |

The `.deb` and `.rpm` depend on all of these and recommend the Vulkan loader.
The AppImage bundles only Clack itself, so on a minimal system install the
packages above first, e.g.:

```sh
sudo apt install libwayland-client0 libwayland-cursor0 libwayland-egl1 libxkbcommon0 libxkbcommon-x11-0 libx11-6 libx11-xcb1 libxcursor1 libxfixes3 libegl1 libgles2
sudo dnf install libwayland-client libwayland-cursor libwayland-egl libxkbcommon libxkbcommon-x11 libX11 libX11-xcb libXcursor libXfixes libglvnd-egl libglvnd-gles
sudo pacman -S --needed wayland libxkbcommon libxkbcommon-x11 libx11 libxcursor libxfixes libglvnd mesa
```

Desktop notifications go over D-Bus to `org.freedesktop.Notifications`
(pure Go, no library needed).
