#!/usr/bin/env python3
"""Writes THIRD_PARTY_LICENSES.txt: the licence texts of what a Clack build
links and embeds.

usage: python3 tools/licenses.py --target GOOS/GOARCH[/cgo] [--target ...]
                                 [--extra TITLE=FILE ...] [--for TEXT]
                                 [-o FILE] PACKAGE

For each target, `go list -deps -json PACKAGE` (from the module root, with
GOOS, GOARCH and CGO_ENABLED set) says what the build contains. The file then
holds, word for word:

  - Go's LICENSE and PATENTS from GOROOT (the runtime and standard library,
    and wasm_exec.js for GOOS=js);
  - each module's licence files (LICENSE, LICENCE, COPYING, UNLICENSE,
    COPYRIGHT, NOTICE, PATENTS and their .txt/.md/-x variants) at its root,
    from the module cache or the third_party copy a replace points at, and in
    the directory of each package the build links, Clack's own included (its
    ports keep their upstream's licence beside them);
  - the licences of third-party files Clack embeds itself (EMBEDDED), each
    only when the build embeds such a file;
  - texts a module needs beyond its own files (EXTRA), each only when the
    build compiles a file that calls for it: a committed copy
    (third_party/licenses/) or the notice a source file carries in its own
    header comment;
  - each --extra file, for what go list can't see (a C toolchain's runtime
    linked into the binary).

A part whose texts are all, word for word, in an earlier section joins
that section, and a text a part has twice is written once. Nothing about the
time or the machine goes in, so the same inputs give the same file.

It fails, writing nothing, when
  - a module has no licence file (a NOTICE or PATENTS alone isn't one), or
    GOROOT has no LICENSE;
  - a listed text is missing;
  - the build embeds a font, a model, a wasm, pack or bin file or a script
    that EMBEDDED or OWN doesn't cover;
  - a file the build compiles has a copyright line, an SPDX licence id or a
    pointer to another project's licence in its header comment that none of
    its module's texts account for (ATTRIBUTED lists the checked exceptions),
so a new dependency, or a file copied into one, can't ship without its
licence.
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
MAIN = 'github.com/some-du6e/clack'
LICENCE_FILE = re.compile(r'^(LICEN[CS]E|COPYING|COPYRIGHT|NOTICE|PATENTS|UNLICENSE)([._-][\w.-]*)?$', re.I)
CODE_NAME = re.compile(r'\.(go|c|h|cc|cpp|cxx|hh|hpp|m|mm|s|sx|py|js|mjs|ts|rs|sh)$', re.I)
# what makes a module's files a licence: not a NOTICE or PATENTS alone
LICENCE_PROPER = re.compile(r'^(LICEN[CS]E|COPYING|UNLICENSE|COPYRIGHT)', re.I)

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
     'QuickJS-NG, wasi-libc (with musl and cloudlibc), React, react-reconciler, scheduler and Unicode data, '
     'in the plugin runtime (internal/jsrt)',
     ['internal/jsrt/vendor/LICENSE.quickjs-ng', 'internal/jsrt/vendor/LICENSE.wasi-libc',
      'internal/jsrt/vendor/LICENSE.wasi-libc-MIT', 'internal/jsrt/vendor/LICENSE.wasi-libc-musl',
      'internal/jsrt/vendor/LICENSE.wasi-libc-cloudlibc', 'internal/jsrt/vendor/LICENSE.react',
      'internal/jsrt/vendor/LICENSE.react-reconciler', 'internal/jsrt/vendor/LICENSE.scheduler',
      'internal/jsrt/vendor/LICENSE.unicode']),
    ('internal/huddle/bgblur/*.tflite', "MediaPipe's selfie segmenter model (internal/huddle/bgblur)",
     ['internal/huddle/bgblur/LICENSE-mediapipe.txt']),
    ('third_party/gioui.org/app/clack_cursors/*.cur',
     "Chromium's panning cursors (third_party/gioui.org/app/clack_cursors)",
     ['third_party/gioui.org/app/clack_cursors/LICENSE']),
]
EMBEDDED_TEXTS = {t for _, _, texts in EMBEDDED for t in texts}
# Embedded files like NEEDS_LICENCE's that are Clack's own work.
OWN = [
    'internal/ui/theme/emojislot.ttf',          # Clack's one-glyph placeholder face
    'internal/slack/tlsfp/openssl-3.0.13.bin',  # a recorded ClientHello (handshake bytes), not OpenSSL's code
    'internal/taut/js/*.js', 'internal/slick/js/*.js', 'internal/ui/composer/plugins.js',  # Clack's plugin runtime
]
# Every file Clack's packages embed that looks like this must be in EMBEDDED
# or OWN. Third-party modules' own embeds (Gio's shaders, rnnoise's weights)
# come under their module's licence files, which are in.
NEEDS_LICENCE = re.compile(r'\.(ttf|otf|ttc|woff2?|tflite|onnx|wasm|pack|bin|js|mjs)$', re.I)

# Texts a module needs beyond its own licence files, when the build compiles
# one of `when` (globs relative to the module's root). A text is ('file',
# path in the repo) or ('header', a source file relative to the module's
# root[, pattern, blocks]): the notice in its header comment, from its first
# copyright line (or the comment block matching pattern), with the next
# blocks up to `blocks` in all.
EXTRA = [
    dict(module='github.com/kazzmir/opus-go', when=['opuscc/*.go', 'opusccenc/*.go'],
         what='libopus 1.6.1, which opus-go is transpiled from',
         texts=[('file', 'third_party/licenses/libopus.COPYING')]),
    dict(module='github.com/refraction-networking/utls', when=['internal/quicvarint/*.go', 'internal/quicvarint/*/*.go'],
         what="quic-go's varint and protocol code, copied into uTLS's internal/quicvarint",
         texts=[('file', 'third_party/licenses/quic-go.LICENSE')]),
    dict(module='github.com/refraction-networking/utls', when=['u_prng.go'],
         what="Psiphon's PRNG, pared down into uTLS's u_prng.go",
         texts=[('header', 'u_prng.go')]),
    dict(module='github.com/klauspost/compress', when=['fse/*.go', 'huff0/*.go', 'zstd/*.go'],
         what="Finite State Entropy (Yann Collet), on which klauspost/compress's fse, huff0 and zstd are based",
         texts=[('file', 'third_party/licenses/FiniteStateEntropy.LICENSE')]),
    dict(module='gioui.org', when=['internal/stroke/*.go'],
         what="tdewolff/canvas, whose stroke algorithms Gio's internal/stroke adapts",
         texts=[('file', 'third_party/licenses/tdewolff-canvas.LICENSE.md')]),
    dict(module='github.com/go-text/typesetting', when=['fontscan/langset_gen.go'],
         what="fontconfig's language orthographies (Keith Packard), generated into typesetting's fontscan/langset_gen.go",
         texts=[('header', 'fontscan/langset_gen.go')]),
    dict(module='gioui.org', when=['app/wayland_xdg_shell.c'],
         what="wayland-protocols' xdg-shell, generated into Gio's app/wayland_xdg_shell.c",
         texts=[('header', 'app/wayland_xdg_shell.c')]),
    dict(module='gioui.org', when=['app/wayland_xdg_decoration.c'],
         what="wayland-protocols' xdg-decoration, generated into Gio's app/wayland_xdg_decoration.c",
         texts=[('header', 'app/wayland_xdg_decoration.c')]),
    dict(module='gioui.org', when=['app/wayland_text_input.c'],
         what="wayland-protocols' text-input-unstable-v3, generated into Gio's app/wayland_text_input.c",
         texts=[('header', 'app/wayland_text_input.c')]),
    # modules whose licence file doesn't name the holder their files do
    dict(module='github.com/andybalholm/brotli', when=['*.go'],
         what="Google's copyright on the Brotli code, as andybalholm/brotli's files carry it (its LICENSE names the Brotli Authors)",
         texts=[('header', 'decode.go')]),
    dict(module='github.com/hajimehoshi/go-mp3', when=['*.go'],
         what="Hajime Hoshi's copyright on go-mp3, as its files carry it (its LICENSE is Apache-2.0's text alone)",
         texts=[('header', 'decode.go')]),
    dict(module='github.com/pion/rtp', when=['*.go'],
         what="the Pion community's copyright on pion/rtp, as its files carry it (its LICENSE leaves MIT's <copyright holders> blank)",
         texts=[('header', 'packet.go')]),
    dict(module='github.com/pion/mdns/v2', when=['*.go'],
         what="the Pion community's copyright on pion/mdns, as its files carry it (its LICENSE leaves MIT's <copyright holders> blank)",
         texts=[('header', 'conn.go')]),
    dict(module='github.com/refraction-networking/utls', when=['u_*.go'],
         what="the uTLS Authors' copyright on uTLS's own files (its LICENSE is Go's, naming the Go Authors)",
         texts=[('header', 'u_quic.go')]),
    dict(module=MAIN, when=['internal/imgcache/internal/jpeg/idct.go'],
         what="the MPEG Software Simulation Group's IDCT, which Clack's internal/imgcache/internal/jpeg has by way of Go's image/jpeg",
         texts=[('header', 'internal/imgcache/internal/jpeg/idct.go', 'MPEG Software Simulation Group', 2)]),
]

# Header lines the scan can't match to a module's texts on its own, each
# checked by hand: (module, regex the line matches, why it's covered).
ATTRIBUTED = []

# SPDX ids the scan knows, and a phrase every text of that licence has.
SPDX = {
    'MIT': 'permission is hereby granted free of charge',
    'MIT-0': 'permission is hereby granted free of charge',
    'BSD-2-Clause': 'redistribution and use in source and binary forms',
    'BSD-3-Clause': 'neither the name',
    'Apache-2.0': 'apache license',
    'ISC': 'permission to use copy modify and or distribute this software',
    'Unlicense': 'free and unencumbered software',
    'Zlib': 'this software is provided as is',
    'LGPL-2.1': 'gnu lesser general public license',
    'LGPL-2.1-only': 'gnu lesser general public license',
    'LGPL-2.1-or-later': 'gnu lesser general public license',
    'CC0-1.0': 'cc0',
    'BSL-1.0': 'boost software license',
    'MPL-2.0': 'mozilla public license',
}

GO_FILE_LISTS = ('GoFiles', 'CgoFiles', 'CFiles', 'CXXFiles', 'MFiles', 'SFiles')
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
    return sorted(f for f in os.listdir(d) if LICENCE_FILE.match(f) and not CODE_NAME.search(f)
                  and os.path.isfile(os.path.join(d, f)))


def rel(path):
    return os.path.relpath(path, ROOT).replace(os.sep, '/')


def norm(s):
    return ' '.join(re.sub(r'[^0-9a-z]+', ' ', s.lower()).split())


def comment_blocks(path, limit=12000):
    """The comment blocks among a source file's first `limit` bytes, each a
    list of lines with the comment markers taken off."""
    with open(path, 'rb') as f:
        s = f.read(limit).decode('utf-8', 'replace').replace('\r\n', '\n')
    blocks, cur, in_c = [], [], False
    for raw in s.split('\n'):
        line = raw.strip()
        if in_c:
            text, end = (line.split('*/', 1)[0], True) if '*/' in line else (line, False)
            cur.append(re.sub(r'^\*+ ?', '', text).rstrip())
            if end:
                in_c = False
                blocks.append(cur)
                cur = []
            continue
        if line.startswith('//'):
            cur.append(line[2:].strip())
            continue
        if cur:
            blocks.append(cur)
            cur = []
        if line.startswith('/*'):
            rest = line[2:]
            if '*/' in rest:
                blocks.append([rest.split('*/', 1)[0].strip()])
            else:
                in_c = True
                cur = [rest.strip()]
    if cur:
        blocks.append(cur)
    return blocks


def header_notice(path, pattern=None, blocks=1):
    """The notice in a source file's header: its first comment block with a
    copyright line (or matching pattern), from that line on, and the blocks
    after it up to `blocks` in all."""
    bs = comment_blocks(path)
    rx = re.compile(pattern if pattern else r'(?i)\bcopyright\b|SPDX-FileCopyrightText')
    for n, b in enumerate(bs):
        for i, line in enumerate(b):
            if rx.search(line):
                out = ['\n'.join(b[i:]).strip('\n')] + ['\n'.join(x).strip('\n') for x in bs[n + 1:n + blocks]]
                return '\n\n'.join(out) + '\n'
    fail('no %s in the header of %s' % ('match for %r' % pattern if pattern else 'copyright notice', path))


COPYRIGHT_LINE = re.compile(r'(?i)(\bcopyright\b.*\b(19|20)\d\d\b|SPDX-FileCopyrightText:)')
POINTER = re.compile(r'(?i)licen[cs]e (?:that )?can be found in the licen[cs]e file of the ([\w.-]+)')
CREDITED_REPO = re.compile(r'(?i)\b(?:github\.com|gitlab\.com|bitbucket\.org)/([\w.-]+/[\w.-]+)[^\n]*licen[cs]ed')


def holder(line):
    s = re.sub(r'(?i)^.*?(spdx-filecopyrighttext:|\bcopyright\b)', '', line)
    s = re.sub(r'(?i)\(c\)|©|&copy;', ' ', s)
    s = re.sub(r'^[\s\d,.+\-–]+', '', s)
    s = re.sub(r'(?i)\ball rights reserved\b.*$', '', s)
    s = re.sub(r'(?i)[,;.]?\s*\b(released|distributed|licensed|available|provided|under|see|use of)\b.*$', '', s)
    return s.strip(' *./,;:-')


def spdx_ok(expr, corpus):
    expr = re.sub(r'[()]', ' ', expr).strip().rstrip('*/').strip()
    for alt in re.split(r'\s+OR\s+', expr):
        ids = [re.sub(r'\s+WITH\s+.*$', '', a).strip().rstrip('+') for a in re.split(r'\s+AND\s+', alt)]
        if all(i in SPDX and SPDX[i] in corpus for i in ids):
            return True
    return False


def unattributed(lines, corpus, module):
    """The lines no text in `corpus` (normalized) accounts for."""
    bad = []
    for line in lines:
        why = None
        m = re.search(r'SPDX-License-Identifier:\s*(.+)', line)
        if m:
            if not spdx_ok(m.group(1), corpus):
                why = 'SPDX id'
        elif COPYRIGHT_LINE.search(line):
            w = norm(holder(line)).split()
            key = ' '.join(w[:3] if w[:1] == ['the'] else w[:2])
            if key and key not in corpus:
                why = 'copyright holder'
        else:
            m = POINTER.search(line) or CREDITED_REPO.search(line)
            if m and norm(m.group(1)) not in corpus:
                why = 'another project'
        if why and not any(mod == module and re.search(rx, line) for mod, rx, _ in ATTRIBUTED):
            bad.append((why, line.strip()))
    return bad


def scanned_lines(path):
    out = []
    for b in comment_blocks(path):
        for line in b:
            if COPYRIGHT_LINE.search(line) or 'SPDX-License-Identifier' in line or POINTER.search(line) \
                    or CREDITED_REPO.search(line):
                out.append(line)
    return out


def compiled_files(p):
    """The files of package p the build compiles, with the headers its C
    files include from the package's directory."""
    d = p.get('Dir', '')
    files = [f for k in GO_FILE_LISTS for f in (p.get(k) or [])]
    hfiles = set(p.get('HFiles') or [])
    seen = set()
    todo = [f for f in files if f.endswith(('.c', '.cc', '.cpp', '.cxx', '.m', '.mm'))]
    while todo:
        f = todo.pop()
        try:
            src = open(os.path.join(d, f), 'rb').read().decode('utf-8', 'replace')
        except OSError:
            continue
        for inc in re.findall(r'^\s*#\s*include\s+"([^"]+)"', src, re.M):
            if inc in hfiles and inc not in seen:
                seen.add(inc)
                todo.append(inc)
    return [os.path.join(d, f) for f in files + sorted(seen)]


