"""Write notices for code embedded in HEIC and JPEG XL decoder modules.

nsgo generates linked Go-module notices itself. Its module scan does not
reach all the licenses of the Rust/C++ code these modules embed as WASM,
so the installer carries their registered original notices beside it.
"""

import argparse
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
EMBEDDED = {
    "HEIC decoder (gen2brain/heic, embedded Rust heic and dependencies)": "third_party/licenses/heic-wasm-notices.txt",
    "JPEG XL decoder (gen2brain/jpegxl, embedded libjxl and Highway)": "third_party/licenses/jpegxl-wasm-notices.txt",
}


def embedded_notices():
    parts = ["FreeView embedded component notices\n\n"
             "These original notices supplement THIRD_PARTY_NOTICES.txt, which nsgo\n"
             "generates for the linked Go modules, Solar icons and Go runtime.\n"]
    for title, relative in EMBEDDED.items():
        source = ROOT / relative
        text = source.read_text(encoding="utf-8").rstrip("\n")
        parts.append(f"\n{'=' * 78}\n{title}\n{'=' * 78}\n\n{text}\n")
    return "".join(parts).encode("utf-8")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("output", type=Path)
    args = parser.parse_args()
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_bytes(embedded_notices())
    print(f"{args.output}  {args.output.stat().st_size} bytes")


if __name__ == "__main__":
    main()
