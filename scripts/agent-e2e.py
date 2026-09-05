#!/usr/bin/env python3
"""Run the local Agent workflow against a generated, offline fixture."""

from __future__ import annotations

import base64
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile


ROOT = Path(__file__).resolve().parents[1]


def item(data: bytes, image_format: str, caption: str) -> dict[str, object]:
    digest = hashlib.md5(data).hexdigest()
    return {
        "md5": digest,
        "sha256": hashlib.sha256(data).hexdigest(),
        "filename": f"emoticons/{digest}.{image_format}",
        "format": image_format,
        "size": len(data),
        "caption": caption,
    }


def write_json(path: Path, value: object) -> bytes:
    data = json.dumps(value, ensure_ascii=False, separators=(",", ":")).encode()
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_bytes(data)
    return data


def write_image(root: Path, image: dict[str, object], data: bytes) -> None:
    path = root / str(image["filename"])
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_bytes(data)


def run(binary: Path, home: Path, *args: str) -> dict[str, object]:
    process = subprocess.run(
        [str(binary), "--home", str(home), *args],
        cwd=ROOT,
        text=True,
        capture_output=True,
        check=False,
    )
    if process.returncode != 0:
        raise AssertionError(
            f"command failed ({process.returncode}): {' '.join(args)}\n"
            f"stdout: {process.stdout}\nstderr: {process.stderr}"
        )
    if process.stderr:
        raise AssertionError(f"command wrote stderr: {' '.join(args)}\n{process.stderr}")
    try:
        envelope = json.loads(process.stdout)
    except json.JSONDecodeError as error:
        raise AssertionError(f"command did not return JSON: {process.stdout}") from error
    if envelope.get("ok") is not True:
        raise AssertionError(f"command returned an unsuccessful envelope: {process.stdout}")
    return envelope["data"]


def require(condition: bool, message: str) -> None:
    if not condition:
        raise AssertionError(message)


def build_binary(directory: Path) -> Path:
    supplied = os.environ.get("STICKER_BIN")
    if supplied:
        binary = Path(supplied).expanduser().resolve()
        require(binary.is_file(), f"STICKER_BIN is not a file: {binary}")
        return binary

    binary = directory / "sticker"
    environment = os.environ.copy()
    environment["CGO_ENABLED"] = "0"
    environment["GOTOOLCHAIN"] = "auto"
    process = subprocess.run(
        ["go", "build", "-trimpath", "-o", str(binary), "./cmd/sticker"],
        cwd=ROOT,
        env=environment,
        text=True,
        capture_output=True,
        check=False,
    )
    if process.returncode != 0:
        raise AssertionError(f"could not build sticker: {process.stdout}\n{process.stderr}")
    return binary


def create_source(root: Path) -> dict[str, dict[str, object]]:
    # One static GIF, one two-frame GIF, and the small static WebP used by the
    # preview implementation. The fixture contains no network-only resources.
    static_data = b"GIF89a agent e2e static"
    animated_data = (
        b"GIF89a\x01\x00\x01\x00\x80\x00\x00\x00\x00\x00\xff\xff\xff"
        b"!\xf9\x04\x00\x0a\x00\x00\x00,\x00\x00\x00\x00\x01\x00\x01\x00\x00"
        b"\x02\x02D\x01\x00!\xf9\x04\x00\x0a\x00\x00\x00,\x00\x00\x00\x00"
        b"\x01\x00\x01\x00\x00\x02\x02L\x01\x00;"
    )
    webp_data = base64.b64decode("UklGRhwAAABXRUJQVlA4TA8AAAAvAUAAAAcQ/Y/+ByKi/wEA")
    images = {
        "static": (static_data, "gif", "调皮回应"),
        "animated": (animated_data, "gif", "调皮庆祝"),
        "webp": (webp_data, "webp", "调皮工作"),
    }
    entries: dict[str, dict[str, object]] = {}
    manifest_items: list[dict[str, object]] = []
    for name, (data, image_format, caption) in images.items():
        entry = item(data, image_format, caption)
        entries[name] = entry
        manifest_items.append(entry)
        write_image(root, entry, data)

    curated = write_json(
        root / "packs" / "curated.json",
        {"schema_version": 1, "collection": "curated", "items": manifest_items},
    )
    write_json(
        root / "packs.json",
        {
            "schema_version": 1,
            "packs": [
                {
                    "id": "curated",
                    "name": "Curated fixture",
                    "description": "Offline Agent acceptance fixture",
                    "manifest": "packs/curated.json",
                    "manifest_sha256": hashlib.sha256(curated).hexdigest(),
                    "count": len(manifest_items),
                    "size": sum(int(entry["size"]) for entry in manifest_items),
                }
            ],
        },
    )
    return entries


