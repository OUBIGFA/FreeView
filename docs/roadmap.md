# FreeView 开发路线

本文区分已实现、已知限制和后续计划。功能目标是用实际测试数据达到甚至超过 Honeyview；没有测量依据时，不承诺速度、内存或体积指标。

## 产品边界

- 应用使用 Go + nsgo 原生窗口、菜单和输入，不使用 Qt、Electron 或 WebView 界面。
- 功能对照 qView 的交互。Honeyview 只作为体验标杆，不复制它的私有代码或素材。
- 平台：第一版只支持 Windows。macOS 和 Linux 需要另行授权后再做。
- 原则：先显示图片；操作不阻塞界面；缓存有上限；错误显示给用户；界面元素尽量少。

## 已实现（0.1.0 及 main 分支）

功能清单见 [README](../README.md#可以用来做什么)。实现方式：

| 主题 | 实现 | 验证 |
| --- | --- | --- |
| 列出、排序、导航 | `internal/catalog`：文件夹用 `os.ReadDir`，名称排序用 Windows 的 `StrCmpLogicalW`（与资源管理器一致） | 单元测试；在私有桌面中实测 |
| 压缩包 | 用 `github.com/mholt/archives` 把压缩包当作 `io/fs` 文件系统读取 | ZIP：单元测试和界面测试；7z：私有桌面实测；RAR/TAR：未用实际文件验证 |
| 解码 | JPEG/PNG/GIF/WebP（含动图）和 SVG 由 nsgo 解码（SVG 尺寸用 `core.ParseSVG` 读取）。BMP/TIFF 由 `golang.org/x/image` 解码成像素。ICO、HEIF/HEIC、AVIF、JPEG XL 和 RAW 由 Windows WIC 解码成像素（`internal/picture/wic_windows.go`）：HEIF/AVIF、JPEG XL、RAW 按容器 GUID 指定解码器，缺少扩展时报出要安装的 Microsoft Store 扩展；RAW 有全尺寸内嵌预览时显示预览。EXIF 方向用 `rwcarlsen/goexif` 读取，RAW 用 WIC 的 `System.Photo.Orientation` | 单元测试（ICO、SVG、损坏文件）；本机（26H2）用样本实测：AVIF（含 irot 旋转，WIC 已转正）、HEIC、ICO、SVG、DNG/NEF/CR3 正常显示，JPEG XL 未装扩展时显示安装提示；私有桌面截图核对 |
| 缩放、平移、旋转 | 按图片像素和屏幕像素比计算位置；用 nsgo Transform 处理旋转和镜像 | 无界面测试检查缩放百分比；私有桌面检查平移 |
| 输入绑定 | `internal/settings` 定义动作、鼠标操作和快捷键，存为 YAML。键盘用 `Scope.OnKey`；滚轮、拖动、双击、右键用 nsgo 事件；中键和侧键由 `internal/winmouse` 子类化窗口接收 | 单元测试；私有桌面实测改过的中键和滚轮绑定 |
| 窗口 | 置顶用 nsgo `SetWindowLevel`。隐藏标题栏用 `SetTitleBar` 和 `SetWindowControls`，顶部拖动条用 `WindowDrag`（HTCAPTION）。拖动图片旁的背景时，`internal/winmouse` 让窗口线程执行 `WM_SYSCOMMAND SC_DRAGMOVE`，由 Windows 移动窗口 | 无界面测试检查拖动条；私有桌面用 `deskrun state/hit` 检查置顶、标题栏、拖动条命中和背景拖动的触发。真实鼠标拖动无法在私有桌面模拟，尚未自动验证 |
| 强调色 | `grayTheme`（`cmd/freeview/main.go`）在应用根部给 nsgo 默认主题换上深灰强调色：浅色外观 `#3c3c3c`，深色外观 `#7a7a7a`（深灰在深色页面上看不清，所以略浅） | 私有桌面截图核对浅色外观；深色外观未验证 |
| 记住窗口 | 用 nsgo `desktop.Frame` 跟踪窗口位置和外框尺寸，停止变化 0.5 秒后写入 `%APPDATA%\FreeView\windows.yaml`（与设置分开，“恢复默认”不影响窗口）。最大化或全屏时不保存：nsgo v0.2.0 在 Windows 上此时报告的是整屏矩形，保存后下次打开会铺满屏幕，看起来像最大化。打开时用 `nsgo.WindowFrame` 和 `WindowOptions.Frame` 恢复；看图窗口由设置项 `remember_window` 控制，默认开启。设置项 `open_maximized`（默认关闭）在窗口显示前最大化 | 单元测试检查读写和最大化时不保存；私有桌面中旧版本最大化后保存了 `-8,-8 3456×1408`，新版本保持最大化前的 `200,150 900×640`；`open_maximized` 打开后窗口以最大化显示 |
| 手形指针 | Windows 没有张开/握拳手形系统指针（nsgo 的 `CursorGrab` 映射为食指手形 `IDC_HAND`），`internal/winmouse/cursor.go` 自绘 16×16 像素图，按系统指针尺寸放大，用 `CreateIconIndirect` 生成，在 `WM_SETCURSOR` 中显示；拖动中由窗口线程直接 `SetCursor` | 私有桌面中用临时插桩确认放大、拖动、松开时依次为张开、握拳、张开；指针外观只渲染像素图核对过 |
| 设置窗口 | 默认 1040×700，分查看、鼠标、键盘、关于四页。“关于”页显示 logo（`assets/branding` 嵌入，首次显示时缩到 128 像素）、版本号（`core.App`）、GitHub、反馈、检查更新和许可链接。侧栏和搜索框由 FreeView 绘制（nsgo `ui.Sidebar`），页面行由 `ui.Settings` 排版；页面两侧各留 40 点边距，最宽 760 点（`ui.Settings` 自带的页面会铺满整个窗格） | 无界面测试检查切换分类和搜索；私有桌面截图核对布局 |
| 预读 | 当前图片前后各一张提前读出（BMP/TIFF/WIC 格式提前解码）。缓存最多 4 张、256 MB | 代码审查 |
| 内存 | 垃圾回收比例设为 50%（默认 100%）；切图停下 1 秒后调用 `debug.FreeOSMemory` 把解码留下的内存还给系统 | 私有桌面 `deskrun mem`，10 张 6000×4000 JPEG 逐张浏览：静止后私有内存 997 MB → 606 MB，工作集 769 MB → 378 MB，浏览中工作集峰值 776 MB → 650 MB；CPU 时间 6.6 s → 7.0 s（单次测量） |
| 自动更新 | nsgo 的 WinSparkle：`nsgo.yaml` 的 `updates.key` 是公钥，WinSparkle 每天读取最新 Release 的 `appcast-windows_x64.xml`，下载、验签、静默安装并重启。安装器被 `tools/build_installer.py` 重新编译和改名后，由 `tools/appcast` 用 `NSGO_UPDATES_KEY` 签名最终安装器并写 appcast | 签名用临时文件验证通过；尚未用真实 Release 走完更新 |

## 已知限制

- **nsgo v0.2.0 在 Windows 上不把中键和侧键交给应用。** `internal/winmouse` 子类化主窗口作为临时方案。nsgo 支持后删除这个包。
- **在 App 模式下 `CloseWindow` 只隐藏主窗口，进程不退出。** 因此“关闭”动作直接调用 `Scope.Quit`。点击窗口的关闭按钮可以正常退出。
- **缩小显示的质量**：nsgo 用 Direct2D 线性插值绘制图片，缩到 50% 以下时细线可能出现锯齿。改进方案是自己用 `golang.org/x/image/draw` 预先缩小后再显示，先做对比测量再决定。
- **预读只读取文件字节。** 解码仍在显示时由 nsgo 完成，所以大 JPEG 首次显示仍需要等待解码。
- 动图不能暂停、逐帧查看或调速，nsgo 的 Image 没有提供这些控制。
- APNG 动画、动态 AVIF/HEIC/JPEG XL 只显示第一帧。
- HEIC、AVIF、JPEG XL、RAW 依赖用户安装的 Windows 扩展；没有扩展时只能显示安装提示。
- SVG 由 nsgo 按每点 4 像素、最长边 2048 像素栅格化，放得很大会变模糊；nsgo 的 SVG 不绘制 `<text>`。
- 0.1.0 没有 WinSparkle，所以 0.1.0 的用户需要手动安装下一个版本；之后的版本才能自动更新。每个正式 Release 都必须带上签名的 `appcast-windows_x64.xml`。
- 只有一个实例：再次启动时，文件交给已经运行的窗口打开。

## 后续计划

1. **测量性能。** 在同一台机器、同一组有公开许可的测试文件上，对比 qView、Honeyview 和 FreeView。记录启动到首帧、切图延迟、缩放和平移的延迟、内存和体积。报告中位数和尾部数据，区分冷缓存和热缓存。
2. **提升显示质量和速度。** 根据测量结果决定是否预先缩小、是否预先解码。
3. **补齐 Honeyview 对标功能。** 包括更多格式（先核对解码库的许可和体积）、动图控制、双页阅读、压缩包之间跳转、EXIF 详情、ICC 色彩。
4. **发布后维护。** 代码签名、更新渠道，以及随新版本同步安装包中的第三方许可。

每项实现前先对照 qView 的行为。复制或翻译 qView 源码时，登记文件、提交、许可、署名和修改。
