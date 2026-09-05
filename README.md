# emoticon-cli

面向人和 AI Agent 的独立 Go 表情包 CLI，**当前处于规划阶段，尚未发布可安装版本**。

目标是不依赖微信账号或 MCP：用户选择精选／全量素材，Agent 离线检索并展示本地原图，用户持续添加、导入和导出私人收藏。

- **实现方向：** Go + Cobra，参考 [larksuite/cli](https://github.com/larksuite/cli) 的 JSON 输出、稳定错误、schema、dry-run 和 Agent Skill。
- **素材与程序拆分：** 本仓库承载 CLI；现有 [wechat-emoticon-pack](https://github.com/9Ashwin/wechat-emoticon-pack) 承载素材，拟改名 `agent-emoticon-packs`。
- **可选素材：** 当前全量 2,638 张，约 1 GiB；精选候选 124 张，约 21.8 MiB，发布前需复核。
- **收藏导入：** 本地原图和清单按现有 `manifest.json` + `emoticons/` 格式合并到个人素材库，不另设收藏格式；公共包更新不会覆盖个人素材。

请阅读 [PRD 草案](tasks/prd-emoticon-cli.md)。需求确认后先完成技术设计和任务拆解，再开始实现。PRD 中的命令均为设计示例，尚不可执行。
