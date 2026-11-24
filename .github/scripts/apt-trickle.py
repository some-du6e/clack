#!/usr/bin/env python3
"""apt-trickle.py [PORT]: an HTTP server on 127.0.0.1:PORT (80) that
answers every request with a body that never ends, one byte every 20 s: a
mirror that hangs without ever going quiet for apt's 30 s read timeout
(Acquire::http::Timeout), as huddle's and windows' first installs did on
2026-11-24 (flake-check apt-stall). The body has no Content-Length (it
ends when the connection closes): apt refuses at once a Release file over
10 MB (Acquire::MaxReleaseFileSize) and a .deb longer than its index
says, which fails a try on its own rather than hanging it. Run as root
for port 80; serves until killed."""
import socketserver
import sys
import time


class Trickle(socketserver.BaseRequestHandler):
    def handle(self):
        self.request.settimeout(60)
        try:
            self.request.recv(65536)
            self.request.sendall(b"HTTP/1.1 200 OK\r\nContent-Type: application/octet-stream\r\nConnection: close\r\n\r\n")
            while True:
                time.sleep(20)
                self.request.sendall(b"\0")
        except OSError:
            pass


class Server(socketserver.ThreadingTCPServer):
    allow_reuse_address = True
    daemon_threads = True


Server(("127.0.0.1", int(sys.argv[1]) if len(sys.argv) > 1 else 80), Trickle).serve_forever()
