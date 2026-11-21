# FreeView

FreeView 是计划中的开源看图软件：轻量、简洁、快速响应。应用采用 **Go + nsgo 原生界面**，全面参考 [qView](https://github.com/jurplel/qView)，以 Honeyview 为功能和体验标杆。

**当前处于仓库准备阶段，尚无可运行应用或软件下载。** 功能和性能目标见 [开发路线](docs/roadmap.md)，不是已实现承诺。

## 当前内容

- [仓库规划](docs/repository-plan.md)：独立 Go/nsgo 项目、GPL-3.0-only、公开 main 分支与跟踪边界。
- [项目工作约定](AGENTS.md)：持续跟进 Go 和 nsgo 官方最新稳定版。
- [品牌素材](assets/branding/README.md)：原始 logo 与可复现 PNG、ICO、ICNS 生成。
- [第三方记录](third_party/README.md)：qView/nsgo 来源、许可、署名与后续引入边界。
- 仓库检查及只读 CI：验证 Git 暂存索引，不公开临时文件、缓存、工具链或发布产物。

## 检查仓库

需要 Git 和 Python 3.11 或更新版本。在仓库根目录执行：

```powershell
python tools/check.py
```

成功时显示仓库索引检查结果与行为测试结果。检查目标是公开文件边界，不是应用功能。生成图标后运行 `python tools/check.py --icons` 可同时检查图标；CI 在 Windows/Linux 执行此入口。

## 生成图标

```powershell
python -m pip install -r tools/requirements-icons.txt
python tools/generate_icons.py
python tools/generate_icons.py --verify
```

真实图标写入 `dist/icons/`，该目录不入库。原图位于 `assets/branding/FreeView_logo.png`；原有 `dist/FreeView_logo.png` 本地文件保留不变。

## 开发与许可

先阅读 [项目约定](AGENTS.md) 和 [仓库规划](docs/repository-plan.md)。当前不提供虚构的应用构建命令或未验证性能数字。

FreeView 采用 [GPL-3.0-only](LICENSE)。qView 是 GPL-3.0 参考项目；nsgo 采用 Apache-2.0，相关许可和 NOTICE 单独保留。当前没有移植 qView 应用代码，也未引入 nsgo 运行时依赖。
