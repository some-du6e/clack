#!/usr/bin/env python3
"""Writes THIRD_PARTY_LICENSES.txt: the licence texts of what a Clack build
links and embeds.

usage: python3 tools/licenses.py --target GOOS/GOARCH[/cgo] [--target ...]
                                 [--for TEXT] [-o FILE] PACKAGE

For each target, `go list -deps -json PACKAGE` (from the module root, with
GOOS, GOARCH and CGO_ENABLED set) says what the build contains. The file then
holds, word for word:

  - Go's LICENSE and PATENTS from GOROOT (the runtime and standard library,
    and wasm_exec.js for GOOS=js);
  - each third-party module's licence files (LICENSE, LICENCE, COPYING,
    NOTICE, PATENTS, UNLICENSE and their .txt/.md/-x variants) at the
    module's root, from the module cache or the third_party copy a replace
    points at, and any in the directory of a package the build links;
  - the licences of third-party files Clack embeds itself (EMBEDDED), each
    only when the build embeds such a file;
  - texts a module needs beyond its own files (EXTRA).

Modules and texts whose licence files are byte for byte the same share one
section. Nothing about the time or the machine goes in, so the same inputs
give the same file.

It fails, writing nothing, when a module has no licence file, a listed text
is missing, or the build embeds a font or a model that EMBEDDED doesn't
cover, so a new dependency can't ship without its licence.
"""
import argparse
import fnmatch
import json
import os
import re
import subprocess
import sys
import textwrap

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
LICENCE_FILE = re.compile(r'^(LICEN[CS]E|COPYING|COPYRIGHT|NOTICE|PATENTS|UNLICENSE)([._-][\w.-]*)?$', re.I)

# Third-party files Clack embeds in its own packages (or in its third_party
# copies, outside a package directory): the embedded path, relative to the
# repo, what it is, and the licence texts that come with it.
EMBEDDED = [
    ('assets/fonts/Lato-*.ttf', 'the Lato fonts (assets/fonts)', ['assets/fonts/OFL.txt']),
    ('internal/ui/theme/keysface-*.ttf',
     'Inter, 15 of its key symbols subset and renamed "Clack Keys" (internal/ui/theme/keysface-*.ttf)',
     ['internal/ui/theme/keysface-OFL.txt']),
    ('internal/emoji/standard.json.gz', 'emoji-data, the standard emoji table (internal/emoji)',
     ['internal/emoji/LICENSE-emoji-data.txt']),
    ('internal/jsrt/assets.pack',
     'QuickJS-NG, React, react-reconciler, scheduler and Unicode data, in the plugin runtime (internal/jsrt)',
     ['internal/jsrt/vendor/LICENSE.quickjs-ng', 'internal/jsrt/vendor/LICENSE.react',
      'internal/jsrt/vendor/LICENSE.react-reconciler', 'internal/jsrt/vendor/LICENSE.scheduler',
      'internal/jsrt/vendor/LICENSE.unicode']),
    ('internal/huddle/bgblur/*.tflite', "MediaPipe's selfie segmenter model (internal/huddle/bgblur)",
     ['internal/huddle/bgblur/LICENSE-mediapipe.txt']),
    ('third_party/gioui.org/app/clack_cursors/*.cur',
     "Chromium's panning cursors (third_party/gioui.org/app/clack_cursors)",
     ['third_party/gioui.org/app/clack_cursors/LICENSE']),
]
# Embedded fonts and models that are Clack's own work.
OWN = ['internal/ui/theme/emojislot.ttf']
# Every embedded file like these must be in EMBEDDED or OWN.
NEEDS_LICENCE = re.compile(r'\.(ttf|otf|ttc|woff2?|tflite|onnx)$', re.I)

# Texts a module needs beyond its own licence files.
EXTRA = {
    'github.com/kazzmir/opus-go': ('libopus 1.6.1, which opus-go is transpiled from',
                                   ['internal/huddle/opus/LICENSE-libopus.txt']),
}

RULE = '=' * 72
THIN = '-' * 72


def fail(msg):
    sys.exit('licenses: ' + msg)


def go(args, env):
    r = subprocess.run(['go'] + args, cwd=ROOT, env=env, capture_output=True, text=True)
    if r.returncode != 0:
        fail('go %s: %s' % (' '.join(args), r.stderr.strip()))
    return r.stdout


def go_list(pkg, goos, goarch, cgo):
    env = dict(os.environ, GOOS=goos, GOARCH=goarch, CGO_ENABLED='1' if cgo else '0')
    out = go(['list', '-deps', '-json', pkg], env)
    dec, i, pkgs = json.JSONDecoder(), 0, []
    while True:
        while i < len(out) and out[i].isspace():
            i += 1
        if i >= len(out):
            return pkgs
        p, i = dec.raw_decode(out, i)
        if p.get('Error') or p.get('DepsErrors'):
            fail('%s/%s: %s' % (goos, goarch, json.dumps(p.get('Error') or p.get('DepsErrors'))))
        pkgs.append(p)


def read(path):
    with open(path, 'rb') as f:
        b = f.read()
    try:
        s = b.decode('utf-8-sig')
    except UnicodeDecodeError:
        s = b.decode('latin-1')
    return s.replace('\r\n', '\n').replace('\r', '\n').rstrip() + '\n'


def licence_files(d):
    if not d or not os.path.isdir(d):
        return []
    return sorted(f for f in os.listdir(d) if LICENCE_FILE.match(f) and os.path.isfile(os.path.join(d, f)))


def rel(path):
    return os.path.relpath(path, ROOT).replace(os.sep, '/')


