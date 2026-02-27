# AI Software Development Team

A reusable AI team that works with Claude Code to analyze, implement, and review code.

## Quick Start

1. Copy `.ai-team/` to your project root
2. Add `.ai-team/` to your `.gitignore`
3. Fill out `context/project.md` with your project details
4. Start Claude Code and say `/team` or "start the AI team"

## How It Works

```
You (Human)
    ↓
Manager (single point of contact)
    ↓
[Architect, Developer, Analyst, Security, QA]
    ↓
Manager synthesizes results
    ↓
You approve → Team executes
```

**Key principle**: You only talk to Manager. Manager orchestrates the team.

## Commands

| Command | Description |
|---------|-------------|
| `/team` | Activate the AI team |
| `/switch <role>` | Talk directly to an agent (architect, developer, analyst, security, qa, seo, content) |
| `/status` | Check current task status |
| `/manager` | Return to Manager |

## Structure

```
.ai-team/
├── TEAM.md                  # Manager prompt (entry point)
├── README.md                # This file
├── agents/
│   ├── architect.md         # System design, tech decisions
│   ├── developer.md         # Implementation, bug fixes
│   ├── analyst.md           # Feature/bug analysis
│   ├── security.md          # Security review
│   ├── qa.md                # Testing strategy
│   ├── seo-specialist.md    # Technical SEO, GSC analysis
│   └── content-strategist.md # Blog writing, GEO optimization
├── config/
│   └── gsc-setup.md         # Google Search Console setup guide
├── context/
│   └── project.md           # Your project details (fill this)
├── mcp/
│   ├── README.md            # MCP reference documentation
│   └── settings.json        # MCP server configurations (template)
└── workflows/
    ├── feature.md           # New feature workflow
    ├── bugfix.md            # Bug fix workflow
    └── review.md            # Code review workflow
```

## Core Rules

1. **Propose, then execute** - Team never makes changes without your approval
2. **Security first** - Security issues are always flagged, never auto-fixed
3. **Stay focused** - Team only does what you ask, proposes extras separately

## Example Usage

**You**: "Add rate limiting to the API"

**Manager**: Spawns Analyst + Architect in parallel to assess requirements and design approach.

**Manager**: Presents proposal with options (Redis vs in-memory, etc.)

**You**: "Go with Redis"

**Manager**: Spawns Developer + Security to implement with security review.

**Manager**: Presents final changes for approval.

**You**: "Approved"

**Manager**: Changes applied.

## Customization

- Edit agent prompts in `agents/` to adjust behavior
- Modify workflows in `workflows/` for your process
- Add project-specific context in `context/project.md`
- Add more agents by creating new files in `agents/`

## MCP Servers (Optional)

MCPs extend the team's capabilities. See `mcp/README.md` for details.

**Quick setup:**
1. Copy needed servers from `mcp/settings.json`
2. Paste into `~/.claude/settings.json` (global) or `.claude/settings.json` (project)
3. Add your API keys, remove `"disabled": true`
4. Restart Claude Code

**Recommended MCPs:**
- `memory` - Team remembers context across sessions
- `github` - PR/issue management
- `tavily` - Web search for docs/research

---

## Multi-Project Support

For monorepos or multiple related projects, add context for each:

```
context/
├── project.md          # Main/shared context
├── backend.md          # Backend-specific
├── frontend.md         # Frontend-specific
└── common.md           # Shared libraries
```

The team will coordinate changes across all projects.
