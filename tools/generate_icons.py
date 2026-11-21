"""Generate and verify deterministic FreeView icons from the original logo."""

import argparse
import hashlib
import json
from pathlib import Path
import struct
import sys

import PIL
from PIL import Image

ROOT = Path(__file__).resolve().parents[1]
SOURCE = ROOT / "assets/branding/FreeView_logo.png"
PNG_SIZES = (16, 20, 24, 32, 40, 48, 64, 96, 128, 256, 512, 1024)
ICO_SIZES = (16, 20, 24, 32, 40, 48, 64, 128, 256)
ICONSET = {f"icon_{size}x{size}{suffix}.png": size * scale
           for size in (16, 32, 128, 256, 512)
           for suffix, scale in (("", 1), ("@2x", 2))}


def file_record(path, relative):
    data = path.read_bytes()
    return {"path": relative, "bytes": len(data), "sha256": hashlib.sha256(data).hexdigest()}


def source_record():
    record = file_record(SOURCE, SOURCE.relative_to(ROOT).as_posix())
    with Image.open(SOURCE) as original:
        record["width"], record["height"] = original.size
    return record


def expected_files():
    files = {f"FreeView_{size}.png": size for size in PNG_SIZES}
    files.update({"FreeView.iconset/" + name: size for name, size in ICONSET.items()})
    files.update({"FreeView.ico": None, "FreeView.icns": None})
    return files


def write_icns(output):
    """Include small non-retina frames that Pillow's ICNS writer omits."""
    sizes = {b"icp4": 16, b"icp5": 32, b"icp6": 64, b"ic07": 128,
             b"ic08": 256, b"ic09": 512, b"ic10": 1024,
             b"ic11": 32, b"ic12": 64, b"ic13": 256, b"ic14": 512}
    chunks = []
    for code, size in sizes.items():
        payload = (output / f"FreeView_{size}.png").read_bytes()
        chunks.append(code + struct.pack(">I", len(payload) + 8) + payload)
    data = b"".join(chunks)
    (output / "FreeView.icns").write_bytes(b"icns" + struct.pack(">I", len(data) + 8) + data)


def verify(output):
    manifest = json.loads((output / "manifest.json").read_text(encoding="utf-8"))
    if manifest["source"] != source_record():
        raise ValueError("source logo differs from the icon manifest")
    expected = expected_files()
    entries = manifest["files"]
    if len(entries) != len(expected) or {entry["path"] for entry in entries} != set(expected):
        raise ValueError("icon manifest contains missing or unexpected files")
    for entry in entries:
        relative = entry["path"]
        path = output / relative
        if file_record(path, relative) != entry:
            raise ValueError("icon hash or size mismatch: " + relative)
        if relative.endswith(".png"):
            with Image.open(path) as image:
                if image.size != (expected[relative], expected[relative]) or image.mode != "RGBA":
                    raise ValueError("invalid PNG dimensions or mode: " + relative)
    with Image.open(output / "FreeView.ico") as icon:
        if icon.ico.sizes() != {(size, size) for size in ICO_SIZES}:
            raise ValueError("ICO does not contain all required sizes")
    header = (output / "FreeView.ico").read_bytes()[:6]
    if struct.unpack("<HHH", header) != (0, 1, len(ICO_SIZES)):
        raise ValueError("invalid ICO container")
    with Image.open(output / "FreeView.icns") as icon:
        available = {width * scale for width, height, scale in icon.info["sizes"]}
        if not {16, 32, 64, 128, 256, 512, 1024}.issubset(available):
            raise ValueError("ICNS does not contain all required sizes")
    data = (output / "FreeView.icns").read_bytes()
    if data[:4] != b"icns" or struct.unpack(">I", data[4:8])[0] != len(data):
        raise ValueError("invalid ICNS container")
    return len(entries)


def generate(output):
    with Image.open(SOURCE) as original:
        if original.width != original.height or original.width < 1024:
            raise ValueError("source logo must be square and at least 1024 pixels")
        image = original.convert("RGBA")
    output.mkdir(parents=True, exist_ok=True)
    iconset = output / "FreeView.iconset"
    iconset.mkdir(exist_ok=True)
    for relative, size in expected_files().items():
        if size is not None:
            image.resize((size, size), Image.Resampling.LANCZOS).save(output / relative, format="PNG")
    image.save(output / "FreeView.ico", format="ICO", sizes=[(size, size) for size in ICO_SIZES])
    write_icns(output)
    manifest = {"source": source_record(),
                "generator": {"library": "Pillow", "version": PIL.__version__, "resampling": "LANCZOS"},
                "files": [file_record(output / name, name) for name in sorted(expected_files())]}
    (output / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n", encoding="utf-8")
    return verify(output)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, default=ROOT / "dist/icons")
    parser.add_argument("--verify", action="store_true", help="verify existing files without rewriting them")
    arguments = parser.parse_args()
    try:
        output = arguments.output.resolve()
        try:
            relative = output.relative_to(ROOT)
        except ValueError:
            raise ValueError("output must be inside this repository's dist/ or _temp/") from None
        if not relative.parts or relative.parts[0] not in {"dist", "_temp"}:
            raise ValueError("output must be inside this repository's dist/ or _temp/")
        count = verify(output) if arguments.verify else generate(output)
    except (OSError, ValueError, KeyError, TypeError, struct.error) as error:
        print("ERROR: " + str(error), file=sys.stderr)
        return 1
    action = "Verified" if arguments.verify else "Generated and verified"
    print(f"{action} {count} icon files: {output.relative_to(ROOT).as_posix()}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
