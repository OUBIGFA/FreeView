<div align="center">

<img src="assets/branding/FreeView_logo.png" width="160" alt="FreeView logo">

# FreeView

**An image viewer for Windows.**

[简体中文](README.md) · English

<a href="https://github.com/OUBIGFA/FreeView/releases/latest"><img src="https://img.shields.io/github/v/release/OUBIGFA/FreeView?color=2E7D32" alt="Latest release"></a>
<a href="https://github.com/OUBIGFA/FreeView/releases"><img src="https://img.shields.io/badge/platform-Windows%2010%20%7C%2011-2E7D32?logo=windows&amp;logoColor=white" alt="Windows 10 and 11"></a>
<a href="LICENSE"><img src="https://img.shields.io/badge/license-GPL--3.0--only-2E7D32" alt="GPL-3.0-only license"></a>

</div>

---

## Preview

[![The default FreeView window](assets/screenshots/FreeView-main.png)](assets/screenshots/FreeView-main.png)

<p align="center">
  <a href="assets/screenshots/FreeView-preferences.png"><img src="assets/screenshots/FreeView-preferences.png" width="80%" alt="The FreeView Preferences window"></a>
</p>

[Main window](assets/screenshots/FreeView-main.png) · [Preferences window](assets/screenshots/FreeView-preferences.png)

## About FreeView

FreeView is an image viewer for Windows. It opens images, folders, and archives on your computer. You can look at images in an archive without extracting it first.

## What you can do

- **View images.** Open JPEG, PNG, GIF, WebP, BMP, TIFF, SVG, and ICO images, as well as PSD/PSB, QOI, TGA, DDS, JPEG XR, HDR, and EXR. GIF, WebP, and APNG animations play automatically and support pause, frame stepping, and speed controls.
- **See colors and details.** Photos with Adobe RGB, Display P3, and other color profiles are shown using the display's color profile. Large images shown smaller are resampled once the view settles to reduce jagged lines.
- **View phone and camera photos.** Open HEIC, AVIF, JPEG XL, camera RAW photos (such as DNG, CR3, NEF, and ARW), and iPhone Live Photos (.livp). Export the video from Live Photos and Android motion photos. Windows decodes these formats when it can. HEIC and JPEG XL also open without a Windows extension. See [Notes](#notes).
- **Browse a folder.** Go to the previous, next, first, or last image. Start a slideshow.
- **Open an archive.** Look at images in ZIP, 7z, RAR, and TAR archives.
- **Zoom and move an image.** Zoom in, zoom out, fit the image to the window, or show the original size. If the image is larger than the window, you can drag it.
- **View an image in full screen.** Rotate or flip an image. Show image information, including the camera, lens, shooting settings, and AI image prompts.
- **Organize images.** Move an image to the Recycle Bin, rename it, or copy or move it to another folder. Copy the image or its path. Open it in an external editor, open its file properties, or show it in File Explorer.
- **Find your place.** Turn on the navigator and bottom filmstrip when needed. Both are off by default.
- **Customize the controls.** Change 13 mouse actions and the shortcut keys for each action.
- **Stay up to date.** FreeView checks for a new version once a day. When it finds one, it asks you before it installs it.

## Download and start

### Install FreeView

1. Open the [latest FreeView download page](https://github.com/OUBIGFA/FreeView/releases/latest).
2. Download the Windows installer from the page's assets.
3. Open the downloaded file.
4. Follow the instructions on the screen to install FreeView.

FreeView runs on 64-bit Windows 10 and Windows 11. See the download page for the latest version and release notes.

FreeView checks GitHub Releases for updates. If an older installation has no update checker or still uses the old update mechanism, manually download and run the latest installer from the download page.

The installer installs FreeView for your user account. You do not need administrator rights. After you install FreeView, open it from the Start menu. You can also create a desktop shortcut during installation.

The installer does not change your default apps. FreeView appears in the "Open with" list for images and archives.

To uninstall FreeView, open Settings. Select Apps. Select Installed apps. Select FreeView.

### Open an image

1. Open FreeView.
2. Drag an image, a folder, or an archive into the window. You can also press Ctrl+O and select a file.
3. Use the wheel or the arrow keys to change images.
4. Right-click to open the themed menu. Full screen, slideshow, information, always-on-top, title bar, overview, and filmstrip switches are directly accessible; zoom, transforms, animation, and file actions have their own submenus.

The window has no title bar by default. To move the window, drag anywhere in it. When a zoomed image is larger than the window, dragging the image moves the image; then drag the background beside the image or the top edge of the window to move the window.

### Common controls

| Action | Result |
| --- | --- |
| Wheel up or down | Previous or next image |
| Ctrl + wheel up or down | Zoom in or out |
| Hold the left button and drag | Move the window; on an image larger than the window, move the image |
| Double-click the left button | Full screen |
| Right-click | Open the menu |
| P | Open Preferences |
| Alt | Show the menu bar |

## Options

- **Change mouse actions.** Open Preferences. On the Mouse page, select an action for each item.
- **Change keyboard shortcuts.** On the Keyboard page, add or remove shortcuts. Click Record. Then press a key.
- **Change how images appear.** On the Display page, choose the fit mode, smooth downscaling, color management, image information, and the navigator.
- **Change browsing and playback.** On the Browse page, choose the sort order, looping, slideshow interval, animation speed, filmstrip, and edge clicks.
- **Change window behavior.** On the General page, adjust the title bar, always-on-top mode, saved window size, startup maximization, and multiple windows.
- **See the top title.** With the title bar hidden, move the pointer to the top of the window to see the full image title. On the General page, turn this feature off or change the title position.
- **Restore the default settings.** Click Restore defaults at the bottom of the sidebar and confirm.
- **Manage file actions.** On the Files page, set the external editor, the target folder for copy and move, and deletion confirmation.
- **See the version and check for updates.** On the About page, see the version, open the GitHub repository, report a problem, or click Check now.

## Notes

- This version of the installer is not digitally signed. Windows can show an "Unknown publisher" message.
- FreeView reuses one main window by default. If you open another image, it appears in that window. Turn on "Allow several images open at once" on the General page in Preferences to open each image in a new window.
- Images inside archives cannot be deleted, renamed, or moved.
- Very large animations (all frames exceeding half of the available cache) cannot be stepped through and only play automatically.
- AVIF and RAW photos need a Windows extension from the Microsoft Store. If the extension is missing, FreeView tells you which one to install:
  - AVIF: AV1 Video Extension.
  - RAW: Raw Image Extension. If the photo contains a full-size preview from the camera, FreeView shows the preview, because it opens faster.
- HEIC and JPEG XL do not need an extension. Without one, FreeView opens them with its own decoder, which is slower and uses more memory. With the extension (HEIC: HEIF Image Extensions and HEVC Video Extensions; JPEG XL: Windows 11 24H2 and JPEG XL Image Extension), they open faster.
- FreeView shows only the first frame of HEIC, AVIF, JPEG XL, and RAW photos. Text in SVG images may not show.

## Sources and license

FreeView is open-source software. It uses the [GPL-3.0-only](LICENSE) license. The source code is in the [GitHub repository](https://github.com/OUBIGFA/FreeView).

[GPL-3.0-only license](LICENSE) · [Third-party components](third_party/README.md)
