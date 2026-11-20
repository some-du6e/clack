#!/usr/bin/env python3
"""Prints goh264's FFmpeg notice: the copyright lines of the FFmpeg files
goh264 is ported from, which goh264's own files don't repeat (they keep only
their SPDX id). third_party/licenses/goh264-ffmpeg.NOTICE is its output, and
tools/licenses.py adds it to every build that links goh264.

usage: python3 tools/ffmpeg_copyrights.py > third_party/licenses/goh264-ffmpeg.NOTICE

It reads goh264's non-test Go files (the version go.mod has, through `go list
-m`) for the FFmpeg tag they name ("FFmpeg n8.0.1") and the libav*/ files they
port, fetches each file at that tag from FFmpeg's GitHub mirror, and lists
the copyright lines in its header. supply-chain runs it and diffs the result
with the committed notice, so a goh264 update that ports more files fails
until the notice is refreshed.
"""
import os
import re
import subprocess
import sys
import urllib.request

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
MODULE = 'github.com/thesyncim/goh264'


def fail(msg):
    sys.exit('ffmpeg_copyrights: ' + msg)


def main():
    r = subprocess.run(['go', 'list', '-m', '-f', '{{.Dir}} {{.Version}}', MODULE], cwd=ROOT,
                       capture_output=True, text=True)
    if r.returncode != 0:
        fail(r.stderr.strip())
    d, version = r.stdout.split()
    src = ''
    for base, _, files in os.walk(d):
        for f in sorted(files):
            if f.endswith('.go') and not f.endswith('_test.go'):
                with open(os.path.join(base, f), encoding='utf-8', errors='replace') as h:
                    src += h.read() + '\n'
    tags = sorted(set(re.findall(r'FFmpeg (n\d+\.\d+(?:\.\d+)?)', src)))
    if len(tags) != 1:
        fail('want one FFmpeg tag in %s, found %s' % (MODULE, tags or 'none'))
    tag = tags[0]
    ported = sorted(set(re.findall(r'\b(libav[a-z]+/[A-Za-z0-9_./-]+\.[ch])\b', src)))
    if not ported:
        fail('no FFmpeg files named in %s' % MODULE)
    out = ['goh264 %s, which Clack links for huddle screen shares, is a source-shaped port of' % version,
           "FFmpeg's H.264 decoder at %s, under the same LGPL-2.1-or-later. goh264's own files" % tag,
           'keep only their SPDX id; the FFmpeg files they port carry these copyright lines', '']
    for f in ported:
        url = 'https://raw.githubusercontent.com/FFmpeg/FFmpeg/%s/%s' % (tag, f)
        with urllib.request.urlopen(url, timeout=60) as resp:
            head = resp.read(6000).decode('utf-8', 'replace')
        lines = []
        for line in head.split('\n'):
            line = re.sub(r'^\s*(/\*+|\*+|//)\s?', '', line).strip()
            if re.search(r'(?i)\bcopyright\b', line) and re.search(r'\b(19|20)\d\d\b', line) and line not in lines:
                lines.append(line)
        out.append(f)
        out += ['  ' + l for l in lines] if lines else ['  (no copyright line in its header)']
    sys.stdout.write('\n'.join(out) + '\n')


if __name__ == '__main__':
    main()
