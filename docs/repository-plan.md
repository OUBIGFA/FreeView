# FreeView 仓库规划

本文记录仓库的方案、上游来源和 Git 跟踪边界。已实现的功能见[开发路线](roadmap.md)。

## 方案

| 方案 | 取舍 | 决定 |
| --- | --- | --- |
| 独立 Go/nsgo 仓库，GPL-3.0 | 不继承 Qt 工程；以后可以按来源登记合规移植 qView 实现 | 已选择 |
| 独立 Go/nsgo 实现，Apache-2.0 | 只能参考 qView 行为，不能复制或翻译它的 GPL 源码 | 未选择 |
| 直接 fork qView | 可以沿用历史，但仍依赖 C++/Qt，不符合全面采用 nsgo 的方向 | 不采用 |

- 仓库：<https://github.com/OUBIGFA/FreeView>，公开，默认分支 `main`。
- 历史：独立历史。不添加上游 remote，避免误推。
- 许可：FreeView 使用 GPL-3.0-only；依赖的许可见[第三方记录](../third_party/README.md)。
- 授权：每次完成代码任务后提交并推送到 `main`，再重新构建测试安装器（见[项目约定](../AGENTS.md)）。这不包括创建 tag、Release 或向上游提交变更。

## 上游

| 来源 | 固定依据 | 用途 | 许可 |
| --- | --- | --- | --- |
| [qView](https://github.com/jurplel/qView) | `b8b993a5e0433ba7fa066e68d2346cc7dd1a7a8c` | 功能和交互参考 | GPL-3.0 |
| [nsgo](https://github.com/nsgo-dev/nsgo) | 正式版 v0.2.0（`go.mod`） | 原生界面引擎 | Apache-2.0 |

Go 和 nsgo 跟随官方最新稳定版。2026-11-22 核对：Go 1.27.4，nsgo v0.2.0（最新的非 draft、非 prerelease 版本）。

## 跟踪边界

| 路径 | Git 跟踪 | 职责 |
| --- | --- | --- |
| 根目录 README、LICENSE、AGENTS、Git 规则、`go.mod`、`go.sum` | 是 | 公开说明、许可、项目约定、依赖锁定 |
| `cmd/freeview/` | 是 | 应用源码、`nsgo.yaml`、语言文件和 nsgo 生成的 `locales/catalog_gen.go` |
| `internal/` | 是 | 不依赖界面的逻辑和 Windows 适配 |
| `docs/` | 是 | 长期文档 |
| `assets/branding/` | 是 | 品牌源图 |
| `assets/screenshots/` | 是 | README 预览图；按二进制登记来源、许可和 SHA-256 |
| `tools/`、`tests/` | 是 | 图标生成、仓库检查、安装器与更新源（`tools/appcast`）构建、私有桌面测试工具和测试脚本 |
| `.github/workflows/` | 是 | 只读检查；不接收发布密钥 |
| `references/`、`third_party/` | 是 | 二进制登记、来源与许可记录 |
| `dist/` | 否 | 构建产物、安装器、派生图标、原有 logo 副本 |
| `.local/`、`.nsgo/` | 否 | 本地工具链（如 NSIS）、上游参考检出、nsgo 缓存、更新签名私钥（`.local/secrets/`） |
| 任意层级的 `_temp/`、`output/` | 否 | 临时文件、测试截图、日志 |

`**/_temp/` 必须是 `.gitignore` 最后一条有效规则，放在所有反忽略规则之后。

品牌源图是 `assets/branding/FreeView_logo.png`，与用户原有的 `dist/FreeView_logo.png` 字节一致。`nsgo build` 从源图生成应用和安装器图标；`tools/generate_icons.py` 另外生成多尺寸 PNG/ICO/ICNS 到 `dist/icons/`。

README 预览图放在 `assets/screenshots/`，只使用 FreeView 的默认窗口和设置窗口截图。修改界面后重新截图，并更新二进制登记。

## 检查

- `python tools/check.py`：读取 Git 暂存区检查文件边界、编码、大小、凭据、机器路径、二进制登记和链接。
- `go test -tags nodynamic ./internal/...`；在 `cmd/freeview/` 中运行 `go tool nsgo check` 和 `go tool nsgo test`。
- 界面测试：`go run ./tools/deskrun <脚本> <exe> <参数>` 在私有桌面中运行，示例脚本见 `tests/desktop/smoke.txt`。

仓库检查通过只说明公开文件符合边界，不说明应用功能或性能达标。
