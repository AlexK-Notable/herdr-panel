"""Slice S against the isolated herdr: install with a real [[build]], size
precedence, key encoding under Bubble Tea, a click, teardown, panics."""
import json, os, time, harness as h

LOG = os.path.join(h.SPIKE, "out", "slice.jsonl")
MARKER = LOG + ".marker"
# The repo is private: git authenticates through `gh auth git-credential`, and
# gh finds its login under XDG_CONFIG_HOME, which isolation moved. Point gh at
# the real login for the install's clone only.
GH = {"GH_CONFIG_DIR": os.path.expanduser("~/.config/gh")}


def records(since, kind=None):
    if not os.path.exists(LOG):
        return []
    with open(LOG) as f:
        rs = [r for r in map(json.loads, f) if r["t"] >= since]
    return [r for r in rs if kind is None or r["kind"] == kind]


def show(title, c):
    print(f"\n=== {title}")
    print("\n".join(l.rstrip() for l in c.text().splitlines() if l.strip()))


def res(r):
    if isinstance(r, tuple):
        rc, o, e = r
        return f"rc={rc} out={o[-300:]!r} err={e[-400:]!r}"
    return json.dumps(r)[:400]


def alive(pid):
    try:
        os.kill(pid, 0)
    except ProcessLookupError:
        return False
    with open(f"/proc/{pid}/stat") as f:  # a zombie is not alive
        return f.read().split()[2] != "Z"


def open_popup(c, since_label, **params):
    t = time.time()
    r = h.api("plugin.pane.open", {"plugin_id": "spike.slice", "entrypoint": "slice",
                                   "placement": "popup", "focus": True, **params})
    drawn = c.wait_for("SLICE", 10)
    print(f"\n{since_label} open: {res(r)}  drawn={drawn}")
    return t


def close_by_q(c):
    c.send("q", 1.2)
    return not c.find("SLICE")


c = h.Client(); time.sleep(3)
for _ in range(4):  # first-run welcome pages (fresh isolated state dir)
    if not c.find("continue"):
        break
    c.send(b"\r", 0.8)

# --- 1. [[build]]: the failing build must stop its install
print("install bad :", res(h.cli("plugin", "install", "AlexK-Notable/herdr-panel/spike/slice-bad",
                                  "--ref", "main", "--yes", timeout=300, extra_env=GH)))
print("install good:", res(h.cli("plugin", "install", "AlexK-Notable/herdr-panel/spike/slice",
                                  "--ref", "main", "--yes", timeout=600, extra_env=GH)))
print("plugin list :", res(h.cli("plugin", "list")))
for root, dirs, files in os.walk(os.path.join(h.XDG, "herdr", "plugins")):
    if root.endswith("/bin"):
        print("built       :", root, files)

# --- 2. size precedence and first frame
t = open_popup(c, "A (no size)", env={"SLICE_VIA": "nosize"})
print("A size:", [(r["w"], r["h"]) for r in records(t, "size")])
print("A first_view ms after open:", [round((r["t"] - t) * 1000) for r in records(t, "first_view")],
      "ms after process start:", [r["ms"] for r in records(t, "first_view")])
print("A gone after q:", close_by_q(c))

t = open_popup(c, "B (70x20)", width=70, height=20, env={"SLICE_VIA": "sized"})
print("B size:", [(r["w"], r["h"]) for r in records(t, "size")])
print("B gone after q:", close_by_q(c))

t = time.time()
print("\nC cli open (manifest):", res(h.cli("plugin", "pane", "open", "--plugin", "spike.slice",
                                            "--entrypoint", "slice", "--focus", "--env", "SLICE_VIA=cli")))
print("C drawn:", c.wait_for("SLICE", 10), "size:", [(r["w"], r["h"]) for r in records(t, "size")])
print("C gone after q:", close_by_q(c))

