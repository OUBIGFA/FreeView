# FreeView 开发路线

本文区分已实现、已知限制和后续计划。功能目标是用实际测试数据达到甚至超过 Honeyview；没有测量依据时，不承诺速度、内存或体积指标。

## 产品边界

- 应用使用 Go + nsgo 原生窗口、菜单和输入，不使用 Qt、Electron 或 WebView 界面。
- 功能对照 qView 的交互。Honeyview 只作为体验标杆，不复制它的私有代码或素材。
- 平台：第一版只支持 Windows。macOS 和 Linux 需要另行授权后再做。
- 原则：先显示图片；操作不阻塞界面；缓存有上限；错误显示给用户；界面元素尽量少。

## 已实现（Windows 测试版，当前 0.1.0）

功能清单见 [README](../README.md#已实现功能)。实现方式：

| 主题 | 实现 | 验证 |
| --- | --- | --- |
| 列出、排序、导航 | `internal/catalog`：文件夹用 `os.ReadDir`，名称排序用 Windows 的 `StrCmpLogicalW`（与资源管理器一致） | 单元测试；在私有桌面中实测 |
| 压缩包 | 用 `github.com/mholt/archives` 把压缩包当作 `io/fs` 文件系统读取 | ZIP：单元测试和界面测试；7z：私有桌面实测；RAR/TAR：未用实际文件验证 |
| 解码 | JPEG/PNG/GIF/WebP（含动图）由 nsgo 解码。BMP/TIFF 由 `golang.org/x/image` 解码成像素。EXIF 方向用 `rwcarlsen/goexif` 读取 | 单元测试；GIF 动画在私有桌面实测 |
| 缩放、平移、旋转 | 按图片像素和屏幕像素比计算位置；用 nsgo Transform 处理旋转和镜像 | 无界面测试检查缩放百分比；私有桌面检查平移 |
| 输入绑定 | `internal/settings` 定义动作、鼠标操作和快捷键，存为 YAML。键盘用 `Scope.OnKey`；滚轮、拖动、双击、右键用 nsgo 事件；中键和侧键由 `internal/winmouse` 子类化窗口接收 | 单元测试；私有桌面实测改过的中键和滚轮绑定 |
| 窗口 | 置顶用 nsgo `SetWindowLevel`。隐藏标题栏用 `SetTitleBar` 和 `SetWindowControls`，顶部拖动条用 `WindowDrag`（HTCAPTION）。拖动图片旁的背景时，`internal/winmouse` 让窗口线程执行 `WM_SYSCOMMAND SC_DRAGMOVE`，由 Windows 移动窗口 | 无界面测试检查拖动条；私有桌面用 `deskrun state/hit` 检查置顶、标题栏、拖动条命中和背景拖动的触发。真实鼠标拖动无法在私有桌面模拟，尚未自动验证 |
| 手形指针 | Windows 没有张开/握拳手形系统指针（nsgo 的 `CursorGrab` 映射为食指手形 `IDC_HAND`），`internal/winmouse/cursor.go` 自绘 16×16 像素图，按系统指针尺寸放大，用 `CreateIconIndirect` 生成，在 `WM_SETCURSOR` 中显示；拖动中由窗口线程直接 `SetCursor` | 私有桌面中用临时插桩确认放大、拖动、松开时依次为张开、握拳、张开；指针外观只渲染像素图核对过 |
| 设置窗口 | 默认 1040×700，分查看、鼠标、键盘三页。侧栏和搜索框由 FreeView 绘制（nsgo `ui.Sidebar`），页面行由 `ui.Settings` 排版；页面两侧各留 40 点边距，最宽 760 点（`ui.Settings` 自带的页面会铺满整个窗格） | 无界面测试检查切换分类和搜索；私有桌面截图核对布局 |
| 预读 | 当前图片前后各一张提前读出字节。缓存最多 6 张、512 MB | 代码审查；还没有性能测量 |

## 已知限制

- **nsgo v0.2.0 在 Windows 上不把中键和侧键交给应用。** `internal/winmouse` 子类化主窗口作为临时方案。nsgo 支持后删除这个包。
- **在 App 模式下 `CloseWindow` 只隐藏主窗口，进程不退出。** 因此“关闭”动作直接调用 `Scope.Quit`。点击窗口的关闭按钮可以正常退出。
- **缩小显示的质量**：nsgo 用 Direct2D 线性插值绘制图片，缩到 50% 以下时细线可能出现锯齿。改进方案是自己用 `golang.org/x/image/draw` 预先缩小后再显示，先做对比测量再决定。
- **预读只读取文件字节。** 解码仍在显示时由 nsgo 完成，所以大 JPEG 首次显示仍需要等待解码。
- 动图不能暂停、逐帧查看或调速，nsgo 的 Image 没有提供这些控制。
- 不支持的格式：APNG 动画（只显示第一帧）、HEIC、AVIF、JPEG XL、RAW、ICO、SVG 查看。
- 只有一个实例：再次启动时，文件交给已经运行的窗口打开。

## 后续计划

1. **测量性能。** 在同一台机器、同一组有公开许可的测试文件上，对比 qView、Honeyview 和 FreeView。记录启动到首帧、切图延迟、缩放和平移的延迟、内存和体积。报告中位数和尾部数据，区分冷缓存和热缓存。
2. **提升显示质量和速度。** 根据测量结果决定是否预先缩小、是否预先解码。
3. **补齐 Honeyview 对标功能。** 包括更多格式（先核对解码库的许可和体积）、动图控制、双页阅读、压缩包之间跳转、EXIF 详情、ICC 色彩。
4. **准备发布。** 包括第三方许可声明随安装包分发、代码签名、更新渠道。首次正式发布需要单独授权。

每项实现前先对照 qView 的行为。复制或翻译 qView 源码时，登记文件、提交、许可、署名和修改。