def parse_target(t):
    parts = t.split('/')
    if len(parts) not in (2, 3) or (len(parts) == 3 and parts[2] != 'cgo'):
        fail('target %r: want GOOS/GOARCH or GOOS/GOARCH/cgo' % t)
    return parts[0], parts[1], len(parts) == 3


def main():
    ap = argparse.ArgumentParser(description=__doc__.split('\n\n')[0])
    ap.add_argument('--target', action='append', required=True, help='GOOS/GOARCH, with /cgo for CGO_ENABLED=1')
    ap.add_argument('--extra', action='append', default=[], metavar='TITLE=FILE',
                    help="a licence text go list can't see, with its section's title")
    ap.add_argument('--for', dest='what', default='', help='what the file ships with, for its first line')
    ap.add_argument('-o', dest='out', help='the file to write (default: stdout)')
    ap.add_argument('package')
    a = ap.parse_args()
    targets = [parse_target(t) for t in a.target]

    mods = {}       # module path -> version, dir, replace, package dirs, compiled files
    embeds = set()  # repo-relative paths of files Clack's own packages embed
    for goos, goarch, cgo in targets:
        for p in go_list(a.package, goos, goarch, cgo):
            if p.get('Standard'):
                continue
            m = p.get('Module')
            if not m:
                fail('%s has no module' % p.get('ImportPath'))
            path = MAIN if m.get('Main') else m['Path']
            d = p.get('Dir', '')
            if path == MAIN or m.get('Replace', {}).get('Path', '').startswith('.'):
                for f in p.get('EmbedFiles') or []:
                    r = rel(os.path.join(d, f))
                    if path == MAIN or any(fnmatch.fnmatchcase(r, e[0]) for e in EMBEDDED):
                        embeds.add(r)
            e = mods.setdefault(path, {'version': m.get('Version', ''), 'dir': m.get('Dir', '') or ROOT,
                                       'replace': (m.get('Replace') or {}).get('Path', ''),
                                       'pkgdirs': set(), 'files': set()})
            e['pkgdirs'].add(d)
            e['files'].update(compiled_files(p))

    env = dict(os.environ, GOOS=targets[0][0], GOARCH=targets[0][1])
    goroot, goversion = go(['env', 'GOROOT', 'GOVERSION'], env).splitlines()[:2]
    js = any(t[0] == 'js' for t in targets)
    go_files = licence_files(goroot)
    if 'LICENSE' not in go_files:
        fail('no LICENSE in GOROOT %s' % goroot)

    # (title, [(file label, path or text)], module) for each part; a part whose
    # module is None is Go's, EMBEDDED's or an --extra
    parts = [('Go %s: the runtime and standard library%s' % (goversion, ', and wasm_exec.js' if js else ''),
              [(f, os.path.join(goroot, f)) for f in go_files], None)]
    missing, corpora = [], {}
    go_texts = [read(os.path.join(goroot, f)) for f in go_files]
    for path in sorted(mods, key=lambda p: (p == MAIN, p)):
        e = mods[path]
        mine = path == MAIN
        files = [] if mine else [(f, os.path.join(e['dir'], f)) for f in licence_files(e['dir'])]
        for d in sorted(e['pkgdirs']):
            if mine or os.path.normpath(d) != os.path.normpath(e['dir']):
                sub = os.path.relpath(d, e['dir']).replace(os.sep, '/')
                # beside Clack's own packages, the licences of data it embeds
                # come in through EMBEDDED, when the build embeds the data
                files += [('%s/%s' % (sub, f), os.path.join(d, f)) for f in licence_files(d)
                          if not (mine and rel(os.path.join(d, f)) in EMBEDDED_TEXTS)]
        if not mine and not any(LICENCE_PROPER.match(os.path.basename(f)) for f, _ in files):
            missing.append(path)
            continue
        if mine:
            title = "Clack's own packages that keep their upstream's licence beside them"
        else:
            title = '%s %s' % (path, e['version']) if e['version'] else path
            if e['replace'].startswith('.'):
                title += ", built from Clack's copy in %s" % e['replace'].lstrip('./')
        if files:
            parts.append((title, files, path))
        # Go's texts are section 1 of every file, so Go code copied into a
        # module (the Go Authors' BSD licence) is accounted for by them
        corpus = [read(p) for _, p in files] + go_texts
        if mine:
            corpus.append(read(os.path.join(ROOT, 'LICENSE')))
        compiled = {os.path.relpath(f, e['dir']).replace(os.sep, '/') for f in e['files']}
        for x in EXTRA:
            if x['module'] == path and any(fnmatch.fnmatchcase(f, w) for f in compiled for w in x['when']):
                texts = []
                for kind, *t in x['texts']:
                    if kind == 'file':
                        t = t[0]
                        texts.append((t, os.path.join(ROOT, t)))
                    else:
                        texts.append(('%s, its header' % t[0],
                                      ('text', header_notice(os.path.join(e['dir'], t[0]), *t[1:]))))
                parts.append((x['what'], texts, path))
                corpus += [t[1] if isinstance(t, tuple) else read(t) for _, t in texts] + [x['what']]
        corpora[path] = norm('\n'.join(corpus))
    if missing:
        fail('no licence file for %s' % ', '.join(missing))

    # copyright lines, SPDX ids and pointers in the headers of what's compiled
    bad = {}  # (module, why, line) -> files
    for path in sorted(mods):
        for f in sorted(mods[path]['files']):
            for why, line in unattributed(scanned_lines(f), corpora[path], path):
                r = rel(f)
                bad.setdefault((path, why, line), []).append(r if not r.startswith('../') else
                                                             os.path.relpath(f, mods[path]['dir']))
    if bad:
        fail('header notices no licence text accounts for (add the text to EXTRA, or the line to ATTRIBUTED):\n'
             + '\n'.join('  %s: %s: %r in %d file(s), e.g. %s' % (m, why, line, len(fs), sorted(fs)[0])
                          for (m, why, line), fs in sorted(bad.items())))

    covered = set()
    for pattern, what, texts in EMBEDDED:
        hit = sorted(f for f in embeds if fnmatch.fnmatchcase(f, pattern))
        if hit:
            covered.update(hit)
            parts.append((what, [(t, os.path.join(ROOT, t)) for t in texts], None))
    covered.update(f for f in embeds if any(fnmatch.fnmatchcase(f, o) for o in OWN))
    loose = sorted(f for f in embeds if NEEDS_LICENCE.search(f) and f not in covered)
    if loose:
        fail('embedded without a licence entry (tools/licenses.py EMBEDDED or OWN): %s' % ', '.join(loose))
    for x in a.extra:
        title, sep, f = x.partition('=')
        if not sep or not title or not f:
            fail('--extra %r: want TITLE=FILE' % x)
        parts.append((title, [(f, os.path.abspath(f))], None))
    for _, files, _ in parts:
        for _, p in files:
            if not isinstance(p, tuple) and not os.path.isfile(p):
                fail('missing licence text %s' % p)

    # a text a part has more than once is written once, under all its
    # labels; a part whose texts are all in an earlier section joins it
    sections = []
    for title, files, _ in parts:
        texts, at = [], {}
        for label, p in files:
            t = p[1] if isinstance(p, tuple) else read(p)
            if t in at:
                texts[at[t]][0].append(label)
            else:
                at[t] = len(texts)
                texts.append(([label], t))
        texts = [(' = '.join(labels) if len(labels) < 3 else '%s (and %d more: %s)' % (
            labels[0], len(labels) - 1, ', '.join(labels[1:])), t) for labels, t in texts]
        have = {t for _, t in texts}
        for sec in sections:
            if have <= {t for _, t in sec[1]}:
                sec[0].append(title)
                break
        else:
            sections.append(([title], texts))

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
        print('licenses: %s: %d sections, %d modules, %d KB' % (a.out, len(sections), len(mods) - (MAIN in mods),
                                                                len(out.encode()) // 1024), file=sys.stderr)
    else:
        sys.stdout.write(out)


if __name__ == '__main__':
    main()
