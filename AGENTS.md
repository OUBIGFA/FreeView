# FreeView 项目工作约定

## 方向与阶段

- 产品名称固定为 FreeView。应用使用 Go + nsgo 原生界面，全面参考 qView，以 Honeyview 为体验和功能标杆。
- 第一版只支持 Windows，包含日常看图、自定义鼠标/键盘绑定、压缩包浏览和动图（2026-11-22 用户授权开始实现）。正式发布版本（tag/Release）或扩大平台范围需要对应任务授权。
- 功能能直接复用成熟开源项目或系统能力时直接复用，不自写近似实现；新依赖登记许可。
- 仓库采用独立历史、公开 main 分支和 GPL-3.0-only。移植 qView 源码前登记固定提交、文件、许可、署名与修改；nsgo 的 Apache-2.0 许可和 NOTICE 另行保留。

## 持续跟进稳定版本

- Go 与 nsgo 始终以官方最新稳定版为默认目标，不把预发布版、nightly 或主分支提交当作稳定版。
- 每次开始开发、更新依赖及准备构建或发布前，联网核对 Go 官方下载列表与 nsgo 正式 Releases。发现新稳定版后及时更新，不长期停留旧版。
- Go 更新先核对当前安装方式和实际命令路径；下载后校验官方 SHA-256，安装后验证 go version、命令解析及最小编译测试。保留用户已有配置，不误改其他项目环境。
- nsgo 更新使用最新非 draft、非 prerelease 的正式版本。建立应用模块后，同步 go.mod、go.sum、工具版本、第三方登记与受影响测试；固定已验证版本，禁止以浮动 main 代替可复现依赖。
- 最新稳定版出现兼容故障时，记录准确版本、失败证据和暂时保留旧版的原因；不得静默降级或宣称已升级成功。恢复兼容后重新更新并验证。
- 稳定版跟进是开发工作约定，不等于授权创建定时任务、自动发布或修改生产环境。

## 每次代码任务的收尾

每次完成代码任务后，按顺序执行：

1. 检查与测试：根目录 `python tools/check.py` 和 `go test -tags nodynamic ./internal/...`；在 `cmd/freeview/` 中运行 `go tool nsgo check` 和 `go tool nsgo test`。改动界面或输入时，用 `go run ./tools/deskrun` 在私有桌面中做界面验收（示例 `tests/desktop/smoke.txt`）。不在用户当前桌面上做界面测试。
2. 提交并推送到 origin/main。
3. 每次代码改动后都重新构建测试安装器：根目录运行 `python tools/build_installer.py`，生成 `dist/releases/FreeView-<版本>.exe`（目录被忽略）。测试安装器只供本地试用，不等于正式发布。

## 版本号

- 版本号只写在 `cmd/freeview/nsgo.yaml` 的 `version`。日常改代码、加功能都不改版本号，测试安装器沿用当前版本号命名。
- 仅在用户授权准备正式发布时更新版本号，tag 必须为 `v<版本>`，与 `version` 一致。创建 tag 或 Release 需要用户另行授权。
- 中英文 README 不写死 FreeView 的当前版本号或具体版本的安装包文件名，下载链接指向最新正式版，版本徽章自动读取 GitHub Release；发布时不再同步 README 版本号。第三方依赖版本与历史验证记录保留准确版本，供来源追溯。

## 开发注意

- 应用改动语言文件后运行 `go tool nsgo i18n .` 重新生成 `locales/catalog_gen.go`，并提交生成结果。
- 在 bash 中给测试进程指定设置目录时，要设置 `APPDATA`（大写）；设置 `AppData` 不会生效。
- Release 只上传安装器 `FreeView-<版本>.exe`，不上传 `.sha256`、appcast 或其他文件。自动更新不用更新源：`internal/update` 读 GitHub 最新 Release 的 tag 和安装器附件，用 GitHub 接口给出的 SHA-256（`digest`）校验下载，再以 `/S /RELAUNCH` 静默安装。因此 tag 必须是 `v<版本>`，附件必须叫 `FreeView-<版本>.exe`；换名或换格式会让已安装的用户检查不到更新。
- `tools/deskrun` 的 `mem` 打印进程内存和 CPU 时间，`omouse`、`oshot` 操作和截图设置窗口等第二个窗口。
- `tools/deskrun` 启动后打印 `visible after <ms>`：FreeView 在打开的图片显示后才显示窗口，所以这个值就是启动到首图的时间。对比速度时先 `go build` 出 deskrun 和新旧两个 exe（旧版用 `git worktree` 放在 `_temp/` 下构建），交替各跑几次；不要用 `go run ./tools/deskrun` 计时。
- 投递的鼠标移动消息不移动真实指针，Windows 马上发出离开消息，所以缩略图带等悬停界面在私有桌面里只能停留几十毫秒，基本无法截图；这类行为用 nsgotest 的 `HoverAt` 验证。
- 速度底线：显示图片的路径（读文件 → `picture.Load` → nsgo 解码显示）不能加入解码像素、色彩转换、缩略图等重活。新的重功能放进 `cmd/freeview/extras.go` 的 `background`（最多 2 个低优先级线程），在图片显示后才开始，并用 `extras.seq` 丢弃过期结果；改动后用上面的方法对比启动到首图时间。
- 从按键或菜单处理函数中打开窗口时，要通过 `Scope.Post` 在根作用域打开。否则窗口属于按键绑定的作用域，设置一变就会被关闭。

## 界面文案

- 界面文案（`locales/*.yaml` 和代码里显示给用户的文字）不使用句号，中文“。”和英文句末的“.”都不用。一句话结束时不加标点；需要停顿时用逗号或分号，需要分句时拆成两条文案。省略号“…”不受限制。

## 文件职责与检查

- [架构与改动边界](docs/architecture.md)记录职责、依赖和异步状态归属；修改加载、缓存或后台工作前先核对，保持图片任务与目录缩略图任务的生命周期分离
- [仓库规划](docs/repository-plan.md)记录跟踪边界；[开发路线](docs/roadmap.md)区分计划与已实现能力。随实际实现更新，不保留陈旧阶段说明。
- 品牌源图放在 assets/branding/FreeView_logo.png。保留用户原有 dist/FreeView_logo.png；派生图标由 tools/generate_icons.py 写入被忽略的 dist/icons/。
- Git 只跟踪可发布输入、可复现测试、依赖锁文件、构建/CI、许可和长期文档。不跟踪下载、工具链、缓存、日志、测试截图、安装包和生成物。README 预览图放在 assets/screenshots/，按二进制登记来源、许可和 SHA-256。
- 临时工作集中在 _temp/<任务>/；可复用本地资料放在 .local/。仅创建确有内容的目录。
- **/_temp/ 必须是 .gitignore 最后一条有效规则，置于所有反忽略规则之后。公开二进制按 references/repository-exceptions.json 精确登记来源、许可、字节数和 SHA-256。
- 统一仓库检查入口为 python tools/check.py；CI 执行同一入口。仓库检查读取 Git index/blob，不能用干净工作区掩盖暂存内容。
- Python 与 Pillow 只用于开发检查和图标生成，不属于 FreeView 应用运行时。
- 源码移植、依赖升级和真实功能验收各自保留来源与证据。仓库卫生、构建成功和图标生成均不能替代应用功能或性能验收。
