#!/usr/bin/env python3
"""Spike probe: records what a process running in a herdr popup receives.

Each run appends one JSON record per event to spike/out/probe.jsonl: the
environment herdr injected, the parsed context, the terminal size, every chunk
of input bytes (keys, SGR mouse reports, replies to the colour query), and how
it ended. It turns on SGR mouse reporting the way a panel would, asks for the
terminal's background colour (OSC 11) and draws its own size, so the harness
can find it on screen. It exits on "q" or after a timeout.
"""

import json
import os
import select
import sys
import termios
import time
import tty

LOG = "/home/user/repos/herdr-panel/spike/out/probe.jsonl"
TIMEOUT_S = float(os.environ.get("SPIKE_TIMEOUT_S", "20"))


def log(kind, **fields):
    with open(LOG, "a") as f:
        f.write(json.dumps({"t": round(time.time(), 3), "pid": os.getpid(),
                            "mode": sys.argv[1] if len(sys.argv) > 1 else "",
                            "kind": kind, **fields}) + "\n")


def snapshot():
    env = {k: v for k, v in os.environ.items()
           if k.startswith(("HERDR_", "SPIKE_")) or k in ("TERM", "COLORTERM", "COLORFGBG", "TERM_PROGRAM")}
    ctx = env.pop("HERDR_PLUGIN_CONTEXT_JSON", None)
    try:
        ctx = json.loads(ctx) if ctx else None
    except ValueError:
        pass
    return env, ctx


def main():
    env, ctx = snapshot()
    if sys.argv[1:2] == ["dump"]:
        log("dump", env=env, context=ctx)
        return

    try:
        size = os.get_terminal_size(sys.stdout.fileno())
        cols, rows = size.columns, size.lines
    except OSError:
        cols = rows = None
    log("start", env=env, context=ctx, cols=cols, rows=rows, isatty=sys.stdin.isatty())

    fd = sys.stdin.fileno()
    saved = termios.tcgetattr(fd)
    out = sys.stdout
    try:
        tty.setraw(fd)
        # alt screen, hide cursor, mouse: button events + drag + SGR encoding
        out.write("\x1b[?1049h\x1b[?25l\x1b[?1000h\x1b[?1002h\x1b[?1006h")
        out.write("\x1b[2J\x1b[H")
        out.write(f"PROBE {sys.argv[1]} {cols}x{rows}\r\n")
        out.write("click here: [BTN]\r\n")
        out.write("q quits\r\n")
        out.write("\x1b]11;?\x07")  # ask for the background colour
        out.flush()

        end = time.time() + TIMEOUT_S
        how = "timeout"
        while time.time() < end:
            ready, _, _ = select.select([fd], [], [], 0.1)
            if not ready:
                continue
            data = os.read(fd, 4096)
            if not data:
                how = "eof"
                break
            log("input", bytes=data.decode("latin-1"))
            if b"q" in data and b"\x1b" not in data:
                how = "q"
                break
        log("end", how=how)
    finally:
        out.write("\x1b[?1006l\x1b[?1002l\x1b[?1000l\x1b[?25h\x1b[?1049l")
        out.flush()
        termios.tcsetattr(fd, termios.TCSADRAIN, saved)


if __name__ == "__main__":
    main()
