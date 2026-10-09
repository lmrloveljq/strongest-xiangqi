# -*- coding: utf-8 -*-
"""生成「墨玉金」皮肤 + 主题底纹的测试配置（截图与验收用）。"""
import json, io, sys, os

sys.stdout.reconfigure(encoding="utf-8")
root = r"C:\最强象棋软件系统"
base = json.load(io.open(os.path.join(root, "config.json"), encoding="utf-8"))

variants = {
    "cfg-inkgold.json": {"theme": "inkgold", "background_style": "texture", "last_mode": "bridge",
                         "max_strength": True, "threads": 12, "hash": 4096, "multipv": 1, "sound_on": False},
    "cfg-classic.json": {"theme": "default", "background_style": "texture", "last_mode": "bridge",
                         "max_strength": True, "threads": 12, "hash": 4096, "multipv": 1, "sound_on": False},
}
for name, over in variants.items():
    cfg = dict(base)
    cfg.setdefault("background_style", "texture")
    cfg.update(over)
    p = os.path.join(root, "_verify", name)
    io.open(p, "w", encoding="utf-8", newline="\n").write(json.dumps(cfg, ensure_ascii=False, indent=2))
    print("wrote", name, "theme=", over["theme"], "bg=", over["background_style"])
