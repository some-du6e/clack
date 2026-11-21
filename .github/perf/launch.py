#!/usr/bin/env python3
"""Launch timing: Clack started the way a user starts it, against fakeslackd.

usage: launch.py --app <Clack.app | clack binary> --fake <fakeslackd> --out <dir>
                 [--library <dir>] [--warm N] [--purged N] [--extra]
                 [--budget-ms N] [--budget-mark real-frame]

The app is a -tags perfharness build (it accepts fakeslackd on 127.0.0.1).
macOS launches the bundle through LaunchServices (`open`), as the Dock and
Finder do, so the Dock bounces until it finishes launching; Linux runs the
binary on $DISPLAY (Xvfb), Windows the .exe. Clack keeps everything where a
user's Clack keeps it (the OS's config, cache and log folders, no
CLACK_HOME): the runner's account is fresh, and the script empties those
folders before it starts.

Every milestone is the startup timeline's (internal/applog/startup.go),
in ms since the OS started the process (applog.ProcessStart): main (the
loader, the runtime and every package's init done), app-main (the main
thread enters AppKit's loop), launched (macOS finished launching: the
Dock's bounce ends), window-shown, first-frame (the boot placeholder or
the client), fonts, cache-read, data-ready, reveal, real-frame (the first
frame of the client itself, from the cache on a warm start). `exec` is
how long the OS took from the launch call to the process (LaunchServices'
part on macOS).

Runs, in one account: cold (nothing of Clack's on disk but creds.json and
ui.json's route, and on macOS/Linux the OS's file cache emptied first),
then N warm (what a normal start has: meta.json, the boot data, the
engine's compile cache, ui.json's theme and plugin slots), then N purged
(warm, with the OS's file cache emptied: the first launch after a reboot).
--extra adds two direct runs: GODEBUG=inittrace=1 (every package's init)
and CLACK_CPUPROFILE (the first 3 s).

With --budget-ms, the median of the warm runs' --budget-mark must be
within it, and every warm run must reach it: else exit 1.
"""
import argparse, json, os, platform, re, shutil, signal, statistics, subprocess, sys, time

sys.dont_write_bytecode = True

THEME = '{"primary":{"palette":"jade"},"highlight1":{"palette":"jade"},"highlight2":{"palette":"aubergine"},"important":{"palette":"jade"},"brightness":6,"sidebarInverted":true,"useCustomHex":false}'
ROUTE = os.environ.get("ROUTE", "C0FAKE00000")
SYS = platform.system()  # Darwin, Linux, Windows
COLS = ["exec", "main", "single-instance", "creds", "ui-new", "attached", "app-main", "run", "appkit-app", "appkit-run", "will-finish-launching",
        "launched", "window", "event-loop", "gio-init", "menu-bar", "window-create", "window-view", "window-ns", "config",
        "window-shown", "frame-event", "gpu-context", "gpu", "first-frame", "presented", "fonts", "cache-read",
        "sidebar-cached", "data-ready", "first-text", "sidebar", "reveal", "real-frame", "js-runtime", "taut-started",
        "slick-started", "plugins", "messages", "core-ready", "sidebar-live", "settled"]


def dirs():
    home = os.path.expanduser("~")
    if SYS == "Darwin":
        return (os.path.join(home, "Library", "Application Support", "clack"), os.path.join(home, "Library", "Caches", "Clack"),
                os.path.join(home, "Library", "Logs", "Clack"))
    if SYS == "Windows":
        appdata, local = os.environ["APPDATA"], os.environ["LOCALAPPDATA"]
        return os.path.join(appdata, "clack"), os.path.join(local, "Clack"), os.path.join(local, "Clack", "logs")
    cfg = os.environ.get("XDG_CONFIG_HOME") or os.path.join(home, ".config")
    cache = os.environ.get("XDG_CACHE_HOME") or os.path.join(home, ".cache")
    state = os.environ.get("XDG_STATE_HOME") or os.path.join(home, ".local", "state")
    return os.path.join(cfg, "clack"), os.path.join(cache, "clack"), os.path.join(state, "clack")


