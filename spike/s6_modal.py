import time, json, harness as h
def opn(via):
    return h.api("plugin.pane.open", {"plugin_id":"spike.probe","entrypoint":"probe","placement":"popup","width":60,"height":14,"focus":True,"env":{"SPIKE_VIA":via}})
c = h.Client(); time.sleep(3)
c.key_prefix(b"?", 1.0)
print("settings/help open:", any(w in c.text() for w in ("settings", "keybinds")))
print("popup while modal open:", json.dumps(opn("under-modal"))[:200])
c.send(b"\x1b", 0.8); c.send(b"\x1b", 0.8)
print("modal closed:", not any(w in c.text() for w in ("theme   indicators",)))
print("popup after modal:", json.dumps(opn("esc-test"))[:120]); c.wait_for("PROBE")
c.send(b"\x1b", 0.6); c.send(b"\x1b[A", 0.4); c.send(b"\x1bx", 0.4); c.send("q", 1.0)
c.close()
