# FreeView 品牌素材

`FreeView_logo.png` 是仓库所有者提供的 1200×1200 RGBA PNG。原始字节不改动；公开使用遵循仓库 [GPL-3.0-only 许可](../../LICENSE)。许可不构成额外商标授权。

`branding.go` 把源图嵌入应用，供设置窗口的“关于”页显示。

原始 SHA-256：`c939bd0ec7fc9d772955daa13aa2ad2881b27b1e6663ab45acf070e4c6319369`。

## 生成图标

在仓库根目录执行：

```powershell
python -m pip install -r tools/requirements-icons.txt
python tools/generate_icons.py
python tools/generate_icons.py --verify
```

生成物在被忽略的 `dist/icons/`。源图保留透明边角和原有比例，不重画、不变色、不裁切。

| 输出 | 用途 |
| --- | --- |
| 16、20、24、32、40、48、64、96、128、256、512、1024 像素 PNG | 高 DPI、菜单、桌面和品牌预览 |
| `FreeView.ico`：16、20、24、32、40、48、64、128、256 像素帧 | Windows 应用与文件关联 |
| `FreeView.icns` 和 `FreeView.iconset/` | macOS 图标；平台显示待后续应用验收 |
| `manifest.json` | 来源、尺寸、文件哈希与容器校验 |

图标与清单是可再生成的派生产物，不加入 Git。修改品牌图时同步精确二进制登记，再重新生成与检查。
