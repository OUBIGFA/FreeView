# 第三方来源与许可

FreeView 没有复制或翻译 qView 源码，只参考它的功能和交互。应用中的依赖按 Go 模块引入，版本固定在 `go.mod` 和 `go.sum` 中。

## 参考来源与开发工具

| 来源 | 用途 | 固定依据 | 许可 |
| --- | --- | --- | --- |
| [qView](https://github.com/jurplel/qView) | 功能与交互参考；没有导入代码 | `b8b993a5e0433ba7fa066e68d2346cc7dd1a7a8c` | GPL-3.0 |
| [Pillow](https://github.com/python-pillow/Pillow) | 开发时生成图标；不随应用分发 | [固定依赖文件](../tools/requirements-icons.txt) | MIT-CMU |
| [NSIS](https://nsis.sourceforge.io) 3.13 | 构建安装器；本地放在 `.local/tools/`，不入库 | SourceForge `nsis-3.13.zip`，SHA-1 `db14f8af2ea346b786c6ae19343f61aaf5e09b8e`（与官方文件列表一致） | zlib/libpng 等，见其 COPYING |
| FreeView logo | 品牌源图 | [精确二进制登记](../references/repository-exceptions.json) | GPL-3.0-only；不另行授予商标权 |

## 编译进应用的 Go 模块

清单来自 `go version -m FreeView.exe`（0.1.0，2026-11-22）。许可按各模块自带的许可文件判断。

| 模块 | 版本 | 许可 | 引入原因 |
| --- | --- | --- | --- |
| github.com/nsgo-dev/nsgo | v0.2.0 | Apache-2.0（[正文](licenses/nsgo-Apache-2.0.txt)、[NOTICE](notices/nsgo-NOTICE.txt)） | 原生界面引擎 |
| github.com/mholt/archives | v0.1.5 | MIT | 读取压缩包 |
| github.com/rwcarlsen/goexif | v0.0.0-20190401172101-9e8deecbddbd | BSD-2-Clause | 读取 EXIF 方向 |
| golang.org/x/image | v0.48.0 | BSD-3-Clause | BMP/TIFF 解码；nsgo 也使用 |
| go.yaml.in/yaml/v3 | v3.0.6 | MIT 与 Apache-2.0 | 设置文件；nsgo 也使用 |
| github.com/nwaples/rardecode/v2 | v2.2.0 | BSD-2-Clause | archives：RAR |
| github.com/bodgit/sevenzip | v1.6.1 | BSD-3-Clause | archives：7z |
| github.com/bodgit/plumbing、github.com/bodgit/windows | v1.3.0、v1.0.1 | BSD-3-Clause | sevenzip 依赖 |
| github.com/STARRY-S/zip | v0.2.3 | BSD-3-Clause | archives：ZIP |
| github.com/klauspost/compress、github.com/klauspost/pgzip | v1.18.0、v1.2.6 | BSD-3-Clause、MIT | 压缩算法 |
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
| golang.org/x/net、golang.org/x/text | v0.62.0、v0.44.0 | BSD-3-Clause | nsgo 依赖 |

以上许可都可以与 GPL-3.0 组合使用。

## 安装在程序旁的库

| 组件 | 版本 | 许可 | 引入原因 |
| --- | --- | --- | --- |
| [WinSparkle](https://github.com/vslavik/winsparkle) | 0.9.4（`WinSparkle.dll`，x64） | MIT；内含 expat（MIT），并含 OpenSSL 署名 | 自动更新。`nsgo build` 从官方 Release 下载并按 nsgo 固定的 SHA-256 `6037df37fc263bd1650a1c4949681a9d40ffe991d01f35892a406cb5d103c976` 校验后放到程序旁 |

## 使用的系统组件

ICO、HEIF/HEIC、AVIF、JPEG XL 和相机 RAW 由 Windows 映像组件（WIC）及用户从 Microsoft Store 安装的扩展解码。FreeView 只调用系统接口，不随安装包分发这些解码器。

## 随安装包分发

- 安装器会在程序目录写入 `LICENSE.txt` 和 `THIRD_PARTY_NOTICES.txt`。
- `THIRD_PARTY_NOTICES.txt` 由 `tools/generate_notices.py` 从构建出的 `FreeView.exe` 和 Go 模块缓存生成。它包含 FreeView、Go 运行时与标准库、上表依赖模块的许可正文和署名，以及 WinSparkle 发布包中的 `COPYING` 和 `COPYING.expat`。
- `THIRD_PARTY_NOTICES.txt` 是可再生成的产物，不入库。`python tools/build_installer.py` 每次构建都会重新生成它，并把它加入安装包。
- 更新依赖时同步本表、`go.mod`、`go.sum` 和受影响的测试，再重新构建并核对 `THIRD_PARTY_NOTICES.txt` 的模块数。
- 引入新的库、移植代码或外部素材时，登记名称、用途、版本、许可、署名和修改。
