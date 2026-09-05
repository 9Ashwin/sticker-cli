# 实施 Issues：sticker-cli

状态：I01…I22 已创建为 GitHub Issues（20 项在 CLI 仓库、2 项在素材仓库）；编号用于保持设计与实际 Issue 的映射。

来源：[PRD](prd-sticker-cli.md)、[SPEC](spec-sticker-cli.md)。P1 为首个可用闭环，P2 为后续生命周期功能。

| 编号 | 标题 | 阶段 | 依赖 | 目标仓库 |
| --- | --- | --- | --- | --- |
| [I01](https://github.com/9Ashwin/sticker-cli/issues/1) | Go 项目与 CLI 入口 | P1 | 无 | sticker-cli |
| [I02](https://github.com/9Ashwin/sticker-cli/issues/2) | 标准素材库 I/O 与安全提交 | P1 | I01 | sticker-cli |
| [I03](https://github.com/9Ashwin/sticker-cli/issues/3) | 命令帮助、schema 与结构化输出 | P1 | I01 | sticker-cli |
| [I04](https://github.com/9Ashwin/sticker-ext/issues/1) | 素材包目录与修订合同 | P1 | 无 | 素材仓库 |
| [I05](https://github.com/9Ashwin/sticker-ext/issues/2) | 精选复核与素材仓库改名 | P1 | I04 | 素材仓库 |
| [I06](https://github.com/9Ashwin/sticker-cli/issues/4) | 素材源发现与离线目录缓存 | P1 | I02、I03、I04 | sticker-cli |
| [I07](https://github.com/9Ashwin/sticker-cli/issues/5) | 安装预检与 dry-run | P1 | I06 | sticker-cli |
| [I08](https://github.com/9Ashwin/sticker-cli/issues/6) | 按需下载与可恢复安装 | P1 | I07 | sticker-cli |
| [I09](https://github.com/9Ashwin/sticker-cli/issues/7) | 离线检索与有界分页 | P1 | I02、I03 | sticker-cli |
| [I10](https://github.com/9Ashwin/sticker-cli/issues/8) | 已验证原图读取 | P1 | I09 | sticker-cli |
| [I11](https://github.com/9Ashwin/sticker-cli/issues/9) | 本地原图与已安装图片收藏 | P1 | I02、I03、I10 | sticker-cli |
| [I12](https://github.com/9Ashwin/sticker-cli/issues/10) | 标准 v1 素材库合并导入 | P1 | I11 | sticker-cli |
| [I13](https://github.com/9Ashwin/sticker-cli/issues/11) | 收藏列表、描述修改与取消 | P2 | I11 | sticker-cli |
| [I14](https://github.com/9Ashwin/sticker-cli/issues/12) | 标准收藏包导出与迁移 | P2 | I12 | sticker-cli |
| [I15](https://github.com/9Ashwin/sticker-cli/issues/13) | 显式素材更新 | P2 | I08、I11 | sticker-cli |
| [I16](https://github.com/9Ashwin/sticker-cli/issues/14) | 素材包卸载与收藏保留 | P2 | I08、I11 | sticker-cli |
| [I17](https://github.com/9Ashwin/sticker-cli/issues/15) | Go 二进制分发与平台验收 | P1 | I05、I08、I10、I12 | sticker-cli |
| [I18](https://github.com/9Ashwin/sticker-cli/issues/16) | Agent Skill 与端到端使用验收 | P1 | I03、I05、I08、I10、I12、I17、I19、I20、I22 | sticker-cli |
| [I19](https://github.com/9Ashwin/sticker-cli/issues/18) | 收藏分类与自定义分组 | P1 | I02、I03、I11、I12 | sticker-cli |
| [I20](https://github.com/9Ashwin/sticker-cli/issues/19) | 收藏排序与批量整理 | P1 | I13、I14、I19 | sticker-cli |
| [I21](https://github.com/9Ashwin/sticker-cli/issues/20) | 收藏整理端到端自动化验证 | P1 | I01–I20 | sticker-cli |
| [I22](https://github.com/9Ashwin/sticker-cli/issues/21) | 一键初始化精选或全量素材 | P1 | I03、I06、I08 | sticker-cli |

素材仓库统一命名为 `9Ashwin/sticker-ext`，由现有素材仓库改名。改名前后的 Issue 链接均指向该最终名称，GitHub 历史链接由平台重定向。

## I01：Go 项目与 CLI 入口

GitHub：[9Ashwin/sticker-cli#1](https://github.com/9Ashwin/sticker-cli/issues/1)

范围：US-001。阶段：P1。类型：infra。优先级：high。

依赖：无。SPEC：§2、7。验证组：T01。

验收：

- [ ] 固定受支持的 Go、Cobra 和开发工具版本，建立无 CGO 构建与 CI。
- [ ] 实现 main/context/退出入口及 version 注入，不下载素材、不依赖 MCP。
- [ ] 建立单测、vet、lint 与跨平台构建命令，运行依赖只含二进制。

相关实现运行聚焦回归、Go test/vet/lint；并发和文件提交增加 race 与目标平台验证。素材任务运行清单/原图校验及文档链接检查。

## I02：标准素材库 I/O 与安全提交

GitHub：[9Ashwin/sticker-cli#2](https://github.com/9Ashwin/sticker-cli/issues/2)

范围：横切 FR-4/7/15。阶段：P1。类型：backend。优先级：high。

依赖：I01。SPEC：§3.1–3.2、5.3、6。验证组：T14。

验收：

- [ ] 读取/校验/写入现有 v1 manifest，不创建收藏专有格式；损坏目标不能当空库覆盖。
- [ ] 实现双哈希、格式签名、字节/条目上限和受根目录限制的文件操作。
- [ ] 实现可取消跨进程锁、临时文件提交及 Unix/Windows 原子替换适配。
- [ ] 故障注入覆盖并发、取消、磁盘错误、链接逃逸与提交后错误；Windows 原生测试单独报告。

相关实现运行聚焦回归、Go test/vet/lint；并发和文件提交增加 race 与目标平台验证。素材任务运行清单/原图校验及文档链接检查。

## I03：命令帮助、schema 与结构化输出

GitHub：[9Ashwin/sticker-cli#3](https://github.com/9Ashwin/sticker-cli/issues/3)

范围：US-002。阶段：P1。类型：backend。优先级：high。

依赖：I01。SPEC：§2、4.1–4.2。验证组：T02。

验收：

- [ ] 建立 Cobra 参数定义与 schema 共用的注册入口，输出 schema、effect、examples 和错误列表。
- [ ] JSON 成功走 stdout，错误走 stderr；冻结 type/subtype/退出码映射。
- [ ] 支持 table、--json 别名，冲突参数和未知参数返回 validation。
- [ ] 完成 help/schema 一致性和 256 KiB 输出边界测试。

相关实现运行聚焦回归、Go test/vet/lint；并发和文件提交增加 race 与目标平台验证。素材任务运行清单/原图校验及文档链接检查。

## I04：素材包目录与修订合同

GitHub：[9Ashwin/sticker-ext#1](https://github.com/9Ashwin/sticker-ext/issues/1)

范围：US-012。阶段：P1。类型：infra。优先级：high。

依赖：无。SPEC：§3.2–3.3。验证组：T12。

验收：

- [ ] 保留根目录全量 v1 清单，新增 packs.json 与精选清单引用，原图不复制。
- [ ] 每包提供 manifest_sha256、count、size、名称描述；验证清单原始字节修订。
- [ ] 维护校验覆盖哈希、路径、条目计数、重复图和精选子集，旧 v1 导入兼容。

相关实现运行聚焦回归、Go test/vet/lint；并发和文件提交增加 race 与目标平台验证。素材任务运行清单/原图校验及文档链接检查。

## I05：精选复核与素材仓库改名

GitHub：[9Ashwin/sticker-ext#2](https://github.com/9Ashwin/sticker-ext/issues/2)

范围：US-012。阶段：P1。类型：infra。优先级：high。

依赖：I04。SPEC：§3.3、10。验证组：T12。

验收：

- [ ] 逐张复核 124 张候选的画面与描述，删除或更正不适合项；数量按最终清单生成。
- [ ] 将素材远端仓库改名 sticker-ext，保留历史与原图。
- [ ] 同步 README、AGENTS、示例和仓库说明中的名称及链接；更新包修订。
- [ ] 对新地址做目录/清单/样本图读取验证，并记录旧消费端兼容结果。

相关实现运行聚焦回归、Go test/vet/lint；并发和文件提交增加 race 与目标平台验证。素材任务运行清单/原图校验及文档链接检查。

## I06：素材源发现与离线目录缓存

GitHub：[9Ashwin/sticker-cli#4](https://github.com/9Ashwin/sticker-cli/issues/4)

范围：US-003。阶段：P1。类型：backend。优先级：high。

依赖：I02、I03、I04。SPEC：§3.3–3.4、4.1、6。验证组：T03。

验收：

- [ ] 实现 packs list 的官方 HTTPS、本地目录与显式 --source。
- [ ] 列出包描述、修订、大小、安装状态；请求目录不下载图片。
- [ ] 缓存按 source 区分，--offline 显示 fetched_at/stale，空缓存报明确错误。
- [ ] 拒绝目录越界、非法 source 与冲突字段，网络处理有超时/重试预算。

相关实现运行聚焦回归、Go test/vet/lint；并发和文件提交增加 race 与目标平台验证。素材任务运行清单/原图校验及文档链接检查。

## I07：安装预检与 dry-run

GitHub：[9Ashwin/sticker-cli#5](https://github.com/9Ashwin/sticker-cli/issues/5)

范围：US-004。阶段：P1。类型：backend。优先级：high。

依赖：I06。SPEC：§3.3–3.4、4.1、5.1。验证组：T04。

验收：

- [ ] 安装必须指定 ID，验证目录与 manifest 原始字节修订及计数大小。
- [ ] 生成新增/复用/下载字节计划，已有文件按双哈希验证。
- [ ] dry-run 可读取远端目录/清单，但不下载图片、不创建库目录/锁/缓存。
- [ ] 覆盖源在调用间改变、包 ID 冲突与损坏已有图片。

相关实现运行聚焦回归、Go test/vet/lint；并发和文件提交增加 race 与目标平台验证。素材任务运行清单/原图校验及文档链接检查。

## I08：按需下载与可恢复安装

GitHub：[9Ashwin/sticker-cli#6](https://github.com/9Ashwin/sticker-cli/issues/6)

范围：US-004。阶段：P1。类型：backend。优先级：high。

依赖：I07。SPEC：§5.1、5.3、6。验证组：T04、T14。

验收：

- [ ] 仅下载选定包原图，staging 下载不持库写锁，使用有界流式校验。
- [ ] 提交时重读状态处理并发/修订竞争；全部图片完成才发布包状态。
- [ ] 失败不破坏旧安装，重试可复用已校验图片，重复安装新增原图下载为零。
- [ ] 测试记录所有图片请求，精选安装不能访问全量独有文件；覆盖取消与 GIF 原始字节。

相关实现运行聚焦回归、Go test/vet/lint；并发和文件提交增加 race 与目标平台验证。素材任务运行清单/原图校验及文档链接检查。

## I09：离线检索与有界分页

GitHub：[9Ashwin/sticker-cli#7](https://github.com/9Ashwin/sticker-cli/issues/7)

范围：US-005。阶段：P1。类型：backend。优先级：high。

依赖：I02、I03。SPEC：§4.3、7。验证组：T05。

验收：

- [ ] 合并个人清单与包清单，按内容去重，执行指定 caption 优先级。
- [ ] 支持 --pack/--favorites 交集、大小写不敏感子串查询及 MD5 稳定排序。
- [ ] 对 fixture 中覆盖多个条目的宽泛场景词返回一组候选；验收不依赖情绪分类器或唯一语义命中。
- [ ] 提供 limit/offset/next_offset/has_more，空结果成功，超限输出仍可续查。
- [ ] 验证离线零网络及 2,638 条基线性能，报告测量值而非预设达标。

相关实现运行聚焦回归、Go test/vet/lint；并发和文件提交增加 race 与目标平台验证。素材任务运行清单/原图校验及文档链接检查。

## I10：已验证原图读取

GitHub：[9Ashwin/sticker-cli#8](https://github.com/9Ashwin/sticker-cli/issues/8)

范围：US-006。阶段：P1。类型：backend。优先级：high。

依赖：I09。SPEC：§4.1、4.3–4.4、5.3。验证组：T06。

验收：

- [ ] get 完整 MD5 校验目标文件，返回绝对路径与来源信息。
- [ ] 缺图/损坏给出稳定错误和恢复建议；结果不含 base64 或原图字节。
- [ ] 保留 GIF 内容并做真实静态/动画客户端展示检查，记录客户端差异。
- [ ] `get --preview` 为静态 WebP 生成或复用 PNG 预览路径，预览不改变原图哈希；动画 WebP 或解码失败返回稳定错误。

相关实现运行聚焦回归、Go test/vet/lint；并发和文件提交增加 race 与目标平台验证。素材任务运行清单/原图校验及文档链接检查。

## I11：本地原图与已安装图片收藏

GitHub：[9Ashwin/sticker-cli#9](https://github.com/9Ashwin/sticker-cli/issues/9)

范围：US-007。阶段：P1。类型：backend。优先级：high。

依赖：I02、I03、I10。SPEC：§3.1–3.2、5.2。验证组：T07。

验收：

- [ ] favorites add 接受 PATH 或 --id 二选一，将原图和条目写入标准个人素材库。
- [ ] 实现内容去重、caption 未传/空串/已有描述语义及冲突拒绝。
- [ ] 支持 dry-run；并发收藏不会覆盖已成功写入的其他条目。
- [ ] 源文件移走后收藏可读取，图片写失败不提交缺图记录。

相关实现运行聚焦回归、Go test/vet/lint；并发和文件提交增加 race 与目标平台验证。素材任务运行清单/原图校验及文档链接检查。

## I12：标准 v1 素材库合并导入

GitHub：[9Ashwin/sticker-cli#10](https://github.com/9Ashwin/sticker-cli/issues/10)

范围：US-009。阶段：P1。类型：backend。优先级：high。

依赖：I11。SPEC：§3.2、5.2、6。验证组：T09。

验收：

- [ ] favorites import 只要求 manifest.json 与 emoticons，不要求 packs.json 或 MCP。
- [ ] 按标准格式合并到目标库，默认保留目标描述，显式选项才覆盖。
- [ ] 批量整体提交；任一错误不提交个人清单，错误计数与 committed 状态明确。
- [ ] 支持 dry-run，测试源目录移走、源等于目标、越界/损坏/重复与并发修改。

相关实现运行聚焦回归、Go test/vet/lint；并发和文件提交增加 race 与目标平台验证。素材任务运行清单/原图校验及文档链接检查。

## I13：收藏列表、描述修改与取消

GitHub：[9Ashwin/sticker-cli#11](https://github.com/9Ashwin/sticker-cli/issues/11)

范围：US-008。阶段：P2。类型：backend。优先级：medium。

依赖：I11。SPEC：§4.1、5.4。验证组：T08。

验收：

- [ ] 提供 favorites list 分页、describe 显式修改描述与 remove。
- [ ] 写命令支持 dry-run；重复取消幂等，原图仍可供已安装包使用。
- [ ] 并发修改不丢失记录，命令仅更新标准个人 manifest。

相关实现运行聚焦回归、Go test/vet/lint；并发和文件提交增加 race 与目标平台验证。素材任务运行清单/原图校验及文档链接检查。

## I14：标准收藏包导出与迁移

GitHub：[9Ashwin/sticker-cli#12](https://github.com/9Ashwin/sticker-cli/issues/12)

范围：US-010。阶段：P2。类型：backend。优先级：medium。

依赖：I12。SPEC：§3.3、5.4。验证组：T10。

验收：

- [ ] 只导出个人清单所引用的原图，生成 v1 manifest 和可安装 packs.json。
- [ ] 同级临时目录发布，目标存在或竞争创建时拒绝覆盖，dry-run 不写目标。
- [ ] 新库 round-trip 导入后的数量、描述与原图哈希完全一致。
- [ ] 失败清理临时结果，禁止自动外部上传或发布。

相关实现运行聚焦回归、Go test/vet/lint；并发和文件提交增加 race 与目标平台验证。素材任务运行清单/原图校验及文档链接检查。

## I15：显式素材更新

GitHub：[9Ashwin/sticker-cli#13](https://github.com/9Ashwin/sticker-cli/issues/13)

范围：US-011。阶段：P2。类型：backend。优先级：medium。

依赖：I08、I11。SPEC：§3.4、5.1。验证组：T11。

验收：

- [ ] packs update 沿用已安装 source，固定新修订并提供 dry-run。
- [ ] 全部图验证后才替换安装状态，更新失败旧包仍可用。
- [ ] 个人 manifest 和个人描述不变，新增/复用/下载计数正确。

相关实现运行聚焦回归、Go test/vet/lint；并发和文件提交增加 race 与目标平台验证。素材任务运行清单/原图校验及文档链接检查。

## I16：素材包卸载与收藏保留

GitHub：[9Ashwin/sticker-cli#14](https://github.com/9Ashwin/sticker-cli/issues/14)

范围：US-011。阶段：P2。类型：backend。优先级：medium。

依赖：I08、I11。SPEC：§4.1、5.4。验证组：T11。

验收：

- [ ] packs remove 只解除包安装状态，支持 dry-run 和重复调用。
- [ ] 首版不自动回收原图，结果明确报告 retained_bytes。
- [ ] 精选→收藏→全量→卸载两包之后，收藏仍可读且描述不变。

相关实现运行聚焦回归、Go test/vet/lint；并发和文件提交增加 race 与目标平台验证。素材任务运行清单/原图校验及文档链接检查。

## I17：Go 二进制分发与平台验收

GitHub：[9Ashwin/sticker-cli#15](https://github.com/9Ashwin/sticker-cli/issues/15)

范围：US-001。阶段：P1。类型：infra。优先级：high。

依赖：I05、I08、I10、I12。SPEC：§7、8。验证组：T01。

验收：

- [ ] 发布 darwin/linux arm64/amd64、windows amd64 原生二进制与 SHA-256。
- [ ] 提供 go install 与各平台 PATH 文档，安装程序本身零素材下载。
- [ ] 固定代码许可证后发布，原图不打进 CLI Release。
- [ ] 测试单二进制无 Go/Node/Python/MCP 运行依赖；原生 smoke 与交叉编译分开记录。
- [ ] 发布版本固定的跨平台归档、`checksums.txt`、Unix shell 和 Windows PowerShell 安装入口；默认写入用户目录、无需 sudo，校验失败拒绝安装且不把原图打进归档。

相关实现运行聚焦回归、Go test/vet/lint；并发和文件提交增加 race 与目标平台验证。素材任务运行清单/原图校验及文档链接检查。

## I18：Agent Skill 与端到端使用验收

GitHub：[9Ashwin/sticker-cli#16](https://github.com/9Ashwin/sticker-cli/issues/16)

范围：US-013。阶段：P1。类型：infra。优先级：high。

依赖：I03、I05、I08、I10、I12、I17。SPEC：§2、4、8。验证组：T13。

验收：

- [ ] 发布简短 Agent Skill：选包、检索、预览、展示、收藏、分组及批量整理。
- [ ] Skill 同时说明 `setup --pack curated|all` convenience，并明确正式 `packs install` 和本地 `favorites import` 接口。
- [ ] 示例与 help/schema 一致，Skill 安装不覆盖用户已有指引。
- [ ] 至少一个真实 Agent 完成精选安装→离线检索→预览展示→收藏→分组排序→批量整理流程。
- [ ] 文件可用、静态预览和动画播放分开报告，不宣称外部聊天发送。

相关实现运行聚焦回归、Go test/vet/lint；并发和文件提交增加 race 与目标平台验证。素材任务运行清单/原图校验及文档链接检查。

## I19：收藏分类与自定义分组

GitHub：[9Ashwin/sticker-cli#18](https://github.com/9Ashwin/sticker-cli/issues/18)

范围：US-014。阶段：P1。类型：backend。优先级：high。

依赖：I02、I03、I11、I12。SPEC：§3.6、4.1、5.5。验证组：T16。

验收：

- [ ] 提供默认收藏分组，以及创建、重命名、列出和删除自定义分组；删除分组前明确处理其中条目。
- [ ] 收藏条目可加入或移出分组，同一原图只保存一份，不破坏标准 `manifest.json`。
- [ ] `favorites import` 没有分组扩展时把条目放入默认分组；有扩展时验证 ID、名称和引用。
- [ ] 分组元数据损坏返回稳定完整性错误，不能覆盖有效数据；写入使用原子提交。

Demo path：在临时 home 中导入两个 v1 条目，创建 `work` 分组并把一个条目加入，再列出分组确认成员和原图路径。

## I20：收藏排序与批量整理

GitHub：[9Ashwin/sticker-cli#19](https://github.com/9Ashwin/sticker-cli/issues/19)

范围：US-015。阶段：P1。类型：backend。优先级：high。

依赖：I13、I14、I19。SPEC：§3.6、4.1、5.4–5.5。验证组：T17。

验收：

- [ ] `favorites list` 支持分组筛选与 `manual`、`added`、`caption`、`md5` 四种稳定排序。
- [ ] 非交互命令支持按 ID 批量移动、重排和取消收藏，`--dry-run` 不写入。
- [ ] 无效 ID、重复顺序或并发版本变化整体失败，已有收藏和顺序保持不变。
- [ ] 导出包含可选分组/顺序扩展；旧客户端仍可读取 v1 清单和原图。

Demo path：在两个分组各放入两个 fixture，分别执行手动重排、caption 排序和批量移动，断言 JSON 顺序与 dry-run 前后一致。

## I21：收藏整理端到端自动化验证

GitHub：[9Ashwin/sticker-cli#20](https://github.com/9Ashwin/sticker-cli/issues/20)

范围：US-016。阶段：P1。类型：infra。优先级：high。

依赖：I01、I02、I03、I04、I05、I06、I07、I08、I09、I10、I11、I12、I13、I14、I15、I16、I17、I18、I19、I20。SPEC：§4、5、8。验证组：T18。

验收：

- [ ] CI 中的自动化 E2E 使用临时 home 和本地 fixture，完成列出精选、安装、离线搜索、读取、v1 清单导入、创建分组、排序和批量整理，并断言 JSON、哈希、分组与最终清单。
- [ ] fixture 图片哈希错误、分组元数据损坏和无效 ID 均返回稳定错误、非零退出且不发布不完整状态。
- [ ] 测试不访问微信、MCP、账号凭据或真实公网素材源，运行后清理临时状态并可独立重复。
- [ ] CI 任务在无 CGO 环境通过，并把原图读取、静态 WebP 预览（如 fixture 包含 WebP）与动图原始字节验证分开报告。
- [ ] E2E 覆盖 `setup --pack curated`、显式本地 `sticker-ext` 源和精选不请求全量独有图片，并覆盖发布归档校验失败。

Demo path：在 CI 的单个测试命令中从空 home 运行完整流程，随后再次运行确认结果和顺序稳定。

## 创建结果

I04/I05 已建在素材仓库，其余已建在 CLI 仓库；各 Issue 正文中的依赖均已改写为真实跨仓库链接。I19…I22 已按确认的整理与 setup 拆分创建。GitHub CLI 版本不支持原生 blocked-by 时，正文保留真实 Issue 链接作为依赖证据。本清单没有新增 SQLite 或 JSONL 任务：首版使用现有 JSON 素材格式，后续索引需求按 SPEC §3.5 再评估。

## I22：一键初始化精选或全量素材

GitHub：[9Ashwin/sticker-cli#21](https://github.com/9Ashwin/sticker-cli/issues/21)

范围：US-004、FR-24。阶段：P1。类型：backend。优先级：high。

依赖：I03、I06、I08。SPEC：§4.1、4.5、5.1。验证组：T04、T18。

验收：

- [ ] 在 I08 合并后的主干上，运行 `sticker setup` 尚不存在；提交后默认调用精选安装并返回 `setup:true`，显式 `--pack all` 才安装全量。
- [ ] `--source`、`--dry-run` 与正式 `packs install` 使用相同的修订、校验、锁和原子提交语义，不复制另一套下载逻辑。
- [ ] 本地 `sticker-ext` 源的精选 setup 不读取全量独有文件；全量必须由 `--pack all` 显式选择。
- [ ] 无效包 ID、损坏清单、哈希错误和取消均返回稳定错误，不发布不完整安装状态；dry-run 不创建库目录、锁或缓存。

Demo path：对本地 `sticker-ext` fixture 运行 `setup --pack curated --dry-run` 和实际 setup，再显式运行 `setup --pack all`，断言请求白名单、输出和安装状态。