def binary(app):
    if SYS == "Darwin" and app.endswith(".app"):
        return os.path.join(app, "Contents", "MacOS", "clack")
    return app


def now_ms():
    return time.time() * 1000


def purge():
    """Empty the OS's file cache (sudo without a password on the runners)."""
    try:
        if SYS == "Darwin":
            subprocess.run(["sudo", "-n", "purge"], check=True, timeout=120)
        elif SYS == "Linux":
            subprocess.run(["sync"], check=True, timeout=60)
            subprocess.run(["sudo", "-n", "sh", "-c", "echo 3 > /proc/sys/vm/drop_caches"], check=True, timeout=60)
        else:
            return False
        return True
    except Exception as e:  # noqa: BLE001
        print(f"purge: {e}", flush=True)
        return False


def alive(pid):
    if SYS == "Windows":
        out = subprocess.run(["tasklist", "/FI", f"PID eq {pid}", "/NH"], capture_output=True, text=True).stdout
        return str(pid) in out
    try:
        os.kill(pid, 0)
        return True
    except ProcessLookupError:
        return False
    except PermissionError:
        return True


def stop(pid, proc=None):
    if pid is None and proc is None:
        return
    if proc is not None:
        pid = proc.pid
    try:
        if SYS == "Windows":
            subprocess.run(["taskkill", "/F", "/PID", str(pid)], capture_output=True, timeout=30)
        else:
            os.kill(pid, signal.SIGTERM)
    except Exception:  # noqa: BLE001
        pass
    for _ in range(100):
        if proc is not None and proc.poll() is not None:
            return
        if proc is None and not alive(pid):
            return
        time.sleep(0.1)
    try:
        if SYS != "Windows":
            os.kill(pid, signal.SIGKILL)
    except Exception:  # noqa: BLE001
        pass


def leftovers(binpath):
    """Clack processes from an earlier run (none expected)."""
    if SYS == "Windows":
        subprocess.run(["taskkill", "/F", "/IM", os.path.basename(binpath)], capture_output=True)
        return
    r = subprocess.run(["pgrep", "-f", binpath], capture_output=True, text=True)
    for p in r.stdout.split():
        if int(p) != os.getpid():
            print(f"  a Clack from before is still running ({p}): stopping it", flush=True)
            stop(int(p))


def parse_timeline(text):
    t = {}
    m = re.search(r"startup: timeline settled=(\d+)ms(.*)", text)
    if m:
        t["settled"] = int(m.group(1))
        for k, v in re.findall(r" ([a-z-]+)=(\d+)ms", m.group(2)):
            t.setdefault(k, int(v))
        r = re.search(r"relayouts=(\d+)", m.group(2))
        if r:
            t["relayouts"] = int(r.group(1))
    p = re.search(r"startup: process (\d+) started at unix ms (\d+), main after", text)
    if p:
        t["pid"], t["start_ms"] = int(p.group(1)), int(p.group(2))
    rv = re.search(r"debug: startup \+\d+ms reveal \(([^)]*)\)", text)
    if rv:
        t["reveal_why"] = rv.group(1).split(";")[0]
    return t


