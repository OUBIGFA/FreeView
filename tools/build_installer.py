"""Build and verify dist/releases/FreeView-<version>.exe with nsgo.

nsgo v0.3.4 builds a native Go installer, including its uninstaller and
linked-module notices. Put FreeView's GPL and the embedded WASM code's
additional notices in its standard resources before that build; never
patch an installer or depend on a generated NSIS script.
"""

import hashlib
import os
from pathlib import Path
import re
import subprocess
import sys
from zipfile import ZipFile

from generate_notices import embedded_notices

ROOT = Path(__file__).resolve().parents[1]
APP = ROOT / "cmd" / "freeview"
DIST = ROOT / "dist"
RELEASES = DIST / "releases"
LICENSE_NAME = "LICENSE.txt"
NOTICES_NAME = "THIRD_PARTY_NOTICES.txt"
EMBEDDED_NAME = "EMBEDDED_COMPONENT_NOTICES.txt"


def setting(name):
    match = re.search(rf"^{name}:\s*(\S+)\s*$", (APP / "nsgo.yaml").read_text(encoding="utf-8"), re.M)
    if not match:
        sys.exit(f"nsgo.yaml has no {name}")
    return match.group(1)


def verify_payload(installer, name, additions):
    # nsgo's unsigned Windows installer holds a normal ZIP payload after
    # its PE image. zipfile reads the original archive and checks its CRCs;
    # this verification does not execute or modify the installer.
    with ZipFile(installer) as archive:
        expected = {f"{name}.exe", "Uninstall.exe", NOTICES_NAME, *additions}
        if set(archive.namelist()) != expected:
            sys.exit(f"unexpected installer payload: {archive.namelist()}")
        bad = archive.testzip()
        if bad is not None:
            sys.exit(f"installer payload checksum failed: {bad}")
        for filename, content in additions.items():
            if archive.read(filename) != content:
                sys.exit(f"installer did not include the current {filename}")
        notices = archive.read(NOTICES_NAME).decode("utf-8")
        for required in ("github.com/nsgo-dev/nsgo", "Go runtime and standard library", "480 Design", "CC BY 4.0"):
            if required not in notices:
                sys.exit(f"generated notices have no {required}")


def main():
    version, name = setting("version"), setting("name")
    resources = APP / "resources"
    additions = {LICENSE_NAME: (ROOT / "LICENSE").read_bytes(), EMBEDDED_NAME: embedded_notices()}
    # Refuse to replace user-authored resources. These two generated paths
    # are ignored, and the build removes only the files it creates here.
    for filename in additions:
        if (resources / filename).exists():
            sys.exit(f"build resource already exists: {resources / filename}")
    resources.mkdir(exist_ok=True)
    try:
        for filename, content in additions.items():
            (resources / filename).write_bytes(content)
        subprocess.run(["go", "tool", "nsgo", "build"], cwd=APP, check=True)
    finally:
        for filename in additions:
            (resources / filename).unlink(missing_ok=True)
        if not any(resources.iterdir()):
            resources.rmdir()

    slug = name.lower()
    built = RELEASES / f"{slug}-{version}-windows-x64-setup.exe"
    verify_payload(built, name, additions)
    target = RELEASES / f"{name}-{version}.exe"
    os.replace(built, target)
    built.with_name(built.name + ".sha256").unlink(missing_ok=True)
    digest = hashlib.sha256(target.read_bytes()).hexdigest()
    target.with_name(target.name + ".sha256").write_text(f"{digest}  {target.name}\n", encoding="utf-8")
    print(f"{target.relative_to(ROOT)}  {target.stat().st_size} bytes  sha256 {digest}")
    print("Verified app, uninstaller, GPL, module notices and embedded component notices")


if __name__ == "__main__":
    main()
