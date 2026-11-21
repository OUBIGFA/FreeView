"""Run the same repository checks locally and in read-only CI."""

import argparse
from pathlib import Path
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--icons", action="store_true", help="also test and verify generated icons")
    arguments = parser.parse_args()
    commands = [[sys.executable, "tools/check_repository.py"],
                [sys.executable, "-m", "unittest", "discover", "-s", "tests/repository", "-v"]]
    if arguments.icons:
        commands += [[sys.executable, "-m", "unittest", "discover", "-s", "tests/icons", "-v"],
                     [sys.executable, "tools/generate_icons.py", "--verify"]]
    for command in commands:
        result = subprocess.run(command, cwd=ROOT)
        if result.returncode:
            return result.returncode
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