# --- 3. keys, paste and a click, as Bubble Tea decodes them
t = open_popup(c, "D (keys)", env={"SLICE_VIA": "keys"})
time.sleep(0.5)
sent = {}
for label, data in [("t", b"t"), ("esc", b"\x1b"), ("alt+x", b"\x1bx"), ("ctrl+a", b"\x01"),
                    ("up", b"\x1b[A"), ("shift+tab", b"\x1b[Z"), ("tab", b"\t"), ("enter", b"\r"),
                    ("paste", b"\x1b[200~pasted text\x1b[201~")]:
    sent[label] = time.time()
    c.send(data, 0.6)
spot = c.find("toggle (t)")
if spot:
    c.click(spot[0] + 1, spot[1])
show("D popup after input", c)
print("keyboard_enhancements:", [r["flags"] for r in records(t, "keyboard_enhancements")] or "NONE")
for r in records(t):
    if r["kind"] in ("key", "paste", "hit", "mouse_click", "background", "msg"):
        print("  ", {k: v for k, v in r.items() if k not in ("pid", "t", "via")})
esc = [r for r in records(t, "key") if r["str"] == "esc"]
if esc:
    print("esc latency ms (send -> decoded):", round((esc[0]["t"] - sent["esc"]) * 1000))

# --- 4. teardown: a herdr-level close
start = (records(t, "start") or [{}])[0]
kids = start.get("children", {})
print("\nE session", start.get("sid"), "pgid", start.get("pgid"), "children", kids)
if os.path.exists(MARKER):
    os.remove(MARKER)
tc = time.time()
print("E popup.close:", res(h.api("popup.close", {})))
time.sleep(1.5)
print("E gone:", not c.find("SLICE"))
print("E signals:", [(r["sig"], round((r["t"] - tc) * 1000)) for r in records(tc, "signal")] or "NONE",
      "end:", [r["err"] for r in records(tc, "end")] or "NONE",
      "marker:", open(MARKER).read().strip() if os.path.exists(MARKER) else "NONE")
for name in ("plain_pid", "setsid_pid"):
    pid = kids.get(name)
    if pid:
        print(f"E {name} {pid} alive after close: {alive(pid)}")
        if alive(pid):
            os.kill(pid, 15)  # ours: a sleeper the slice started

# --- 5. panics
t = open_popup(c, "F (panic, Bubble Tea's own recovery)", env={"SLICE_VIA": "panic"})
c.send("p", 1.5)
show("F after p", c)
print("F still open:", bool(c.find("SLICE")) or bool(c.find("panic")), "end:", [r["err"] for r in records(t, "end")])

t = open_popup(c, "G (panic, hold screen)", env={"SLICE_VIA": "hold", "SLICE_HOLD": "1"})
c.send("p", 1.5)
show("G after p", c)
print("G hold visible:", bool(c.find("crashed")))
c.send("x", 1.0)
print("G gone after key:", not c.find("crashed"), "hold_end:", [r["how"] for r in records(t, "hold_end")])

t = open_popup(c, "H (panic inside a Cmd, hold)", env={"SLICE_VIA": "cmdpanic", "SLICE_HOLD": "1"})
c.send("P", 1.5)
print("H hold visible:", bool(c.find("crashed")), "end:", [r["err"] for r in records(t, "end")])
c.send("x", 1.0)

# Every run leaves its Setsid sleeper behind (that is the point); kill ours,
# identified by the plugin id in their inherited environment.
for pid in os.listdir("/proc"):
    try:
        env = open(f"/proc/{pid}/environ", "rb").read().split(b"\0")
        argv = open(f"/proc/{pid}/cmdline", "rb").read()
    except OSError:
        continue
    if argv == b"sleep\x00120\x00" and b"HERDR_PLUGIN_ID=spike.slice" in env:
        os.kill(int(pid), 15)
        print("killed Setsid survivor", pid)

print("\npyte errors:", c.errors[:3])
c.close()
print("stop:", h.stop_server())
