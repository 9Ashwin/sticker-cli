# sticker-cli

面向人和 AI Agent 的独立 Go 表情包 CLI。CLI 程序与表情素材分开发布，安装程序本身不会下载原图。

目标是不依赖微信账号或 MCP：用户选择精选／全量素材，Agent 离线检索并展示本地原图，用户持续添加、导入和导出私人收藏。

- **实现方向：** Go + Cobra，默认 JSON 输出、稳定错误、schema、dry-run 和 Agent Skill。
- **素材与程序拆分：** 本仓库承载 CLI；[sticker-ext](https://github.com/9Ashwin/sticker-ext) 独立承载公共素材。
- **可选素材：** 当前全量 2,638 张，约 1 GiB；精选 120 张，约 20.7 MiB；精选包不含 WebP，全量包含 15 张 WebP。
- **兼容预览：** `get <id>` 保留原图；客户端无法展示静态 WebP 时，可用 `get <id> --preview` 生成 PNG 预览，不改原图哈希。
- **收藏导入：** 本地原图和清单按现有 `manifest.json` + `emoticons/` 格式合并到个人素材库，不另设收藏格式；公共包更新不会覆盖个人素材。

[PRD](tasks/prd-sticker-cli.md) 已确认，[技术 SPEC](tasks/spec-sticker-cli.md) 和 [实施 GitHub Issues](tasks/issues-sticker-cli.md) 已就绪。首版使用现有 JSON 清单，SQLite 可重建索引作为有明确需求后的选项，JSONL 不作为素材库格式。

已进入按 Issue 实施阶段，`packs list`、`packs install`（含 `--dry-run`）、`packs update`、`packs remove`、`setup`、`get` 以及个人收藏的添加、导入、导出、列表、描述修改和取消已实现。收藏可以通过 `favorites collections` 创建、重命名和删除自定义分组，也可以用 `favorites organize` 在分组间批量移动、重排或移出条目；`favorites list` 支持 `--collection` 和 `--sort manual|added|caption|md5`，整理前可用 `--dry-run` 预览。分组元数据保存在 `.sticker/collections.json`，导出时附带可选的 `collections.json` 扩展，不改标准 `manifest.json` 或原图。

```bash
sticker favorites collections create work
sticker favorites organize --collection favorites --ids <id> --move-to work
sticker favorites list --collection work
sticker favorites list --collection work --sort caption
sticker favorites organize --collection work --order <id2>,<id1> --dry-run
sticker favorites collections list
```

## 安装 CLI

发布归档提供 Linux amd64/arm64、macOS amd64/arm64 和 Windows amd64 二进制，并附带 SHA-256 校验。CI 在 Linux/macOS/Windows 上分别做原生 smoke test；发布工作流的跨平台构建不把素材放进归档。CLI 代码使用 MIT 许可证，素材仍以素材仓库中的声明为准。

源码安装：

```bash
go install github.com/9Ashwin/sticker-cli/cmd/sticker@latest
```

Unix 用户也可以下载发布页的 `install.sh`。它默认把程序写入 `$HOME/.local/bin`（可用 `STICKER_INSTALL_DIR` 覆盖），支持显式版本：

```bash
curl -fsSL https://github.com/9Ashwin/sticker-cli/releases/latest/download/install.sh -o /tmp/sticker-install.sh
sh /tmp/sticker-install.sh v0.1.0
```

Windows 用户下载发布页的 `install.ps1` 后在 PowerShell 中运行：

```powershell
.\install.ps1 -Version v0.1.0
```

安装器先校验归档和二进制的 SHA-256，失败时不会替换目标程序；素材需要之后通过 `packs install` 或 `setup` 单独选择。

## Agent Skill 与验收

跨客户端的使用指引位于 [`skills/sticker/SKILL.md`](skills/sticker/SKILL.md)，覆盖选包、离线检索、静态预览、原图展示、收藏导入、分组、排序和批量整理。可将它安装到任意支持 `SKILL.md` 的 Agent 技能目录；安装器遇到已有目录会拒绝操作，不覆盖现有指引：

```bash
./scripts/install-skill.sh /path/to/agent/skills/sticker
```

用临时 home 和本地 fixture 验收完整 Agent 流程（不访问微信、MCP 或公网素材源）：

```bash
make e2e-agent
```

## 源码开发

固定 Go 1.26.8、Cobra 1.10.2 与 golangci-lint 2.13.2。

```bash
make quality
go test -race ./...
./bin/sticker version
```

构建的二进制不依赖 Go、Python、Node.js、MCP 服务或微信账号。原图不会随程序构建下载。
