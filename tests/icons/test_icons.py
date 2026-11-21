import hashlib
import json
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest

from PIL import Image

ROOT = Path(__file__).resolve().parents[2]
GENERATOR = ROOT / "tools/generate_icons.py"
SOURCE = ROOT / "assets/branding/FreeView_logo.png"


class IconTests(unittest.TestCase):
    """Verify real PNG/ICO/ICNS containers rather than generator internals."""

    @classmethod
    def setUpClass(cls):
        temporary = ROOT / "_temp"
        temporary.mkdir(exist_ok=True)
        cls.output = Path(tempfile.mkdtemp(prefix="icon-tests-", dir=temporary))
        cls.result = subprocess.run([sys.executable, str(GENERATOR), "--output", str(cls.output)],
                                    capture_output=True, text=True, encoding="utf-8")

    def require_generated(self):
        self.assertEqual(self.result.returncode, 0, self.result.stdout + self.result.stderr)

    def test_png_sizes_and_transparency(self):
        """Allow at most 2/255 Lanczos coverage in otherwise clear corners."""
        self.require_generated()
        for size in (16, 20, 24, 32, 40, 48, 64, 96, 128, 256, 512, 1024):
            with self.subTest(size=size), Image.open(self.output / f"FreeView_{size}.png") as image:
                self.assertEqual(image.size, (size, size))
                self.assertEqual(image.mode, "RGBA")
                self.assertLessEqual(image.getpixel((0, 0))[3], 2)
                self.assertEqual(image.getchannel("A").getextrema()[1], 255)

    def test_windows_multiframe_icon(self):
        self.require_generated()
        with Image.open(self.output / "FreeView.ico") as icon:
            expected = {(size, size) for size in (16, 20, 24, 32, 40, 48, 64, 128, 256)}
            self.assertEqual(icon.ico.sizes(), expected)

    def test_macos_icon_and_iconset(self):
        self.require_generated()
        with Image.open(self.output / "FreeView.icns") as icon:
            available = {width * scale for width, height, scale in icon.info["sizes"]}
            self.assertTrue({16, 32, 64, 128, 256, 512, 1024}.issubset(available))
        self.assertEqual(len(list((self.output / "FreeView.iconset").glob("*.png"))), 10)

    def test_source_hash_and_output_hashes(self):
        self.require_generated()
        manifest = json.loads((self.output / "manifest.json").read_text(encoding="utf-8"))
        self.assertEqual(manifest["source"]["sha256"], hashlib.sha256(SOURCE.read_bytes()).hexdigest())
        for entry in manifest["files"]:
            data = (self.output / entry["path"]).read_bytes()
            self.assertEqual(entry["bytes"], len(data))
            self.assertEqual(entry["sha256"], hashlib.sha256(data).hexdigest())

    def test_verification_and_repeatability(self):
        self.require_generated()
        before = (self.output / "manifest.json").read_bytes()
        repeat = subprocess.run([sys.executable, str(GENERATOR), "--output", str(self.output)],
                                capture_output=True, text=True, encoding="utf-8")
        self.assertEqual(repeat.returncode, 0, repeat.stdout + repeat.stderr)
        self.assertEqual(before, (self.output / "manifest.json").read_bytes())
        verified = subprocess.run([sys.executable, str(GENERATOR), "--output", str(self.output), "--verify"],
                                  capture_output=True, text=True, encoding="utf-8")
        self.assertEqual(verified.returncode, 0, verified.stdout + verified.stderr)

    def test_rejects_unsafe_output(self):
        result = subprocess.run([sys.executable, str(GENERATOR), "--output", str(SOURCE.parent)],
                                capture_output=True, text=True, encoding="utf-8")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("output must be", result.stdout + result.stderr)

    def test_rejects_changed_icon(self):
        self.require_generated()
        corrupted = self.output.parent / (self.output.name + "-corrupt")
        shutil.copytree(self.output, corrupted)
        (corrupted / "FreeView_16.png").write_bytes(b"changed icon")
        result = subprocess.run([sys.executable, str(GENERATOR), "--output", str(corrupted), "--verify"],
                                capture_output=True, text=True, encoding="utf-8")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("mismatch", result.stdout + result.stderr)


if __name__ == "__main__":
    unittest.main()