def one(name, a, cfg, logdir, out, mode="open", env=None, settle_wait=60.0, min_secs=0.0, purged=False, shots=(), until=""):
    binpath = binary(a.app)
    leftovers(binpath)
    for f in ("clack.log", "clack.log.1", "clack.log.2"):
        try:
            os.remove(os.path.join(logdir, f))
        except FileNotFoundError:
            pass
    did_purge = purge() if purged else False
    so = open(os.path.join(out, f"{name}.stdout"), "wb")
    se = open(os.path.join(out, f"{name}.stderr"), "wb")
    e = dict(os.environ)
    e.update(env or {})
    proc = None
    t0 = now_ms()
    if SYS == "Darwin" and mode == "open":
        # LaunchServices, as the Dock and Finder launch it
        r = subprocess.run(["open", a.app], timeout=60, stdout=so, stderr=se)
        if r.returncode != 0:
            print(f"  {name}: open exited {r.returncode}", flush=True)
    else:
        proc = subprocess.Popen([binpath], env=e, stdout=so, stderr=se, stdin=subprocess.DEVNULL)
    log = os.path.join(logdir, "clack.log")
    text = ""
    deadline = time.time() + settle_wait
    shots = list(shots)
    while time.time() < deadline:
        try:
            with open(log, encoding="utf-8", errors="replace") as f:
                text = f.read()
        except FileNotFoundError:
            text = ""
        if shots and now_ms() - t0 > shots[0]:  # the dock run only: a capture takes CPU from the launch
            subprocess.run(["screencapture", "-x", os.path.join(out, f"{name}-{shots.pop(0)}ms.png")], timeout=30, capture_output=True)
        if "startup: timeline settled" in text:
            break
        if proc is not None and proc.poll() is not None:
            break
        time.sleep(0.1)
    while time.time() - t0 / 1000 < min_secs:
        time.sleep(0.5)
    if until:  # the cold run: until the boot data is on disk (20 s after boot), for the warm runs
        end = time.time() + 90
        while time.time() < end and until not in text:
            time.sleep(0.5)
            try:
                with open(log, encoding="utf-8", errors="replace") as f:
                    text = f.read()
            except FileNotFoundError:
                pass
        if until not in text:
            print(f"  {name}: no {until!r} in the log after 90 s", flush=True)
        time.sleep(1)  # the rest of what the save writes
    t = parse_timeline(text)
    stop(t.get("pid") if proc is None else None, proc)
    try:
        with open(log, encoding="utf-8", errors="replace") as f:
            text = f.read()
    except FileNotFoundError:
        pass
    with open(os.path.join(out, f"{name}.log"), "w", encoding="utf-8") as f:
        f.write(text)
    so.close()
    se.close()
    t = parse_timeline(text)
    if "start_ms" in t:
        t["exec"] = round(t["start_ms"] - t0)
    t["name"], t["purged"] = name, did_purge
    if "settled" not in t:
        t["missing"] = True
        print(f"  {name}: no timeline line (log {len(text)} bytes)", flush=True)
    return t


def inits(path):
    """GODEBUG=inittrace=1's lines: (total ms, top packages)."""
    rows = []
    try:
        for l in open(path, encoding="utf-8", errors="replace"):
            m = re.match(r"init (\S+) @([\d.]+) ms, ([\d.]+) ms clock, (\d+) bytes, (\d+) allocs", l)
            if m:
                rows.append((m.group(1), float(m.group(2)), float(m.group(3)), int(m.group(4)), int(m.group(5))))
    except FileNotFoundError:
        pass
    return rows


