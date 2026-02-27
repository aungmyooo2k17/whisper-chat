# MCP Server Configuration

MCP (Model Context Protocol) servers extend Claude Code with additional capabilities.

## Setup

1. Copy servers you need from `settings.json` to your Claude config:
   - **Global**: `~/.claude/settings.json` (all projects)
   - **Per-project**: `.claude/settings.json` (this project only)

2. Remove `"disabled": true` to enable a server

3. Replace placeholder values (API keys, paths, etc.)

4. Restart Claude Code

## Available Servers

### Web Search & Documentation

**Tavily** - Web search and research
```json
"tavily": {
  "command": "npx",
  "args": ["-y", "tavily-mcp@latest"],
  "env": { "TAVILY_API_KEY": "your-key" }
}
```
Get API key: https://tavily.com

---

**Context7** - Documentation lookup for libraries/frameworks
```json
"context7": {
  "command": "docker",
  "args": ["run", "-i", "--rm", "context7-mcp"]
}
```
Better than web search for API docs - returns structured library documentation.

---

### Memory

**Memory (Simple)** - In-memory storage
```json
"memory": {
  "command": "npx",
  "args": ["-y", "@modelcontextprotocol/server-memory"]
}
```
Resets when Claude restarts. Good for single-session context.

---

**Memory Bank (Persistent)** - File-based persistent memory
```json
"allpepper-memory-bank": {
  "command": "docker",
  "args": [
    "run", "-i", "--rm",
    "-e", "MEMORY_BANK_ROOT",
    "-v", "/your/path/memory-bank:/mnt/memory_bank",
    "memory-bank-mcp:local"
  ],
  "env": { "MEMORY_BANK_ROOT": "/mnt/memory_bank" }
}
```
Survives restarts. Change volume path to your preferred location.

---

### Databases

**PostgreSQL (npx)**
```json
"postgres": {
  "command": "npx",
  "args": ["-y", "@modelcontextprotocol/server-postgres"],
  "env": { "DATABASE_URL": "postgres://user:pass@localhost:5432/db" }
}
```

**PostgreSQL (Docker)**
```json
"postgres": {
  "command": "docker",
  "args": ["run", "-i", "--rm", "mcp/postgres",
           "postgresql://user:pass@host.docker.internal:5432/db"]
}
```
Use `host.docker.internal` to reach localhost from Docker.

---

**SQLite**
```json
"sqlite": {
  "command": "npx",
  "args": ["-y", "@modelcontextprotocol/server-sqlite", "--db-path", "./db.sqlite"]
}
```

---

### Browser & Automation

**Playwright** - Browser control and E2E testing
```json
"playwright": {
  "command": "npx",
  "args": ["-y", "@playwright/mcp@latest"]
}
```

---

**N8N** - Workflow automation
```json
"n8n-mcp": {
  "command": "docker",
  "args": [
    "run", "-i", "--rm", "--init",
    "-e", "MCP_MODE=stdio",
    "-e", "LOG_LEVEL=error",
    "-e", "DISABLE_CONSOLE_OUTPUT=true",
    "-e", "N8N_API_URL=http://host.docker.internal:5678",
    "-e", "N8N_API_KEY=your-n8n-api-key",
    "ghcr.io/czlonkowski/n8n-mcp:latest"
  ]
}
```
Trigger and manage N8N workflows. Great for automation pipelines.

---

### GitHub

**GitHub** - Repository, PR, and issue management
```json
"github": {
  "command": "npx",
  "args": ["-y", "@modelcontextprotocol/server-github"],
  "env": { "GITHUB_TOKEN": "your-token" }
}
```
Create token: https://github.com/settings/tokens

---

### Filesystem

**Filesystem (npx)**
```json
"filesystem": {
  "command": "npx",
  "args": ["-y", "@modelcontextprotocol/server-filesystem", "./"]
}
```

**Filesystem (Docker)**
```json
"filesystem": {
  "command": "docker",
  "args": [
    "run", "-i", "--rm",
    "--mount", "type=bind,src=/your/project,dst=/projects",
    "mcp/filesystem", "/projects"
  ]
}
```

---

### Reasoning

**Sequential Thinking** - Enhanced multi-step reasoning

npx:
```json
"sequential-thinking": {
  "command": "npx",
  "args": ["-y", "@modelcontextprotocol/server-sequential-thinking"]
}
```

Docker:
```json
"sequentialthinking": {
  "command": "docker",
  "args": ["run", "--rm", "-i", "mcp/sequentialthinking"]
}
```

---

## Docker vs npx

| Aspect | npx | Docker |
|--------|-----|--------|
| Setup | Simpler | Requires images |
| Isolation | Runs in your env | Fully isolated |
| Performance | Faster startup | Slight overhead |
| Networking | Use localhost | Use host.docker.internal |

Use Docker when you want isolation or already have images. Use npx for quick setup.

---

## Recommended for AI Team

| Agent | Recommended MCPs |
|-------|------------------|
| **Manager** | memory-bank, github |
| **Analyst** | tavily, context7, github |
| **Architect** | sequential-thinking, context7 |
| **Developer** | github, filesystem, postgres |
| **Security** | tavily (CVE lookup) |
| **QA** | playwright |

## Quick Start Combo

For most projects, enable these three:

```json
{
  "mcpServers": {
    "context7": {
      "command": "docker",
      "args": ["run", "-i", "--rm", "context7-mcp"]
    },
    "allpepper-memory-bank": {
      "command": "docker",
      "args": ["run", "-i", "--rm", "-e", "MEMORY_BANK_ROOT", "-v",
               "/your/path:/mnt/memory_bank", "memory-bank-mcp:local"],
      "env": { "MEMORY_BANK_ROOT": "/mnt/memory_bank" }
    },
    "github": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-github"],
      "env": { "GITHUB_TOKEN": "your-token" }
    }
  }
}
```

---

## Troubleshooting

**Server not loading?**
- Check `claude mcp` in terminal to see status
- Ensure npx/node/docker is in PATH
- Check API keys are valid

**Docker connection issues?**
- Use `host.docker.internal` instead of `localhost`
- Ensure Docker is running

**Permission errors?**
- Some servers need explicit path permissions
- Check volume mounts are correct
