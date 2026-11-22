"""Build the local test installer as dist/releases/FreeView-<version>.exe.

nsgo names its installer <slug>-<version>-windows-x64-setup.exe; FreeView's
convention is the brand and version alone, so this runs `go tool nsgo build`
with the local NSIS on PATH and renames the installer and its checksum.
"""

import hashlib
import os
from pathlib import Path
import re
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]
APP = ROOT / "cmd" / "freeview"
RELEASES = ROOT / "dist" / "releases"
NSIS = ROOT / ".local" / "tools" / "nsis-3.13" / "Bin"


def main():
    match = re.search(r"^version:\s*(\S+)\s*$", (APP / "nsgo.yaml").read_text(encoding="utf-8"), re.M)
    if not match:
        sys.exit("nsgo.yaml has no version")
    version = match.group(1)
    env = dict(os.environ, PATH=str(NSIS) + os.pathsep + os.environ.get("PATH", ""))
    subprocess.run(["go", "tool", "nsgo", "build"], cwd=APP, env=env, check=True)

    built = RELEASES / f"freeview-{version}-windows-x64-setup.exe"
    target = RELEASES / f"FreeView-{version}.exe"
    os.replace(built, target)
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
