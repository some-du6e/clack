# Offline detection

How Clack decides the client is offline or Slack's service is down, as Slack's web client does (build 889ac449a32d). The connection states the user sees (the composer line, the DegradedBanner, the offline lists) are in [CONNECTION.md](CONNECTION.md).

## The two flags

The connectivity store (client-boot C4fL) has `clientOnline` and `serviceOnline`. Every `setClientOnline` stamps `lastClientConnection`, whether or not the value changed. `serviceOnline` doesn't stamp it.
- `isClientOnline` is `clientOnline`: `App.ClientOnline`.
- `isOnline` is both: `App.membersOnline`.
- `wasRecentlyOnline` is `isOnline`, or less than 10 s since `lastClientConnection`: `App.RecentlyOnline`.

Each stamp emits `client_online`, so the views re-read the grace from its new start.

## What sets them

| Source | Slack | Clack |
|---|---|---|
| The window's offline and online events (`navigator.onLine`) | boot-render upsertClientOfflineStatus (500 ms throttle), upsertClientOnlineStatus | The interface watcher (core/online.go). It sets `clientOnline` only when its own reading changes. |
| Ten API tries in a row with no answer, or `clientOnline` already false | 7quX `_passedErrorThreshold`, then setClientOffline and awaitNetworkReconnection | internal/slack/apihealth.go counts; core/connectivity.go `awaitNetwork` |
| Ten answers of 500 or more in a row | 7quX, then setServiceOffline and awaitServiceReconnection | the same; `awaitService` |
| The socket, after a failed connect while offline | RpHI WAIT_FOR_CONNECTIVITY awaits awaitNetworkReconnection | `RTM.AwaitOnline`, which waits on `awaitNetwork` and clears the socket's failure count |

## Counting (7quX `_countError`)

- An answer of 500 or more adds a service error and resets the client errors.
- No HTTP answer (CLIENT_UNKNOWN, status 0) adds a client error and resets the service errors.
- A success, a timeout, a cancel, a 429 or any other status reset both.
- Every try counts, the queue's retries included. A request the backoff guard holds back never goes out, so it counts nothing. Chromium's resend of a read whose pooled connection was dead is part of the same try.

## The waits

Each wait runs ConnectivityManager rounds (RFm0): up to 10 tries, each after the last one's `retryAfter` (at most 300 s), and 5 s between rounds.
- **The network** (d55I, 2Aob): GET `https://a.slack-edge.com/f70ec/img/test_cdn_pixel.gif?q=…` and the same on `b.`, through `slack.PublicHTTP` with no credentials. The first to load wins.
  - While the watcher says offline, a try fails at once. Every 10th try still loads, until the trust check has run once, and so does every 100th.
  - A load while the watcher says offline runs the trust check. If the watcher still says offline after a second load, its reading counts as online for the rest of the session.
  - A failed round sets `clientOnline` false again, which stamps it. A success sets it true and resets the counts.
- **The service** (3h1+, aZ2/): POST `https://www.slack.com/api/api.test`.
  - Slack's request carries `_x_reason=api_connectivity_test`. Clack sends none of Slack's `_x_*` metadata (docs/PLUGINS-api-compat.md) and no credentials, since api.test needs none.
  - A failed round sets `serviceOnline` false again without a stamp. An answer sets both flags true.

## Not ported

- **Holding calls while offline.** The queue sends nothing while `!isOnline` (7quX `_tick`). Clack's calls still go out. This is the next step.
- **The `_x_mode` metadata** (online, degraded or offline on every call). See the line on `_x_*` above.

## Tests

On internal/fakeslack with `RouteSlackHosts`: the pixel (`SetFault("test_cdn_pixel", …)`) and api.test (`SetFault("api.test", …)`) are faultable.
- internal/slack TestAPIErrorCountsLikeSlack and TestRTMWaitForConnectivityLikeSlack.
- internal/core TestAPIFailuresTakeClientOfflineLikeSlack and TestServiceErrorsTakeServiceOfflineLikeSlack.
