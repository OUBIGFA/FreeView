"""Check publishable Git index blobs; never trust clean worktree contents."""

import argparse
import hashlib
import json
from pathlib import Path, PurePosixPath
import posixpath
import re
import subprocess
import sys
import unicodedata
from urllib.parse import unquote, urlsplit

MAX_BYTES = 1024 * 1024
LOCAL_DIRECTORIES = {"_temp", ".local", ".nsgo", "output", "dist", "bin", "build",
                     "node_modules", ".venv", "__pycache__", ".idea", ".vscode",
                     ".agent", ".agents", ".claude", ".codex"}
PRIVATE_SUFFIXES = {".log", ".key", ".pem", ".p12", ".pfx", ".jks", ".keystore"}
BINARY_SUFFIXES = {".png", ".jpg", ".jpeg", ".gif", ".webp", ".ico", ".icns", ".bin",
                   ".exe", ".dll", ".zip", ".pdf", ".woff", ".woff2", ".ttf", ".otf"}
CREDENTIAL = re.compile(r"gh[pousr]_[A-Za-z0-9]{36,255}|github_pat_[A-Za-z0-9_]{60,}"
                        r"|AKIA[0-9A-Z]{16}|sk-(?:proj-)?[A-Za-z0-9_-]{32,}"
                        r"|-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----")
MACHINE_PATH = re.compile(r"(?<![A-Za-z0-9])[A-Za-z]:[\\/]|/(?:Users|home)/[^/\s]+/")
RESERVED = re.compile(r"^(CON|PRN|AUX|NUL|COM[1-9]|LPT[1-9])(?:[.]|$)", re.IGNORECASE)


def git(root, *arguments, data=None, allowed=(0,)):
    result = subprocess.run(["git", *arguments], cwd=root, input=data, capture_output=True)
    if result.returncode not in allowed:
        raise RuntimeError("Git operation failed")
    return result.stdout


def read_index(root):
    entries = []
    for record in git(root, "ls-files", "--stage", "-z").split(b"\0"):
        if not record:
            continue
        metadata, name = record.split(b"\t", 1)
        mode, digest, stage = metadata.decode("ascii").split()
        entries.append((mode, digest, stage, name.decode("utf-8")))
    digests = list(dict.fromkeys(entry[1] for entry in entries if entry[2] == "0"))
    response = git(root, "cat-file", "--batch", data="".join(value + "\n" for value in digests).encode())
    blobs = {}
    position = 0
    for digest in digests:
        newline = response.index(b"\n", position)
        metadata = response[position:newline].decode("ascii").split()
        if len(metadata) != 3 or metadata[1] != "blob":
            raise RuntimeError("Index entry is not a blob")
        size = int(metadata[2])
        position = newline + 1
        blobs[digest] = response[position:position + size]
        position += size + 1
    return entries, blobs


def ignored_paths(root, names):
    if not names:
        return set()
    payload = b"\0".join(name.encode("utf-8") for name in names) + b"\0"
    output = git(root, "check-ignore", "--no-index", "-z", "--stdin", data=payload, allowed=(0, 1))
    return {name.decode("utf-8") for name in output.split(b"\0") if name}


def binary_registry(files, errors):
    manifest = files.get("references/repository-exceptions.json")
    if manifest is None:
        return {}
    try:
        document = json.loads(manifest.decode("utf-8"))
        entries = document.get("binaries", [])
        if not isinstance(entries, list):
            raise ValueError("Invalid binary entries")
        registry = {}
        for entry in entries:
            path = entry["path"]
            if not isinstance(path, str) or path in registry:
                raise ValueError("Invalid or duplicate binary path")
            if not all(isinstance(entry[field], str) and entry[field]
                       for field in ("source", "license", "reason", "sha256")):
                raise ValueError("Missing binary provenance")
            if type(entry["bytes"]) is not int or not re.fullmatch(r"[0-9a-f]{64}", entry["sha256"]):
                raise ValueError("Invalid binary digest or size")
            registry[path] = entry
        for path in registry:
            if path not in files:
                errors.append(f"{path}: registered binary is missing from index")
        return registry
    except (UnicodeError, json.JSONDecodeError, ValueError, KeyError, TypeError, AttributeError):
        errors.append("references/repository-exceptions.json: invalid JSON or binary registration")
        return {}


def markdown_links(path, content, files, errors):
    visible = re.sub(r"(?ms)^```[^\n]*\n.*?^```[^\n]*", "", content)
    visible = re.sub(r"`[^`\n]*`", "", visible)
    targets = re.findall(r"\[[^\]]*\]\(([^)]+)\)", visible)
    targets += re.findall(r"(?m)^\s*\[[^\]]+\]:\s*(\S+)", visible)
    for target in targets:
        target = target.strip().split(' "', 1)[0].strip("<>")
        parsed = urlsplit(target)
        if parsed.scheme or parsed.netloc or not parsed.path:
            continue
        resolved = posixpath.normpath(posixpath.join(posixpath.dirname(path), unquote(parsed.path)))
        if resolved not in files and not any(name.startswith(resolved + "/") for name in files):
            errors.append(f"{path}: broken relative link to {target}")


