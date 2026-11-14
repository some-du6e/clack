#!/usr/bin/env python3
"""The memory workflow's report (perf.yml's scale jobs).

usage:
  scale.py <out> <platform> <sha> <ref>   one platform's report: <out>/scale.md
                                          and <out>/scale-row.md (history)
  scale.py --combine <dir>... > report.md the platforms' reports, one page

<out>/<side>/scale/{prime,warm}/ hold the driver's files
(internal/ui/perfdrive.go): <stage>.mem.txt (as it was), <stage>.trimmed.txt
(after a GC and FreeOSMemory), <stage>.external.txt (read from outside the
process) and <stage>.heap.pprof. The heap profiles are read here directly
(gzipped protobuf), so the report needs no Go toolchain.

"Owner" is the first frame of an allocation's stack, from the allocation
up, that isn't the standard library or golang.org/x: the code that asked
for the memory (store.PutUsers rather than encoding/json).
"""
import collections
import datetime
import gzip
import os
import re
import sys

STAGES = ["users", "startup", "picker", "picker-all", "scroll", "channels", "idle", "gifs", "gifs-left"]
STAGE_NAMES = {"users": "cold start, 32k users loaded", "startup": "warm start, settled", "picker": "emoji picker open",
               "picker-all": "picker scrolled through", "scroll": "long channels scrolled", "channels": "40 more channels",
               "idle": "idle 10 min", "gifs": "#gif-wall's 300 GIFs scrolled, resting", "gifs-left": "#gif-wall left 10 s"}


# ---- pprof (profile.proto) ----

def _varint(b, i):
    r = s = 0
    while True:
        c = b[i]
        i += 1
        r |= (c & 0x7f) << s
        s += 7
        if c < 0x80:
            return r, i


def _fields(b):
    i, n = 0, len(b)
    while i < n:
        k, i = _varint(b, i)
        f, t = k >> 3, k & 7
        if t == 0:
            v, i = _varint(b, i)
            yield f, t, v
        elif t == 2:
            ln, i = _varint(b, i)
            yield f, t, b[i:i + ln]
            i += ln
        elif t == 1:
            yield f, t, int.from_bytes(b[i:i + 8], "little")
            i += 8
        elif t == 5:
            yield f, t, int.from_bytes(b[i:i + 4], "little")
            i += 4
        else:
            raise ValueError("wire type %d" % t)


def _packed(v, t):
    if t == 0:
        return [v]
    out, i = [], 0
    while i < len(v):
        x, i = _varint(v, i)
        out.append(x)
    return out


def load_profile(path):
    """[(stack leaf-first, {sample_type: value})]"""
    raw = open(path, "rb").read()
    if raw[:2] == b"\x1f\x8b":
        raw = gzip.decompress(raw)
    st, stypes, samples, locs, funcs = [], [], [], {}, {}
    for f, t, v in _fields(raw):
        if f == 6:
            st.append(v.decode("utf-8", "replace"))
        elif f == 1:
            d = {ff: vv for ff, tt, vv in _fields(v)}
            stypes.append((d.get(1, 0), d.get(2, 0)))
        elif f == 2:
            lids, vals = [], []
            for ff, tt, vv in _fields(v):
                if ff == 1:
                    lids += _packed(vv, tt)
                elif ff == 2:
                    vals += [x - (1 << 64) if x >= 1 << 63 else x for x in _packed(vv, tt)]
            samples.append((lids, vals))
        elif f == 4:
            lid, lines = 0, []
            for ff, tt, vv in _fields(v):
                if ff == 1:
                    lid = vv
                elif ff == 4:
                    d = {a: c for a, b_, c in _fields(vv)}
                    lines.append(d.get(1, 0))
            locs[lid] = lines
        elif f == 5:
            d = {ff: vv for ff, tt, vv in _fields(v)}
            funcs[d.get(1, 0)] = d.get(2, 0)
    names = [st[a] for a, b in stypes]
    out = []
    for lids, vals in samples:
        fr = [st[funcs.get(fid, 0)] for lid in lids for fid in locs.get(lid, [])]
        out.append((fr, dict(zip(names, vals))))
    return out


