# SPEC：独立 Go 表情包 CLI

输入：[已确认 PRD](prd-sticker-cli.md)。基线提交：`5380f25`。目标仓库：`9Ashwin/sticker-cli`，文档分支 `main`。
状态：实施 Issue 的技术设计基线；命令与结构均为实施合同，不是现有实现。

## 1. 范围与主要决策

覆盖 US-001–016、FR-1–25。按 PRD 的 P1/P2 交付；直接微信迁移、MCP、图片生成与外部消息发送不进入运行依赖。

| 决策 | 选择 | 原因 |
| --- | --- | --- |
| 运行时 | Go 单二进制，无 CGO | 满足跨平台、Agent 可执行和运行依赖少的目标 |
| 命令树 | Cobra，参数可出现在位置参数前后 | 统一命令注册、帮助和参数解析 |
| 数据 | v1 manifest + 原始文件 | 直接兼容现有素材库，不引入收藏专有格式或数据库 |
| 内容标识 | MD5 定位，SHA-256 验证冲突 | 兼容旧文件名，同时拒绝 MD5 相同而内容不同的文件 |
| 输出 | 默认 JSON envelope；可选 table | 机器解析稳定，人与 Agent 共用命令 |
| 包版本 | 清单原始字节 SHA-256 | 通用 HTTPS 目录和本地目录均可验证，不依赖 GitHub API |
| 下载 | 首期顺序、有界、可取消 | 先保证可恢复与选择性下载，后续再按测量优化并发 |
| 发布 | 原图先落盘，清单最后提交 | 失败最多遗留未引用文件，不产生已发布缺图清单 |

Go 最低版本与工具版本在初始化 Issue 中核对官方支持状态后固定于 go.mod 和 CI；这属于开工时的版本选择，不改变本设计的接口。

## 2. 组件与依赖

```text
cmd/sticker/        main：信号、退出码、版本注入
internal/cli/      Cobra 注册、help/schema、参数投影
internal/output/   JSON/table、错误 envelope
internal/apperror/ 稳定 type/subtype 与退出码
internal/library/ v1 清单、文件校验、导入、收藏、读写锁与提交
internal/packs/   包目录、HTTPS/本地源、安装更新、安装状态
skills/sticker/   跨命令 Agent 工作流
```

依赖方向：`cli → packs/library/output`；`packs → library`；`output → apperror`。library 不导入 cli、packs 或 Cobra。首次实现按这些职责组织，单个操作无需拆成新 package。

Cobra 注册真实参数；schema 读取实际参数定义，并附带每条命令的一份结果 schema、错误 subtype 清单、示例及 effect（read/write）。不维护另一棵手工命令树。运行用例接收 context 和明确参数，不读取 Cobra FlagSet 或直接打印。

单次调用从 main 的信号 context 传至下载、复制与提交前检查。stdout 不包含日志、进度、ANSI 或图片字节。

## 3. 本地与远端数据模型

### 3.1 CLI 数据根目录

```text
<home>/
  manifest.json                 个人素材的标准 v1 清单
  emoticons/<md5>.<format>       共享原图，收藏与已安装包共用
  .sticker/
    packs/<id>.json             已安装包状态，内含该包的 v1 清单
    catalogs/<source-hash>.json 包目录缓存与 fetched_at
    write.lock                 跨进程文件锁
    staging/                   尚未发布的下载或导入文件
```

`manifest.json` 只收录用户添加、收藏或导入的条目；搜索集合是它与已安装包条目的并集。单独读取此标准清单即可使用个人素材，不要求理解 `.sticker/`。个人库始终不依赖公共包状态才能找到原图。

`--home` > `STICKER_HOME` > `os.UserConfigDir()/sticker`。输出路径先转成绝对路径；纯读命令在空库不创建目录。手工编辑素材前应结束正在运行的 CLI 写操作。

### 3.2 标准素材清单

保持现有结构：`schema_version: 1`、`collection: string`、`items: Item[]`。

