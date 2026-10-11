import time, harness as h
def prompt(c):
    for l in c.text().splitlines():
        if "spike]$" in l: return l.split("spike]$",1)[1].rstrip()
    return "<no prompt>"
c = h.Client(); time.sleep(3); c.send(b"\x15", 0.5)
print("start             :", repr(prompt(c)))
c.key_prefix(b"x", 1.0);       print("prefix+x (unbound):", repr(prompt(c)))
c.send(b"\x15", 0.3)
c.key_prefix(b"?", 1.0); c.send(b"\x1b", 0.8); print("prefix+? then esc :", repr(prompt(c)))
c.send(b"\x15", 0.3)
c.send(b"\x02", 1.0);          print("ctrl+b alone      :", repr(prompt(c)))
c.send(b"\x1b", 0.5)
c.send(b"\x15", 0.3)
# what herdr wrote to the host terminal around a prefix press: look for keyboard-protocol sequences
n = len(c.raw); c.key_prefix(b"x", 1.0)
tail = bytes(c.raw[n:])
import re
print("CSI..u sequences herdr sent to host after prefix+x:", re.findall(rb"\x1b\[[<>=?]?[0-9;:]*u", tail)[:8])
print("any CSI..u since start:", sorted(set(re.findall(rb"\x1b\[[<>=?]?[0-9;:]*u", bytes(c.raw))))[:10])
c.close()
