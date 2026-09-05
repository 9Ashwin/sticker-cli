---
name: sticker
description: Use the sticker CLI to find, preview, display, save, import, and organize local reaction images.
---

# Sticker CLI

Use the `sticker` executable for local reaction images. The CLI returns JSON by
default in an envelope with `ok` and `data`; read paths and IDs from the
returned JSON instead of constructing them. Use `sticker --help` for a human
summary and `sticker schema [command...]` for the machine-readable contract.

## Install the skill safely

This directory can be copied into any Agent client that loads `SKILL.md` files.
Use the repository helper with the client's skill directory as its argument:

```bash
./scripts/install-skill.sh /path/to/agent/skills/sticker
```

The helper refuses an existing destination and exits without changing it. If a
client already has a `sticker` guide, keep that guide or choose another empty
skill directory. Installing this guide does not install the `sticker` binary;
install the binary separately with a release archive or:

```bash
go install github.com/9Ashwin/sticker-cli/cmd/sticker@latest
```

## Select and install a pack

Choose a pack explicitly. `setup` is the short path and defaults to the
curated pack; use `--pack all` only when the full pack is wanted.

```bash
sticker packs list --source /path/to/sticker-ext
sticker setup --source /path/to/sticker-ext
sticker setup --pack curated --source /path/to/sticker-ext --dry-run
sticker setup --pack all --source /path/to/sticker-ext
```

For scripts that need the formal install command, use:

```bash
sticker packs install curated --source /path/to/sticker-ext
sticker packs install all --source /path/to/sticker-ext
```

Use `--home /path/to/local-library` or `STICKER_HOME` to select the local
library. After installation, search, get, and favorites operations are local
and do not need the source directory or a network connection.

## Search, preview, and display

Search captions with broad scene words and inspect several candidates. Captions
describe possible uses; they are not a promise of one exact meaning.

```bash
sticker search "回应" --limit 8
sticker search "工作" --pack curated --limit 8
sticker search "可爱" --favorites --limit 8
```

For a selected result, pass its `data.items[].id` to `get`:

```bash
sticker get <id>
sticker get <id> --preview
```

`data.item.path` is the verified absolute path to the original. For a static
WebP, `--preview` adds `data.item.preview_path`, an on-demand PNG path; the
original WebP, MD5, and SHA-256 stay unchanged. For a GIF or an animated image,
use the original path so a capable client can play it; do not treat a static
preview as the animation. If the current Agent client supports local file
rendering, render the returned path in the current response. The CLI only
reports local paths and does not send an image to an external chat.

## Save and import favorites

Save one local original by path, or copy an already installed item by ID:

```bash
sticker favorites add /path/to/original.gif --caption '调皮回应'
sticker favorites add --id <id>
sticker favorites list --limit 20
```

Import a standard v1 material directory. It only needs `manifest.json` and the
referenced `emoticons/` files; `packs.json` is optional. When no collection
extension is present, imported items enter the default `favorites` collection.

```bash
sticker favorites import /path/to/v1-pack
sticker favorites import /path/to/v1-pack --dry-run
```

The standard v1 library is the exchange format: `manifest.json` and original
files are retained, and imported content is deduplicated by MD5.

## Group, sort, and batch organize

The default collection is `favorites`. Custom collection IDs are lowercase
letters followed by lowercase letters, digits, `-`, or `_`; their display
names can be changed independently.

```bash
sticker favorites collections list
sticker favorites collections create work
sticker favorites collections rename work 工作
sticker favorites list --collection work --sort manual
sticker favorites list --collection work --sort added
sticker favorites list --collection work --sort caption
sticker favorites list --collection work --sort md5
```

Use `organize` for an atomic move, complete manual order, or removal from a
collection. Preview a write before applying it with `--dry-run`.

```bash
sticker favorites organize --collection favorites --ids <id1>,<id2> --move-to work --dry-run
sticker favorites organize --collection favorites --ids <id1>,<id2> --move-to work
sticker favorites organize --collection work --order <id2>,<id1>
sticker favorites organize --collection work --ids <id2> --move-to favorites
sticker favorites collections remove work
```

`--order` is the complete order for the selected collection. Validate all IDs
before applying a batch operation; a failed operation must not be described as
partially committed. Use the JSON `committed` and `dry_run` fields to report
what happened.

## Agent workflow

1. Discover with `schema` or `--help` when the exact command contract is unknown.
2. Install `curated` (or explicitly `all`) and search using a broad scene word.
3. Choose a candidate, call `get`, and use `--preview` only for a static WebP.
4. Display the returned local path if the client supports local files; otherwise
   report that the path is ready for the client.
5. Save with `favorites add` or import a standard v1 directory.
6. Create a collection, preview a batch with `organize --dry-run`, then move or
   reorder it and verify with `favorites list`.

Never claim that a successful CLI command delivered an image to a chat. A
client's local rendering result is a separate display check.
