import time, json, harness as h
def prompt(c):
    for l in c.text().splitlines():
        if "]$" in l: return l.split("]$",1)[1].rstrip()
    return "<no prompt>"
def herdr_prompt(pid):
    rc, o, e = h.cli("pane", "read", pid, "--source", "visible")
    ls = [l for l in o.splitlines() if "]$" in l]
    return repr(ls[-1].split("]$",1)[1].rstrip()) if ls else f"<none> rc={rc} {e[:100]}"
c = h.Client(); time.sleep(3)
pid = json.loads(h.cli("pane","list")[1])["result"]["panes"][0]["pane_id"]
c.send(b"\x15", 0.5)
print("pane", pid, "| before: harness", repr(prompt(c)), "| herdr", herdr_prompt(pid))
c.key_prefix(b"p", 1.5); print("popup drawn:", c.wait_for("PROBE")); c.send("q", 1.0); time.sleep(0.5)
print("after prefix+p,q: harness", repr(prompt(c)), "| herdr", herdr_prompt(pid))
c.close()