def cell(t, c):
    v = t.get(c)
    return "—" if v is None else f"{v:,}"


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--app", required=True)
    ap.add_argument("--fake", required=True)
    ap.add_argument("--out", required=True)
    ap.add_argument("--library", default="")
    ap.add_argument("--warm", type=int, default=5)
    ap.add_argument("--purged", type=int, default=0)
    ap.add_argument("--cold-secs", type=float, default=12.0)  # plugin slots are learned within 10 s
    ap.add_argument("--extra", action="store_true")
    ap.add_argument("--screenshot", action="store_true")
    ap.add_argument("--latency", default="150ms")
    ap.add_argument("--budget-ms", type=int, default=0)
    ap.add_argument("--budget-mark", default="real-frame")
    ap.add_argument("--label", default=SYS)
    a = ap.parse_args()
    if os.environ.get("GITHUB_ACTIONS") != "true":
        sys.exit("launch.py empties this account's Clack folders (credentials included): it runs on CI runners only")
    a.app = os.path.abspath(a.app)
    os.makedirs(a.out, exist_ok=True)
    cfg, cache, logdir = dirs()
    for d in (cfg, cache, logdir):
        shutil.rmtree(d, ignore_errors=True)
    os.makedirs(cfg, exist_ok=True)

    fake = subprocess.Popen([a.fake, "-addr", "127.0.0.1:8931", "-creds", os.path.join(cfg, "creds.json"),
                             "-latency", a.latency, "-img-latency", "40ms", "-ia-theme", THEME],
                            stdout=open(os.path.join(a.out, "fakeslackd.log"), "wb"), stderr=subprocess.STDOUT)
    try:
        for _ in range(300):
            if os.path.exists(os.path.join(cfg, "creds.json")):
                break
            time.sleep(0.1)
        else:
            sys.exit("fakeslackd wrote no creds.json")
        with open(os.path.join(cfg, "ui.json"), "w") as f:
            json.dump({"route": "c/" + ROUTE, "last_channel": ROUTE, "realtime": False}, f)
        plugins = seed_plugins(a.library, cfg)
        print(f"{a.label}: plugins {plugins}; config {cfg}", flush=True)

        runs = [one("cold", a, cfg, logdir, a.out, min_secs=a.cold_secs, purged=True, until="cache: boot data saved")]
        for i in range(a.warm):
            runs.append(one(f"warm{i + 1}", a, cfg, logdir, a.out))
        for i in range(a.purged):
            runs.append(one(f"purged{i + 1}", a, cfg, logdir, a.out, purged=True))
        if a.screenshot and SYS == "Darwin":
            # can a capture see the Dock (its bounce)? CI's TCC may give the desktop alone
            runs.append(one("dock", a, cfg, logdir, a.out, shots=(150, 400, 700)))
        extra = {}
        if a.extra:
            runs.append(one("inittrace", a, cfg, logdir, a.out, mode="direct", env={"GODEBUG": "inittrace=1"}))
            extra["inits"] = inits(os.path.join(a.out, "inittrace.stderr"))
            prof = os.path.join(a.out, "cpu-warm.pprof")
            runs.append(one("cpuprofile", a, cfg, logdir, a.out, mode="direct", min_secs=4.5,  # the profile ends at 3 s
                            env={"CLACK_CPUPROFILE": prof, "CLACK_CPUPROFILE_SECS": "3"}))
            time.sleep(0.5)
    finally:
        fake.terminate()

    report(a, runs, extra, plugins, cache)
    if a.budget_ms:
        warm = [r for r in runs if r["name"].startswith("warm")]
        got = [r[a.budget_mark] for r in warm if a.budget_mark in r]
        if len(got) < len(warm) or not got:
            print(f"::error::{a.label}: {len(warm) - len(got)} of {len(warm)} warm launches never reached {a.budget_mark}")
            return 1
        med = statistics.median(got)
        if med > a.budget_ms:
            print(f"::error::{a.label}: warm launch to {a.budget_mark} took {med:.0f} ms (median of {got}), over the {a.budget_ms} ms budget")
            return 1
        print(f"{a.label}: warm launch to {a.budget_mark}: {med:.0f} ms (median of {got}), within the {a.budget_ms} ms budget")
    return 0


def seed_plugins(lib, cfg):
    """Karim-like plugins, as perf's startup.sh seeds them: Slick's ChatTabs and five Taut plugins."""
    if not lib or not os.path.exists(os.path.join(lib, "index.json")):
        return "none"
    tauts = ["HumanCount", "LastSeen", "WhoReacted", "NoTrack", "ShutUpSlackbot"]
    on = []
    os.makedirs(os.path.join(cfg, "taut", "plugins"), exist_ok=True)
    os.makedirs(os.path.join(cfg, "slick", "plugins"), exist_ok=True)
    for t in tauts:
        src = os.path.join(lib, f"taut-{t}.js")
        if os.path.exists(src):
            shutil.copy(src, os.path.join(cfg, "taut", "plugins", f"{t}.js"))
            on.append(t)
    with open(os.path.join(cfg, "taut", "config.json"), "w") as f:
        json.dump({"plugins": {t: {"enabled": True} for t in on}}, f)
    names = list(on)
    src = os.path.join(lib, "slick-ChatTabs.js")
    if os.path.exists(src):
        shutil.copy(src, os.path.join(cfg, "slick", "plugins", "ChatTabs.js"))
        main_js = os.path.join(lib, "slick-ChatTabs.main.js")
        if os.path.exists(main_js):
            shutil.copy(main_js, os.path.join(cfg, "slick", "plugins", "ChatTabs.main.js"))
        with open(os.path.join(cfg, "slick", "settings.json"), "w") as f:
            json.dump({"enabled": True, "plugins": {"ChatTabs": {"enabled": True}}}, f)
        names.insert(0, "ChatTabs")
    return " + ".join(names) if names else "none"


