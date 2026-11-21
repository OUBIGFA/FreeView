import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
CHECKER = ROOT / "tools/check_repository.py"
IGNORE = ".local/\n.nsgo/\n/dist/\noutput/\nnode_modules/\n.env\n**/_temp/\n"


class RepositoryTests(unittest.TestCase):
    """Exercise real staged blobs, including changes hidden by the worktree."""

    @classmethod
    def setUpClass(cls):
        temporary = ROOT / "_temp"
        temporary.mkdir(exist_ok=True)
        cls.sandbox = Path(tempfile.mkdtemp(prefix="repository-tests-", dir=temporary))
        cls.template = cls.sandbox / "template"
        cls.template.mkdir()
        cls.environment = dict(os.environ, GIT_CONFIG_NOSYSTEM="1", GIT_CONFIG_GLOBAL=os.devnull)
        subprocess.run(["git", "init", "--initial-branch=main", str(cls.template)],
                       env=cls.environment, check=True, capture_output=True)
        (cls.template / ".gitignore").write_text(IGNORE, encoding="utf-8")
        (cls.template / "README.md").write_text("# Public fixture\n", encoding="utf-8")
        subprocess.run(["git", "-c", "core.autocrlf=false", "add", "."],
                       cwd=cls.template, env=cls.environment, check=True, capture_output=True)

    def setUp(self):
        self.repo = self.sandbox / self._testMethodName
        shutil.copytree(self.template, self.repo)

    def git(self, *arguments, data=None):
        return subprocess.run(["git", "-c", "core.autocrlf=false", "-c", "core.protectNTFS=false", *arguments],
                              cwd=self.repo, env=self.environment, input=data,
                              check=True, capture_output=True).stdout

    def stage(self, name, data, mode="100644"):
        content = data.encode("utf-8") if isinstance(data, str) else data
        digest = self.git("hash-object", "-w", "--stdin", data=content).decode().strip()
        self.git("update-index", "--add", "--cacheinfo", f"{mode},{digest},{name}")
        return digest

    def check(self, expected=None):
        result = subprocess.run([sys.executable, str(CHECKER), "--root", str(self.repo)],
                                env=self.environment, capture_output=True, text=True, encoding="utf-8")
        output = result.stdout + result.stderr
        if expected is None:
            self.assertEqual(result.returncode, 0, output)
            self.assertIn("Repository index OK", output)
        else:
            self.assertNotEqual(result.returncode, 0, output)
            self.assertIn(expected, output)
        return output

    def test_clean_index(self):
        self.check()

    def test_empty_index(self):
        self.git("rm", "-r", "--cached", ".")
        self.check("empty index")

    def test_nested_temporary_file(self):
        self.stage("nested/_temp/private.txt", "local")
        self.check("local-only path")

    def test_final_ignore_rule(self):
        changed = IGNORE + "!nested/_temp/**\n"
        self.stage(".gitignore", changed)
        (self.repo / ".gitignore").write_text(changed, encoding="utf-8")
        self.check("final active rule")

    def test_changed_worktree_ignore(self):
        (self.repo / ".gitignore").write_text("", encoding="utf-8")
        self.check("differs from index")

    def test_staged_secret_is_not_hidden_by_clean_worktree(self):
        token = "ghp_" + "A" * 36
        self.stage("config.txt", token)
        (self.repo / "config.txt").write_text("clean", encoding="utf-8")
        output = self.check("credential pattern")
        self.assertNotIn(token, output)

    def test_machine_path(self):
        self.stage("config.txt", "/".join(("C:", "Users", "Someone", "sdk", "go")))
        self.check("machine-private path")

    def test_invalid_utf8(self):
        self.stage("broken.txt", b"\xff\xfe")
        self.check("UTF-8")

    def test_utf8_bom(self):
        self.stage("bom.txt", b"\xef\xbb\xbfhello")
        self.check("BOM")

    def test_text_nul(self):
        self.stage("text-nul.txt", b"hello\x00world")
        self.check("NUL")

    def test_invalid_json(self):
        self.stage("broken.json", "{not json}")
        self.check("invalid JSON")

    def test_unregistered_binary(self):
        self.stage("asset.png", b"\x89PNG\r\n\x1a\n\x00")
        self.check("unregistered binary")

    def test_large_file(self):
        self.stage("large.txt", b"a" * (1024 * 1024 + 1))
        self.check("size limit")

    def test_case_collision(self):
        self.stage("readme.md", "lowercase")
        self.check("case collision")

    def test_windows_reserved_name(self):
        self.stage("pictures/CON.txt", "reserved")
        self.check("Windows-incompatible path")

    def test_windows_trailing_dot(self):
        self.stage("name.", "trailing")
        self.check("Windows-incompatible path")

    def test_symlink(self):
        self.stage("link", "README.md", mode="120000")
        self.check("symlink")

    def test_unresolved_conflict(self):
        digest = self.git("hash-object", "-w", "--stdin", data=b"conflict").decode().strip()
        entries = f"100644 {digest} 1\tconflict.txt\n100644 {digest} 2\tconflict.txt\n"
        self.git("update-index", "--index-info", data=entries.encode())
        self.check("unresolved conflict")

    def test_ignored_tracked_file(self):
        changed = "hidden.txt\n" + IGNORE
        self.stage(".gitignore", changed)
        (self.repo / ".gitignore").write_text(changed, encoding="utf-8")
        self.stage("hidden.txt", "hidden")
        self.check("tracked file is ignored")

    def test_missing_ignore_boundary(self):
        changed = IGNORE.replace(".local/\n", "")
        self.stage(".gitignore", changed)
        (self.repo / ".gitignore").write_text(changed, encoding="utf-8")
        self.check("ignore boundary")

    def test_broken_markdown_link(self):
        self.stage("README.md", "[Missing](missing.md)\n")
        self.check("broken relative link")

    def test_link_uses_index_not_worktree(self):
        (self.repo / "untracked.md").write_text("not staged", encoding="utf-8")
        self.stage("README.md", "[Untracked](untracked.md)\n")
        self.check("broken relative link")

    def test_valid_relative_link(self):
        self.stage("docs/guide.md", "[Root](../README.md)\n")
        self.stage("README.md", "[Guide](docs/guide.md)\n[Web](https://example.com)\n")
        self.check()

    def test_registered_binary(self):
        content = b"\x89PNG\r\n\x1a\n\x00"
        self.stage("assets/logo.png", content)
        entry = {"path": "assets/logo.png", "source": "fixture owner", "license": "MIT",
                 "reason": "public test fixture", "bytes": len(content),
                 "sha256": hashlib.sha256(content).hexdigest()}
        self.stage("references/repository-exceptions.json", json.dumps({"binaries": [entry]}))
        self.check()

    def test_registered_binary_hash_mismatch(self):
        content = b"\x00fixture"
        self.stage("assets/fixture.bin", content)
        entry = {"path": "assets/fixture.bin", "source": "fixture owner", "license": "MIT",
                 "reason": "public fixture", "bytes": len(content), "sha256": "0" * 64}
        self.stage("references/repository-exceptions.json", json.dumps({"binaries": [entry]}))
        self.check("binary registration mismatch")


if __name__ == "__main__":
    unittest.main()