def parse_target(t):
    parts = t.split('/')
    if len(parts) not in (2, 3) or (len(parts) == 3 and parts[2] != 'cgo'):
        fail('target %r: want GOOS/GOARCH or GOOS/GOARCH/cgo' % t)
    return parts[0], parts[1], len(parts) == 3


def main():
    ap = argparse.ArgumentParser(description=__doc__.split('\n\n')[0])
    ap.add_argument('--target', action='append', required=True, help='GOOS/GOARCH, with /cgo for CGO_ENABLED=1')
    ap.add_argument('--for', dest='what', default='', help='what the file ships with, for its first line')
    ap.add_argument('-o', dest='out', help='the file to write (default: stdout)')
    ap.add_argument('package')
    a = ap.parse_args()
    targets = [parse_target(t) for t in a.target]

    mods = {}      # module path -> version, dir, replace, package dirs
    embeds = set()  # repo-relative paths of embedded files
    for goos, goarch, cgo in targets:
        for p in go_list(a.package, goos, goarch, cgo):
            if p.get('Standard'):
                continue
            d = p.get('Dir', '')
            for f in p.get('EmbedFiles') or []:
                r = rel(os.path.join(d, f))
                if not r.startswith('../'):
                    embeds.add(r)
            m = p.get('Module')
            if not m:
                fail('%s has no module' % p.get('ImportPath'))
            if m.get('Main'):
                continue
            e = mods.setdefault(m['Path'], {'version': m.get('Version', ''), 'dir': m.get('Dir', ''),
                                            'replace': (m.get('Replace') or {}).get('Path', ''), 'pkgdirs': set()})
            e['pkgdirs'].add(d)

    env = dict(os.environ, GOOS=targets[0][0], GOARCH=targets[0][1])
    goroot, goversion = go(['env', 'GOROOT', 'GOVERSION'], env).splitlines()[:2]
    js = any(t[0] == 'js' for t in targets)

    # (title, [(file label, path)]) for each part
    parts = [('Go %s: the runtime and standard library%s' % (goversion, ', and wasm_exec.js' if js else ''),
              [(f, os.path.join(goroot, f)) for f in licence_files(goroot)])]
    missing = []
    for path in sorted(mods):
        e = mods[path]
        title = '%s %s' % (path, e['version']) if e['version'] else path
        if e['replace'].startswith('.'):
            title += ", built from Clack's copy in %s" % e['replace'].lstrip('./')
        files = [(f, os.path.join(e['dir'], f)) for f in licence_files(e['dir'])]
        for d in sorted(e['pkgdirs']):
            if os.path.normpath(d) != os.path.normpath(e['dir']):
                sub = os.path.relpath(d, e['dir']).replace(os.sep, '/')
                files += [('%s/%s' % (sub, f), os.path.join(d, f)) for f in licence_files(d)]
        if not files:
            missing.append(path)
            continue
        parts.append((title, files))
        if path in EXTRA:
            what, texts = EXTRA[path]
            parts.append((what, [(t, os.path.join(ROOT, t)) for t in texts]))
    if missing:
        fail('no licence file for %s' % ', '.join(missing))

    covered = set(OWN)
    for pattern, what, texts in EMBEDDED:
        hit = sorted(f for f in embeds if fnmatch.fnmatchcase(f, pattern))
        if hit:
            covered.update(hit)
            parts.append((what, [(t, os.path.join(ROOT, t)) for t in texts]))
    loose = sorted(f for f in embeds if NEEDS_LICENCE.search(f) and f not in covered)
    if loose:
        fail('embedded without a licence entry (tools/licenses.py EMBEDDED or OWN): %s' % ', '.join(loose))
    for _, files in parts:
        for _, p in files:
            if not os.path.isfile(p):
                fail('missing licence text %s' % p)

    # parts whose texts are the same, word for word, share a section
    sections = []
    by_texts = {}
    for title, files in parts:
        texts = [(label, read(p)) for label, p in files]
        key = tuple(t for _, t in texts)
        if key in by_texts:
            by_texts[key][0].append(title)
        else:
            s = ([title], texts)
            by_texts[key] = s
            sections.append(s)

    built = ', '.join('%s/%s%s' % (o, g, ' with cgo' if c else '') for o, g, c in targets)
    intro = ('Clack itself is MIT licensed (the LICENSE file beside this one). This build '
             '(%s, %s) also contains the software below, linked as Go code or embedded as data. '
             "Each section is that software's own licence text, as it ships with the version "
             'linked; THIRD_PARTY_NOTICES says what each part is for. tools/licenses.py in '
             "Clack's repository writes this file from `go list -deps`." % (built, goversion))
    lines = ['Third-party licences%s' % (': ' + a.what if a.what else ''), '']
    lines += textwrap.wrap(intro, 72) + ['', 'Contents', '']
    for n, (titles, _) in enumerate(sections, 1):
        lines.append('%3d. %s' % (n, titles[0]))
        lines += ['     %s' % t for t in titles[1:]]
    for n, (titles, texts) in enumerate(sections, 1):
        lines += ['', '', RULE, '%d. %s' % (n, titles[0])] + ['   %s' % t for t in titles[1:]] + [RULE]
        for i, (label, text) in enumerate(texts):
            lines += ['', label if i == 0 else THIN + '\n' + label, '', text.rstrip('\n')]
    out = '\n'.join(lines) + '\n'
    if a.out:
        os.makedirs(os.path.dirname(os.path.abspath(a.out)), exist_ok=True)
        with open(a.out, 'w', encoding='utf-8', newline='\n') as f:
            f.write(out)
        print('licenses: %s: %d sections, %d modules, %d KB' % (a.out, len(sections), len(mods), len(out.encode()) // 1024),
              file=sys.stderr)
    else:
        sys.stdout.write(out)


if __name__ == '__main__':
    main()
