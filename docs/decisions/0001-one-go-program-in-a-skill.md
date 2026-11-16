# 1. One Go program in a skill

Date: 2026-11-16

## Decision
unscraped is a Claude Code skill that builds one Go `package main` from `plugin/scripts/`, with a vendored copy of the YAML parser and no other dependencies. It isn't an MCP server.

## Why
The agent needs to run a check over files it can already read, and a skill with a script does that. An MCP server would add a binary to install and a process to run for no new capability. A single package with no module downloads builds offline with `go build` or `go run`, and the same binary works in CI without Claude.
