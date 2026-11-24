# Solar Linear 图标

FreeView 界面上的图标取自 [saoudi-h/solar-icons](https://github.com/saoudi-h/solar-icons) 的 Linear 风格，只复制用到的文件，内容不改。

- 固定提交：`ab241e1bf578dcfe45b0197aee59bd9a08b5fe68`（`@solar-icons/core` 2.3.0）
- 来源压缩包：`solar-icons-ab241e1bf578dcfe45b0197aee59bd9a08b5fe68.tar.gz`，SHA-256 `3a2a416eda190d089eab9283cd99f2f1896ff9005e86fbe0e18195e477a0a4ba`
- 许可：仓库代码为 MIT（[LICENSE](LICENSE)，Copyright (c) 2024 Hakim Saoudi）；图标设计为 CC BY 4.0，作者 [480 Design](https://www.figma.com/community/file/1166831539721848736) 的 Solar Icons Set（[LICENSE-THIRD-PARTY](LICENSE-THIRD-PARTY)）
- 修改：`solar.go` 在运行时把 `stroke="black"` 换成 `stroke="currentColor"`，让图标跟随文字颜色；文件本身保持上游原样

| 文件 | 上游路径 | 用途 | 字节 | SHA-256 |
| --- | --- | --- | --- | --- |
| `magnifier.svg` | `packages/core/svgs/search/Linear/magnifier.svg` | 设置窗口的搜索框 | 300 | `3cf85ed5efae91a9fda3278f9279216d1bcc655a833b8c9c1a3179c6fc263260` |
| `alt-arrow-down.svg` | `packages/core/svgs/arrows/Linear/alt-arrow-down.svg` | 下拉框末尾的箭头（替换 nsgo `ui.Select` 使用的 `lucide.ChevronDown`） | 212 | `eab60f152580c4984bb13af4fa272f5140de787b56aaed2bc4e4d910b73b56b9` |
| `LICENSE` | `LICENSE` | MIT 许可 | 1322 | `79ca906d7d543e10e4630cddb239c3765678fa18d624a33b99b65bf19e51741a` |
| `LICENSE-THIRD-PARTY` | `LICENSE-THIRD-PARTY` | CC BY 4.0 署名说明 | 685 | `055cc14f0eca227d9af49d408e3c5d3407fb688158e123b28f67177e1ec5f7d8` |

按需添加：界面需要新图标时，从同一提交的 `packages/core/svgs/*/Linear/` 复制，在 `solar.go` 加访问函数，并补上本表。nsgo 组件内部写死的 Lucide 图标只能在 `cmd/freeview/icons.go` 中整体替换；FreeView 当前只用到 `ui.Select` 的箭头。
