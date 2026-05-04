# MiniAgent

MiniAgent is a small Go learning project for building a minimal command-line agent harness.

It demonstrates the core pieces of a coding agent:

- OpenAI-compatible chat completion
- tool calling
- an agent loop
- local read-only tools
- JSONL session persistence
- multiple sessions
- `AGENTS.md` project instructions
- recent-message context trimming
- debug output for raw API request/response JSON

The code intentionally stays compact and standard-library first so each layer is easy to inspect while learning.

## Quick Start

Set an API key for an OpenAI-compatible provider:

```bash
export ZAI_API_KEY="your-api-key"
```

Optional provider settings:

```bash
export ZAI_BASE_URL="https://open.bigmodel.cn/api/paas/v4"
export ZAI_MODEL="glm-4.6v"
```

Run one prompt:

```bash
go run ./cmd/miniagent "hello"
```

Run interactive mode:

```bash
go run ./cmd/miniagent
```

## Interactive Commands

Inside interactive mode:

```text
/help
/history
/debug-api
/clear
/session
/session list
/session use <id>
/session new <id>
/exit
```

`/history` prints the in-memory `[]llm.Message` history for the current session.

`/debug-api` toggles raw and pretty JSON printing for API requests and responses. This is useful for seeing `tools`, `tool_calls`, and trimmed message payloads.

`/clear` clears the current session history in memory and in `sessions/<id>.jsonl`.

`/session use <id>` switches to a saved session, creating an empty one if no file exists yet.

## Environment Variables

Model/provider:

```text
ZAI_API_KEY
ZAI_BASE_URL
ZAI_MODEL
```

Fallback API variables are also supported:

```text
ZHIPUAI_API_KEY / ZHIPUAI_BASE_URL / ZHIPUAI_MODEL
GLM_API_KEY / GLM_BASE_URL / GLM_MODEL
OPENAI_API_KEY / OPENAI_BASE_URL / OPENAI_MODEL
MINIAGENT_API_KEY / MINIAGENT_BASE_URL / MINIAGENT_MODEL
```

Runtime:

```text
MINIAGENT_SESSION=study
MINIAGENT_DEBUG_API=1
MINIAGENT_CONTEXT_MESSAGES=40
```

`MINIAGENT_CONTEXT_MESSAGES` controls how many non-system messages are sent to the model. The full session is still kept in memory and persisted to disk.

## Tools

MiniAgent currently exposes four local tools to the model:

```text
get_time
list_files
read_file
grep_text
```

The model sees only each tool's schema and description. The Go program decides whether the requested tool exists, executes it locally, then feeds the result back as a `role=tool` message.

Examples:

```text
现在几点？
递归列出当前项目文件，最多 20 个
读取 README.md
搜索 generaterequest，不区分大小写，最多 10 个
```

The file tools are read-only and constrained to the workspace.

## Project Structure

```text
cmd/miniagent/          CLI entry point and current orchestration glue
internal/llm/           provider-neutral message types and OpenAI-compatible client
internal/agent/         tool dispatcher and agent struct skeleton
internal/tools/         local tool interface and tool implementations
internal/session/       JSONL session store
internal/contextx/      context trimming
internal/project/       AGENTS.md discovery
internal/prompt/        system prompt construction
docs/                   learning notes
sessions/               JSONL session files
AGENTS.md               project-level instructions loaded into the system prompt
```

## Runtime Flow

```mermaid
flowchart TD
    A["program start"] --> B["load AGENTS.md"]
    B --> C["build system prompt"]
    C --> D["load session JSONL"]
    D --> E["full in-memory messages"]
    E --> F["user input"]
    F --> G["append user message"]
    G --> H["context trimming"]
    H --> I["send modelMessages + tools"]
    I --> J{"tool_calls?"}
    J -- "yes" --> K["execute local tools"]
    K --> L["append tool result messages"]
    L --> H
    J -- "no" --> M["append final assistant message"]
    M --> N["append new messages to JSONL"]
```

The important separation is:

```text
full messages   = complete session history for /history and JSONL persistence
modelMessages   = trimmed messages sent to the model for one request
```

## Learning Notes

Current notes:

- `docs/day03-context-and-session.md`
- `docs/day10-mini-harness.md`

The original learning path:

```text
Day 1  minimal chat CLI
Day 2  streaming output
Day 3  message/session memory model
Day 4  first tool
Day 5  agent loop
Day 6  useful read-only tools
Day 7  JSONL session persistence
Day 8  AGENTS.md project instructions
Day 9  context trimming
Day 10 mini harness cleanup
```

## Tests

Run:

```bash
go test ./...
```

The tests cover:

- agent loop tool-result feedback
- context trimming behavior
- `AGENTS.md` discovery
- system prompt composition
- JSONL session store
- file tools

