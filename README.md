# Whisper

Anonymous real-time chat application that matches users by shared interests. Built to scale to 1M concurrent connections.

## Architecture

```
┌──────────┐    ┌──────────┐    ┌───────────┐
│ SvelteKit│───▶│ HAProxy  │───▶│ wsserver  │
│   SPA    │ WS │   L7 LB  │    │ (Go)      │
└──────────┘    └──────────┘    └─────┬─────┘
                                      │
                          ┌───────────┼───────────┐
                          ▼           ▼           ▼
                     ┌────────┐ ┌─────────┐ ┌──────────┐
                     │ Redis  │ │  NATS   │ │ Postgres │
                     │sessions│ │ pub/sub │ │ reports  │
                     │& queue │ │         │ │ (future) │
                     └────────┘ └────┬────┘ └──────────┘
                                     │
                              ┌──────┴──────┐
                              ▼             ▼
                         ┌─────────┐  ┌───────────┐
                         │ matcher │  │ moderator │
                         │ (Go)    │  │ (Go)      │
                         └─────────┘  └───────────┘
```

**Services:**

- **wsserver** — WebSocket gateway handling client connections via gobwas/ws + epoll (~5KB/conn)
- **matcher** — Background service that pairs users using a tiered interest-matching algorithm
- **moderator** — Content filtering and abuse prevention (stub)

**Infrastructure:** Redis (sessions & queue), NATS (inter-service messaging), PostgreSQL (future reports), HAProxy (load balancing with sticky sessions)

## Tech Stack

| Layer      | Technology                          |
|------------|-------------------------------------|
| Backend    | Go 1.24, gobwas/ws, epoll           |
| Frontend   | SvelteKit 5, TypeScript, Vite       |
| Messaging  | NATS 2 (JetStream)                  |
| Storage    | Redis 7, PostgreSQL 16              |
| Proxy      | HAProxy 3.0                         |
| Deploy     | Docker Compose, multi-stage builds  |

## Prerequisites

- Go 1.24+
- Node.js 20+
- Docker & Docker Compose

## Getting Started

```bash
# Start infrastructure (Redis, NATS, PostgreSQL, HAProxy)
make docker-up

# Run backend services (in separate terminals)
make run           # wsserver on :8080
make run-matcher   # matcher service

# Start frontend dev server
make fe-install
make fe-dev        # dev server on :5173, proxies WS to :8080
```

## Makefile Targets

```
make build            Build all Go binaries into bin/
make test             Run all Go tests with race detection
make lint             Run go vet
make fmt              Format Go source files
make tidy             Tidy module dependencies

make docker-up        Start infrastructure containers
make docker-down      Stop and remove containers
make docker-logs      Follow container logs

make fe-install       Install frontend dependencies
make fe-dev           Start frontend dev server
make fe-build         Build frontend for production

make all              Build everything (Go + frontend)
make clean            Remove build artifacts
```

## Project Structure

```
cmd/
  wsserver/           WebSocket server entrypoint + Dockerfile
  matcher/            Matcher service entrypoint + Dockerfile
  moderator/          Moderator service entrypoint + Dockerfile
internal/
  ws/                 WebSocket server, epoll, connection pool
  session/            Redis-backed session management
  matching/           Tiered matching algorithm & queue
  chat/               Chat room lifecycle & validation
  messaging/          NATS pub/sub abstraction
  protocol/           JSON message envelope definitions
  moderation/         Content filtering (stub)
pkg/utils/            Shared utilities
frontend/             SvelteKit SPA
haproxy/              HAProxy configuration
```

## Matching Algorithm

Users select 2-5 interests and enter the matching queue:

1. **Tier 1** (0-10s) — Exact match: all interests must align
2. **Tier 2** (10-30s) — Relaxed: at least 1 shared interest
3. **Timeout** (30s) — No match found, user notified

## License

TBD