| Item 字段 | 合同 |
| --- | --- |
| md5 | 32 位小写十六进制，条目 ID；同一清单不得重复 |
| sha256 | 64 位小写十六进制 |
| filename | 必须精确等于 `emoticons/<md5>.<format>`；相对素材根目录 |
| format | `gif / png / jpg / webp` |
| size | 正整数，最多 32 MiB |
| caption | 可省略的 UTF-8 字符串，最多 4,096 字节；不执行、不当成指令 |

用户新建个人清单使用 `collection: personal`；导入不覆盖目标已有 collection。items 写入按 MD5 排序。接收清单最多 8 MiB、20,000 项，拒绝重复 JSON 键及不合法 UTF-8；允许忽略未知非必需字段，但未知 schema_version 报错。更新 v1 清单只写已定义字段。

空库缺失个人 manifest 等价于零项；已存在但损坏的文件报 integrity 错误，不能当空库覆盖。文件签名和哈希不证明画面或动画完整可解码，显示验收单独完成。

### 3.3 素材仓库包目录

仓库保留根目录全量 `manifest.json`，增加 `packs/curated.json` 与 `packs.json`，原图不复制。包目录草案：

```json
{
  "schema_version": 1,
  "packs": [{
    "id": "curated",
    "name": "精选",
    "description": "适合日常交流的表情，已复核描述",
    "manifest": "packs/curated.json",
    "manifest_sha256": "<64 lowercase hex>",
    "count": 120,
    "size": 21703567
  }]
}
```

`all` 指向 `manifest.json`。id 满足 `[a-z][a-z0-9_-]{0,63}`，目录内唯一；manifest 只接受根目录 `manifest.json` 或 `packs/<id>.json`。count/size 必须与清单一致。manifest_sha256 就是修订号，禁止把此字段写进被哈希的清单造成自引用。

先验证目录中声明的 manifest_sha256，再解析清单，图片按清单自己的哈希验证。期间源变更导致校验不匹配时失败，不混用新清单；下一次显式调用从新目录开始。官方 raw 源无需先克隆仓库或调用 GitHub API。

本地素材仓库目前的 packs.json 仍是未发布草稿，缺少修订字段；素材合同 Issue 负责升级草稿，不能把它当最终格式。

### 3.4 安装状态

`.sticker/packs/<id>.json` 保存 `schema_version:1`、`id`、`source`、`revision`、`installed_at`、`manifest`。manifest 是完整标准 v1 对象，其文件路径仍以 `<home>` 为根，不能以状态文件所在目录为根。

同一个 home 的包 ID 唯一；同 ID 不同源返回 `conflict/source_conflict`，需先移除旧包。相同修订重复安装仅校验和复用原图。CLI 版本与素材修订独立。

缓存按规范化 source 的 SHA-256 区分。只接受无 userinfo/query/fragment 的 HTTPS URL 或绝对化本地目录；不记录账号或临时签名地址。

### 3.5 为什么首版使用 JSON，而非 JSONL 或 SQLite

| 方案 | 适用点 | 对本项目的取舍 |
| --- | --- | --- |
| JSON manifest | 现有标准、Git 可评审、可直接导入导出 | 首版权威清单，原图继续是普通文件 |
| JSONL | 按行流式处理和追加记录 | 不作为权威素材库；修改、删除和去重仍需重写或事件归并，没有解决索引与事务 |
| SQLite | 本地结构化查询、事务、全文索引 | 当前不增加第二份状态；出现明确需求时评估为可重建索引 |

