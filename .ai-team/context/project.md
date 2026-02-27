# Project Context — Whisper

## Overview

**Name**: Whisper
**Description**: Anonymous real-time chat application with interest-based matching, ephemeral messaging, and abuse prevention. No user accounts — fully anonymous.
**Scale Target**: 1,000,000 concurrent WebSocket connections
**Architecture Doc**: `ARCHITECTURE.md`
**Kanban Board**: `.ai-team/context/kanban.md`

## Tech Stack

```
Frontend:       SvelteKit 2 (SPA mode, Svelte 5 runes)
Backend:        Go 1.23+
WebSocket:      gobwas/ws + epoll
Message Broker: NATS
Cache/State:    Redis 7+ (primary data store for ephemeral data)
Database:       PostgreSQL 16 (abuse reports only)
Load Balancer:  HAProxy 3.0
CDN:            Cloudflare (free tier)
Containers:     Docker Compose (local dev), Kubernetes (production)
Monitoring:     Prometheus + Grafana
```

## Project Structure (Planned)

```
chat_app/
├── cmd/
│   ├── wsserver/           # WebSocket server entry point
│   ├── matcher/            # Matching service entry point
│   └── moderator/          # Moderation service entry point
├── internal/
│   ├── ws/                 # WebSocket + epoll logic
│   ├── matching/           # Matching algorithm
│   ├── chat/               # Chat session management
│   ├── moderation/         # Content filtering
│   ├── session/            # Redis session management
│   └── protocol/           # WebSocket message types
├── pkg/                    # Shared utilities
├── frontend/               # SvelteKit 2 SPA
│   ├── src/
│   │   ├── lib/            # Components, stores, WebSocket client
│   │   └── routes/         # SvelteKit pages
│   ├── svelte.config.js
│   └── package.json
├── docker-compose.yml      # Redis, NATS, PostgreSQL, HAProxy
├── Makefile
├── go.mod
├── ARCHITECTURE.md         # Full architecture document
└── .ai-team/               # AI team config
    ├── context/
    │   ├── project.md      # This file
    │   └── kanban.md       # Task board
    ├── agents/             # Agent definitions
    └── workflows/          # Team workflows
```

## Services

| Service | Language | Purpose | Port |
|---------|----------|---------|------|
| **wsserver** | Go | WebSocket connections, message routing | 8080 |
| **matcher** | Go | Interest-based user matching | internal |
| **moderator** | Go | Content filtering, toxicity scoring | internal |
| **frontend** | SvelteKit | SPA for chat UI | 5173 (dev) |

## Infrastructure (Docker Compose)

| Service | Image | Purpose | Port |
|---------|-------|---------|------|
| Redis 7 | redis:7-alpine | Sessions, matching queue, rate limits | 6379 |
| NATS | nats:latest | Message broker between services | 4222 |
| PostgreSQL 16 | postgres:16-alpine | Abuse reports only | 5432 |
| HAProxy 3.0 | haproxy:3.0 | WebSocket-aware load balancing | 443/80 |

## Key Patterns

- **Ephemeral by design**: No messages stored. All data in Redis with TTLs.
- **gobwas/ws + epoll**: OS-level I/O multiplexing, ~5KB per connection (not gorilla's 2-goroutines-per-conn model)
- **NATS pub/sub**: Fire-and-forget message routing between WS server instances via `chat.<chat_id>` subjects
- **Tiered matching**: Exact match → overlap match → single interest → random → timeout (30s max)
- **Fingerprint-based abuse prevention**: No accounts, so browser fingerprinting for ban enforcement

## Data Flow

```
User connects → HAProxy → WS Server → Redis (session created)
User finds match → WS Server → NATS → Matching Service → Redis (queue) → NATS → WS Servers (notify both users)
User sends message → WS Server → NATS (chat.<id>) → WS Server (partner) → WebSocket push
User disconnects → WS Server → NATS (partner_left) → Redis (cleanup) → gone
```

## Commands

```bash
make docker-up       # Start all backing services
make docker-down     # Stop all services
make build           # Build Go binaries
make run             # Run WS server locally
make test            # Run Go tests
make lint            # Run linters
make fe-dev          # Start frontend dev server
make fe-build        # Build frontend
```

---

*Update this file as the project evolves.*