def create_import(root: Path) -> tuple[dict[str, object], bytes]:
    data = b"GIF89a agent e2e imported"
    entry = item(data, "gif", "导入后的调皮回应")
    write_image(root, entry, data)
    write_json(root / "manifest.json", {"schema_version": 1, "collection": "shared", "items": [entry]})
    return entry, data


def install_skill(skill_script: Path, skill_source: Path, root: Path) -> None:
    destination = root / "agent-skills" / "sticker"
    process = subprocess.run(
        [str(skill_script), str(destination)],
        cwd=ROOT,
        text=True,
        capture_output=True,
        check=False,
    )
    require(process.returncode == 0, f"skill install failed: {process.stdout}\n{process.stderr}")
    require((destination / "SKILL.md").read_bytes() == skill_source.read_bytes(), "installed skill differs from source")

    existing = root / "existing-skill"
    existing.mkdir()
    marker = b"user guide stays intact\n"
    (existing / "SKILL.md").write_bytes(marker)
    process = subprocess.run(
        [str(skill_script), str(existing)],
        cwd=ROOT,
        text=True,
        capture_output=True,
        check=False,
    )
    require(process.returncode == 3, "skill installer accepted an existing destination")
    require((existing / "SKILL.md").read_bytes() == marker, "skill installer overwrote an existing guide")


