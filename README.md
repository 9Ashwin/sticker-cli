# emoticon-cli

面向人和 AI Agent 的独立 Go 表情包 CLI，**当前处于规划阶段，尚未发布可安装版本**。

目标是不依赖微信账号或 MCP：用户选择精选／全量素材，Agent 离线检索并展示本地原图，用户持续添加、导入和导出私人收藏。

- **实现方向：** Go + Cobra，参考 [larksuite/cli](https://github.com/larksuite/cli) 的 JSON 输出、稳定错误、schema、dry-run 和 Agent Skill。
- **素材与程序拆分：** 本仓库承载 CLI；现有 [wechat-emoticon-pack](https://github.com/9Ashwin/wechat-emoticon-pack) 承载素材，拟改名 `agent-emoticon-packs`。
- **可选素材：** 当前全量 2,638 张，约 1 GiB；精选候选 124 张，约 21.8 MiB，发布前需复核。
- **收藏导入：** 本地原图和清单按现有 `manifest.json` + `emoticons/` 格式合并到个人素材库，不另设收藏格式；公共包更新不会覆盖个人素材。

[PRD](tasks/prd-emoticon-cli.md) 已确认，[技术 SPEC](tasks/spec-emoticon-cli.md) 和 [18 项 GitHub Issues](tasks/issues-emoticon-cli.md) 已就绪。首版使用现有 JSON 清单，SQLite 可重建索引作为有明确需求后的选项，JSONL 不作为素材库格式。

目前已创建实施 Issues，尚未开始实现或发布可用版本。文档中的命令均为设计示例，尚不可执行。
