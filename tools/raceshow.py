# -*- coding: utf-8 -*-
import io, sys, re

sys.stdout.reconfigure(encoding="utf-8")
path = r"C:\最强象棋软件系统\_verify\race1.err"
txt = io.open(path, encoding="utf-8", errors="replace").read()
blocks = txt.split("WARNING: DATA RACE")

idx = int(sys.argv[1]) if len(sys.argv) > 1 else 1
n = int(sys.argv[2]) if len(sys.argv) > 2 else 1
for b in blocks[idx:idx + n]:
    lines = b.split("\n")
    # 只保留 "at ... by X:" 头 + 每侧的 app 帧 + 侧头
    out = []
    keep = False
    for ln in lines:
        s = ln.strip()
        if re.match(r"(Previous )?(Write|Read|Atomic)", s):
            out.append("---- " + s[:120])
            keep = True
            continue
        if s.startswith("Goroutine") or s.startswith("goroutine"):
            out.append(s[:120])
            continue
        if "最强象棋软件系统" in s or s.startswith("xiangqi/ui.") or "fyne.io/fyne" in s:
            out.append("    " + s[:150])
            continue
        if s.startswith("=================="):
            break
    print("\n".join(out))
    print("=" * 100)
