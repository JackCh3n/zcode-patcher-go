# zcode-patcher

![banner](assets/banner.png)

> ZCode 桌面客户端本地增强补丁集：**Go 单二进制，零运行时依赖**（无需 Python/Go 环境）。
> 让自定义模型的思考档位真正生效、解锁完整用量图表、加一枚实时 TPS 状态胶囊、
> 加宽模型选择弹窗、在设置页带来「一键拉取模型」按钮。全部可检查 / 可一键还原，
> ZCode 升级后重打即可。

*Unofficial local enhancement patches for the ZCode desktop client: real reasoning-effort
passthrough, untruncated usage charts, a live TPS status pill, a wider model picker,
and one-click model pulling — shipped as a single static Go binary.*

> **非官方项目**，与 ZCode（智谱）官方无任何关联。本仓库不包含 ZCode 的任何代码或资源，
> 所有改动均在本地对已安装的客户端打补丁，随时可还原。

> **来源说明**：本项目是两份社区作品的**整合与优化**，不是从零原创——初版
> `zcode-patcher`（思考等级补丁 / TPS 状态栏 / Skill 框架）来自 [linux.do · lanvv 的分享帖](https://linux.do/t/topic/2886711)，
> 模型拉取按钮来自 [HHQ-666/zcode-model-puller](https://github.com/HHQ-666/zcode-model-puller)。
> 详见文末 [致谢与来源](#致谢与来源)。

---

## 功能一览

| # | 补丁 | 效果 | 命令 |
|---|------|------|------|
| 1 | **思考等级透传** | 自定义模型（内核白名单之外）的档位真正下发到请求体：anthropic 协议发 `budget_tokens`，OpenAI 兼容发 `reasoning_effort` 原名。内核补丁惰性兜底，白名单模型行为零变化 | `kernel` |
| 2 | **用量页去截断** | 「设置 → 用量」趋势图不再只画 Top 6、饼图不再只画 Top 5 + 「其他模型」，全部模型完整展示 | `usage-chart` |
| 3 | **TPS 状态胶囊** | 输入框工具栏常驻胶囊：`● 21:03 · 首 3.7s · max 45 · avg 32 · min 12 · now 38 · out 1.7k`——当前会话最近一轮，流式中实时刷新，切换会话即消失，无常驻服务 | `tps` |
| 4 | **模型弹窗加宽** | 模型选择浮窗（含渠道二级菜单）192px → 320px，长模型名不再被截断成 `cn:deepseek-…` | `model-width` |
| 5 | **模型拉取按钮** | 设置页「⚡️ 自动拉取模型」：从供应商 `/models` 拉取模型列表，勾选即写入配置；**新增供应商一步到位**（自动建条目）；元数据感知（上下文 / 输出上限 / 思考档位按服务器真实值写入） | `model-puller` |
| 6 | **CLI 拉模型** | 不动客户端，直接同步 `~/.zcode/v2/config.json`；`--refresh` 按服务器元数据刷新已有模型的 limit 与档位 | `model-pull --all` |

---

## 环境要求

| 项目 | 要求 |
|---|---|
| ZCode | 桌面客户端（Windows 实测 3.8.1 / 3.9.x / 3.11.2 / 3.14.5 / 3.16.2 / 3.18.2；macOS / Linux 逻辑支持） |
| 运行时 | **目标机无需 Python / Go**：发布版是单个静态二进制（约 8MB）；仅自行编译源码时需要 Go ≥ 1.22 |

## 快速开始（Windows，推荐）

1. 把**整个仓库文件夹**拷到任意位置（或下载 Release 二进制放进同一目录）；
2. **完全退出 ZCode**（注意右下角托盘也要退出）；
3. 右键 `install_tps_patch.bat`（或管理员运行 `zcode-patcher.exe install`）→ 认准输出 **`TPS 统计栏注入: 已打`**；
4. 启动 ZCode，随便发一条消息，输入框工具栏中部出现胶囊。

> bat 与 `zcode-patcher.exe` 同目录即可互指（相对路径 `%~dp0`），文件夹放哪里都能跑；无中文乱码（全部 GBK）。

## 命令行方式（Windows / macOS / Linux 通用）

```bash
# 只读检查全部补丁状态（可放心先跑）
./zcode-patcher check

# 完全退出 ZCode 后打全部补丁（macOS/Linux 按安装位置酌情 sudo）
sudo ./zcode-patcher install
#    安装位置没被探测到时显式指定：
#    ./zcode-patcher install "C:\Program Files\ZCode"
#    ./zcode-patcher install "/Applications/ZCode.app"

# 单补丁操作
./zcode-patcher kernel check|install|revert
./zcode-patcher tps check|install|revert
./zcode-patcher usage-chart check|install|revert
./zcode-patcher model-width check|install|revert
./zcode-patcher model-puller check|install|revert

# 从供应商接口同步模型列表（不动客户端）
./zcode-patcher model-pull --all                  # 全量同步
./zcode-patcher model-pull --all --dry-run        # 只看会新增什么，不写配置
./zcode-patcher model-pull --all --refresh         # 按服务器元数据刷新已有条目
./zcode-patcher model-pull --test <BASE_URL> [KEY] # 只测连通并看元数据

# 紧急还原（补丁异常导致 ZCode 起不来时用，从干净备份整包恢复，无需重装）
./zcode-patcher restore            # 列出可用备份
./zcode-patcher restore --latest   # 用最新备份还原（需先完全退出 ZCode）
./zcode-patcher restore --backup   # 把当前安装另存为干净备份
```

打完**完全退出并重启 ZCode** 生效。升级 ZCode 会整体还原 app.asar（补丁消失属预期），升级后重新执行 `install` 即可。

### macOS 注意事项

修改 `.app` 会破坏代码签名，若启动异常执行：

```bash
sudo codesign --force --deep --sign - /Applications/ZCode.app
```

### 自编译（开发者）

```bash
go build -o zcode-patcher ./cmd        # 本机架构
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o zcode-patcher.exe ./cmd   # 交叉编译示例
```

两个被注入的脚本经 `//go:embed` 内嵌进二进制（`cmd/embed/`），改胶囊行为只需改
`scripts/zcode-tps.js` / `scripts/zcode-model-puller.js` 后重新编译。

## 还原（随时可退，精确到字节）

```bash
./zcode-patcher revert                       # 全部外科手术式还原
./zcode-patcher revert tps                   # 单补丁
./zcode-patcher restore --latest             # 从干净备份整包恢复（最后防线）
```

每个补丁首次执行时自动生成整包备份（`.bak`）与逐字节记录（sidecar json）。

## 思考等级怎么用

ZCode 的档位选择器对"非内核白名单"的自定义模型能选中档位，但参数不会下发到请求体——本补丁在内核取参处加惰性兜底，按档位名现场合成参数。

**1）在 `~/.zcode/v2/config.json` 给模型配档位**（先完全退出 ZCode）：

```json
"reasoning": { "enabled": true, "variants": ["low", "high", "max"], "defaultVariant": "max" }
```

档位名任意（`variants` 里写什么，UI 就显示什么）。**手改 config 建议同时给该条目 `"zcode": {"modified": true}`**（防目录同步覆盖；走 UI 添加的模型客户端会自动带此标记）。

**2）档位 → 请求参数映射**（补丁内置，可在源码 `kernelHelper` 中修改）：

| 档名 | anthropic `budget_tokens` | OpenAI 兼容 `reasoning_effort` |
|---|---|---|
| low / medium / high | 4000 / 8000 / 16000 | 同名透传 |
| xhigh / max | 32000 / 32000 | 原名透传 |
| 其它任意名 | 16000 兜底 | 不带 |
| off / none / disabled | 关闭 thinking | `"off"` |

**3）验证**：以供应商后台的请求日志为准（本地 rollout 日志脱敏，`thinking` 字段会被剥掉）。

## 平台支持

| 平台 | 支持 | 备注 |
|---|---|---|
| Windows | ✅ 实测 | 打 asar 补丁前需完全退出 ZCode（运行中会锁定文件）；Program Files 下需管理员终端 |
| macOS | ✅ 逻辑支持 | 修改 `.app` 会破坏代码签名，若启动异常：`sudo codesign --force --deep --sign - /Applications/ZCode.app`；`/Applications` 下需 sudo |
| Linux | ✅ 逻辑支持 | 探测 `/opt`、`/usr/share` |

开发与实测基于 **ZCode 3.14.5 / 3.16.2 / 3.18.2（Windows）**。对未知版本/未知结构，一律拒绝盲改并报告原因，不会写坏文件。

## 工作原理（简述）

- **asar 补丁**：直接解析 `app.asar` 头（不依赖任何 Node/asar 工具），区分两种手法——同长度字节级原地覆盖（用量图表、弹窗加宽，头部零改动），与保留 unpacked 原生模块的精确重打包（TPS、拉取按钮，改动条目重算 SHA256 integrity，写临时文件回读校验后原子替换）；
- **内核补丁**：`zcode.cjs` 是 esbuild 压缩产物、符号名随版本重排；按「完整函数原文」做多版本锚点匹配，恰好唯一命中才动手；
- **注入脚本**：renderer 脚本 + preload IPC 桥 + main handler；锚点用语义字符串（IPC 通道名等非压缩符号）配正则捕获压缩别名，跨版本存活；
- **全部补丁**：幂等、`check` 可查、`revert` 可精确还原、多补丁互不干扰；进程内缓存让全量检查 385MB asar 仅数秒级。

## 仓库结构

```
cmd/                     Go 源码（main.go 分发、asar.go 解析/重打包、各补丁单文件）
cmd/embed/               内嵌注入脚本（go:embed，二进制自包含的来源）
scripts/                 被注入的 TPS 胶囊 / 模型拉取脚本（源实现，可在此迭代）
install_tps_patch.bat    Windows 一键安装（GBK，相对路径 %~dp0）
revert_tps_patch.bat     Windows 一键还原（GBK，相对路径 %~dp0）
build.bat                本地编译（kill 旧进程 → go build → 打开 README）
kill_process.bat         结束 zcode-patcher 进程（占用 asar 前先跑它）
Dockerfile               多阶段镜像（GHCR 发布）
.github/workflows/       CI/CD：六平台二进制 + Release + Docker
package.json             版本单一来源（SemVer）
VERSION                  版本镜像（与 package.json 保持一致）
SKILL.md                 AI 代执行流程与逆向分析笔记
```

## 作为 ZCode Skill 使用

把整个目录放进 `~/.zcode/skills/zcode-patcher/`，ZCode 的 AI 会按 `SKILL.md` 的流程代执行（含只读核实、备份确认、还原命令展示等安全步骤）。

## 致谢与来源

本项目为两份社区作品的**整合与优化**，基础代码与思路均来自社区：

- **[linux.do · 这样的 zcode 太酷辣！（加个状态栏）](https://linux.do/t/topic/2886711)** — 作者 **lanvv**（2026-09-10）。本项目最初版本的来源：思考等级透传补丁、TPS 状态栏前端脚本（`scripts/zcode-tps.js`）、以及 Skill 的整体框架与流程设计。原帖明确 "可以直接借鉴定制你自己的状态"。
- **[HHQ-666/zcode-model-puller](https://github.com/HHQ-666/zcode-model-puller)**（MIT License, (c) HHQ-666）— 模型拉取按钮的前端脚本（`scripts/zcode-model-puller.js`，文件头保留原授权署名）与整体思路。

我们在两者基础上做的整合与优化：

| 方向 | 具体改造 |
|---|---|
| 打包内核 | 统一为纯 Go 实现：解析/重打包 asar 不依赖 Node，保留 unpacked 原生模块，重算 SHA256 integrity，写临时文件回读校验后原子替换 |
| 发布形态 | Go 单文件静态二进制（目标机零依赖，无需 Python/Go）；六平台交叉构建 + GitHub Actions 自动打 tag / Release / GHCR Docker 镜像 |
| 跨平台 | Windows / macOS / Linux 安装位置自动探测（运行中进程 → 注册表 → 常规目录；注册表单次递归查询，避免逐键起进程） |
| 跨版本 | 内核锚点多版本匹配；注入锚点用语义字符串配正则捕获压缩别名，版本升级不失效 |
| 可靠性 | 全外科手术式还原（不依赖整包备份、多补丁共存互不误伤）、幂等、`check` 可查、asr 解析进程内缓存 |
| 模型拉取 | 自动创建供应商（破解客户端"先有模型才能存供应商"死锁）、API 协议判定、元数据感知模板（真实上下文 / 输出上限 / 思考档位）、React 表单兼容修复 |
| TPS 胶囊改进 | 多会话防冻结（30 秒静默判定，绿点转灰、速度取终值）、格式精简（首/max/avg/min/now/out，单位省略）、溢出降级顺序保护 |

> Python 时代的辅助诊断工具（`probe_max_tokens.py` 请求钳制探测、`tap_proxy.py` 请求捕获代理）
> 已随 Go 化迁移退役；其功能说明保留在 SKILL.md 的逆向笔记中，需要时可按说明重建。

## 免责声明

本项目仅供学习交流与个人本机使用。请遵守 ZCode 软件许可协议；因使用本工具产生的一切后果由使用者自行承担。补丁会随客户端升级失效，升级后重打即可。

## License

[MIT](LICENSE)