def main() -> int:
    with tempfile.TemporaryDirectory(prefix="sticker-agent-e2e-") as temporary:
        workspace = Path(temporary)
        binary = build_binary(workspace)
        source = workspace / "source"
        home = workspace / "home"
        imported = workspace / "v1-import"
        entries = create_source(source)
        imported_entry, imported_data = create_import(imported)

        install_skill(ROOT / "scripts" / "install-skill.sh", ROOT / "skills" / "sticker" / "SKILL.md", workspace)

        setup = run(binary, home, "setup", "--pack", "curated", "--source", str(source))
        require(setup["setup"] is True and setup["pack"]["id"] == "curated", "curated setup did not select curated")
        require(setup["added"] == 3 and setup["dry_run"] is False, "curated setup did not install the fixture")

        # Make the following calls prove that the installed library works with
        # its source unavailable. Search and get never receive a source flag.
        source_offline = source.with_name("source-unavailable")
        source.rename(source_offline)
        search = run(binary, home, "search", "调皮", "--limit", "10")
        require(search["total"] == 3 and len(search["items"]) == 3, "offline search missed fixture captions")

        webp_id = str(entries["webp"]["md5"])
        static_id = str(entries["static"]["md5"])
        animated_id = str(entries["animated"]["md5"])
        preview = run(binary, home, "get", webp_id, "--preview")
        preview_item = preview["item"]
        preview_path = Path(str(preview_item["preview_path"]))
        original_path = Path(str(preview_item["path"]))
        require(preview_path.read_bytes().startswith(b"\x89PNG\r\n\x1a\n"), "static WebP preview is not PNG")
        require(original_path.read_bytes() == base64.b64decode("UklGRhwAAABXRUJQVlA4TA8AAAAvAUAAAAcQ/Y/+ByKi/wEA"), "WebP original changed")

        animated = run(binary, home, "get", animated_id)
        animated_path = Path(str(animated["item"]["path"]))
        require(
            hashlib.sha256(animated_path.read_bytes()).hexdigest() == entries["animated"]["sha256"],
            "animated original changed",
        )

        first_favorite = run(binary, home, "favorites", "add", "--id", static_id)
        require(first_favorite["added"] is True and first_favorite["item"]["id"] == static_id, "installed item was not added to favorites")
        local_data = b"GIF89a agent e2e local favorite"
        local_path = workspace / "local-favorite.gif"
        local_path.write_bytes(local_data)
        local_entry = item(local_data, "gif", "调皮本地收藏")
        local_favorite = run(binary, home, "favorites", "add", str(local_path), "--caption", str(local_entry["caption"]))
        local_id = str(local_favorite["item"]["id"])
        require(local_favorite["added"] is True and local_id == str(local_entry["md5"]), "local original was not added")

        run(binary, home, "favorites", "collections", "create", "work")
        preview_move = run(binary, home, "favorites", "organize", "--collection", "favorites", "--ids", f"{static_id},{local_id}", "--move-to", "work", "--dry-run")
        require(preview_move["dry_run"] is True and preview_move["committed"] is False, "organize dry-run committed a change")
        moved = run(binary, home, "favorites", "organize", "--collection", "favorites", "--ids", f"{static_id},{local_id}", "--move-to", "work")
        require(moved["moved"] == 2 and moved["committed"] is True, "batch move did not commit two favorites")
        sorted_items = run(binary, home, "favorites", "list", "--collection", "work", "--sort", "caption", "--limit", "10")
        require(sorted_items["total"] == 2 and sorted_items["items"][0]["caption"] <= sorted_items["items"][1]["caption"], "collection caption sort is unstable")
        reordered = run(binary, home, "favorites", "organize", "--collection", "work", "--order", f"{local_id},{static_id}")
        require(reordered["reordered"] is True and reordered["committed"] is True, "manual collection order did not commit")
        manual = run(binary, home, "favorites", "list", "--collection", "work", "--sort", "manual", "--limit", "10")
        require([entry["id"] for entry in manual["items"]] == [local_id, static_id], "manual order was not retained")

        imported_result = run(binary, home, "favorites", "import", str(imported))
        require(imported_result["added"] == 1 and imported_result["committed"] is True, "v1 import did not commit")
        default_items = run(binary, home, "favorites", "list", "--collection", "favorites", "--limit", "10")
        require(default_items["total"] == 1 and default_items["items"][0]["id"] == imported_entry["md5"], "v1 import did not enter default favorites")
        manifest = json.loads((home / "manifest.json").read_text())
        require(len(manifest["items"]) == 3 and (home / str(imported_entry["filename"])).read_bytes() == imported_data, "final standard v1 library is incomplete")

        schema = run(binary, home, "schema", "favorites", "organize")
        parameter_names = {parameter["name"] for parameter in schema["parameters"]}
        require({"--collection", "--ids", "--move-to", "--order", "--dry-run"}.issubset(parameter_names), "organize schema is missing a documented flag")
        setup_help = run(binary, home, "help", "setup")["help"]
        require("--pack" in setup_help and "curated" in setup_help and "all" in setup_help, "setup help does not expose pack choices")
        get_help = run(binary, home, "help", "get")["help"]
        require("--preview" in get_help, "get help does not expose preview")

        skill = (ROOT / "skills" / "sticker" / "SKILL.md").read_text()
        for command in (
            "sticker setup --pack curated",
            "sticker setup --pack all",
            "sticker packs install curated",
            "sticker search",
            "sticker get <id> --preview",
            "sticker favorites add --id <id>",
            "sticker favorites import",
            "sticker favorites collections create work",
            "sticker favorites organize",
            "sticker favorites list --collection work --sort manual",
        ):
            require(command in skill, f"skill is missing documented workflow: {command}")
        require("external chat" in skill and "does not send" in skill, "skill must describe local display limits")
        print(f"static WebP preview verified: {preview_path}")
        print(f"animated GIF original verified: {animated_path}")
        print("agent workflow verified: curated setup -> offline search -> get/preview -> favorites -> collection -> sort -> batch organize -> v1 import")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except AssertionError as error:
        print(f"agent e2e failed: {error}", file=sys.stderr)
        raise SystemExit(1) from error
