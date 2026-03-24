# WeClaw

[中文文档](README_CN.md)

WeChat AI Agent Bridge — connect WeChat to AI agents (Claude, Codex, Gemini, Kimi, etc.).

> This project is inspired by [@tencent-weixin/openclaw-weixin](https://npmx.dev/package/@tencent-weixin/openclaw-weixin). For personal learning only, not for commercial use.

| | | |
|:---:|:---:|:---:|
| <img src="previews/preview1.png" width="280" /> | <img src="previews/preview2.png" width="280" /> | <img src="previews/preview3.png" width="280" /> |

## Quick Start

```bash
# One-line install
curl -sSL https://raw.githubusercontent.com/fastclaw-ai/weclaw/main/install.sh | sh

# Start (first run will prompt QR code login)
weclaw start
```

That's it. On first start, WeClaw will:
1. Show a QR code — scan with WeChat to login
2. Auto-detect installed AI agents (Claude, Codex, Gemini, etc.)
3. Save config to `~/.weclaw/config.json`
4. Start receiving and replying to WeChat messages

Use `weclaw login` to add additional WeChat accounts.

### Other install methods

```bash
# Via Go
go install github.com/fastclaw-ai/weclaw@latest

# Via Docker
docker run -it -v ~/.weclaw:/root/.weclaw ghcr.io/fastclaw-ai/weclaw start
```

## How It Works

<p align="center">
  <img src="previews/architecture.png" width="600" />
</p>

**Agent modes:**

| Mode | How it works | Examples |
|------|-------------|----------|
| ACP  | Long-running subprocess, JSON-RPC over stdio. Fastest — reuses process and sessions. | Claude, Codex, Kimi, Gemini, Cursor, OpenCode, OpenClaw |
| CLI  | Spawns a new process per message. Supports session resume via `--resume`. | Claude (`claude -p`), Codex (`codex exec`) |
| HTTP | OpenAI-compatible chat completions API. | OpenClaw (HTTP fallback) |

Auto-detection picks ACP over CLI when both are available.

## Chat Commands

Send these as WeChat messages:

| Command | Description |
|---------|-------------|
| `hello` | Send to default agent |
| `/codex write a function` | Send to a specific agent |
| `/cc explain this code` | Send to agent by alias |
| `/claude` | Switch default agent to Claude |
| `/status` | Show current agent info |
| `/help` | Show help message |

### Aliases

| Alias | Agent |
|-------|-------|
| `/cc` | claude |
| `/cx` | codex |
| `/cs` | cursor |
| `/km` | kimi |
| `/gm` | gemini |
| `/ocd` | opencode |
| `/oc` | openclaw |

Switching default agent is persisted to config — survives restarts.

## Media Messages

WeClaw supports sending images, videos, and files to WeChat.

**From agent replies:** When an AI agent returns markdown with images (`![](url)`), WeClaw automatically extracts the image URLs, downloads them, uploads to WeChat CDN (AES-128-ECB encrypted), and sends them as image messages.

**Markdown handling:** Agent responses are automatically converted from markdown to plain text for WeChat display — code fences are stripped, links show display text only, bold/italic markers are removed, etc.

## Proactive Messaging

Send messages to WeChat users without waiting for them to message first.

**CLI:**

```bash
# Send text
weclaw send --to "user_id@im.wechat" --text "Hello from weclaw"

# Send image
weclaw send --to "user_id@im.wechat" --media "https://example.com/photo.png"

# Send text + image
weclaw send --to "user_id@im.wechat" --text "Check this out" --media "https://example.com/photo.png"

# Send file
weclaw send --to "user_id@im.wechat" --media "https://example.com/report.pdf"
```

**HTTP API** (runs on `127.0.0.1:18011` when `weclaw start` is running):

```bash
# Send text
curl -X POST http://127.0.0.1:18011/api/send \
  -H "Content-Type: application/json" \
  -d '{"to": "user_id@im.wechat", "text": "Hello from weclaw"}'

# Send image
curl -X POST http://127.0.0.1:18011/api/send \
  -H "Content-Type: application/json" \
  -d '{"to": "user_id@im.wechat", "media_url": "https://example.com/photo.png"}'

# Send text + media
curl -X POST http://127.0.0.1:18011/api/send \
  -H "Content-Type: application/json" \
  -d '{"to": "user_id@im.wechat", "text": "See this", "media_url": "https://example.com/photo.png"}'
```

Supported media types: images (png, jpg, gif, webp), videos (mp4, mov), files (pdf, doc, zip, etc.).

Set `WECLAW_API_ADDR` to change the listen address (e.g. `0.0.0.0:18011`).

## Configuration

Config file: `~/.weclaw/config.json`

```json
{
  "default_agent": "claude",
  "agents": {
    "claude": {
      "type": "acp",
      "command": "/usr/local/bin/claude-agent-acp",
      "env": {
        "ANTHROPIC_API_KEY": "sk-ant-xxx"
      },
      "model": "sonnet"
    },
    "codex": {
      "type": "acp",
      "command": "/usr/local/bin/codex-acp",
      "env": {
        "OPENAI_API_KEY": "sk-xxx"
      }
    },
    "openclaw": {
      "type": "http",
      "endpoint": "https://api.example.com/v1/chat/completions",
      "api_key": "sk-xxx",
      "model": "openclaw:main"
    }
  }
}
```

Environment variables:
- `WECLAW_DEFAULT_AGENT` — override default agent
- `OPENCLAW_GATEWAY_URL` — OpenClaw HTTP fallback endpoint
- `OPENCLAW_GATEWAY_TOKEN` — OpenClaw API token

Custom agent CLI environment variables:

```json
{
  "default_agent": "...",
  "agents": {
    "...": {
      ...
      "env": {
        "ENV_NAME": "ENV_VALUE"
      }
    },
  }
}
```

### Permission bypass

By default, some agents require interactive permission approval which doesn't work in WeChat. Add `args` to your agent config to bypass:

| Agent | Flag | What it does |
|-------|------|-------------|
| Claude (CLI) | `--dangerously-skip-permissions` | Skip all tool permission prompts |
| Codex (CLI) | `--skip-git-repo-check` | Allow running outside git repos |

Example:

```json
{
  "claude": {
    "type": "cli",
    "command": "/usr/local/bin/claude",
    "cwd": "/home/user/my-project",
    "args": ["--dangerously-skip-permissions"]
  },
  "codex": {
    "type": "cli",
    "command": "/usr/local/bin/codex",
    "cwd": "/home/user/my-project",
    "args": ["--skip-git-repo-check"]
  }
}
```

Set `cwd` to specify the agent's working directory (workspace). If omitted, defaults to `~/.weclaw/workspace`.

> **Warning:** These flags disable safety checks. Only enable them if you understand the risks. ACP agents handle permissions automatically and don't need these flags.

## Background Mode

```bash
# Start (runs in background by default)
weclaw start

# Check if running
weclaw status

# Stop
weclaw stop

# Run in foreground (for debugging)
weclaw start -f
```

Logs are written to `~/.weclaw/weclaw.log`.

### System service (auto-start on boot)

**macOS (launchd):**

```bash
cp service/com.fastclaw.weclaw.plist ~/Library/LaunchAgents/
launchctl load ~/Library/LaunchAgents/com.fastclaw.weclaw.plist
```

**Linux (systemd):**

```bash
sudo cp service/weclaw.service /etc/systemd/system/
sudo systemctl enable --now weclaw
```

## Docker

```bash
# Build
docker build -t weclaw .

# Login (interactive — scan QR code)
docker run -it -v ~/.weclaw:/root/.weclaw weclaw login

# Start with HTTP agent
docker run -d --name weclaw \
  -v ~/.weclaw:/root/.weclaw \
  -e OPENCLAW_GATEWAY_URL=https://api.example.com \
  -e OPENCLAW_GATEWAY_TOKEN=sk-xxx \
  weclaw

# View logs
docker logs -f weclaw
```

> Note: ACP and CLI agents require the agent binary inside the container.
> The Docker image ships only WeClaw itself. For ACP/CLI agents, mount
> the binary or build a custom image. HTTP agents work out of the box.

## Release

```bash
# Tag a new version to trigger GitHub Actions build & release
git tag v0.1.0
git push origin v0.1.0
```

The workflow builds binaries for `darwin/linux` x `amd64/arm64`, creates a GitHub Release, and uploads all artifacts with checksums.

## Development

```bash
# Hot reload
make dev

# Build
go build -o weclaw .

# Run
./weclaw start
```

## Contributors

<a href="https://github.com/fastclaw-ai/weclaw/graphs/contributors">
  <img src="https://contrib.rocks/image?repo=fastclaw-ai/weclaw" />
</a>

## Star History

[![Star History Chart](https://api.star-history.com/svg?repos=fastclaw-ai/weclaw&type=Timeline)](https://star-history.com/#fastclaw-ai/weclaw&Timeline)

## License

[MIT](LICENSE)