STD = re.compile(r"^(runtime|encoding|reflect|strings|bytes|sort|sync|image|compress|io|bufio|fmt|strconv|math|unicode|os|time|"
                 r"internal|slices|maps|container|net|crypto|errors|path|regexp|hash|context|text/template|mime|html|log|syscall|"
                 r"vendor|golang\.org/x)[./]")


def owner(stack):
    for fn in stack:
        if not STD.match(fn):
            return fn
    return stack[-1] if stack else "?"


CLACK = "github.com/some-du6e/clack/"


def clack_owner(stack):
    """The first Clack frame up the stack, after the library frame that
    allocated: "ui.(*App).Run > gio/gpu" for Gio's renderer."""
    lib = owner(stack)
    if lib.startswith(CLACK):
        return short(lib)
    for fn in stack:
        if fn.startswith(CLACK):
            return "%s > %s" % (short(fn), area(lib))
    return short(lib)


def area(fn):
    m = re.match(r"github\.com/some-du6e/clack/internal/((?:ui/)?[a-z0-9_]+(?:/[a-z0-9_]+)?)", fn)
    if m:
        pkg = m.group(1)
        if pkg.startswith("ui/") and "/" in pkg[3:]:
            pkg = pkg.rsplit("/", 1)[0]
        return pkg
    for pre, name in (("gioui.org/gpu", "gio/gpu"), ("gioui.org/text", "gio/text"), ("gioui.org/internal/ops", "gio/ops"),
                      ("gioui.org/op", "gio/ops"), ("gioui.org/internal/stroke", "gio/stroke"), ("gioui.org/", "gio/other"),
                      ("github.com/go-text/typesetting/font", "go-text fonts"), ("github.com/go-text/", "go-text shaping"),
                      ("github.com/tetratelabs/wazero", "wazero (JS engine)"), ("github.com/pion", "webrtc"),
                      ("github.com/some-du6e/clack/assets", "assets"), ("main.", "cmd/clack")):
        if fn.startswith(pre):
            return name
    return re.sub(r"^((?:[^/]+/){0,2}[^./]+).*", r"\1", fn)


def short(fn):
    return fn.replace("github.com/some-du6e/clack/internal/", "").replace("github.com/go-text/typesetting/", "go-text/")


def heap_tables(path, idx="inuse_space", n=25):
    try:
        prof = load_profile(path)
    except Exception as e:  # noqa: BLE001 (a report never fails on one profile)
        return None, None, 0, str(e)
    by_area, by_owner = collections.Counter(), collections.Counter()
    total = 0
    for fr, v in prof:
        x = v.get(idx, 0)
        if x <= 0:
            continue
        by_area[area(owner(fr))] += x
        by_owner[clack_owner(fr)] += x
        total += x
    return by_area.most_common(n), by_owner.most_common(n), total, None


# ---- driver files ----

def read_kv(path):
    kv = {}
    try:
        for line in open(path, encoding="utf-8", errors="replace"):
            line = line.strip()
            if " " in line:
                k, v = line.split(" ", 1)
                if k not in kv:
                    kv[k] = v
    except OSError:
        pass
    return kv


def mb(v):
    m = re.match(r"([0-9.]+) ?MB", v or "")
    if m:
        return float(m.group(1))
    m = re.match(r"([0-9]+) kB", v or "")
    if m:
        return int(m.group(1)) / 1024
    return None


def fmt_mb(x):
    return "—" if x is None else ("%.0f MB" % x)


def stage_rows(d, platform):
    rows = []
    for st in STAGES:
        mem, trim, ext = (read_kv(os.path.join(d, st + s)) for s in (".mem.txt", ".trimmed.txt", ".external.txt"))
        if not mem:
            continue
        r = {"stage": st, "mem": mem, "trim": trim, "ext": ext}
        if platform == "windows":
            r["total"] = mb(ext.get("ext_private_working_set")) or mb(mem.get("win_private_working_set"))
            r["total_trim"] = mb(trim.get("win_private_working_set"))
            r["commit"] = mb(mem.get("win_commit"))
            r["go_res"] = mb(mem.get("win_ws_private_go_arenas"))
            r["other_res"] = mb(mem.get("win_ws_private_other"))
            r["img_res"] = (mb(mem.get("win_ws_private_images")) or 0) + (mb(mem.get("win_ws_private_mapped")) or 0)
        else:
            r["total"] = mb(mem.get("linux_private_total"))
            r["total_trim"] = mb(trim.get("linux_private_total"))
            r["rss"] = mb(mem.get("linux_VmRSS"))
            r["go_res"] = mb(mem.get("linux_rss_anon_go_arenas"))
            r["other_res"] = (mb(mem.get("linux_rss_anon_other")) or 0) + (mb(mem.get("linux_rss_anon_brk_heap")) or 0)
            r["img_res"] = mb(mem.get("linux_dirty_file")) if mem.get("linux_dirty_file") else mb(mem.get("linux_private_file"))
        rows.append(r)
    return rows


