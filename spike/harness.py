#!/usr/bin/env python3
"""Drives an isolated herdr client in a pseudo-terminal for the panel spike.

The client runs with XDG_CONFIG_HOME=spike/xdg and every HERDR_* variable
removed, so its server, socket, config and plugin registry are the spike's own
and nothing reaches the live session. Input is written as raw bytes, mouse
included (SGR reports), and the screen is rendered with pyte.
"""

import fcntl
import json
import os
import pty
import select
import struct
import subprocess
import termios
import threading
import time

import pyte

SPIKE = os.path.dirname(os.path.abspath(__file__))
XDG = os.path.join(SPIKE, "xdg")
COLS, ROWS = 120, 36
KITTY_KEYS = __import__("re").compile(rb"\x1b\[[<>=?][0-9;:]*u")


def isolated_env():
    env = {k: v for k, v in os.environ.items() if not k.startswith("HERDR_")}
    env.update(XDG_CONFIG_HOME=XDG, TERM="xterm-256color", COLORTERM="truecolor")
    return env


def cli(*args, timeout=15):
    """Runs the herdr CLI against the spike's server only."""
    r = subprocess.run(["herdr", *args], env=isolated_env(), capture_output=True,
                       text=True, timeout=timeout, stdin=subprocess.DEVNULL)
    return r.returncode, r.stdout.strip(), r.stderr.strip()


class ReplyingScreen(pyte.Screen):
    """A pyte screen that answers the host-terminal queries herdr sends,
    writing the replies back into the pty the way a real terminal would."""

    reply = None

    def write_process_input(self, data):
        if self.reply:
            self.reply(data.encode())

    def report_device_status(self, mode, **kwargs):
        # herdr also sends the private form, CSI ? 6 n, which pyte rejects.
        super().report_device_status(mode)


class Client:
    def __init__(self):
        self.screen = ReplyingScreen(COLS, ROWS)
        self.stream = pyte.ByteStream(self.screen)
        self.lock = threading.Lock()
        self.raw = bytearray()
        self.errors = []
        pid, fd = pty.fork()
        if pid == 0:
            os.execvpe("herdr", ["herdr"], isolated_env())
        self.pid, self.fd = pid, fd
        self.screen.reply = lambda data: os.write(fd, data)
        fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", ROWS, COLS, 0, 0))
        threading.Thread(target=self._pump, daemon=True).start()

    def _pump(self):
        while True:
            try:
                r, _, _ = select.select([self.fd], [], [], 0.2)
                if r:
                    data = os.read(self.fd, 65536)
                    if not data:
                        return
                    with self.lock:
                        self.raw += data
                        try:
                            # pyte does not know the kitty keyboard protocol
                            # herdr pushes and pops around prefix mode, and
                            # draws part of CSI < 1 u as text ("1u"); verified
                            # against herdr's own pane read, which was clean.
                            self.stream.feed(KITTY_KEYS.sub(b"", data))
                        except Exception as e:  # keep pumping; note what pyte choked on
                            self.errors.append(repr(e))
            except OSError:
                return

    def send(self, data, pause=0.3):
        os.write(self.fd, data if isinstance(data, bytes) else data.encode())
        time.sleep(pause)

    def key_prefix(self, key, pause=0.6):
        self.send(b"\x02", 0.15)  # ctrl+b
        self.send(key, pause)

    def click(self, x, y, button=0, pause=0.3):
        """Left click at 1-based screen column x, row y, as SGR press then release."""
        self.send(f"\x1b[<{button};{x};{y}M", 0.05)
        self.send(f"\x1b[<{button};{x};{y}m", pause)

    def wheel(self, x, y, up=True, pause=0.3):
        self.send(f"\x1b[<{64 if up else 65};{x};{y}M", pause)

    def text(self):
        with self.lock:
            return "\n".join(self.screen.display)

    def find(self, needle):
        """1-based (x, y) of needle's first character on screen, or None."""
        with self.lock:
            for i, line in enumerate(self.screen.display):
                j = line.find(needle)
                if j >= 0:
                    return j + 1, i + 1
        return None

    def wait_for(self, needle, timeout=8.0):
        end = time.time() + timeout
        while time.time() < end:
            if self.find(needle):
                return True
            time.sleep(0.1)
        return False

    def close(self):
        try:
            os.kill(self.pid, 15)
        except ProcessLookupError:
            pass


def probe_records(since=0.0):
    path = os.path.join(SPIKE, "out", "probe.jsonl")
    if not os.path.exists(path):
        return []
    with open(path) as f:
        return [r for r in map(json.loads, f) if r["t"] >= since]


def api(method, params, timeout=10):
    """One raw socket request to the spike's server: NDJSON, one per connection."""
    import socket
    sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    sock.settimeout(timeout)
    sock.connect(os.path.join(XDG, "herdr", "herdr.sock"))
    sock.sendall((json.dumps({"id": "spike", "method": method, "params": params}) + "\n").encode())
    buf = b""
    while not buf.endswith(b"\n"):
        chunk = sock.recv(65536)
        if not chunk:
            break
        buf += chunk
    sock.close()
    return json.loads(buf)


def stop_server():
    """Stops the spike's herdr server, refusing unless herdr reports the
    spike's own socket: a stray HERDR_* variable must never stop a live one."""
    _, out, _ = cli("status")
    socket_line = next((l for l in out.splitlines() if "socket:" in l), "")
    if XDG + "/" not in socket_line:
        raise SystemExit(f"refusing to stop: not the spike's server ({socket_line.strip()!r})")
    return cli("server", "stop")
