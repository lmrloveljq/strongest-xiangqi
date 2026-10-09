# 引擎存放目录

本目录用于放置第三方中国象棋引擎。**引擎不随仓库分发**，需自行下载。

## 放置 Pikafish（皮卡鱼）

1. 从官网 <https://www.pikafish.com/zh-cn/> 或 GitHub Releases
   <https://github.com/official-pikafish/Pikafish/releases> 下载 Windows 纯引擎文件
   （新手推荐 `universal` 版本，可自动适配 CPU 指令集）。

2. 在本目录下新建一个子文件夹，把 **exe 与权重文件放在一起**：

   ```
   engines/
   └── pikafish/
       ├── Pikafish-Windows-x86-64-universal.exe
       └── pikafish.nnue
   ```

3. 启动软件，首次运行会自动扫描并注册；若未自动识别，在菜单
   **「引擎 → 重新扫描引擎库」** 手动触发。

## 注意事项

- **exe 与 `.nnue` 权重必须在同一目录**，否则引擎能启动但棋力会严重退化。
- 每个引擎建议单独放一个子文件夹；目录名可用英文 + 版本号（如 `pikafish-20260925`）。
- 不要把无关的 exe（安装程序、其他 GUI）放进本目录，它们会被逐个探测并产生无效提示。
- 也支持 UCCI 引擎（如旋风、名手、象眼等），放置方式相同，软件会自动识别协议。
- 若引擎被杀毒软件拦截，请将引擎目录加入白名单。

完整说明见 [`../docs/ENGINES.md`](../docs/ENGINES.md)。
