# 第三方来源与许可

FreeView 没有复制或翻译 qView 源码，只参考它的功能和交互。应用中的依赖按 Go 模块引入，版本固定在 `go.mod` 和 `go.sum` 中。

## 参考来源与开发工具

| 来源 | 用途 | 固定依据 | 许可 |
| --- | --- | --- | --- |
| [qView](https://github.com/jurplel/qView) | 功能与交互参考；没有导入代码 | `b8b993a5e0433ba7fa066e68d2346cc7dd1a7a8c` | GPL-3.0 |
| [Pillow](https://github.com/python-pillow/Pillow) | 开发时生成图标；不随应用分发 | [固定依赖文件](../tools/requirements-icons.txt) | MIT-CMU |
| [NSIS](https://nsis.sourceforge.io) 3.13 | 旧版安装器使用；nsgo v0.3.4 后由 Go 构建原生安装器，不再需要 NSIS | SourceForge `nsis-3.13.zip`，SHA-1 `db14f8af2ea346b786c6ae19343f61aaf5e09b8e` | zlib/libpng 等，见其 COPYING |
| [Solar Icons](https://github.com/saoudi-h/solar-icons)（Linear） | 界面图标：右键菜单、设置分类、搜索框和控件箭头；复制到 `assets/icons/solar/`，内容不改 | `ab241e1bf578dcfe45b0197aee59bd9a08b5fe68`（[来源与文件登记](../assets/icons/solar/README.md)） | 代码 MIT（Copyright (c) 2024 Hakim Saoudi）；图标 CC BY 4.0，作者 480 Design |
| FreeView logo | 品牌源图 | [精确二进制登记](../references/repository-exceptions.json) | GPL-3.0-only；不另行授予商标权 |

## 编译进应用的 Go 模块

清单来自 `go version -m FreeView.exe`（nsgo v0.3.4，2026-11-26）。许可按各模块自带的许可文件判断；本次 nsgo 升级的 LICENSE、NOTICE 与 v0.3.0 相同。

| 模块 | 版本 | 许可 | 引入原因 |
| --- | --- | --- | --- |
| github.com/nsgo-dev/nsgo | v0.3.4 | Apache-2.0（[正文](licenses/nsgo-Apache-2.0.txt)、[NOTICE](notices/nsgo-NOTICE.txt)） | 原生界面引擎 |
| github.com/mholt/archives | v0.1.5 | MIT | 读取压缩包 |
| github.com/gen2brain/heic | v0.7.2 | MIT；内嵌的 HEIC 解码器（Rust crate `heic` 0.1.6）为 AGPL-3.0-only，其余 Rust 依赖为 MIT/Apache-2.0/Zlib（[清单与正文](licenses/heic-wasm-notices.txt)） | Windows 缺少 HEIF/HEVC 扩展时解码 HEIC |
| github.com/gen2brain/jpegxl | v0.6.0 | MIT；内嵌 libjxl v0.11.2（BSD-3-Clause 及专利授权）和 Highway（Apache-2.0 或 BSD-3-Clause）（[正文](licenses/jpegxl-wasm-notices.txt)） | Windows 缺少 JPEG XL 扩展时解码 JPEG XL |
| github.com/tetratelabs/wazero | v1.12.0 | Apache-2.0 | 运行上面两个 WebAssembly 解码器 |
| github.com/Masterminds/semver/v3 | v3.5.0 | MIT | 比较版本号，检查更新 |
| github.com/rwcarlsen/goexif | v0.0.0-20190401172101-9e8deecbddbd | BSD-2-Clause | 读取 EXIF 方向和信息面板的拍摄参数 |
| github.com/oov/psd | v0.0.0-20260818185439-a5d50ec0acac | MIT | PSD/PSB 合成图 |
| github.com/xfmoulet/qoi | v0.2.0 | MIT | QOI |
| github.com/blezek/tga | v0.0.0-20150626111426-80720cbc1017 | MIT（Copyright © 2012 Serge Zirukin，ftrvxmtrx/tga 的分支） | TGA；格式早已定型，库多年未更新 |
| github.com/mdouchement/hdr | v0.2.5 | MIT | Radiance HDR |
| github.com/mrjoshuak/go-openexr | v1.4.38 | Apache-2.0，带 NOTICE | OpenEXR |
| github.com/mrjoshuak/go-jpeg2000 | v1.5.6 | Apache-2.0 | go-openexr 依赖 |
| golang.org/x/image | v0.48.0 | BSD-3-Clause | BMP/TIFF 解码；nsgo 也使用 |
| golang.org/x/sys | v0.50.0 | BSD-3-Clause | Windows 后台打开默认浏览器；nsgo 也使用 |
| go.yaml.in/yaml/v3 | v3.0.6 | MIT 与 Apache-2.0 | 设置文件；nsgo 也使用 |
| github.com/nwaples/rardecode/v2 | v2.2.0 | BSD-2-Clause | archives：RAR |
| github.com/bodgit/sevenzip | v1.6.1 | BSD-3-Clause | archives：7z |
| github.com/bodgit/plumbing、github.com/bodgit/windows | v1.3.0、v1.0.1 | BSD-3-Clause | sevenzip 依赖 |
| github.com/STARRY-S/zip | v0.2.3 | BSD-3-Clause | archives：ZIP |
| github.com/klauspost/compress、github.com/klauspost/pgzip | v1.20.4、v1.2.6 | BSD-3-Clause、MIT | 压缩算法 |
| github.com/ulikunitz/xz | v0.5.15 | BSD-3-Clause | 压缩算法 |
| github.com/mikelolasagasti/xz | v1.0.1 | 0BSD | 压缩算法 |
| github.com/dsnet/compress | v0.0.2-0.20230904184137-39efe44ab707 | BSD-3-Clause | 压缩算法 |
| github.com/pierrec/lz4/v4 | v4.1.22 | BSD-3-Clause | 压缩算法 |
| github.com/andybalholm/brotli | v1.3.0 | MIT | 压缩算法；nsgo 也使用 |
| github.com/minio/minlz | v1.0.1 | Apache-2.0 | 压缩算法 |
| github.com/sorairolake/lzip-go | v0.3.8 | MIT 或 Apache-2.0 | 压缩算法 |
| github.com/hashicorp/golang-lru/v2 | v2.0.7 | MPL-2.0 | archives 依赖 |
| github.com/spf13/afero | v1.15.0 | Apache-2.0 | archives 依赖 |
| go4.org | v0.0.0-20230225012048-214862532bf5 | Apache-2.0 | sevenzip 依赖 |
| github.com/yuin/goldmark | v1.8.7 | MIT | nsgo 依赖 |
| github.com/makiuchi-d/gozxing | v0.1.1 | MIT | nsgo 依赖（读取条码） |
| golang.org/x/xerrors | v0.0.0-20200804184101-5ec99f83aff1 | BSD-3-Clause | gozxing 依赖 |
| golang.org/x/net、golang.org/x/text | v0.62.0、v0.44.0 | BSD-3-Clause | nsgo 依赖 |

以上许可都可以与 GPL-3.0 组合使用。`heic` crate 的 AGPL-3.0-only 按 GPL-3.0 第 13 条与 FreeView 组合发布（2026-11-22 用户确认），这部分按 AGPL-3.0 的条款提供，源码见 [imazen/heic](https://github.com/imazen/heic)。

两个 WebAssembly 解码器的许可正文整理自 crates.io 上对应版本的 crate 包、gen2brain/jpegxl 自带的 `lib/LICENSE.libjxl`、libjxl v0.11.2 的 `PATENTS` 和 google/highway 的 `LICENSE`。升级这两个模块时，按它们 `lib/` 中的 `Cargo.lock` 和 `Makefile` 重新核对。构建时启用 `nodynamic` 标签（`nsgo.yaml`），它们不加载系统中的 libheif 或 libjxl DLL。

## 使用的系统组件

ICO、DDS、JPEG XR、HEIF/HEIC、AVIF、JPEG XL 和相机 RAW 由 Windows 映像组件（WIC）及用户从 Microsoft Store 安装的扩展解码。色彩管理用 WIC 的颜色转换（`IWICColorTransform`）和显示器的 ICC 配置文件，缩小后的平滑显示在解码时用 WIC 的 Fant 缩放；缩略图带和鸟瞰图用资源管理器的缩略图（`IShellItemImageFactory`），文件属性用 Shell 的“属性”命令。FreeView 只调用系统接口，不随安装包分发这些组件。

[JarkViewer](https://github.com/jark006/JarkViewer)（GPL-3.0，参考提交 `625273890ea9cf36221d7ae954540c11b7fcbffe`）只作为功能与规则参考：停稳后重采样、缓存预算、动图计时、按失败类型提示等。没有复制或翻译它的代码。

## 随安装包分发

- 安装器会在程序目录写入 `LICENSE.txt`、`THIRD_PARTY_NOTICES.txt` 和 `EMBEDDED_COMPONENT_NOTICES.txt`
- `THIRD_PARTY_NOTICES.txt` 由 nsgo 的正式构建流程根据实际链接的 Go 包生成，包含 FreeView、Solar 图标（MIT 正文与 CC BY 4.0 署名）、Go 运行时与标准库、依赖模块的许可正文和署名
- `EMBEDDED_COMPONENT_NOTICES.txt` 由 `tools/generate_notices.py` 汇总已登记的 HEIC / JPEG XL 内嵌 WASM 代码许可，补足 Go 模块许可不能覆盖的 Rust / C++ 组件
- 这些产物不入库；`python tools/build_installer.py` 将 GPL 和补充声明临时放入 nsgo 标准资源目录，构建原生 Go 安装器后清理源目录，最后读取真实安装包的 ZIP 内容并检查文件、CRC、许可证原文和必要署名，再命名为 `FreeView-<版本>.exe`
- 更新依赖时同步本表、`go.mod`、`go.sum` 和受影响的测试，再重新构建并核对许可文件
- 引入新的库、移植代码或外部素材时，登记名称、用途、版本、许可、署名和修改。