def check_repository(root):
    errors = []
    entries, blobs = read_index(root)
    if not entries:
        return ["empty index: stage the intended public files first"], 0
    files = {name: blobs[digest] for mode, digest, stage, name in entries if stage == "0"}
    registry = binary_registry(files, errors)
    casing = {}
    texts = {}
    for mode, digest, stage, name in entries:
        if stage != "0":
            errors.append(f"{name}: unresolved conflict")
            continue
        if mode != "100644" and mode != "100755":
            kind = "symlink" if mode == "120000" else "unsupported index mode"
            errors.append(f"{name}: {kind}; declare a reviewed consumer before tracking it")
            continue
        parts = PurePosixPath(name).parts
        if any(part in LOCAL_DIRECTORIES for part in parts[:-1]) or parts[-1].startswith(".env") \
                or PurePosixPath(name).suffix.lower() in PRIVATE_SUFFIXES:
            errors.append(f"{name}: local-only path")
        if any(RESERVED.match(part) or part.endswith((" ", "."))
               or any(ord(character) < 32 or character in '<>:"\\|?*' for character in part)
               for part in parts):
            errors.append(f"{name}: Windows-incompatible path")
        for count in range(1, len(parts) + 1):
            prefix = "/".join(parts[:count])
            folded = unicodedata.normalize("NFC", prefix).casefold()
            if folded in casing and casing[folded] != prefix:
                errors.append(f"{name}: case collision with {casing[folded]}")
            casing[folded] = prefix
        data = blobs[digest]
        if len(data) > MAX_BYTES:
            errors.append(f"{name}: size limit exceeded ({MAX_BYTES} bytes)")
            continue
        if name in registry:
            entry = registry[name]
            if len(data) != entry["bytes"] or hashlib.sha256(data).hexdigest() != entry["sha256"]:
                errors.append(f"{name}: binary registration mismatch")
            continue
        if PurePosixPath(name).suffix.lower() in BINARY_SUFFIXES:
            errors.append(f"{name}: unregistered binary")
            continue
        if b"\0" in data:
            errors.append(f"{name}: NUL in text or unregistered binary")
            continue
        if data.startswith(b"\xef\xbb\xbf"):
            errors.append(f"{name}: UTF-8 BOM is not allowed")
        try:
            content = data.decode("utf-8")
        except UnicodeDecodeError:
            errors.append(f"{name}: invalid UTF-8 or unregistered binary")
            continue
        texts[name] = content
        if CREDENTIAL.search(content):
            errors.append(f"{name}: credential pattern detected (value withheld)")
        if MACHINE_PATH.search(content):
            errors.append(f"{name}: machine-private path detected")
        if name.endswith(".json"):
            try:
                json.loads(content)
            except json.JSONDecodeError:
                errors.append(f"{name}: invalid JSON")
    for name, data in files.items():
        if PurePosixPath(name).name == ".gitignore":
            working = root / name
            if not working.is_file() or working.read_bytes() != data:
                errors.append(f"{name}: worktree .gitignore differs from index")
    active_rules = [line.strip() for line in texts.get(".gitignore", "").splitlines()
                    if line.strip() and not line.lstrip().startswith("#")]
    if not active_rules or active_rules[-1] != "**/_temp/":
        errors.append(".gitignore: **/_temp/ must be the final active rule")
    for name in sorted(ignored_paths(root, list(files))):
        errors.append(f"{name}: tracked file is ignored")
    probes = ["_temp/probe.txt", "nested/_temp/probe.txt", ".local/probe", ".nsgo/probe",
              "dist/probe", "output/probe", "nested/output/probe", "node_modules/probe", ".env"]
    for name in files:
        if name.endswith("/.gitignore"):
            probes.append(posixpath.dirname(name) + "/_temp/probe.txt")
    ignored = ignored_paths(root, probes)
    for probe in probes:
        if probe not in ignored:
            errors.append(f".gitignore: missing ignore boundary for {probe}")
    for name, content in texts.items():
        if name.endswith(".md"):
            markdown_links(name, content, files, errors)
    return sorted(set(errors)), len(files)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, default=Path(__file__).resolve().parents[1])
    arguments = parser.parse_args()
    try:
        errors, count = check_repository(arguments.root.resolve())
    except (RuntimeError, OSError, UnicodeError, ValueError):
        print("ERROR: Git index could not be read safely", file=sys.stderr)
        return 1
    for error in errors:
        print("ERROR: " + error, file=sys.stderr)
    if errors:
        return 1
    print(f"Repository index OK: {count} files")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
