import time, harness as h
c = h.Client()
time.sleep(4)
print("=== screen after start"); print(c.text())
print("=== status:", h.cli("status"))
print("=== link:", h.cli("plugin", "link", h.SPIKE + "/probe"))
print("=== plugins:", h.cli("plugin", "list"))
print("=== config check:", h.cli("config", "check"))
c.close()
