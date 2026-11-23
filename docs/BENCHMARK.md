# Clack vs Slack, Slick and Taut: startup benchmark

Measured on 2026-11-22 on one Windows PC. Every number is the median of 5 launches, each after a warm-up launch that isn't counted.

| Client | Time to window | Time to an open channel | Memory | Commit | CPU, first 30 s | Processes |
|---|---:|---:|---:|---:|---:|---:|
| **Clack** v0.1.0-alpha.140 | **212 ms** | **570 ms** | **128 MB** | **262 MB** | **3.8 s** | **1** |
| Slack 4.53.292 | 1059 ms | 3658 ms | 601 MB | 803 MB | 18.2 s | 7 |
| Slick 2.0.160, Snappy on | 1816 ms | 5025 ms | 805 MB | 1031 MB | 25.5 s | 6 |
| Taut 3.4.1, Faster Slack on | 2579 ms | (opened Activity) | 804 MB | 1069 MB | 30.6 s | 6 |

## In short

| Clack compared with | Slack | Slick | Taut |
|---|---|---|---|
| To a window | 5x faster | 8.6x faster | 12x faster |
| To an open channel | 6.4x faster | 8.8x faster | not measured |
| Memory | 4.7x less | 6.3x less | 6.3x less |
| CPU in the first 30 s | 4.8x less | 6.8x less | 8x less |

Clack runs as one process. Slack, Slick and Taut each run as 6 or 7.

## What each column means

- **Time to window:** from launch until the app's main window exists.
- **Time to an open channel:** from launch until the window title names a conversation, which is when the client has a channel open.
- **Memory:** private working set (Task Manager's "Memory" column), summed over all of the app's processes, 30 s after launch.
- **Commit:** committed memory over the same processes, at the same moment.
- **CPU, first 30 s:** CPU seconds that all of the app's processes used in their first 30 seconds.

## Setup

- **PC:** Ryzen 5 4500, 8 GB RAM, RTX 3050, Windows build 26300.
- **Clients:** each signed in to the same workspace, with the same account.
- **Order:** the apps were launched one at a time, each closed before the next started, in a rotating order.
- **Speed plugins:** Slick ran with its Snappy plugin and Taut with its Faster Slack plugin, both on by default.

## Caveats

- **Each app reopened its own last view.** Slack and Slick opened a channel. Clack opened one too (#apple). Taut reopened Activity, so it has no channel time.
- **One slow Clack run.** One of Clack's five channel runs took 3.1 s. The median doesn't count it.
- **One machine, one network, one day.** Your numbers will differ with hardware, network and the workspace.

A PowerShell script launched each app and timed it from its window title, so no message content was recorded.
