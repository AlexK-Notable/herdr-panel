import json, time, harness as h

def show(title, c):
    print(f"\n=== {title}")
    print("\n".join(l.rstrip() for l in c.text().splitlines() if l.strip()))

def res(r):  # compact CLI/api result
    if isinstance(r, tuple):
        rc, o, e = r
        return f"rc={rc} out={o[:220]!r} err={e[:300]!r}"
    return json.dumps(r)[:400]

print("relink:", res(h.cli("plugin", "link", h.SPIKE + "/probe")))
c = h.Client(); time.sleep(3)
for _ in range(4):                       # dismiss the first-run welcome pages
    if not c.find("continue"): break
    c.send(b"\r", 0.8)
show("after welcome", c)

# A: popup opened over the socket with explicit size
t0 = time.time()
print("\nA open:", res(h.api("plugin.pane.open", {"plugin_id": "spike.probe", "entrypoint": "probe",
      "placement": "popup", "width": 60, "height": 14, "focus": True, "env": {"SPIKE_VIA": "socket"}})))
print("probe drawn:", c.wait_for("PROBE"))
show("A popup", c)
btn = c.find("[BTN]")
print("BTN at", btn)
if btn:
    c.click(btn[0] + 1, btn[1]); c.wheel(btn[0] + 1, btn[1]); c.click(btn[0] + 1, btn[1], button=2)
c.send("x", 0.4)
print("\nwhile popup open:")
print(" workspace list :", res(h.cli("workspace", "list")))
print(" workspace rename:", res(h.cli("workspace", "rename", "w1", "renamed-under-popup")))
print(" action invoke  :", res(h.cli("plugin", "action", "invoke", "dump", "--plugin", "spike.probe")))
print(" notification   :", res(h.cli("notification", "show", "spike", "--body", "while popup open")))
print(" 2nd popup      :", res(h.api("plugin.pane.open", {"plugin_id": "spike.probe", "entrypoint": "probe", "placement": "popup"})))
time.sleep(0.5)
c.send("q", 1.0)
print("popup gone after q:", not c.find("PROBE"))
print("label now:", [w["label"] for w in json.loads(h.cli("workspace", "list")[1])["result"]["workspaces"]])

# B: keybinding -> plugin action -> popup
c.key_prefix(b"p", 1.5); print("\nB drawn:", c.wait_for("PROBE", 5)); show("B", c); c.send("q", 1.0)
# C: built-in popup keybinding
c.key_prefix(b"o", 1.5); print("\nC drawn:", c.wait_for("PROBE", 5)); show("C", c); c.send("q", 1.0)
# D: smallest size
print("\nD open 1x1:", res(h.api("plugin.pane.open", {"plugin_id": "spike.probe", "entrypoint": "probe",
      "placement": "popup", "width": 1, "height": 1, "focus": True, "env": {"SPIKE_VIA": "min"}})))
time.sleep(1.5); show("D", c); c.send("q", 1.0)
# E: CLI open (manifest placement), without --focus
print("\nE cli open:", res(h.cli("plugin", "pane", "open", "--plugin", "spike.probe", "--entrypoint", "probe", "--env", "SPIKE_VIA=cli-nofocus")))
print("E drawn:", c.wait_for("PROBE", 5)); c.send("q", 1.5); print("E gone after q:", not c.find("PROBE"))
print("\npyte errors:", c.errors[:3])
c.close()
json.dump({"t0": t0}, open(h.SPIKE + "/out/s2_t0.json", "w"))