def img_note(m):
    # the decoded images' entries, and (a side with GIF players) the
    # animated GIFs playing and their bytes
    n = str(m.get("app_img_entries", "—"))
    if m.get("app_img_players") is not None:
        n += ", %s playing: %s" % (m.get("app_img_players"), fmt_mb(mb(m.get("app_img_player_bytes"))))
    return n


def users_cell(m):
    # app_users_asked: the ids the session sent to users/info (lazy-users
    # on); an older side doesn't count them
    held = m.get("app_users", "—")
    asked = m.get("app_users_asked")
    return "%s (%s)" % (held, asked) if asked is not None else str(held)


def side_report(base, side, platform):
    d = os.path.join(base, side, "scale", "warm")
    p = os.path.join(base, side, "scale", "prime")
    out = []
    rows = stage_rows(p, platform) + stage_rows(d, platform)
    if not rows:
        return ["_%s: no stage files (the driver didn't run; a tree without internal/ui/perfdrive.go)_" % side, ""], {}
    total_name = "private working set" if platform == "windows" else "private (anonymous + dirty)"
    extra = " commit |" if platform == "windows" else ""
    out.append("| stage | %s | after GC + trim |%s Go heap resident | other private (C, driver, GPU) | DLL/file private | Go heap live (after GC) | Go heap unreturned | stacks | users held (asked of users/info) | messages (store / lists) | images |" % (total_name, extra))
    out.append("|---|---|---|" + ("---|" if extra else "") + "---|---|---|---|---|---|---|---|---|")
    for r in rows:
        m, tr = r["mem"], r["trim"]
        live = mb(tr.get("go_heap_alloc")) or mb(m.get("go_heap_alloc"))
        commit = (" %s |" % fmt_mb(r.get("commit"))) if extra else ""
        out.append("| %s | **%s** | %s |%s %s | %s | %s | %s | %s | %s | %s | %s / %s | %s (%s) |" % (
            STAGE_NAMES.get(r["stage"], r["stage"]), fmt_mb(r["total"]), fmt_mb(r["total_trim"]), commit, fmt_mb(r["go_res"]),
            fmt_mb(r["other_res"]), fmt_mb(r["img_res"]), fmt_mb(live), fmt_mb(mb(m.get("go_heap_unreturned"))),
            fmt_mb(mb(m.get("go_stack_inuse"))), users_cell(m), m.get("app_timeline_msgs", "—"), m.get("app_model_msgs", "—"),
            fmt_mb(mb(m.get("app_img_bytes"))), img_note(m)))
    out.append("")
    if platform == "windows":
        out.append("Windows may trim an idle process's working set (pages go to the standby list or the page file), so "
                   "the private working set can fall while commit, what the process holds, stays: Task Manager's Memory "
                   "column on a machine with RAM to spare reads closer to commit.")
        out.append("")
    if platform == "windows":
        r = rows[-1]["mem"]
        allocs = [l for l in open(os.path.join(d if os.path.exists(os.path.join(d, rows[-1]["stage"] + ".mem.txt")) else p, rows[-1]["stage"] + ".mem.txt"), encoding="utf-8", errors="replace")
                  if l.startswith(("win_private_alloc", "win_private_image"))]
        if allocs:
            out.append("<details><summary>%s: the largest private allocations outside the Go heap, and DLLs' private pages (%s)</summary>" % (side, rows[-1]["stage"]))
            out.append("")
            out.append("```")
            out += [l.rstrip() for l in allocs]
            out.append("```")
            out.append("</details>")
            out.append("")
    # Go heap by area at the key stages
    for st in ("startup", "picker-all", "scroll", "idle"):
        path = os.path.join(d, st + ".heap.pprof")
        if not os.path.exists(path):
            continue
        areas, owners, total, err = heap_tables(path)
        if err:
            out.append("_%s heap profile unreadable: %s_" % (st, err))
            continue
        out.append("<details><summary>%s, %s: live Go heap %.1f MB by owner</summary>" % (side, STAGE_NAMES[st], total / 1048576))
        out.append("")
        out.append("| area | MB | | owner function | MB |")
        out.append("|---|---|---|---|---|")
        for i in range(max(len(areas), len(owners))):
            a = areas[i] if i < len(areas) else ("", 0)
            o = owners[i] if i < len(owners) else ("", 0)
            out.append("| %s | %s | | `%s` | %s |" % (a[0], "%.1f" % (a[1] / 1048576) if a[0] else "", o[0], "%.1f" % (o[1] / 1048576) if o[0] else ""))
        out.append("")
        out.append("</details>")
        out.append("")
    # what the whole session allocated (alloc_space of the last profile)
    for st in ("idle", "channels", "scroll"):
        path = os.path.join(d, st + ".heap.pprof")
        if os.path.exists(path):
            areas, owners, total, err = heap_tables(path, "alloc_space", 20)
            if not err:
                out.append("<details><summary>%s: allocated over the whole session (alloc_space at %s): %.0f MB</summary>" % (side, st, total / 1048576))
                out.append("")
                out.append("| owner function | MB |")
                out.append("|---|---|")
                for k, v in owners:
                    out.append("| `%s` | %.0f |" % (k, v / 1048576))
                out.append("")
                out.append("</details>")
                out.append("")
            break
    return out, {r["stage"]: r for r in rows}


