# Agent Hub Design — 跨 Agent 上下文共享

## Problem

WeClaw supports multiple AI agents (Claude, Codex, Gemini, etc.), each with isolated conversation sessions. Users cannot easily share context between agents for collaborative workflows like:
- Multi-agent debate
- Chain-of-thought analysis (Agent A outputs → Agent B reviews)
- Collaborative content creation

## Solution: Agent Hub

A shared context layer built directly into WeClaw's Go codebase, using the filesystem as a persistent message board.

### Architecture

```
WeChat ←→ WeClaw (handler.go)
              │
              ├── Agent A (isolated session)
              ├── Agent B (isolated session)  
              └── Agent Hub (shared context)
                    │
                    ├── ~/.weclaw/hub/shared/    # shared context files
                    ├── ~/.weclaw/hub/templates/  # prompt templates
                    └── /hub, /save commands
```

### New Commands

| Command | Description | Example |
|---------|-------------|---------|
| `/hub` | Read all shared files and inject as context | `/hub 基于以上分析，给出你的反驳` |
| `/hub {filename}` | Read specific file from shared | `/hub round1_claude.md 基于此反驳` |
| `/save {filename} {message}` | Send message and save reply to shared | `/save round1.md 分析AI未来` |
| `/hub ls` | List files in shared directory | `/hub ls` |
| `/hub clear` | Clear all shared files | `/hub clear` |
| `/hub pipe {target}` | Send to agent, save result, auto-chain | `/hub pipe gemini` |

### Workflow Examples

#### Multi-Agent Debate
```
1. /save round1_claude.md 从哲学角度分析AI代理是否会替代人类决策
   → Claude replies, result saved to shared/round1_claude.md
   
2. @gemini /hub round1_claude.md 从技术可行性角度反驳以上观点
   → Gemini reads round1_claude.md, replies with rebuttal
   
3. /save round2_gemini.md @gemini 从技术可行性角度反驳
   → Gemini's rebuttal saved to shared/round2_gemini.md
   
4. @claude /hub round2_gemini.md 作为哲学派，回应技术派的反驳
   → Claude reads the rebuttal, responds
   
5. /hub 综合两方观点，给出最终结论
   → Default agent sees all shared files, synthesizes
```

#### Chain Collaboration
```
1. /save draft.md 写一个关于量子计算的技术博客大纲
2. @gemini /hub draft.md 基于大纲扩写完整文章
3. /save article.md @gemini 基于大纲扩写完整文章
4. @claude /hub article.md 审查文章质量并优化
```

### Implementation Plan

#### Phase 1: File-based shared context (current)
- `~/.weclaw/hub/shared/` — shared context files
- `~/.weclaw/hub/templates/` — prompt templates
- New commands in `handler.go`: `/hub`, `/save`

#### Phase 2: Auto-save with context injection
- Agent replies auto-saved when `/save` is used
- `/hub` auto-injects shared files as system prompt prefix
- File naming with timestamp and agent name

#### Phase 3: Chain mode (future)
- `/hub pipe {agent}` — automatic chain: send → save → next
- Template system for structured workflows
- History tracking per collaboration session

### File Format

Shared files use Markdown with YAML frontmatter:

```markdown
---
agent: claude
timestamp: 2026-04-02T01:30:00+08:00
session: debate-001
round: 1
---

# Claude's Analysis

[content here]
```

### Integration Points in weclaw

1. **`messaging/handler.go`** — Add command parsing for `/hub` and `/save`
2. **`hub/hub.go`** (new package) — Hub logic: read/write shared files, inject context
3. **`cmd/start.go`** — Initialize Hub with default directory

### Key Design Decisions

1. **Filesystem over database** — Simple, inspectable, no extra dependencies
2. **Markdown with frontmatter** — Human-readable, agent-friendly, extensible
3. **Opt-in via commands** — No automatic cross-contamination of agent sessions
4. **Go-native** — No Python dependencies, fits weclaw's architecture
