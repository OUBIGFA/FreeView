"""Build the Windows installer as dist/releases/FreeView-<version>.exe.

nsgo names its installer <slug>-<version>-windows-x64-setup.exe and compiles
it from the package directory. FreeView's convention is the brand and version
alone, and the installer also installs the GPL text and the notices of the
software it includes. So this runs `go tool nsgo build` with the local NSIS
on PATH, adds the license files to the package, recompiles its NSIS script and
renames the installer and its checksum.

Recompiling changes the installer, so the appcast nsgo signed for its own
setup no longer matches. With NSGO_UPDATES_KEY set, as a release build has,
tools/appcast signs the final installer and writes appcast-windows_x64.xml
beside it for WinSparkle; without it no appcast is written, as nsgo does.
"""

import hashlib
import os
from pathlib import Path
import re
import subprocess
import sys
import zipfile

ROOT = Path(__file__).resolve().parents[1]
APP = ROOT / "cmd" / "freeview"
DIST = ROOT / "dist"
RELEASES = DIST / "releases"
NSIS = ROOT / ".local" / "tools" / "nsis-3.13" / "Bin"
NOTICES = ROOT / "tools" / "generate_notices.py"
LICENSE = ROOT / "LICENSE"
LICENSE_NAME = "LICENSE.txt"
NOTICES_NAME = "THIRD_PARTY_NOTICES.txt"
APPCAST_NAME = "appcast-windows_x64.xml"
# nsgo v0.2.0 puts WinSparkle.dll from this pinned release, which it keeps
# in the user cache (cmd/nsgo/updates.go, winSparkleRelease), beside the app.
WINSPARKLE = "WinSparkle-0.9.4"
WINSPARKLE_LICENSES = ("COPYING", "COPYING.expat")


def setting(name):
    match = re.search(rf"^{name}:\s*(\S+)\s*$", (APP / "nsgo.yaml").read_text(encoding="utf-8"), re.M)
    if not match:
        sys.exit(f"nsgo.yaml has no {name}")
    return match.group(1)


def winsparkle_licenses():
    """Extract WinSparkle's license files from nsgo's cached release."""
    archive = Path(os.environ["LOCALAPPDATA"]) / "nsgo" / "downloads" / f"{WINSPARKLE}.zip"
    target = DIST / "winsparkle-licenses"
    target.mkdir(parents=True, exist_ok=True)
    with zipfile.ZipFile(archive) as z:
        for name in WINSPARKLE_LICENSES:
            (target / name).write_bytes(z.read(f"{WINSPARKLE}/{name}"))
    return "WinSparkle 0.9.4 (WinSparkle.dll)=" + ",".join(str(target / name) for name in WINSPARKLE_LICENSES)


def setting_block(block, name):
    # The first "name:" in the indented lines under "block:".
    match = re.search(rf"^{block}:\n(?:[ \t]+.*\n)*?[ \t]+{name}:\s*(\S+)\s*$",
                      (APP / "nsgo.yaml").read_text(encoding="utf-8"), re.M)
    if not match:
        sys.exit(f"nsgo.yaml has no {block}.{name}")
    return match.group(1)


def add_licenses(package, name, version, env):
    executable = package / f"{name}.exe"
    script = package / f"{name}.nsi"
    if not executable.is_file() or not script.is_file():
        sys.exit(f"{package} has no {name}.exe or {name}.nsi")
    command = [sys.executable, str(NOTICES), str(executable), str(package / NOTICES_NAME), "--version", version]
    if (package / "WinSparkle.dll").is_file():
        command += ["--extra", winsparkle_licenses()]
    subprocess.run(command, env=env, check=True)
    (package / LICENSE_NAME).write_bytes(LICENSE.read_bytes())

    text = script.read_text(encoding="utf-8-sig")
    output = re.search(r'^OutFile "([^"]+)"', text, re.M)
    if not output:
        sys.exit(f"{script} has no OutFile")
    # The last lines of the uninstaller, after the files nsgo lists.
    old = '  Delete "$INSTDIR\\Uninstall.exe"\n  RMDir "$INSTDIR"'
    new = (
        '  Delete "$INSTDIR\\Uninstall.exe"\n'
        f'  Delete "$INSTDIR\\{LICENSE_NAME}"\n'
        f'  Delete "$INSTDIR\\{NOTICES_NAME}"\n'
        '  RMDir "$INSTDIR"'
    )
    if text.count(old) != 1:
        sys.exit(f"{script} does not have the uninstaller block this script knows")
    script.write_text(text.replace(old, new), encoding="utf-8-sig", newline="\n")
    return script, package / output.group(1)


def main():
    version = setting("version")
    name = setting("name")
    package = DIST / f"{name}-{version}-windows_x64"
    env = dict(os.environ, PATH=str(NSIS) + os.pathsep + os.environ.get("PATH", ""))
    if not (NSIS / "makensis.exe").is_file():
        sys.exit(f"NSIS is missing from {NSIS}")
    subprocess.run(["go", "tool", "nsgo", "build"], cwd=APP, env=env, check=True)

    script, built = add_licenses(package, name, version, env)
    result = subprocess.run(
        [str(NSIS / "makensis.exe"), "-V4", "-INPUTCHARSET", "UTF8", script.name],
        cwd=package, env=env, capture_output=True, text=True,
    )
    if result.returncode != 0:
        sys.exit(f"makensis failed:\n{result.stdout}\n{result.stderr}")
    missing = [file for file in (LICENSE_NAME, NOTICES_NAME) if f'File: "{file}"' not in result.stdout]
    if missing:
        sys.exit(f"the installer did not include {', '.join(missing)}")

    target = RELEASES / f"{name}-{version}.exe"
    os.replace(built, target)
    # nsgo also leaves its own setup and checksum in the release folder; this
    # project releases only the FreeView-named installer.
    for stale in (RELEASES / built.name, RELEASES / (built.name + ".sha256")):
        stale.unlink(missing_ok=True)
    digest = hashlib.sha256(target.read_bytes()).hexdigest()
    # The checksum names the file it covers, so it moves with a new name.
    old_sum = built.with_name(built.name + ".sha256")
    new_sum = target.with_name(target.name + ".sha256")
    if old_sum.exists():
        os.replace(old_sum, new_sum)
    new_sum.write_text(f"{digest}  {target.name}\n", encoding="utf-8")
    print(f"{target.relative_to(ROOT)}  {target.stat().st_size} bytes  sha256 {digest}")
    write_appcast(target, version)


def write_appcast(installer, version):
    appcast = RELEASES / APPCAST_NAME
    # nsgo's appcast, if it wrote one, offers its own setup, which is not released.
    appcast.unlink(missing_ok=True)
    if not os.environ.get("NSGO_UPDATES_KEY"):
        print(f"no {APPCAST_NAME}: set NSGO_UPDATES_KEY to sign a release installer for WinSparkle")
        return
    subprocess.run(
        ["go", "run", "./tools/appcast", "-installer", str(installer), "-version", version,
         "-repo", setting_block("release", "github"), "-key", setting_block("updates", "key"), "-out", str(appcast)],
        cwd=ROOT, check=True,
    )
    print(f"{appcast.relative_to(ROOT)}  signed for {installer.name}")


if __name__ == "__main__":
    main()
