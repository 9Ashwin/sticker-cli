#!/usr/bin/env python3
"""Run the local Agent workflow against a generated, offline fixture."""

from __future__ import annotations

import base64
from contextlib import contextmanager
from functools import partial
import hashlib
from http.server import SimpleHTTPRequestHandler, ThreadingHTTPServer
import json
import os
from pathlib import Path
import platform
import shutil
import ssl
import subprocess
import sys
import tarfile
import tempfile
import threading


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


def run_failure(
    binary: Path,
    home: Path,
    *args: str,
    exit_code: int,
    error_type: str,
    error_subtype: str,
) -> dict[str, object]:
    process = subprocess.run(
        [str(binary), "--home", str(home), *args],
        cwd=ROOT,
        text=True,
        capture_output=True,
        check=False,
    )
    require(process.returncode == exit_code, f"unexpected exit code for {' '.join(args)}: {process.returncode}\n{process.stderr}")
    require(process.stdout == "", f"failed command wrote stdout: {' '.join(args)}\n{process.stdout}")
    try:
        envelope = json.loads(process.stderr)
    except json.JSONDecodeError as error:
        raise AssertionError(f"failed command did not return JSON: {process.stderr}") from error
    require(envelope.get("ok") is False, f"failed command returned a successful envelope: {process.stderr}")
    error = envelope.get("error", {})
    require(error.get("type") == error_type, f"unexpected error type: {process.stderr}")
    require(error.get("subtype") == error_subtype, f"unexpected error subtype: {process.stderr}")
    return error


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
    all_only = item(b"GIF89a agent e2e full-only", "gif", "全量专属调皮")
    entries["all_only"] = all_only
    all_manifest = write_json(
        root / "manifest.json",
        {"schema_version": 1, "collection": "all", "items": manifest_items + [all_only]},
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
                },
                {
                    "id": "all",
                    "name": "Full fixture",
                    "description": "Intentionally includes one full-only image",
                    "manifest": "manifest.json",
                    "manifest_sha256": hashlib.sha256(all_manifest).hexdigest(),
                    "count": len(manifest_items) + 1,
                    "size": sum(int(entry["size"]) for entry in manifest_items) + int(all_only["size"]),
                },
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