此结论来自当前素材规模和兼容要求，不是“SQLite 不适合 CLI”。[SQLite 官方](https://www.sqlite.org/whentouse.html) 明确列出本地应用文件与缓存的用途；[JSON Lines](https://jsonlines.org/) 定义逐行 JSON，适用于流式处理。[FTS5](https://www.sqlite.org/fts5.html) 提供全文检索，但中文分词、短词和子串匹配需另做检索质量验证，不能仅打开 FTS 就假定支持现有中文检索语义。

引入 SQLite 的触发条件：代表性数据上扫描无法达到既定 p95 目标，或明确新增全文相关度、复杂组合筛选等需求。先测量再决定，不按固定条目数武断切换。

若仅增加查询索引：JSON 与原图仍是事实来源；索引保存清单内容哈希，修改后失效并可重建；索引丢失不丢收藏，SQLite 文件不进入素材仓库。若未来希望数据库成为权威写入存储，应另行修订产品合同，不能悄悄引入 JSON/SQLite 双重权威。首版也不增加 JSONL 事件日志。

### 3.6 收藏分组与排序元数据

标准 v1 `manifest.json` 继续只保存条目和原图信息；分组、成员关系与顺序保存在 CLI 自有的 `.sticker/collections.json`。这样旧客户端仍可读取标准清单，整理功能也不会把非标准字段写入公共素材格式。

```json
{
  "schema_version": 1,
  "collections": [{
    "id": "favorites",
    "name": "我的收藏",
    "position": 0,
    "items": [{"id": "<md5>", "position": 0, "added_at": "2026-01-02T03:04:05Z"}]
  }]
}
```

`favorites` 是不可删除的默认分组；自定义分组 ID 使用 `[a-z][a-z0-9_-]{0,63}`，名称为非空 UTF-8 文本且不超过 128 字节。一个条目可以出现在多个分组，但原图在 `emoticons/` 中只有一份。分组引用的 ID 必须存在于个人 `manifest.json`，否则读取和写入均返回 `integrity/invalid_collection`，不能自动清空或修复有效数据。

列表支持 `manual`、`added`、`caption` 和 `md5` 四种排序。`manual` 使用分组内的 position；`added` 使用分组条目的 `added_at`，旧导入条目缺失时按 manifest 顺序补齐；`caption` 和 `md5` 以 MD5 作为最终稳定键。批量移动、重排和取消收藏先完整校验所有 ID，再在同一个 `.sticker/collections.json` 原子替换中提交；任何无效 ID 都不产生部分变更。导出可附带同名 `collections.json` 扩展，导入时文件缺失即把所有条目放入默认分组，旧客户端忽略该扩展不影响 v1 读取。

## 4. CLI 合同

### 4.1 公共参数与命令

公共 `--home PATH`、`--format json|table`；默认 json。`--json` 是 `--format json` 的兼容别名，同时显式传冲突值时报参数错误。无 stdin 隐式交互；未指明 pack 不默认全量。`--help` 为文本，schema 始终 JSON。

| 命令 | 参数 | data 关键字段 | 阶段 |
| --- | --- | --- | --- |
| version | 无 | version, commit | P1 |
| schema [command…] | Cobra 命令路径 | command, parameters, result_schema, errors, effect, examples | P1 |
| packs list | --source ROOT, --offline | items（含 installed/revision）, fetched_at, stale | P1 |
| packs install ID | --source ROOT, --dry-run | pack, revision, added, reused, download_bytes | P1 |
| search QUERY | --pack ID, --favorites, --limit N, --offset N | items, total, next_offset, has_more | P1 |
| setup | --pack curated\|all（默认 curated）, --source ROOT, --dry-run | 与 install 相同并附 setup 标记 | P1 |
| get ID | 完整小写 MD5；可选 `--preview` | item（含可选 preview_path） | P1 |
| favorites add [PATH] | --id ID（二选一）, --caption TEXT, --dry-run | item, added, updated | P1 |
| favorites list | --collection ID, --sort manual\|added\|caption\|md5, --limit N, --offset N | 与 search 相同分页结构 | P1 |
| favorites collections | list；create NAME；rename ID NAME；remove ID | collections, changed | P1 |
| favorites organize | --collection ID, --ids ID..., --move-to ID, --order ID..., --dry-run | moved, reordered, removed, committed | P1 |
| favorites import DIR | --overwrite-captions, --dry-run | added, skipped, updated, conflicts, failed, committed | P1 |
| favorites describe ID | 必须显式 --caption TEXT, --dry-run | item, updated | P2 |
| favorites remove ID... | --dry-run | removed, retained_original, committed | P2 |
| favorites export DIR | --dry-run | path, count, size | P2 |
| packs update ID | --dry-run；沿用已保存 source | 与 install 相同 | P2 |
| packs remove ID | --dry-run | removed, retained_bytes | P2 |

item：`id`（等于 md5）、`md5`、`sha256`、`filename`、`format`、`size`、`caption`、`path`（绝对）、`favorite`、`packs`（排序后的包 ID 数组）。`get --preview` 在静态 WebP 时额外返回绝对 `preview_path`（PNG）；预览使用 SHA-256 作为缓存身份，原图 `path` 与内容标识不变。没有自定义 Markdown 拼接，也不自动打开外部应用。

### 4.2 输出与错误

成功：`{"ok":true,"data":{...},"meta":{"schema_version":1}}`，退出 0。
失败：`{"ok":false,"error":{"type":"…","subtype":"…","message":"…","hint":"…","retryable":false}}`，写 stderr，stdout 为空。导入失败可在 error.details 中返回有界计数与最多 20 个条目错误；不同时打印成功 envelope。

message/hint 为面向人的文字，不是分支依据；type/subtype/退出码必须稳定。JSON 输出最多 256 KiB；列表缩短本页并返回真实 next_offset。单条超过上限则显式 `output_limit`，不悄悄截断路径或 caption。

| type | subtype 示例 | 退出码 |
| --- | --- | --- |
| validation | invalid_argument, unsupported_schema, unsafe_path, output_limit, unsupported_format | 2 |
| not_found | pack_not_found, item_not_found, source_not_found | 3 |
| network | timeout, request_failed, http_error | 4 |
| integrity | hash_mismatch, invalid_manifest, invalid_image, invalid_collection | 5 |
| conflict | digest_conflict, source_conflict, destination_exists, library_busy, state_changed | 6 |
| io | permission_denied, disk_full, read_failed, write_failed | 7 |
| internal | unexpected, unimplemented | 1 |
| cancelled | interrupted | 130 |

### 4.3 搜索与分页

按 caption 做 Unicode 小写后子串匹配，不做分词或语义推断。来源集合：先按 --pack 筛选，再与 --favorites 取交集；不存在的 --pack 报错。同一 MD5 不同 SHA-256 视作冲突，不任选一项。

重复项 caption 优先级：存在个人记录时使用个人 caption（包括显式空值）；否则按 pack ID 字典序选择首个非空 caption。查询结果按 MD5 升序。offset ≥ total 返回空列表；limit 1–100，默认 10；offset ≥ 0。

翻页时修改素材会改变顺序，用户应重启检索；首版不承诺跨变更快照游标。get 校验文件，search 只读清单而不全量读图；路径是查询时的位置，后续外部删除会使其失效。

### 4.4 WebP 预览

`get <id>` 始终返回经过完整性校验的原图路径，不改变 GIF、WebP 或其他格式的原始字节。显式传入 `--preview` 时，CLI 仅为静态 WebP 在 `.sticker/previews/<sha256>.png` 生成或复用 PNG 预览，并在结果中返回 `preview_path`；生成过程使用有界读取和原子文件提交，不写回标准 manifest。动画 WebP 或无法解码的文件返回 `integrity/invalid_image` 或 `validation/unsupported_format`，同时保留原图可读取路径。客户端应优先展示 `preview_path`，需要动画时使用原图 `path`。

### 4.5 一键初始化与发布安装

`setup` 是对正式安装流程的 convenience 包装，不引入第二套状态或下载逻辑。未传 `--pack` 时使用 `curated`；只有显式传入 `--pack all` 才安装全量。它透传 `--source` 和 `--dry-run`，返回与 `packs install` 相同的修订、计数和字节字段，并额外标记 `setup:true`。命令帮助必须指向正式的 `packs install`，不能让 Agent 依赖隐式默认全量。

Release 为每个支持的平台提供版本固定的归档、`checksums.txt`，以及 Unix shell 和 Windows PowerShell 安装入口。入口识别 OS/架构，默认写入用户可写目录，不要求 sudo；下载后先校验 SHA-256，校验失败删除临时文件并以非零退出。二进制安装与素材安装分开：脚本不把图片打入归档，也不绕过 `packs install` 的包修订、dry-run 和本地源合同。安装入口支持显式版本，未指定版本使用发布页声明的稳定版本。

## 5. 用例与一致性

### 5.1 安装与更新

1. 读取目录（HTTP 超时有界或本地文件），选 ID，获取并验证固定修订清单。
2. 检查当前安装来源与已有文件，生成计划。dry-run 到此结束，不创建缓存、锁文件或数据目录。
3. 将缺少的图片下载到本次调用独有的 staging 子目录，同文件系统临时文件；流式计算大小与双哈希，限制读取到声明 size+1。网络下载不持有库写锁。
4. 发布时取得 home 的跨进程排他锁；读操作只在读清单快照期间持共享锁。等待最多 5 秒且可取消，超时返回 library_busy。使用跨平台 OS 文件锁，进程退出自动释放；不以遗留空目录判断进程存活。
5. 锁内重读状态并重新检查来源、修订与目标文件。若另一个调用已安装相同修订则复用；若它将同包更新到另一个修订，返回 conflict/state_changed 而不覆盖。验证过的暂存图提交到规范文件名。
6. 已有原图匹配时复用；损坏时返回 integrity 并提示先备份/修复，不静默覆盖可能被收藏使用的内容。
7. 所有原图持久化成功且 context 未取消，提交单个安装状态文件。根目录个人 manifest 不变。更新替换旧状态，失败继续使用旧状态。

进程取消发生在提交之后时，操作已成功，不返回可促使 Agent 重复执行的“未提交”提示；提交开始后完成该原子步骤再报告结果。网络失败时不发布新清单，未引用但已验证的原图可留待下次复用。

### 5.2 添加与批量导入

PATH 和 --id 二选一；--id 读取安装或个人清单中的原图。传 --caption（包括空串）表示显式修改；不传则保留原个人描述，新条目可继承已有素材描述。

导入 DIR 不要求 packs.json，只读取 DIR/manifest.json 和被引用图片；最终复制到 home/emoticons，合并 home/manifest.json。源与目标同目录允许幂等校验，不能盲目自覆盖。

采用**清单整体提交**：预校验全部元数据，遍历验证并复制图片，任何失败均不提交个人 manifest。校验与暂存不持有库写锁，发布时加锁重读最新个人 manifest 并重新合并，防止丢失并发收藏。已验证的孤立原图可保留；错误计数中的 added/updated 是拟变更数，`committed:false` 明确未生效。成功时 `committed:true`，所有计数都是实际结果。dry-run 校验输入和冲突，可读取源图，但不写文件。

同 MD5/SHA256 复用；SHA 不同报冲突。默认重复导入保留目标 caption，--overwrite-captions 才替换；新条目采用源 caption。标准 manifest 与图片足以迁移，绝不写独立 favorites.json 格式。

### 5.3 文件提交与并发

使用单一文件原子替换适配：同目录 CreateTemp → 写入 → Sync → Close → 原子替换。Unix 用 rename 并同步父目录；Windows 使用系统支持的原子替换路径，目标不存在与目标存在分别覆盖测试。不能先删除旧文件再重命名，也不能仅凭 Windows 交叉编译声称替换语义正确。

如果替换已成功而后续目录同步报错，返回 I/O 错误并在 details 标记 `committed:true`，提示重新读取状态；不能声称变更未发生。对断电文件系统耐久性的保证以平台测试能力为界，不把进程崩溃测试等同断电测试。

共享锁覆盖清单读取；获取所需原图前持锁打开文件，读完后释放。不需要跨多个 metadata 文件的数据库事务：每个写命令只修改一个权威清单或一个包状态。缓存失败不影响已提交业务结果。

### 5.4 取消收藏、卸载与导出

取消收藏仅修改个人 manifest，卸载仅移除对应安装状态。首版**不自动回收原图文件**，响应报告 retained_bytes/retained_original；因此不会误删其他包、个人收藏或外部使用中的图片。未来垃圾回收须单独设计引用检查，不混入这批 Issue。

导出只选择个人 manifest 中的条目，复制到目标同级临时目录并校验，最后发布到尚不存在的目标目录；目标创建竞争也必须拒绝覆盖。产物为标准 manifest、emoticons，并附 packs.json 方便作为包安装。失败不留下完整目标目录；源数据不变。

### 5.5 收藏整理

分组命令只修改 `.sticker/collections.json`，图片和标准个人 `manifest.json` 仍是独立事实来源。创建、重命名和删除分组均在写锁内校验；默认 `favorites` 分组不可删除，删除自定义分组时必须显式选择将条目移入默认分组或一并移除收藏关系。后者只移除个人条目，不回收仍被其他包或分组引用的原图。

`favorites organize` 支持一次请求中的加入、移出、移动到另一分组和手动重排；`--dry-run` 只返回计划。命令先解析并验证全部 ID、分组和顺序是否唯一，再使用临时文件加 fsync 原子替换元数据。任一无效 ID、重复顺序或并发版本变化都返回稳定冲突/校验错误，旧元数据保持可读。

`favorites list --sort manual|added|caption|md5` 先按分组成员过滤，再排序和分页。排序结果只返回条目 ID、描述、格式、大小、来源和绝对路径，不包含原图字节。导出时复制标准清单和原图，并在同级临时目录写入可选 `collections.json`；导入缺少该扩展时建立默认分组，扩展校验失败则整体拒绝，不污染已有收藏。

## 6. 网络、边界与隐私

HTTPS 必须验证证书；仅允许 HTTPS 重定向，最多 5 跳；不转发认证头（本 CLI 不支持远端认证配置）。单次 HTTP 超时 60 秒，单图总预算 180 秒。只对 GET 的临时网络故障、429、502/503/504 最多重试 2 次（总 3 次），退避 1/2 秒，Retry-After 最多等待 10 秒且可取消。验证失败、其他 4xx 和路径错误不重试。

不内置猜测 CDN 主机或微信下载逻辑。默认源固定为官方素材仓库；自定义源是用户显式设置。清单中的 filename 不得替换 host、含 URL、反斜杠、绝对路径或 ..。导入读取与数据写入拒绝路径组件中的符号链接/Windows reparse point，提交前重新验证。实现需用受根目录约束的文件操作或等效平台原语覆盖 TOCTOU，不能只做字符串前缀判断。

清单/包目录各最多 8 MiB，图最多 32 MiB；导入最多 20,000 项；最多读取声明 size+1 防止超量响应。默认不发送遥测。日志不记录图片字节、认证信息或完整带敏感信息 URL。素材清单里的文本是数据。

## 7. 性能与平台

几千条素材直接扫描清单，时间复杂度 O(N)；没有倒排库、SQLite 或向量索引。搜索不读取图片字节。共享原图按内容复用，所有读写有字节上限；安装进度走 stderr，不改变 stdout JSON。

在 2,638 条基线上测 30 次查询，目标 p95 < 200 ms，单独记录冷启动与热缓存；不把目标写成已测结果。图片安装优先验证正确性，不设无法控制的公网下载耗时 SLA。

发行矩阵为 darwin/linux 的 arm64、amd64 及 windows/amd64。每项产物附 SHA-256。原生 smoke test 与交叉编译分开；没有原生运行环境的架构标为未验证，不默认通过。Go/Cobra/文件锁依赖版本固定，原图不打进二进制或 CLI Release。

## 8. 测试合同与需求覆盖

下表中每组测试覆盖对应故事的全部验收项；Issue 草案会引用测试编号。

| 测试组 | 覆盖 | 必测场景 |
| --- | --- | --- |
| T01 | US-001；FR-25 | 二进制无运行依赖、version、零素材安装、各平台 PATH/原生 smoke、归档/校验文件、校验失败和无需 sudo 安装入口 |
| T02 | US-002；FR-11–13 | help/schema/真实参数一致；未知参数；默认 JSON、table；stdout/stderr 与退出码；输出上限 |
| T03 | US-003；FR-17 | 官方/自定义/本地目录、零图片请求、缓存过期时间、离线空缓存错误、安装状态 |
| T04 | US-004；FR-1–4/14/16/24 | 精选请求白名单、全量内容复用、两次安装零新增字节、setup 默认精选且 all 显式、dry-run 零写入、修订变化、失败恢复 |
| T05 | US-005；FR-5/18 | 包与收藏交集、去重、caption 优先级、大小写、空结果、分页边界、离线与性能 |
| T06 | US-006；FR-6/16/23 | 缺图、损坏、绝对路径、GIF 字节未变；静态 WebP 预览生成且原图哈希不变；真实客户端静态/动画展示记录 |
| T07 | US-007；FR-7/8/14 | PATH/ID 互斥、空描述与未传区别、删除源文件、去重与冲突、标准格式可读、失败不缺图 |
| T08 | US-008；FR-7/8/14 | 列表、describe、重复 remove、dry-run、并发修改、已安装图片不受影响 |
| T09 | US-009；FR-9/14/15 | 无 packs.json 导入、旧 collection、原图缺失/超限/越界、零部分清单提交、覆盖策略、源目录移走 |
| T10 | US-010；FR-10/14 | round-trip 数量/描述/哈希、目标存在与创建竞争、dry-run、失败清理、零外部发布 |
| T11 | US-011；FR-8/14 | update 来源保留、失败旧包可用、remove 重试、共享图与收藏保留、未回收字节准确 |
| T12 | US-012；FR-1/16 | 原图唯一、精选子集、计数/哈希、v1 兼容、改名链接、画面描述复核 |
| T13 | US-013 | Skill 指令与 CLI 一致、不覆盖已有指引、真实 Agent 端到端，平台限制如实报告 |
| T14 | FR-4/7/15；横切 | 原子替换故障注入、并发进程、磁盘满、取消前后、Windows reparse、symlink/TOCTOU、锁释放 |
| T15 | US-013 | Skill 指令与 CLI 一致、不覆盖已有指引、真实 Agent 端到端，平台限制如实报告 |
| T16 | US-014；FR-19 | 创建/重命名/删除分组、成员引用校验、缺失扩展回退默认分组、旧 v1 清单兼容、损坏元数据不覆盖有效数据 |
| T17 | US-015；FR-20/21 | 分组筛选、四种稳定排序、批量移动/重排/取消、dry-run、无效 ID 整体失败、并发原子提交 |
| T18 | US-016；FR-22/24 | 临时目录与本地 fixture 完成 setup 精选安装、离线搜索、原图读取、清单导入、分组创建/排序/批量整理；显式本地素材源、精选不访问全量独有文件、损坏哈希和元数据失败且不发布不完整状态；CI 无外部服务并可重复运行 |

每个实现 Issue 跑相关回归、go test、vet 与 lint；并发/文件提交跑 race 和平台测试。文档阶段只检查映射、示例、链接与差异；当前尚未执行产品验收。

## 9. 实施顺序

依赖与具体 Issue 草案见 [实施任务清单](issues-sticker-cli.md)。该文件使用本地编号 I01…，不是 GitHub Issue 编号。

P1：Go 基础 → 标准素材库 I/O → 素材目录/包发现 → 安装/搜索/原图读取 → 原图与清单导入 → 收藏分组与整理 → setup convenience → 分发及 Agent 验收 → 自动化端到端回归。
P2：收藏描述、导出与包更新卸载。可以在依赖满足后独立开发，但本轮不派发实施。

## 10. 已决事项与剩余风险

- PRD 已确认命名：CLI sticker-cli，命令 sticker，素材 sticker-ext；远端仓库重命名随各自迁移步骤执行。
- 不另建 favorites 专有清单；根目录个人 manifest 与公共包清单具有同一种 v1 格式。
- 分组与排序写入 `.sticker/collections.json` 扩展；标准 v1 清单和原图仍是跨客户端交换格式。
- 首版不自动释放卸载包占用的原图空间；这是保护共享素材的明确范围选择，命令必须报告。
- 精选清单当前为 120 张，已完成逐张画面复核；后续变更仍需按清单重新校验。
- Windows 原子替换与路径边界必须在 Windows 原生测试，不能用 Unix 行为代替。
- 代码许可证尚需在首次代码发布前确定；不影响本轮技术设计，不能给素材自动套用代码许可证。

实施时保持：标准个人库与包状态的边界、整体提交的导入规则，以及首版卸载保留原图的行为。
