# 第三方来源与许可

当前没有移植 qView 应用代码，也未引入 nsgo 运行时。参考来源、开发工具与实际分发依赖分别登记，不能把候选名单当作许可审查已经完成。

| 来源 | 当前用途 | 固定依据 | 许可与署名 |
| --- | --- | --- | --- |
| [qView](https://github.com/jurplel/qView) | 功能与实现参考；不导入 Qt 工程 | `b8b993a5e0433ba7fa066e68d2346cc7dd1a7a8c` | GPL-3.0，通用许可正文见根 [LICENSE](../LICENSE) |
| [nsgo](https://github.com/nsgo-dev/nsgo) | 后续原生界面引擎；当前仅保留来源记录 | 稳定版 v0.2.0；调查提交 `fa11ccdcc082c70e58fd30a5889e623bca252b08` | [Apache-2.0](licenses/nsgo-Apache-2.0.txt)、[NOTICE](notices/nsgo-NOTICE.txt) |
| [Pillow](https://github.com/python-pillow/Pillow) | 开发时生成图标；不随应用分发 | [PyPI 12.2.0](https://pypi.org/project/pillow/12.2.0/)、[固定依赖文件](../tools/requirements-icons.txt) | MIT-CMU，许可随安装包提供 |
| FreeView logo | 仓库所有者提供的品牌源图 | [精确二进制登记](../references/repository-exceptions.json) | GPL-3.0-only；不另行授予商标权 |

nsgo NOTICE 的保留只记录来源，不表示其中所有组件都被 FreeView 使用或分发。实际引入时按发布范围保留适用署名，核对 Go 锁文件中的直接和传递依赖。

## 后续实际引入

实际引入库、移植代码或外部素材后，再建立对应 manifest。每项记录名称、用途、消费者、来源/下载 URL、准确版本或提交、字节数、SHA-256、SPDX 许可、许可正文、署名、依赖关系、修改和验证命令。

- 移植 qView 源码或翻译实现时保留来源、许可与修改说明，不能更名后宣称原创。
- nsgo 跟进最新正式稳定版，同时固定已验证版本、更新锁文件与登记。
- 未核对来源和许可的组件标为未完成，不以空 manifest 或只改版本号代替审查。
- 安装包、模型、字体、工具链与下载缓存不入库。公开夹具只使用有许可的最小输入。
- 发布前检查全部适用依赖与署名。本阶段不创建虚构运行时清单或空 manifest 目录。
