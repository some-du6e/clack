#!/usr/bin/env bash
# Downloads the published plugin Library (index.json + every bundle it lists)
# into $1 through the public release-download URLs. Those are plain
# github.com redirects, not REST API calls, so parallel shards don't burn the
# job token's API rate limit the way `gh release download` did. Each bundle's
# sha256 is checked against index.json.
set -euo pipefail
out="$1"
base="https://github.com/${GITHUB_REPOSITORY}/releases/download/plugins"
mkdir -p "$out"
curl -fsSL --retry 20 --retry-delay 30 --retry-max-time 900 --retry-all-errors -o "$out/index.json" "$base/index.json"
python3 -I - "$out" "$base" <<'EOF'
import hashlib, json, os, subprocess, sys
out, base = sys.argv[1], sys.argv[2]
idx = json.load(open(os.path.join(out, "index.json")))
def fetch(f, want):
    dst = os.path.join(out, f)
    subprocess.run(["curl", "-fsSL", "--retry", "20", "--retry-delay", "30", "--retry-max-time", "900",
                    "--retry-all-errors", "-o", dst, f"{base}/{f}"], check=True)
    got = hashlib.sha256(open(dst, "rb").read()).hexdigest()
    if want and got != want:
        sys.exit(f"{f}: sha256 {got} != index {want}")
n = 0
for p in idx["plugins"]:
    # Every file an entry names: the bundle, plus a Slick page-build's
    # separate main-world script ("main": {"file", "sha256"}).
    for e in (p, p.get("main")):
        if isinstance(e, dict) and e.get("file"):
            fetch(e["file"], e.get("sha256"))
            n += 1
print(len(idx["plugins"]), "plugins,", n, "files")
EOF
test -s "$out/index.json"
