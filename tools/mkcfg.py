import json, io, sys, os

sys.stdout.reconfigure(encoding="utf-8")
root = r"C:\最强象棋软件系统"
base = json.load(io.open(os.path.join(root, "config.json"), encoding="utf-8"))

variants = {
    # R0：用户当前这套（满配 15 线程 / 4096 哈希 / 固定深度 20），人机对弈，人执红
    "cfg-r0.json": {"last_mode": "human", "time_mode": "depth", "depth": 20, "move_time_ms": 1000,
                    "max_strength": True, "threads": 15, "hash": 4096, "multipv": 1, "sound_on": False},
    # R1：只把时间模式换成「无限分析」，其余退回省资源档（隔离变量）
    "cfg-r1.json": {"last_mode": "human", "time_mode": "infinite", "depth": 20, "move_time_ms": 1000,
                    "max_strength": False, "threads": 4, "hash": 512, "multipv": 4, "sound_on": False},
    # R2：满配下发测试的起点：省资源档（4 线程 / 512 哈希），启动后由钩子点「最强引擎模式」
    "cfg-r2.json": {"last_mode": "human", "time_mode": "depth", "depth": 20, "move_time_ms": 1000,
                    "max_strength": False, "threads": 4, "hash": 512, "multipv": 4, "sound_on": False},
}

for name, over in variants.items():
    cfg = dict(base)
    cfg.update(over)
    cfg["think_human"] = {"time_mode": over["time_mode"], "move_time_ms": over["move_time_ms"], "depth": over["depth"]}
    p = os.path.join(root, "_verify", name)
    io.open(p, "w", encoding="utf-8", newline="\n").write(json.dumps(cfg, ensure_ascii=False, indent=2))
    print("wrote", p, over["time_mode"], "max=", over["max_strength"], "t=", over["threads"])