def main():
    if sys.argv[1] == "--combine":
        for d in sys.argv[2:]:
            f = os.path.join(d, "scale.md")
            if os.path.exists(f):
                sys.stdout.write(open(f, encoding="utf-8").read() + "\n")
        return
    base, platform, sha, ref = sys.argv[1:5]
    sides = [s for s in ("base", "head") if os.path.isdir(os.path.join(base, s, "scale"))]
    out = ["## Memory at Hack Club scale, %s" % platform, ""]
    out.append("fakeslackd -scale (fakeslack.HackClubScale): 65,846 emoji.list entries, 32,000 users (all in the metadata cache: "
               "the prime run loads them), 2,006 user groups, 300 channels and 120 DMs, long channels of 6,000 messages, text in other scripts. "
               "The real binary, driven by internal/ui/perfdrive.go. %s." % (
                   "windows-latest, no GPU: Direct3D 11 on the Microsoft Basic Render Driver (adapter.txt)" if platform == "windows"
                   else "ubuntu-latest, Xvfb + Mesa llvmpipe (textures in process memory)"))
    out.append("")
    hist = {}
    for s in sides:
        label = os.environ.get("BASE_LABEL", "base") if s == "base" else "%s (%s)" % (ref, sha[:7])
        out.append("### %s: %s" % (s, label))
        out.append("")
        lines, rows = side_report(base, s, platform)
        out += lines
        hist[s] = rows
    open(os.path.join(base, "scale.md"), "w", encoding="utf-8").write("\n".join(out) + "\n")
    h = hist.get("head", {})

    def cell(st):
        r = h.get(st)
        return fmt_mb(r["total"]) if r else "—"
    idle = h.get("idle", {}).get("mem", {})
    row = "| %s | `%s` | %s | %s | %s | %s | %s | %s | %s | %s | %s |\n" % (
        os.environ.get("RUN_DATE") or datetime.date.today().isoformat(), sha[:7], ref, platform, cell("startup"), cell("picker-all"),
        cell("scroll"), cell("channels"), cell("idle"), fmt_mb(mb(idle.get("go_heap_alloc"))), fmt_mb(mb(idle.get("go_stack_inuse"))))
    open(os.path.join(base, "scale-row.md"), "w", encoding="utf-8").write(row)


main()
