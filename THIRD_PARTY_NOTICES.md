# Third-party notices

Clack is MIT licensed (see `LICENSE`). It includes or links the following third-party software, each under its own license.

| Component | Used for | License |
|---|---|---|
| [Gio](https://gioui.org) (`gioui.org`, `gioui.org/x`, `gioui.org/shader`; `gioui.org` v0.10.3 is built from a patched copy in `third_party/gioui.org`, see its `CLACK_PATCHES.md`) | GPU UI toolkit | Unlicense OR MIT |
| [go-text/typesetting](https://github.com/go-text/typesetting) | text shaping | Unlicense OR BSD-3-Clause |
| [Lato](https://www.latofonts.com) fonts (`assets/fonts`) | UI font | SIL Open Font License 1.1 (`assets/fonts/OFL.txt`) |
| [Roboto Mono](https://github.com/googlefonts/robotomono) (`internal/woff2/testdata`: the Cyrillic subset as Slack's CDN serves it, test data only) | woff2 decoder tests | Apache-2.0 (`internal/woff2/testdata/LICENSE-RobotoMono.txt`) |
| [emoji-data](https://github.com/iamcal/emoji-data) (`internal/emoji/standard.json`) | standard emoji table | MIT |
| [coder/websocket](https://github.com/coder/websocket) | realtime gateway | ISC |
| [uTLS](https://github.com/refraction-networking/utls) (`refraction-networking/utls`, a fork of Go's crypto/tls) | the TLS handshake toward Slack (`internal/slack/tlsfp`) | BSD-3-Clause |
| [andybalholm/brotli](https://github.com/andybalholm/brotli) | WOFF2 web fonts (`internal/woff2`, Appearance > Font) and uTLS certificate compression | MIT |
| [klauspost/compress](https://github.com/klauspost/compress) (zstd) | linked by uTLS for certificate compression | BSD-3-Clause |
| [wazero](https://github.com/tetratelabs/wazero) | WebAssembly runtime for plugins | Apache-2.0 |
| [QuickJS-NG](https://github.com/quickjs-ng/quickjs) (compiled to `internal/jsrt` wasm) | JS engine for plugins | MIT (`internal/jsrt/vendor/LICENSE.quickjs-ng`) |
| [React](https://react.dev), react-reconciler, scheduler (`internal/jsrt/vendor`) | plugin UI rendering | MIT (`internal/jsrt/vendor/LICENSE.*`) |
| Unicode data (`internal/jsrt/vendor`) | text processing | Unicode License (`internal/jsrt/vendor/LICENSE.unicode`) |
| [Pion WebRTC](https://github.com/pion/webrtc) and its modules (dtls, ice, interceptor, rtp, rtcp, sctp, sdp, srtp, stun, turn, transport, mdns, datachannel, logging, randutil) | huddle media | MIT |
| [goh264](https://github.com/thesyncim/goh264) | H.264 decoding (huddle screen shares) | LGPL-2.1 |
| [opus-go](https://github.com/kazzmir/opus-go) (libopus) | Opus audio | BSD-3-Clause |
| [malgo](https://github.com/gen2brain/malgo) (miniaudio) | audio devices | Unlicense (miniaudio: public domain / MIT-0) |
| [rnnoise-go](https://github.com/VoiceBlender/rnnoise-go) v0.3.0, a pure-Go port of [RNNoise](https://gitlab.xiph.org/xiph/rnnoise), with its trained weights (`third_party/rnnoise-go`, go.mod lowered to Go 1.24, see its `CLACK_PATCHES.md`) | huddle microphone noise suppression, and the voice detection behind its gain control | BSD-3-Clause (`third_party/rnnoise-go/LICENSE`, `COPYING.rnnoise`; weights: `model/LICENSE.weights`) |
| [godbus/dbus](https://github.com/godbus/dbus) | Linux notifications, screen-lock events | BSD-2-Clause |
| [wlynxg/anet](https://github.com/wlynxg/anet) | network interfaces | BSD-3-Clause |
| [google/uuid](https://github.com/google/uuid) | ids | BSD-3-Clause |
| Go standard library and `golang.org/x/*` | everything | BSD-3-Clause |

## LGPL-2.1 (goh264)
goh264 is linked statically. Under LGPL-2.1 §6 you may relink Clack against a modified goh264: Clack's complete source is public at https://github.com/some-du6e/clack. Replace the module with `go mod edit -replace github.com/thesyncim/goh264=<your copy>` and build with `go build ./cmd/clack`.

## Taut and Slick plugins
Clack does **not** include Taut or Slick code. The built-in plugin Library downloads plugins, which are GPL-3.0 and built from the upstream projects ([Taut](https://github.com/jeremy46231/taut), [Slick](https://github.com/3kh0/slick)), at the user's request, from this repository's `plugins` pre-release. That release carries their licenses and the upstream commits each bundle was built from.

## Slack
Clack is an unofficial client and isn't affiliated with Slack Technologies or Salesforce. "Slack" is their trademark. Clack ships no Slack assets. Emoji images, avatars and file previews load at runtime from Slack's CDNs, as the official client does. So do the web fonts of Preferences > Appearance > Font (Roboto Mono, Apache-2.0; Noto Sans, SIL OFL 1.1; Atkinson Hyperlegible Next, the Braille Institute's license; OpenDyslexic, Bitstream's Vera license with the author's Creative Commons changes), which are kept in the user's cache directory and not redistributed. Spellcheck dictionaries are fetched the same way: Chromium's `.bdic` builds of the Hunspell dictionaries from Slack's dictionary CDN (`spellcheck.slack-edge.com/dictionaries_v2/`), into the user's cache directory. Each dictionary keeps its own licence (the English ones: SCOWL's permissive licence; others GPL, LGPL or MPL per language, as listed in Chromium's `third_party/hunspell_dictionaries`); Clack doesn't ship any. `internal/spell/hunspell` reads Chromium's BDict format (`third_party/hunspell/google/bdict.h`, BSD-3-Clause) with new code; it contains no Hunspell source. The huddle lounge music (Preferences > Audio & video > "When alone in a huddle… Play music": Smooth Jazz, Subway Music and Slack's other songs) is Slack's: Clack fetches the file from Slack's public CDN at runtime, without any cookie, as the official client does, and keeps it in the user's cache directory. It isn't redistributed and carries no licence for that.
