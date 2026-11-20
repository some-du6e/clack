#!/usr/bin/env python3
"""Every Gio patch test in gpu/headless runs on all three GPUs.

ci.yml runs third_party/gioui.org/gpu/headless's Clack tests by name, in
three `-run '^(...)$' gioui.org/gpu/headless` lists: linux-gio (Mesa's
llvmpipe), windows-applog (WARP) and macos (Metal). Two branches that each
add a patch's test conflict on those lines, and a merge that keeps one
side drops the other's tests from CI with every job still green
(third_party/gioui.org/CLACK_PATCHES.md, patch 52's "Merging"). This fails
unless the three lists are the same and hold every TestClack* function of
gpu/headless's tests, but the ones listed below with where they run.
"""
import glob
import re
import sys

# measurements, not checks: run by their own workflows
ELSEWHERE = {
    "TestClackPlacementNoise": "flake-check.yml, its placement probe",
}

ci = open(".github/workflows/ci.yml").read()
lists = [set(m.group(1).split("|")) for m in re.finditer(r"-run '\^\(([^)']*)\)\$' gioui\.org/gpu/headless", ci)]
tests = set()
for f in glob.glob("third_party/gioui.org/gpu/headless/clack_*_test.go"):
    tests |= set(re.findall(r"^func (TestClack\w+)\(t \*testing\.T\)", open(f).read(), re.M))
bad = []
if len(lists) != 3:
    bad.append(f"ci.yml has {len(lists)} -run '^(...)$' gioui.org/gpu/headless lists, want 3 (linux-gio, windows-applog, macos)")
for i, l in enumerate(lists):
    if l != lists[0]:
        bad.append(f"list {i + 1} differs from list 1: {sorted(l ^ lists[0])}")
for t in sorted(tests - set(ELSEWHERE)):
    for i, l in enumerate(lists):
        if t not in l:
            bad.append(f"{t} is missing from list {i + 1}")
for b in bad:
    print("::error::" + b)
if bad:
    sys.exit(1)
print(f"{len(tests)} gpu/headless Clack tests, in all {len(lists)} lists ({len(ELSEWHERE)} run elsewhere)")
