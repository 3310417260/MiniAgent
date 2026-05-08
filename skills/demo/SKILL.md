---
name: demo
description: Use this skill to demonstrate MiniAgent skill routing, full SKILL.md loading, and script discovery without changing files.
---

# Demo Skill

This is a deliberately small and safe skill for learning the complete skill package shape.

Use this skill when the user asks to test, inspect, or demonstrate MiniAgent skills.

## What This Skill Demonstrates

- A `SKILL.md` file with metadata and instructions.
- A `scripts/` directory packaged with the skill.
- Safe direct and Python scripts that print arguments and do not write files.
- The difference between discovering a script and executing a script.

## Available Script

- `scripts/echo_args.sh`: prints a short demo message and echoes the arguments it receives.
- `scripts/inspect_args.py`: prints JSON describing a string label and integer count.

## Safety Notes

MiniAgent can list this script with `/skill-scripts demo`.

MiniAgent can execute this script only through the dedicated `run_skill_script` tool. That tool requires approval and applies an allowlist, path checks, timeout, argument limits, and output limits.

## Expected Dry-Run Behavior

When loaded through `/skill-load`, explain that this demo skill is useful for observing the skill lifecycle:

```text
catalog -> route -> load selected SKILL.md -> discover scripts -> future approved execution
```
