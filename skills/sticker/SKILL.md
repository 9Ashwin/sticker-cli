---
name: sticker
version: 1.0.0
description: "本地贴纸/表情包：当用户说“发个表情包”“来个表情”“找张图”“换个贴纸”，或要求搜索、预览、展示、收藏、导入、分类、排序表情时，使用 sticker CLI。"
metadata:
  requires:
    bins: ["sticker"]
  cliHelp: "sticker --help"
---

# Sticker CLI

Use the `sticker` executable for local reaction images. The CLI returns JSON by
default in an envelope with `ok` and `data`; read paths and IDs from the
returned JSON instead of constructing them. Use `sticker --help` for a human
summary and `sticker schema [command...]` for the machine-readable contract.

## 意图路由

把下列口语请求直接路由到 CLI，用户不需要记住命令名：

| 用户意图 | CLI 流程 |
| --- | --- |
| “发个表情包给我”“来个表情”“换个表情” | 使用宽泛场景词执行 `search` → `get` → 展示返回的本地路径 |
| “找一个调皮/开心/上班的表情” | 执行 `search "<scene>" --limit 8`，检查多个候选后对选中 ID 调用 `get` |
| “看看这个表情”“预览一下” | 对 ID 调用 `get`；仅静态 WebP 加 `--preview` |
| “把这张加入收藏”“保存这个” | 用返回的 ID 或本地路径执行 `favorites add` |
| “导入这个表情包/素材库” | 执行 `favorites import <v1-directory>` |
| “把收藏分类/整理/排序” | 使用 `favorites collections`、`favorites list` 或 `favorites organize` |

用户没有指定场景时，使用 `调皮`、`回应` 等宽泛词，选择一个通用、轻松的
候选，不要追问精确语义。检查多个结果后选一个，把经过校验的绝对路径
`data.item.path` 渲染到当前 Agent 回复中。GIF 和动图必须使用原图路径；静态
WebP 才可以使用生成的预览路径。

“发给我”默认表示在当前 Agent 对话中展示。CLI 只返回本地路径，不向微信、飞书
或其他外部聊天发送。

### 首次使用

先执行搜索。如果返回的 `data.setup_required` 为 `true`，说明本地还没有素材包；除非
用户明确要求全量，否则直接安装精选包并重试原始搜索：

```bash
sticker setup --pack curated
```

用户提供本地 `sticker-ext` checkout 时，用 `--source /path/to/sticker-ext` 指定
来源，也可以设置 `STICKER_PACK_SOURCE` 作为默认来源。安装完成后重试原始搜索；之后
搜索和展示都可以离线完成。

## Install the skill safely

This directory can be copied into any Agent client that loads `SKILL.md` files.
For a release installation that includes both the CLI and this Skill, use the
platform installer; the Skill is always installed or preserved:

```bash
curl --proto '=https' --tlsv1.2 -fsSL \
  https://raw.githubusercontent.com/9Ashwin/sticker-cli/main/scripts/install.sh | bash
```

To initialize the default curated pack in the same invocation, append
`| bash -s -- --pack curated`. Use `--pack all` only when the full pack is wanted.
The installer always installs or preserves the Skill.

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
curated pack; use `--pack all` only when the full pack is wanted. Set
`STICKER_PACK_SOURCE` when the default HTTPS source is not reachable.

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
