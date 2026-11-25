# Reports and crash reports

Clack can send two kinds of reports to its developers' Sentry project (Karim, 2026-11-24: "sentry for the app, hyper+r = report + comment"). Neither is a Slack feature, and neither goes to Slack. Only release builds can send them: `release.yml` sets the project's DSN at build time from the repo's `SENTRY_DSN` Actions secret, and only for a tag's build. Any other build (a local one, CI's tests, a release dry run) has no DSN and sends nothing.

## The report box (Hyper+R)

Hyper+R opens it: ⌃⌥⇧⌘R on macOS, Ctrl+Alt+Shift+R on Linux and Windows, with Super (Win) held too or not. GNOME takes Ctrl+Alt+Shift+R for its screen recorder, so there add Super. The box works whatever the inspect tool's gate says ([INSPECT.md](INSPECT.md)), but not over a running inspect session or on the sign-in screen.

Pressing the shortcut takes two frames, with nothing drawn:

1. The first frame is recorded as an inspect session records one.
2. The second reads what the pointer is over in that frame. Its report is the one an inspect pick of that element would make: the component, element and classes, the rect, the region and the page, the ids as one-way tags, the colours, the window, the experiments its code read and the patched components. With nothing under the pointer, it is the whole window's report.
3. That frame is shot whole, at the window's own pixels (`inspect.Tool.StartReport`).

Then the box opens. It is an SK modal drawn the way `/feedback`'s dialog is (`internal/ui/popups/report_box.go`), and it holds:

- **What happened?**: the comment, up to 2,000 characters, with a hint not to include anything private.
- **What's sent with it**: the report's rows, exactly as they'll be sent.
- **Attach a screenshot of the window**: off until ticked, because the shot shows everything on screen. Once it's ticked, a thumbnail shows what will be sent, and **Blur parts of it** opens the inspect review over the shot (`inspect.Tool.ReviewShot`). Rects dragged there are blurred out, P draws, ⌫ undoes. Enter brings the blurred shot back to the box, and Esc leaves the shot as it was. Nothing of what was under a blurred rect is kept ([INSPECT.md](INSPECT.md), "Blurring before it's copied").

Nothing leaves before **Send**: not on open, not while typing. Send posts one envelope to Sentry. The envelope holds a user feedback item, whose message is the comment, with the rows as the `report` context and the component, region and screenshot flag as tags, plus the screenshot as `window.png` when it's ticked. A failed send keeps the box and the comment, and says why.

In a build without the DSN the box still opens and shows what a report would hold, says it can't be sent from this build, and Send stays disabled.

## Crash reports

**Preferences > Clack > Send crash reports** is off by default. The row is greyed in a build that can't send.

Clack already keeps its crash records. The runtime's crash output goes to `crash.log`, and on Windows the native capture's record goes there too (applog). The run record says how each run ended, and Windows' Application Error event is looked up for a run that ended without saying why. Ten seconds into a start, `cmd/clack` hands the previous run's crash to `ui.App.ReportCrash`:

- **Off:** nothing is sent. Nothing is kept for later either: `crash.log` starts afresh at every start, so turning the pref on later sends no old crash.
- **On:** one event is sent, `sentry.CrashEvent`:
  - its type: `panic`, `fatal error` or `native exception`;
  - its message, with what Go quotes in it (a `%q` value) left out, Slack ids and message timestamps replaced, and emails replaced;
  - the crashing goroutine's frames: the function, the file (module-relative in a release build) and the line, with no arguments and no addresses;
  - the crashed run's header, its exit line and Windows' record.

  A send that fails (offline) is logged, and the crash is still in `clack.log`.

## What a report never holds

Every string an event carries goes through `sentry.Scrub` before it's sent: `applog.Redact`'s rules (xox* tokens, the d, d-s and lc cookies, `token=` and the other secrets in query strings, Cookie and Authorization headers, Bearer credentials, secrets in JSON, the query of a `slack://` link), then the user's home directory as `~` (either separator, in any case on Windows and macOS) and any other home's user name as `<user>`. The inspect report never collects message text, names, topics or search queries, and its ids are salted one-way tags. The event has no user, hostname, request or breadcrumbs field at all.

Sentry sees the connection's IP address, as any server does. The screenshot shows what was on screen, which is why it's off until ticked and can be blurred first.

## How it's built

`internal/sentry` is a small envelope sender over Go's own transport: sentry.io isn't a Slack host, and nothing of Slack's goes with a report (`internal/slack/tlsfp`'s scan lists `sentry.go`). It doesn't use getsentry/sentry-go. That module would add a dependency (go.mod, THIRD_PARTY_NOTICES) and its automatic capture: a global hub, breadcrumbs, the hostname as `server_name`, integrations. The small sender sends only the events it's handed. It takes https DSNs, and plain http only to a loopback address, for the tests' fake.

Tests send to `internal/sentry/sentrytest`, a fake envelope endpoint on loopback, never to Sentry. flake-check's `report-sentry` job runs them all:

- `internal/sentry`: DSN parsing; the feedback envelope; failures; scrubbing; crash parsing; release.yml's `-X` path.
- `internal/applog`: `PreviousCrash`.
- `internal/ui`:
  - `TestReportBoxHyperR`: the rows; nothing sent before Send; one feedback with no message text, name, token, cookie, id or home path.
  - `TestReportBoxScreenshot`, `TestReportBoxBlur`, `TestReportBoxUnavailable`, `TestReportBoxSendFails`.
  - `TestCrashReportsOptIn` (off sends nothing, ever; on sends one scrubbed event) and `TestCrashReportsRow`.
