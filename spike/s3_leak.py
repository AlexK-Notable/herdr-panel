import time, harness as h
def prompt(c):
    for l in c.text().splitlines():
        if "spike]$" in l: return l.split("spike]$",1)[1].rstrip()
    return "<no prompt>"
def opn(via):
    return h.api("plugin.pane.open", {"plugin_id":"spike.probe","entrypoint":"probe","placement":"popup","width":60,"height":14,"focus":True,"env":{"SPIKE_VIA":via}}).get("result",{}).get("type")
c = h.Client(); time.sleep(3)
c.send(b"\x15", 0.5)  # ctrl+u: clear the shell's line
print("start          :", repr(prompt(c)))
steps = [
 ("open+q",          lambda: (opn("leak1"), c.wait_for("PROBE"), c.send("q", 1.0))),
 ("open+click+q",    lambda: (opn("leak2"), c.wait_for("PROBE"), c.click(20, 14), c.send("q", 1.0))),
 ("open+wheel+q",    lambda: (opn("leak3"), c.wait_for("PROBE"), c.wheel(20, 14), c.send("q", 1.0))),
 ("open+rclick+q",   lambda: (opn("leak4"), c.wait_for("PROBE"), c.click(20, 14, button=2), c.send("q", 1.0))),
 ("prefix+p, q",     lambda: (c.key_prefix(b"p", 1.5), c.wait_for("PROBE"), c.send("q", 1.0))),
 ("prefix+o, q",     lambda: (c.key_prefix(b"o", 1.5), c.wait_for("PROBE"), c.send("q", 1.0))),
 ("plain ctrl+b p (no popup expected? = opens)", lambda: None),
]
for name, fn in steps[:-1]:
    fn(); time.sleep(0.5)
    print(f"{name:15}:", repr(prompt(c)))
print("pyte errors:", c.errors[:3])
c.close()
