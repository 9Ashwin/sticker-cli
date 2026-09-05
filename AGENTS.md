# sticker-cli

当前按 GitHub Issues 顺序实施。先阅读 tasks/prd-sticker-cli.md 与 tasks/spec-sticker-cli.md，以具体 Issue 的验收条件界定改动。

实现使用 Go + Cobra。代码、注释和面向用户的文档只描述本项目，不提及设计参考来源。每个 Issue 使用独立分支，验证和 review 后通过 PR 合并。

素材属于独立仓库，禁止在本仓库提交原图全集、私人收藏、密钥和开发者本机数据。实现时保持默认 JSON、稳定错误、离线读取、选择性安装与私人收藏保留契约。

表情 caption 用于描述画面和可使用的场景，搜索应允许宽泛关键词返回多种候选；不要把单一情绪分类或唯一语义命中当成 CLI 合同。精选素材按场景多样性和描述与画面相符维护。

执行 make quality，并对并发和文件操作执行 go test -race ./...。不要将尚未实现或验证的能力写成已交付功能。
