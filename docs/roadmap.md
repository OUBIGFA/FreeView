# FreeView 开发路线

以下均为计划，不是当前已支持的功能。以真实功能和测试数据媲美甚至超越 Honeyview，不先承诺没有依据的速度、内存或体积。

## 产品边界

- 品牌：FreeView，使用仓库中的品牌源图。
- 应用：Go + nsgo。使用原生窗口、菜单和输入，不保留 Qt 界面，也不增加 Electron/WebView 界面。
- 功能：全面对照 qView 的交互与实现，合规移植时记录 GPL-3.0 来源。Honeyview 仅作为标杆，不复制私有代码或资产。
- 平台建议：先做可靠的 Windows 桌面体验，再扩展 macOS/Linux。使用跨平台引擎不等于 FreeView 已支持所有平台。
- 原则：先显示首帧、操作不阻塞、缓存有上限、错误可见、界面少干扰。按当前需求添加模块，不先建框架。

## 对照与差距清单

| 主题 | qView 对照入口 | FreeView 后续核查 |
| --- | --- | --- |
| 文件、目录、排序、上一张/下一张 | [qvimagecore.h](https://github.com/jurplel/qView/blob/b8b993a5e0433ba7fa066e68d2346cc7dd1a7a8c/src/qvimagecore.h) | 自然排序、Unicode/长路径、坏图、目录变动 |
| 缩放、平移、适应窗口 | [qvgraphicsview.cpp](https://github.com/jurplel/qView/blob/b8b993a5e0433ba7fa066e68d2346cc7dd1a7a8c/src/qvgraphicsview.cpp) | 缩放锚点、高 DPI、超大图、拖动响应 |
| 加载、动画、预读、缓存 | [qvimagecore.cpp](https://github.com/jurplel/qView/blob/b8b993a5e0433ba7fa066e68d2346cc7dd1a7a8c/src/qvimagecore.cpp) | 取消过期请求、连续切图、帧计时与内存上限 |
| 菜单、快捷键、设置 | [actionmanager.cpp](https://github.com/jurplel/qView/blob/b8b993a5e0433ba7fa066e68d2346cc7dd1a7a8c/src/actionmanager.cpp)、[settingsmanager.cpp](https://github.com/jurplel/qView/blob/b8b993a5e0433ba7fa066e68d2346cc7dd1a7a8c/src/settingsmanager.cpp) | 原生菜单、键鼠一致性、可恢复设置 |
| 信息、旋转、系统打开方式 | [qvinfodialog.cpp](https://github.com/jurplel/qView/blob/b8b993a5e0433ba7fa066e68d2346cc7dd1a7a8c/src/qvinfodialog.cpp)、[openwith.cpp](https://github.com/jurplel/qView/blob/b8b993a5e0433ba7fa066e68d2346cc7dd1a7a8c/src/openwith.cpp) | EXIF 方向、ICC 色彩、只读浏览与写文件分离 |
| Honeyview 标杆 | 后续在同一机器、同一公开测试集实测 | 压缩包、格式覆盖、整理与持续切图体验；逐项查证，不假设 qView 已有同等功能 |

这些文件是调查入口，不表示已逐行审计或完成移植。每项实现前核对行为；复制或翻译源码时登记文件、提交、许可、署名、改动及测试。

## 建议实现顺序

### 1. 验证 nsgo 看图路径

按[项目约定](../AGENTS.md)核对最新稳定 Go/nsgo，建立真实模块和锁文件。先验证一个窗口显示本地图片，检查图像上传、缩放平移、窗口缩放、高 DPI 与错误呈现。

验收：Windows 可构建并打开 JPEG/PNG；记录首帧时间和内存。不用空窗口或编译成功替代看图验收。nsgo 缺少关键能力时，先确定最小补充方案，不另起第二套界面。

### 2. 完成日常看图闭环

实现文件打开、目录导航、自然排序、缩放平移、适应窗口、全屏、快捷键和图片信息。后台加载与绘制分开，先做有上限的缓存与过期请求取消，再增加必要预读。

验收：固定公开夹具连续切图，不显示旧请求结果；坏图不崩溃；重复看图不无限增加缓存；输入与首帧有可重复记录。

### 3. 补齐 Honeyview 对标项

按实测差距加入动画、压缩包、更多格式、EXIF/ICC、旋转和整理。先核对解码库许可、体积与目标格式；压缩包限制条目大小和展开量。写文件、删除、覆盖与只读浏览分离。

验收：每种正式支持的格式都有公开许可样本；修改图片的动作可观察、可确认，不静默覆盖文件。

### 4. 用数据决定性能工作

在相同硬件、显示设置和文件集上对照 qView、Honeyview 与 FreeView。记录版本、冷/热缓存、样本数量和重复次数，报告中位数与尾部数据。

| 指标 | 测量边界 |
| --- | --- |
| 启动到首帧 | 进程启动至实际图片可见，不用窗口出现代替 |
| 切图延迟 | 输入至下一张图片可见，区分已预读/未预读 |
| 缩放和平移 | 输入到呈现延迟与丢帧，统一刷新率 |
| 内存 | 空闲、常用目录、超大图、动画和压缩包分别记录 |
| 体积 | 分开记录可执行文件、安装/压缩包和完整依赖 |

先建基线，再定数字目标。素材必须有公开许可，不把用户照片、软件安装包或实测日志加入 Git。

### 5. 准备跨平台与发布

核心闭环和基准稳定后，再处理 macOS/Linux、文件关联、安装卸载、签名、无障碍和多语言。发布前重新核对直接/传递依赖、源码可获取性、许可与署名。

验收：正式支持平台有构建和桌面功能证据。首次发布需单独授权；本阶段不建 tag、不上传安装包。
