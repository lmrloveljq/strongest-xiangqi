# -*- coding: utf-8 -*-
import io, re, sys

sys.stdout.reconfigure(encoding="utf-8")
path = sys.argv[1] if len(sys.argv) > 1 else r"C:\最强象棋软件系统\_verify\race-smoke.err"
txt = io.open(path, encoding="utf-8", errors="replace").read()
blocks = txt.split("WARNING: DATA RACE")
print("报告数:", len(blocks) - 1)

for i, b in enumerate(blocks[1:], 1):
    print(f"\n########## 报告 {i} ##########")
    who = None
    for ln in b.split("\n"):
        s = ln.strip()
        m = re.match(r"((?:Previous )?(?:Write|Read|Atomic \w+)) at \S+ by (.+?):", s)
        if m:
            who = m.group(2)
            print(f"  === {m.group(1)} by {who}")
            continue
        if s.startswith("=================="):
            break
        if who is None:
            continue
        if s.startswith("xiangqi/") or "最强象棋软件系统" in s:
            print("      APP:", s.split(" +0x")[0][:140])
        elif s.startswith("goroutine ") or s.startswith("Goroutine "):
            print("      ", s[:80])
