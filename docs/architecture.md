# FreeView 架构与改动边界

FreeView 是一个 Go/nsgo Windows 应用。界面线程持有可变状态，后台任务只计算结果，再通过 `Scope.Post` 回传。模块按实际职责组织，不增加插件框架、服务容器或通用任务平台。

## 文件职责

| 改动主题 | 主要入口 | 边界与验证 |
| --- | --- | --- |
| 应用与窗口 | `cmd/freeview/main.go`、`windows.go`、`viewer.go` | 启动、窗口状态、主界面组合；窗口验收必须使用私有桌面 |
| 加载与缓存 | `cmd/freeview/loading.go` | 打开目录、合并读取、缓存淘汰、双显示层交接；`lifecycle_test.go`、`app_test.go` |
| 输入与命令 | `commands.go`、`actions.go`、`menus.go` | 快捷键、动作分派、菜单共用同一动作定义；`menus_test.go` |
| 几何与画质 | `geometry.go`、`quality.go` | 缩放、坐标、色彩转换、平滑显示；像素运算交给内部模块 |
| 动图 | `animation.go`、`internal/anim/` | 界面负责播放时钟；内部模块负责解码、合成、内存预算 |
| 鸟瞰图与缩略图 | `navigator.go`、`filmstrip.go`、`internal/thumbs/` | 鸟瞰图随显示图片失效，缩略图请求随目录失效 |
| 信息 | `details.go`、`internal/meta/`、`internal/livephoto/` | 打开信息层后才读取元数据；LIVP 看图只取静态图，导出时才取视频 |
| 文件操作 | `fileactions.go`、`internal/fileops/` | 操作绑定发起时的文件和目录；结束后不改变另一个目录；不覆盖已有目标 |
| 设置 | `preferences.go`、`internal/settings/` | 界面按事件顺序保存快照；存储层串行替换并使用独立临时文件 |
| 更新与安装器 | `updates.go`、`internal/update/`、`tools/build_installer.py` | 更新协议、下载校验、随包许可分别验证；构建不等于发布 |
| Windows 适配 | `internal/winmouse/`、各 `*_windows.go` | 原生接口和临时兼容代码留在适配边界 |

表中未带目录的 Go 文件均位于 `cmd/freeview/`。`extras.go` 只持有增强功能的公共状态、生命周期和后台入口。

## 依赖方向

- `cmd/freeview` 组合内部模块；内部模块不反向引用应用入口
- `catalog` 依赖 `picture` 提供的格式列表，`picture` 依赖 `livephoto` 读取 LIVP；`thumbs` 依赖 `resample`
- `picture` 使用 nsgo 的 SVG 解析器，因此 `internal` 不等同于完全独立于 nsgo；不为消除这一必要依赖复制解析器
- `settings`、`update`、`fileops`、`anim`、`meta` 和 `colorprofile` 各自拥有数据规则；界面负责本地化、确认和结果展示

## 异步状态约定

1. `openSeq` 决定哪次目录打开可以提交结果；较早请求不能覆盖较晚请求
2. `loadSeq` 决定当前图片选择；同一路径的预读与显示请求共用一个读取任务，同时最多读取两个文件
3. `shown.cat` 记录屏幕上图片的真实目录；新目录加载期间，旧图片的文件操作仍指向旧文件
4. `ready` 区分“已解码”与“只是源字节相同”；未解码的背面显示层不能提前呈现
5. `extras.work` 随图片更新而取消；`thumbWork` 随目录更新而取消；取消队列中未开始的工作，已运行的系统解码完成后丢弃过期结果
6. 增强功能共用最多两个低优先级工作线程；内部重采样不得另开全机线程。手动复制图片的编码保留用户发起时的源图
7. 缓存、进行中的读取和缩略图失效由 `loading.go` 的 `forget` 统一处理；不要在功能文件里单独删除缓存键

## 并行改动的约定

任务应按上表划分文件所有权。只有涉及公共状态或生命周期时才修改 `viewer`、`extras` 和 `loading.go`，这类修改由一个集成者合并。不要为了并行而各自复制动作表、配置规范化或缓存逻辑。

新增行为先补相邻的行为测试。迭代中只运行受影响用例；收敛后按 [AGENTS.md](../AGENTS.md) 执行全部检查、私有桌面验收、提交推送和测试安装器构建。更换显示路径需要用预编译的新旧程序交替测量。
