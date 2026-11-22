"""Build the Windows installer as dist/releases/FreeView-<version>.exe.

nsgo names its installer <slug>-<version>-windows-x64-setup.exe and compiles
it from the package directory. FreeView's convention is the brand and version
alone, and the installer also installs the GPL text and the notices of the
software it includes. So this runs `go tool nsgo build` with the local NSIS
on PATH, adds the license files to the package, recompiles its NSIS script and
renames the installer and its checksum.
"""

import hashlib
import os
from pathlib import Path
import re
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]
APP = ROOT / "cmd" / "freeview"
DIST = ROOT / "dist"
RELEASES = DIST / "releases"
NSIS = ROOT / ".local" / "tools" / "nsis-3.13" / "Bin"
NOTICES = ROOT / "tools" / "generate_notices.py"
LICENSE = ROOT / "LICENSE"
LICENSE_NAME = "LICENSE.txt"
NOTICES_NAME = "THIRD_PARTY_NOTICES.txt"


def setting(name):
    match = re.search(rf"^{name}:\s*(\S+)\s*$", (APP / "nsgo.yaml").read_text(encoding="utf-8"), re.M)
    if not match:
        sys.exit(f"nsgo.yaml has no {name}")
    return match.group(1)


def add_licenses(package, name, version, env):
    executable = package / f"{name}.exe"
    script = package / f"{name}.nsi"
    if not executable.is_file() or not script.is_file():
        sys.exit(f"{package} has no {name}.exe or {name}.nsi")
    subprocess.run(
        [sys.executable, str(NOTICES), str(executable), str(package / NOTICES_NAME), "--version", version],
        env=env, check=True,
    )
    (package / LICENSE_NAME).write_bytes(LICENSE.read_bytes())

    text = script.read_text(encoding="utf-8-sig")
    output = re.search(r'^OutFile "([^"]+)"', text, re.M)
    if not output:
        sys.exit(f"{script} has no OutFile")
    old = f'  Delete "$INSTDIR\\{name}.exe"\n  Delete "$INSTDIR\\Uninstall.exe"\n  RMDir "$INSTDIR"'
    new = (
        f'  Delete "$INSTDIR\\{name}.exe"\n'
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


if __name__ == "__main__":
    main()
