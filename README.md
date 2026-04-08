# Weclaw-Oorz

[English](README_EN.md)

> Weclaw-Oorz（修改版）
>
> 本项目基于 https://github.com/fastclaw-ai/weclaw 修改，仅供个人学习，不得用于商业用途。

<details>
<summary>更新内容</summary>

### 2026-04-08

- 项目名称调整为 `Weclaw-Oorz`
- 新增 Hermes 全局接入支持
- 新增 Hermes 命令透传支持
- 新增 `CLI Proxy API` 等 Hermes 自定义模型配置说明
- README 补充安装方式和 Hermes 集成说明

</details>

微信 AI Agent 桥接器，用来把微信消息接到 Claude、Codex、Hermes、Gemini、Kimi 等 Agent。

## 快速开始

```bash
# 一键安装
curl -sSL https://raw.githubusercontent.com/Aaowu/weclaw/main/install.sh | sh

# 启动
weclaw start
```

首次启动会自动显示二维码，你扫一下就能登录微信。

## 其他安装方式

```bash
# 通过 Go 安装
go install github.com/Aaowu/weclaw@latest

# 从源码构建
git clone https://github.com/Aaowu/weclaw.git
cd weclaw
go build -o weclaw .
```

## Hermes 集成

这个 fork 的目标很简单。

- Hermes 保持原版安装和原版更新
- WeClaw 只负责接微信和切换 Agent
- 切到 Hermes 后，`/help`、`/skills`、`/new` 这类命令按 Hermes 方式处理
- 切回 Claude、Codex 之后，还是 WeClaw 自己原来的命令逻辑

推荐这样配置 Hermes：

```json
{
  "agents": {
    "hermes": {
      "type": "acp",
      "command": "hermes",
      "args": ["acp"],
      "aliases": ["hm"]
    }
  }
}
```

Hermes 的模型配置不在 WeClaw 里改，而是在 Hermes 自己的 `~/.hermes/config.yaml` 里改。比如接 `CLI Proxy API`：

```yaml
model:
  default: gpt-5.4
  provider: custom
  base_url: http://127.0.0.1:8317/v1
  api_key: your-key
```

## 常用命令

```text
/claude    切到 Claude
/codex     切到 Codex
/hermes    切到 Hermes
/hm        Hermes 别名
/new       新会话
/help      帮助
/skills    Hermes Skills Hub
/cwd 路径   切换工作目录
```

## 详细文档

完整中文文档看这里：

[README_CN.md](README_CN.md)