def report(a, runs, extra, plugins, cache):
    warm = [r for r in runs if r["name"].startswith("warm")]

    def med(c):
        v = [r[c] for r in warm if c in r]
        return statistics.median(v) if v else None
    marks = [c for c in COLS if any(c in r for r in runs)]
    marks += sorted({k for r in runs for k, v in r.items() if k not in marks and isinstance(v, int) and not isinstance(v, bool)
                     and k not in ("pid", "start_ms", "relayouts")})
    # in the order a warm launch reaches them (exec first)
    marks.sort(key=lambda c: (c != "exec", med(c) if med(c) is not None else 1e9))
    names = [r["name"] for r in runs]
    md = [f"## Launch timeline ({a.label})", "",
          f"Clack launched as a user launches it ({'Clack.app through LaunchServices: open' if SYS == 'Darwin' else 'the binary'}) "
          f"against fakeslackd ({a.latency} per API call), plugins: {plugins}. Each mark is ms since the OS started the "
          "process (internal/applog/startup.go); `exec` is from the launch call to the process start. `launched` is "
          "macOS finishing the launch (the Dock's bounce ends); `real-frame` is the first frame of the client itself, not "
          "the boot placeholder. cold: nothing of Clack's on disk and the OS file cache purged; warm: a normal start; "
          "purged: warm, with the OS file cache purged (a first launch after a reboot).", "",
          "| mark | " + " | ".join(names) + " | **warm median** |", "|" + "---|" * (len(names) + 2)]
    for c in marks:
        m = med(c)
        md.append(f"| {c} | " + " | ".join(cell(r, c) for r in runs) + f" | **{'—' if m is None else f'{m:,.0f}'}** |")
    md.append("| reveal waited for | " + " | ".join(r.get("reveal_why", "—") for r in runs) + " | |")
    if extra.get("inits"):
        rows = extra["inits"]
        tot = sum(r[2] for r in rows)
        last = max(r[1] + r[2] for r in rows)
        md += ["", f"Package inits (GODEBUG=inittrace=1): {len(rows)} packages, {tot:.1f} ms of init clock, the last done "
               f"{last:.1f} ms after the runtime started. Slowest:", "", "| package | ms | bytes | allocs |", "|---|---|---|---|"]
        for p, _, ms, b, n in sorted(rows, key=lambda r: -r[2])[:25]:
            md.append(f"| {p} | {ms:.2f} | {b:,} | {n:,} |")
    sizes = []
    for root, _, files in os.walk(cache):
        for fn in files:
            p = os.path.join(root, fn)
            if "img" in os.path.relpath(p, cache).split(os.sep):
                continue
            sizes.append((os.path.relpath(p, cache), os.path.getsize(p)))
    if sizes:
        md += ["", "<details><summary>the account's cache files after the runs</summary>", "", "```"]
        md += [f"{s:>10,}  {p}" for p, s in sorted(sizes)]
        md += ["```", "</details>"]
    text = "\n".join(md) + "\n"
    print(text, flush=True)
    with open(os.path.join(a.out, "launch.md"), "w") as f:
        f.write(text)
    with open(os.path.join(a.out, "launch.json"), "w") as f:
        json.dump({"label": a.label, "runs": runs, "inits": extra.get("inits", [])}, f, indent=1)
    if os.environ.get("GITHUB_STEP_SUMMARY"):
        with open(os.environ["GITHUB_STEP_SUMMARY"], "a") as f:
            f.write(text)


if __name__ == "__main__":
    sys.exit(main())
