# -*- coding: utf-8 -*-
import io, re, sys

sys.stdout.reconfigure(encoding="utf-8")
path = sys.argv[1] if len(sys.argv) > 1 else r"C:\最强象棋软件系统\_verify\race-theme.err"
txt = io.open(path, encoding="utf-8", errors="replace").read()
blocks = txt.split("WARNING: DATA RACE")
print("报告数:", len(blocks) - 1)

for i, b in enumerate(blocks[1:], 1):
    lines = [l.rstrip() for l in b.split("\n")]
    # 找 header，并只打印 header 之后的栈（遇 "created at:" / ===== 停）
    hdr = None
    for j, l in enumerate(lines):
        if re.match(r"\s*(Previous )?(Write|Read|Atomic \w+) at \S+ by ", l):
            hdr = j
            break
    print(f"\n########## 报告 {i} ##########")
    if hdr is None:
        print("  (没识别到 header)")
        continue
    print("  " + lines[hdr].strip())
    printed = 0
    for l in lines[hdr + 1:]:
        s = l.strip()
        if s.startswith("created at:") or s.startswith("Goroutine") or s.startswith("=================="):
            break
        if s == "":
            continue
        if printed < 8:
            print("      " + s[:150])
        printed += 1
    print(f"      …（共 {printed} 帧）")