def create_broken_source(source: Path, workspace: Path, entries: dict[str, dict[str, object]]) -> Path:
    broken = workspace / "broken-source"
    shutil.copytree(source, broken)
    broken_image = broken / str(entries["static"]["filename"])
    broken_image.write_bytes(b"GIF89a agent e2e corrupted")
    return broken


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(64 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def create_release_fixture(workspace: Path, binary: Path) -> tuple[Path, str]:
    """Create a validly checksummed archive with one forbidden entry."""
    package = workspace / "release-package"
    package.mkdir()
    archive_binary = package / "sticker"
    shutil.copy2(binary, archive_binary)
    archive_binary.chmod(0o755)
    shutil.copy2(ROOT / "LICENSE", package / "LICENSE")
    (package / "VERSION").write_text("v0.1.0\n")
    (package / "unexpected.txt").write_text("this must be rejected\n")
    inner_checksum = f"{sha256_file(archive_binary)}  sticker\n"
    (package / "checksums.txt").write_text(inner_checksum)

    system = platform.system().lower()
    machine = platform.machine().lower()
    os_name = {"darwin": "darwin", "linux": "linux"}.get(system)
    arch = {"x86_64": "amd64", "amd64": "amd64", "aarch64": "arm64", "arm64": "arm64"}.get(machine)
    require(os_name is not None and arch is not None, f"unsupported release fixture platform: {system}/{machine}")
    archive_name = f"sticker_{os_name}_{arch}.tar.gz"
    release = workspace / "release"
    release.mkdir()
    archive_path = release / archive_name
    with tarfile.open(archive_path, "w:gz") as archive:
        for path in sorted(package.iterdir()):
            archive.add(path, arcname=path.name, recursive=False)
    (release / "checksums.txt").write_text(f"{sha256_file(archive_path)}  {archive_name}\n")
    return release, archive_name


@contextmanager
def local_https_server(directory: Path, certificate: Path, key: Path):
    class QuietHandler(SimpleHTTPRequestHandler):
        def log_message(self, _format: str, *_args: object) -> None:
            return

    handler = partial(QuietHandler, directory=str(directory))
    server = ThreadingHTTPServer(("127.0.0.1", 0), handler)
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    context.load_cert_chain(certificate, key)
    server.socket = context.wrap_socket(server.socket, server_side=True)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    try:
        host, port = server.server_address
        yield f"https://{host}:{port}"
    finally:
        server.shutdown()
        thread.join(timeout=5)
        server.server_close()


def verify_release_archive_rejection(binary: Path, workspace: Path) -> None:
    release, _archive_name = create_release_fixture(workspace, binary)
    install_dir = workspace / "release-install"
    certificate = workspace / "release-test.crt"
    key = workspace / "release-test.key"
    certificate_process = subprocess.run(
        [
            "openssl",
            "req",
            "-x509",
            "-newkey",
            "rsa:2048",
            "-nodes",
            "-keyout",
            str(key),
            "-out",
            str(certificate),
            "-days",
            "1",
            "-subj",
            "/CN=127.0.0.1",
            "-addext",
            "subjectAltName=IP:127.0.0.1",
        ],
        cwd=ROOT,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
        check=False,
    )
    require(certificate_process.returncode == 0, "could not create the local HTTPS certificate")
    environment = os.environ.copy()
    environment["STICKER_INSTALL_DIR"] = str(install_dir)
    environment["STICKER_VERSION"] = "v0.1.0"
    environment["CURL_CA_BUNDLE"] = str(certificate)
    with local_https_server(release, certificate, key) as base_url:
        environment["STICKER_RELEASE_BASE_URL"] = base_url
        process = subprocess.run(
            [str(ROOT / "scripts" / "install.sh"), "v0.1.0"],
            cwd=ROOT,
            env=environment,
            text=True,
            capture_output=True,
            check=False,
        )
    require(process.returncode != 0, "installer accepted an archive with an unexpected entry")
    require("unexpected path" in process.stderr, f"installer returned an unstable archive error: {process.stderr}")
    require(not (install_dir / "sticker").exists(), "failed archive validation published a binary")


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
        broken_source = create_broken_source(source, workspace, entries)
        imported_entry, imported_data = create_import(imported)

        install_skill(ROOT / "scripts" / "install-skill.sh", ROOT / "skills" / "sticker" / "SKILL.md", workspace)

        catalog = run(binary, home, "packs", "list", "--source", str(source))
        explicit_source = str(source.absolute())
        require(catalog["source"] == explicit_source, "pack discovery did not retain the explicit local source")
        require([pack["id"] for pack in catalog["items"]] == ["all", "curated"], "pack discovery did not list both selectable packs")

        broken_home = workspace / "broken-home"
        run_failure(
            binary,
            broken_home,
            "setup",
            "--pack",
            "curated",
            "--source",
            str(broken_source),
            exit_code=5,
            error_type="integrity",
            error_subtype="hash_mismatch",
        )
        require(not (broken_home / "manifest.json").exists(), "failed image validation published a personal manifest")
        require(not (broken_home / ".sticker" / "packs" / "curated.json").exists(), "failed image validation published pack state")
        require(not (broken_home / str(entries["static"]["filename"])).exists(), "failed image validation published an image")

        verify_release_archive_rejection(binary, workspace)

        setup = run(binary, home, "setup", "--pack", "curated", "--source", str(source))
        require(
            setup["setup"] is True
            and setup["pack"]["id"] == "curated"
            and setup["source"] == explicit_source,
            "curated setup did not select the explicit local source",
        )
        require(setup["added"] == 3 and setup["dry_run"] is False, "curated setup did not install the fixture")
        require(not (home / str(entries["all_only"]["filename"])).exists(), "curated setup requested a full-only image")
        original_bytes = {
            name: (source / str(entry["filename"])).read_bytes()
            for name, entry in entries.items()
            if name in {"static", "animated", "webp"}
        }
        for name, data in original_bytes.items():
            require(sha256_file(home / str(entries[name]["filename"])) == hashlib.sha256(data).hexdigest(), f"installed {name} hash changed")

        # Make the following calls prove that the installed library works with
        # its source unavailable. Search and get never receive a source flag.
        source_offline = source.with_name("source-unavailable")
        source.rename(source_offline)
        search = run(binary, home, "search", "调皮", "--limit", "10")
        require(
            search["total"] == 3
            and {entry["id"] for entry in search["items"]} == {entries[name]["md5"] for name in ("static", "animated", "webp")},
            "offline search missed fixture captions",
        )

        webp_id = str(entries["webp"]["md5"])
        static_id = str(entries["static"]["md5"])
        animated_id = str(entries["animated"]["md5"])
        static = run(binary, home, "get", static_id)
        require(Path(str(static["item"]["path"])).read_bytes() == original_bytes["static"], "static original changed")
        preview = run(binary, home, "get", webp_id, "--preview")
        preview_item = preview["item"]
        preview_path = Path(str(preview_item["preview_path"]))
        original_path = Path(str(preview_item["path"]))
        require(preview_path.read_bytes().startswith(b"\x89PNG\r\n\x1a\n"), "static WebP preview is not PNG")
        require(original_path.read_bytes() == original_bytes["webp"], "WebP original changed")

        animated = run(binary, home, "get", animated_id)
        animated_path = Path(str(animated["item"]["path"]))
        require(
            animated_path.read_bytes() == original_bytes["animated"] and "preview_path" not in animated["item"],
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

        created = run(binary, home, "favorites", "collections", "create", "work")
        require(created["changed"] is True and created["collection"]["id"] == "work", "favorite collection was not created")
        collection_path = home / ".sticker" / "collections.json"
        manifest_path = home / "manifest.json"
        collections_before_move = collection_path.read_bytes()
        manifest_before_move = manifest_path.read_bytes()
        preview_move = run(binary, home, "favorites", "organize", "--collection", "favorites", "--ids", f"{static_id},{local_id}", "--move-to", "work", "--dry-run")
        require(preview_move["dry_run"] is True and preview_move["committed"] is False, "organize dry-run committed a change")
        require(collection_path.read_bytes() == collections_before_move and manifest_path.read_bytes() == manifest_before_move, "organize dry-run changed state")
        moved = run(binary, home, "favorites", "organize", "--collection", "favorites", "--ids", f"{static_id},{local_id}", "--move-to", "work")
        require(moved["moved"] == 2 and moved["committed"] is True, "batch move did not commit two favorites")
        collection_metadata = json.loads(collection_path.read_text())
        work_members = next(collection for collection in collection_metadata["collections"] if collection["id"] == "work")["items"]
        added_at = {member["id"]: member.get("added_at", "") for member in work_members}
        expected_ids = [static_id, local_id]
        expected_added = sorted(expected_ids, key=lambda value: (added_at[value], value))
        expected_caption = sorted(expected_ids, key=lambda value: (str({static_id: "调皮回应", local_id: "调皮本地收藏"}[value]).lower(), value))
        expected_md5 = sorted(expected_ids)
        expected_sorts = {
            "manual": expected_ids,
            "added": expected_added,
            "caption": expected_caption,
            "md5": expected_md5,
        }
        for sort_name, expected in expected_sorts.items():
            sorted_items = run(binary, home, "favorites", "list", "--collection", "work", "--sort", sort_name, "--limit", "10")
            require(sorted_items["total"] == 2 and [entry["id"] for entry in sorted_items["items"]] == expected, f"collection {sort_name} sort is unstable")
        reordered = run(binary, home, "favorites", "organize", "--collection", "work", "--order", f"{local_id},{static_id}")
        require(reordered["reordered"] is True and reordered["committed"] is True, "manual collection order did not commit")
        manual = run(binary, home, "favorites", "list", "--collection", "work", "--sort", "manual", "--limit", "10")
        require([entry["id"] for entry in manual["items"]] == [local_id, static_id], "manual order was not retained")

        valid_collections = collection_path.read_bytes()
        valid_manifest = manifest_path.read_bytes()
        unknown_id = "0" * 32
        run_failure(
            binary,
            home,
            "favorites",
            "organize",
            "--collection",
            "work",
            "--ids",
            f"{local_id},{unknown_id}",
            "--move-to",
            "favorites",
            exit_code=3,
            error_type="not_found",
            error_subtype="item_not_found",
        )
        require(collection_path.read_bytes() == valid_collections and manifest_path.read_bytes() == valid_manifest, "invalid batch ID changed state")
        unchanged = run(binary, home, "favorites", "list", "--collection", "work", "--sort", "manual", "--limit", "10")
        require([entry["id"] for entry in unchanged["items"]] == [local_id, static_id], "invalid batch ID changed collection order")

        collection_path.write_bytes(b'{"schema_version":1,"collections":[')
        run_failure(
            binary,
            home,
            "favorites",
            "collections",
            "list",
            exit_code=5,
            error_type="integrity",
            error_subtype="invalid_collection",
        )
        require(manifest_path.read_bytes() == valid_manifest, "corrupt metadata changed the standard manifest")
        collection_path.write_bytes(valid_collections)
        restored = run(binary, home, "favorites", "list", "--collection", "work", "--sort", "manual", "--limit", "10")
        require([entry["id"] for entry in restored["items"]] == [local_id, static_id], "restored collection order changed")

        imported_result = run(binary, home, "favorites", "import", str(imported))
        require(imported_result["added"] == 1 and imported_result["committed"] is True, "v1 import did not commit")
        default_items = run(binary, home, "favorites", "list", "--collection", "favorites", "--limit", "10")
        require(default_items["total"] == 1 and default_items["items"][0]["id"] == imported_entry["md5"], "v1 import did not enter default favorites")
        manifest = json.loads(manifest_path.read_text())
        expected_manifest_ids = sorted([static_id, local_id, str(imported_entry["md5"])])
        require(
            [entry["md5"] for entry in manifest["items"]] == expected_manifest_ids,
            f"final standard v1 manifest order is unstable: {[entry['md5'] for entry in manifest['items']]} != {expected_manifest_ids}",
        )
        for manifest_item in manifest["items"]:
            image_path = home / str(manifest_item["filename"])
            require(image_path.is_file() and sha256_file(image_path) == manifest_item["sha256"], f"final manifest hash mismatch for {manifest_item['md5']}")
        require((home / str(imported_entry["filename"])).read_bytes() == imported_data, "final standard v1 library is incomplete")
        final_collections = json.loads(collection_path.read_text())
        require([collection["id"] for collection in final_collections["collections"]] == ["favorites", "work"], "final collection order is unstable")
        final_by_id = {collection["id"]: collection for collection in final_collections["collections"]}
        require([item["id"] for item in final_by_id["favorites"]["items"]] == [str(imported_entry["md5"])], "imported favorite is in the wrong collection")
        require([item["id"] for item in final_by_id["work"]["items"]] == [local_id, static_id], "final work collection order is unstable")
        for collection in final_collections["collections"]:
            require([item["position"] for item in collection["items"]] == list(range(len(collection["items"]))), f"{collection['id']} positions are not normalized")
        for name, data in original_bytes.items():
            require((home / str(entries[name]["filename"])).read_bytes() == data, f"final {name} original changed")

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
        print("release archive rejection verified: no binary published")
        print("failure atomicity verified: image hash, metadata, and invalid batch ID")
        print("agent workflow verified: local curated setup -> offline search/get -> preview -> favorites -> four sorts -> batch organize -> v1 import")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except AssertionError as error:
        print(f"agent e2e failed: {error}", file=sys.stderr)
        raise SystemExit(1) from error
