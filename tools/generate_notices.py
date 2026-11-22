"""Write the third-party notices that the Windows installer installs.

The modules come from `go version -m` on the built executable, so the file
lists exactly what the binary contains. Each module's license and notice
files come from the Go module cache at the version the build used.
"""

import argparse
import json
import os
from pathlib import Path
import re
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]
LICENSE_NAME = re.compile(r"^(?:LICEN[CS]E|COPYING|NOTICE|PATENT)", re.IGNORECASE)


def run(*arguments):
    result = subprocess.run(arguments, capture_output=True, text=True)
    if result.returncode != 0:
        sys.exit(f"{' '.join(arguments)} failed: {result.stderr.strip()}")
    return result.stdout


def module_dir(path, version):
    metadata = json.loads(run("go", "list", "-m", "-json", f"{path}@{version}"))
    directory = metadata.get("Dir")
    if not directory:
        sys.exit(f"{path}@{version} is not in the module cache")
    return Path(directory)


def license_files(directory):
    names = sorted(p.name for p in directory.iterdir() if p.is_file() and LICENSE_NAME.match(p.name))
    if not names:
        sys.exit(f"{directory} has no license file")
    return names


def section(title, files):
    lines = [
        "=" * 78,
        title,
        "=" * 78,
    ]
    for name, path in files:
        lines += ["", name, "-" * 78, ""]
        text = path.read_text(encoding="utf-8").replace("\r\n", "\n").replace("\r", "\n")
        lines.append(text.rstrip("\n"))
    lines.append("")
    return "\n".join(lines) + "\n"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("exe", type=Path, help="the built FreeView.exe")
    parser.add_argument("output", type=Path, help="where to write THIRD_PARTY_NOTICES.txt")
    parser.add_argument("--version", required=True, help="the FreeView version")
    arguments = parser.parse_args()

    executable = arguments.exe.resolve()
    if not executable.is_file():
        sys.exit(f"{executable} does not exist")
    build = run("go", "version", "-m", str(executable))
    modules = re.findall(r"^\tdep\t(\S+)\t(\S+)", build, re.MULTILINE)
    if not modules:
        sys.exit(f"{executable} lists no module dependencies")

    go_root = Path(run("go", "env", "GOROOT").strip())
    go_version = run("go", "env", "GOVERSION").strip()
    own_license = ROOT / "LICENSE"
    if not own_license.is_file():
        sys.exit("LICENSE is missing")

    parts = [
        "\n".join([
            "FreeView third-party notices",
            "",
            f"FreeView {arguments.version}",
            "Copyright (C) 2026 FreeView contributors",
            "FreeView is licensed under GPL-3.0-only.",
            "Source code: https://github.com/OUBIGFA/FreeView",
            "",
            "This file is installed with FreeView. It reproduces the licenses and notices",
            "of the software FreeView includes: FreeView itself, its dependencies, the Go",
            "runtime and the Go standard library.",
            "",
        ]) + "\n",
    ]
    parts.append(section(f"FreeView {arguments.version}", [("LICENSE", own_license)]))

    go_files = [("LICENSE", go_root / "LICENSE")]
    if (go_root / "PATENTS").is_file():
        go_files.append(("PATENTS", go_root / "PATENTS"))
    parts.append(section(f"Go runtime and standard library ({go_version})", go_files))

    for path, version in modules:
        directory = module_dir(path, version)
        files = [(name, directory / name) for name in license_files(directory)]
        parts.append(section(f"{path} {version}", files))

    arguments.output.parent.mkdir(parents=True, exist_ok=True)
    arguments.output.write_text("".join(parts), encoding="utf-8", newline="\n")
    print(f"{arguments.output}  {arguments.output.stat().st_size} bytes  {len(modules)} modules")


if __name__ == "__main__":
    main()
