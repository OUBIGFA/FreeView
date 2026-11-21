# FreeView

FreeView 是一个开源看图软件：轻量、简洁、快速响应。它使用 **Go + [nsgo](https://github.com/nsgo-dev/nsgo) 原生界面**，交互参考 [qView](https://github.com/jurplel/qView)，以 Honeyview 为功能和体验标杆。

**当前版本 0.1.0 是 Windows 测试版。** 尚未正式发布，没有提供下载页面。

## 已实现功能

- **打开**：命令行、资源管理器“打开方式”、拖放或 Ctrl+O 打开图片、文件夹或压缩包。再次启动时，文件交给已经运行的窗口打开。
- **格式**：JPEG、PNG、GIF、WebP、BMP、TIFF。GIF 和 WebP 动图会自动播放。JPEG 按 EXIF 方向摆正。
- **压缩包**：ZIP/CBZ、7z/CB7、RAR/CBR、TAR/CBT 按文件夹方式浏览，不需要先解压。ZIP 和 7z 已用实际文件验证；RAR 和 TAR 的读取由依赖库提供，还没有用实际文件验证。
- **浏览**：上一张、下一张、第一张、最后一张、幻灯片。排序方式可选资源管理器的自然排序（`2.jpg` 排在 `10.jpg` 前面）、修改日期或大小，也可以选择是否循环。相邻图片会提前读入。
- **查看**：适应窗口、原始大小、以鼠标位置为中心缩放、拖动平移、旋转、水平/垂直翻转、全屏、图片信息叠加层。
- **整理**：移到回收站（带确认）、复制图片、复制路径、在资源管理器中显示。
- **自定义**：下面 13 种鼠标操作都可以绑定任意动作：滚轮（可配合 Ctrl/Shift/Alt）、中键、两个侧键、左键双击、右键。每个动作都可以设置多个快捷键，也可以录制快捷键。设置保存在 `%APPDATA%\FreeView\config.yaml`，这个文件也可以手动编辑。

默认鼠标操作参照 Honeyview：

| 操作 | 动作 |
| --- | --- |
| 滚轮向上 / 向下 | 上一张 / 下一张 |
| Ctrl + 滚轮 | 放大 / 缩小 |
| 中键 | 关闭 |
| 侧键（后退 / 前进） | 上一张 / 下一张 |
| 左键双击 | 全屏 |
| 右键 | 菜单 |

按 P 打开设置，按 Alt 显示菜单栏。

## 构建

需要 Windows、Go（版本见 `go.mod`）。要生成安装器，还需要 [NSIS](https://nsis.sourceforge.io) 3.06 或更新版本，并把 `makensis` 加入 `PATH`。

```powershell
cd cmd/freeview
go tool nsgo dev      # 开发运行
go tool nsgo check    # 检查
go tool nsgo test     # 无界面测试
go tool nsgo build    # 构建到 dist/，安装器在 dist/releases/
```

在仓库根目录运行内部包测试：`go test ./internal/...`。运行仓库检查：`python tools/check.py`。

## 目录

| 路径 | 内容 |
| --- | --- |
| `cmd/freeview/` | 应用：界面、动作、设置窗口、语言文件和 `nsgo.yaml` |
| `internal/catalog/` | 列出文件夹和压缩包中的图片，排序，读取图片 |
| `internal/picture/` | 读取尺寸和 EXIF 方向，解码 BMP/TIFF |
| `internal/settings/` | 动作、鼠标操作、快捷键与 YAML 设置文件 |
| `internal/winmouse/` | 接收 nsgo 暂未提供给应用的中键和侧键 |
| `tools/deskrun/` | 在独立的私有桌面中运行界面测试 |

更多说明：[开发路线](docs/roadmap.md)、[仓库规划](docs/repository-plan.md)、[第三方组件](third_party/README.md)、[品牌素材](assets/branding/README.md)。

## 许可

FreeView 采用 [GPL-3.0-only](LICENSE) 许可。所用依赖的许可见[第三方记录](third_party/README.md)。
