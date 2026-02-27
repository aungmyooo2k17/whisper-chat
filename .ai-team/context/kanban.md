# Project Kanban -- Whisper (Anonymous Real-Time Chat)

> Last updated: 2026-02-28 (Sprint 12 complete)
> Target: 1,000,000 concurrent WebSocket connections
> Environment: Local development via Docker Compose
> Epics: 9 | Total tasks: 80 | MVP tasks: 41 | First release tasks: 73

---

## Legend

- **Size**: `[S]` < 1 session | `[M]` 1-2 sessions | `[L]` 2+ sessions
- **Category**: `[feature]` | `[infra]` | `[security]` | `[refactor]`
- **Priority**: `[MVP]` = required for basic matching + chat | unmarked = post-MVP
- **Dependencies**: `Blocked by: TASK-ID` = cannot start until that task is done

---

## Backlog

> Ordered by priority within each epic. MVP tasks are listed first.

### Epic 1: Project Setup & Infrastructure (11 tasks)

- [ ] **SETUP-1** [infra][S][MVP] Initialize Go module with project structure (`cmd/wsserver/`, `cmd/matcher/`, `internal/`, `pkg/`)
- [ ] **SETUP-2** [infra][S][MVP] Scaffold SvelteKit 2 project with SPA adapter (`adapter-static`) and Svelte 5 runes
- [ ] **SETUP-3** [infra][M][MVP] Create Docker Compose file with Redis 7, NATS, PostgreSQL 16, and HAProxy services
- [ ] **SETUP-4** [infra][S][MVP] Create multi-stage Dockerfiles for Go services (builder + scratch base, ~15MB images)
  - Blocked by: SETUP-1
- [ ] **SETUP-5** [infra][S][MVP] Create Dockerfile for SvelteKit frontend (build stage + nginx to serve static output)
  - Blocked by: SETUP-2
- [ ] **SETUP-6** [infra][M][MVP] Configure HAProxy for local dev (WebSocket-aware routing, `leastconn`, sticky sessions via `SERVERID` cookie, `/health` checks)
  - Blocked by: SETUP-3
- [ ] **SETUP-7** [infra][S][MVP] Add Makefile with targets: `build`, `run`, `test`, `lint`, `docker-up`, `docker-down`, `logs`
- [ ] **SETUP-10** [infra][S][MVP] Define WebSocket protocol message types as Go structs and constants (per Appendix A of ARCHITECTURE.md)
  - Blocked by: SETUP-1
- [ ] **SETUP-8** [infra][M] Set up Prometheus + Grafana in Docker Compose with pre-configured dashboards for connection count, message throughput, and latency
  - Blocked by: SETUP-3
- [ ] **SETUP-9** [infra][S] Create kernel tuning script (`sysctl.conf` + `limits.conf`) for high file descriptor and socket buffer settings
- [ ] **SETUP-11** [infra][S] Set up PostgreSQL schema migrations using golang-migrate; create `abuse_reports` table (per Section 3.3)
  - Blocked by: SETUP-1, SETUP-3

### Epic 2: WebSocket Server (10 tasks)

- [ ] **WS-1** [feature][L][MVP] Implement gobwas/ws WebSocket server with epoll event loop (OS-level I/O multiplexing, small worker goroutine pool)
  - Blocked by: SETUP-1, SETUP-3
- [ ] **WS-2** [feature][M][MVP] Implement connection manager (map session IDs to file descriptors, track connection count per instance, thread-safe operations)
  - Blocked by: WS-1
- [ ] **WS-3** [feature][M][MVP] Implement session creation on WebSocket connect (generate UUID v4, store session hash in Redis with 1h TTL, return `session_created` message)
  - Blocked by: WS-2
- [ ] **WS-4** [feature][M][MVP] Implement WebSocket JSON message parser (deserialize incoming frames into typed Go structs per protocol spec)
  - Blocked by: WS-1, SETUP-10
- [ ] **WS-5** [feature][M][MVP] Implement message router (dispatch parsed messages to appropriate handler: match, chat, system, report)
  - Blocked by: WS-4
- [ ] **WS-6** [feature][S][MVP] Implement ping/pong heartbeat mechanism (server sends ping every 30s, client must respond within 10s or connection is closed)
  - Blocked by: WS-1
- [ ] **WS-7** [feature][M][MVP] Implement graceful shutdown (stop accepting new connections, drain existing connections with 30s timeout, clean up Redis sessions)
  - Blocked by: WS-2
- [ ] **WS-8** [feature][S][MVP] Add `/health` HTTP endpoint returning 200 OK with connection count for HAProxy health checks
  - Blocked by: WS-1
- [ ] **WS-9** [feature][M][MVP] Integrate NATS client (connect to NATS cluster, publish/subscribe helpers, reconnect logic, subject patterns for `match.*` and `chat.*`)
  - Blocked by: WS-1, SETUP-3
- [ ] **WS-10** [feature][M][MVP] Implement Redis session state management (create, read, update status, refresh TTL, delete on disconnect)
  - Blocked by: WS-3

### Epic 3: Matching System (9 tasks)

- [ ] **MATCH-1** [feature][M][MVP] Implement Redis matching queue data structures (sorted set `match:queue`, sets `match:exact:<hash>`, sets `match:interest:<tag>`, hash `match:session:<sid>`)
  - Blocked by: SETUP-3
- [ ] **MATCH-2** [feature][M][MVP] Implement Tier 1 exact match algorithm (sort + hash interests, SPOP from `match:exact:<hash>`, validate candidate is still waiting)
  - Blocked by: MATCH-1
- [ ] **MATCH-3** [feature][M][MVP] Implement Tier 2 overlap match algorithm (SMEMBERS scan per interest, score by overlap count, select best candidate)
  - Blocked by: MATCH-1
- [ ] **MATCH-5** [feature][M][MVP] Implement background matcher worker (2s polling loop, re-run matching for queued users, handle tier escalation by wait time)
  - Blocked by: MATCH-2, MATCH-3
- [ ] **MATCH-6** [feature][S][MVP] Implement match timeout handling (30s max wait, send `match_timeout` to client, remove user from all queues)
  - Blocked by: MATCH-5
- [ ] **MATCH-7** [feature][M][MVP] Implement accept/decline flow (15s deadline, both-accept starts chat, decline re-queues accepter, timeout re-queues both)
  - Blocked by: MATCH-2, WS-9
- [ ] **MATCH-8** [feature][S][MVP] Publish match results via NATS on `match.found.<session_id>` subjects so WS servers can notify clients
  - Blocked by: MATCH-2, WS-9
- [ ] **MATCH-9** [feature][S][MVP] Implement stale queue entry cleanup (remove disconnected/expired users from sorted sets and interest sets every 5s)
  - Blocked by: MATCH-1
- [ ] **MATCH-4** [feature][M] Implement Tier 3 single-interest fallback (10-20s wait) and Tier 4 random matching (20-30s wait, pair anyone with anyone)
  - Blocked by: MATCH-3, MATCH-5

### Epic 4: Chat System (7 tasks)

- [ ] **CHAT-1** [feature][M][MVP] Implement NATS-based chat message routing (subscribe to `chat.<chat_id>` on match acceptance, publish messages to chat subject)
  - Blocked by: WS-9
- [ ] **CHAT-6** [feature][S][MVP] Create chat session in Redis on match acceptance (HSET `chat:<chat_id>` with user_a, user_b, status, created_at; 2h TTL)
  - Blocked by: MATCH-7, WS-10
- [ ] **CHAT-2** [feature][M][MVP] Implement real-time message delivery (receive from NATS `chat.<chat_id>`, look up target connection, push to WebSocket)
  - Blocked by: CHAT-1, WS-5
- [ ] **CHAT-7** [feature][S][MVP] Implement message validation (reject empty messages, enforce 4KB frame size limit, validate chat_id ownership)
  - Blocked by: CHAT-2
- [ ] **CHAT-3** [feature][S][MVP] Implement typing indicator events (client sends `typing` message, relay via NATS to partner, debounce on server side)
  - Blocked by: CHAT-1
- [ ] **CHAT-4** [feature][M][MVP] Implement end chat and disconnect flow (delete `chat:<chat_id>` from Redis, unsubscribe from NATS subject, clean up session state)
  - Blocked by: CHAT-1, WS-10
- [ ] **CHAT-5** [feature][S][MVP] Implement partner disconnect notification (detect WebSocket close or timeout, publish `partner_disconnected` via NATS, notify remaining user)
  - Blocked by: CHAT-4

### Epic 5: Abuse Prevention (10 tasks)

- [ ] **ABUSE-1** [security][M] Implement rate limiting middleware using Redis INCR+EXPIRE (5 msg/10s per session, 10 match req/min per fingerprint, 5 conn/min per IP)
  - Blocked by: WS-5
- [ ] **ABUSE-2** [security][M] Create keyword blocklist filter (~500 terms loaded at startup, in-memory Aho-Corasick or trie-based matching, < 0.1ms per message)
  - Blocked by: WS-5
- [ ] **ABUSE-10** [security][S] Enforce WebSocket frame size limit (4KB max per message, reject oversized frames before parsing)
  - Blocked by: WS-4
- [ ] **ABUSE-3** [security][S] Implement regex-based spam detection patterns (URL matching, phone number patterns, repeated character flooding)
  - Blocked by: ABUSE-2
- [ ] **ABUSE-4** [security][M] Integrate FingerprintJS open-source library on frontend (canvas + WebGL + timezone + language + screen hashing)
  - Blocked by: FE-1
- [ ] **ABUSE-5** [security][S] Implement fingerprint-based ban check on WebSocket connect (check `ban:<fingerprint>` key in Redis, reject if exists)
  - Blocked by: WS-3, ABUSE-4
- [ ] **ABUSE-6** [security][M] Implement escalating ban system (1st offense = 15min, 2nd = 1h, 3rd = 24h; track via `reports:<fingerprint>` counter with 24h TTL)
  - Blocked by: ABUSE-5
- [ ] **ABUSE-7** [security][M] Implement user reporting flow (one-click report with reason, store in PostgreSQL `abuse_reports` table, capture last 5 messages from memory buffer)
  - Blocked by: CHAT-2, SETUP-11
- [ ] **ABUSE-8** [security][S] Implement auto-ban trigger (3 reports against same fingerprint in 24h triggers 1h ban automatically)
  - Blocked by: ABUSE-7, ABUSE-6
- [ ] **ABUSE-9** [security][L] Implement behavioral analysis engine (track messages-per-chat, avg chat duration, skip rate; flag anomalies like 50+ msgs in 30s or 10+ skips in 5min)
  - Blocked by: ABUSE-1, MATCH-7

### Epic 6: Frontend -- SvelteKit SPA (12 tasks)

- [ ] **FE-1** [feature][M][MVP] Implement WebSocket client service with auto-reconnect, exponential backoff (max 30s), and jitter (0-5s random delay)
  - Blocked by: SETUP-2
- [ ] **FE-7** [feature][M][MVP] Implement client-side state management with Svelte 5 runes (states: disconnected, connected, selecting_interests, matching, match_found, chatting, chat_ended)
  - Blocked by: FE-1
- [ ] **FE-2** [feature][M][MVP] Create landing page with interest tag selection UI (grid of ~50 tags in 8 categories, 1-5 selection limit, "Start Matching" button)
  - Blocked by: SETUP-2
- [ ] **FE-3** [feature][M][MVP] Create matching/searching screen (animated spinner, "Looking for someone..." text, 30s countdown timer, cancel button)
  - Blocked by: FE-1, FE-7
- [ ] **FE-4** [feature][M][MVP] Create match found screen (display shared interests, accept/decline buttons, 15s countdown timer with visual urgency)
  - Blocked by: FE-3
- [ ] **FE-5** [feature][L][MVP] Create chat screen (scrollable message list, text input with send button, typing indicator, "End Chat" button, partner status display)
  - Blocked by: FE-4
- [ ] **FE-8** [feature][S][MVP] Add "partner left" and "chat ended" UI states (notification banner, "Find New Match" button to return to landing)
  - Blocked by: FE-5
- [ ] **FE-10** [feature][M][MVP] Responsive mobile-first CSS design for all screens (touch-friendly targets, viewport-aware layouts, soft keyboard handling for chat input)
  - Blocked by: FE-5
- [ ] **FE-6** [feature][S] Create report dialog UI (reason dropdown: harassment/spam/explicit/other, optional comment, submit button with confirmation)
  - Blocked by: FE-5
- [ ] **FE-9** [feature][S] Add banned/rate-limited UI feedback (display ban reason and countdown to unban, rate limit retry timer)
  - Blocked by: FE-7
- [ ] **FE-11** [feature][S] Add "users online" counter on landing page (subscribe to periodic broadcast from server)
  - Blocked by: FE-2, WS-8
- [ ] **FE-12** [feature][S] Add terms of service and community guidelines display on landing page
  - Blocked by: FE-2

### Epic 7: Moderation Service (6 tasks)

- [ ] **MOD-1** [feature][S] Scaffold moderation service as separate Go binary (`cmd/moderator/`), add Dockerfile and Docker Compose entry
  - Blocked by: SETUP-1, SETUP-3
- [ ] **MOD-2** [feature][M] Implement NATS subscription for flagged messages (subscribe to `moderation.check` subject, receive messages escalated by keyword filter)
  - Blocked by: MOD-1, ABUSE-2
- [ ] **MOD-3** [feature][M] Implement advanced keyword/pattern matching engine (beyond basic blocklist: context-aware patterns, leetspeak normalization)
  - Blocked by: MOD-2
- [ ] **MOD-4** [feature][M] Integrate Perspective API client (HTTP client with 1 QPS rate limiting, toxicity score parsing, retry with backoff)
  - Blocked by: MOD-2
- [ ] **MOD-5** [feature][M] Implement warn/kick/ban logic (toxicity > 0.85 = warn user, 3 warns in session = kick + 15min ban on fingerprint, update Redis ban records)
  - Blocked by: MOD-4, ABUSE-6
- [ ] **MOD-6** [feature][S] Implement in-memory message buffer per chat (ring buffer of last 5 messages for report context, no persistence)
  - Blocked by: CHAT-2

### Epic 8: Load Testing (8 tasks)
> All blockers listed are from earlier epics. See dependency graph for full chain.


- [ ] **LOAD-1** [infra][S] Research and select WebSocket load testing tooling (evaluate: k6 with xk6-websockets, Artillery, custom Go client using gobwas/ws)
- [ ] **LOAD-2** [infra][M] Write connection saturation test (open N WebSocket connections, hold them idle, measure memory and CPU per connection)
  - Blocked by: WS-1, LOAD-1
- [ ] **LOAD-3** [infra][M] Write matching flow load test (N clients connect, select random interests, trigger matching, measure match latency)
  - Blocked by: MATCH-7, LOAD-2
- [ ] **LOAD-4** [infra][L] Write full chat lifecycle load test (connect -> match -> exchange messages -> disconnect; measure end-to-end latency and throughput)
  - Blocked by: CHAT-2, LOAD-3
- [ ] **LOAD-5** [infra][S] Create kernel tuning automation for load test client machine (raise ulimits, ephemeral port range, TCP buffer tuning)
  - Blocked by: LOAD-1
- [ ] **LOAD-6** [infra][M] Set up metrics collection during load tests (Prometheus scraping targets, Grafana dashboards for connections, latency percentiles, error rates)
  - Blocked by: SETUP-8, LOAD-2
- [ ] **LOAD-7** [infra][L] Execute tiered load test benchmarks (10K -> 100K -> 500K -> 1M connections, record metrics at each tier, identify breaking points)
  - Blocked by: LOAD-4, LOAD-5, LOAD-6
- [ ] **LOAD-8** [infra][M] Analyze load test results, document bottlenecks, and apply performance tuning (connection pooling, buffer sizes, goroutine pool sizing)
  - Blocked by: LOAD-7

### Epic 9: Release & Deployment (7 tasks)

- [ ] **RELEASE-1** [infra][S] Create Dockerfile for SvelteKit frontend (build stage + nginx to serve static files)
  - Blocked by: FE-10
- [ ] **RELEASE-2** [infra][M] Create production Docker Compose with all services (wsserver, matcher, moderator, frontend, Redis, NATS, PostgreSQL, HAProxy)
  - Blocked by: RELEASE-1
- [ ] **RELEASE-3** [infra][M] Configure HAProxy for production (SSL termination, WebSocket upgrade detection, multiple WS server backends, connection draining)
  - Blocked by: RELEASE-2
- [ ] **RELEASE-4** [infra][S] Create environment variable documentation and `.env.example` file for all services
  - Blocked by: RELEASE-2
- [ ] **RELEASE-5** [infra][M] End-to-end integration test — manual walkthrough of full user journey (connect → match → chat → end → report → ban) on Docker Compose
  - Blocked by: RELEASE-2
- [ ] **RELEASE-6** [infra][S] Create `scripts/deploy.sh` — single-command Docker Compose production startup with health checks and validation
  - Blocked by: RELEASE-3
- [ ] **RELEASE-7** [infra][S] Write DEPLOYMENT.md with setup instructions, environment requirements, and ops runbook (start, stop, monitor, scale, troubleshoot)
  - Blocked by: RELEASE-6

---

## Ready

> Tasks with no blockers or whose blockers are all Done.

_(empty — Sprint 13 tasks RELEASE-6 and RELEASE-7 are next)_

---

## In Progress

_(empty)_

---

## Review

_(empty)_

---

## Done

- [x] **SETUP-1** [infra][S] Go module + project structure
- [x] **SETUP-2** [infra][S] SvelteKit 2 scaffold
- [x] **SETUP-3** [infra][M] Docker Compose (Redis, NATS, PostgreSQL, HAProxy)
- [x] **SETUP-4** [infra][S] Multi-stage Dockerfiles for Go services
- [x] **SETUP-7** [infra][S] Makefile with dev commands
- [x] **SETUP-10** [infra][S] WebSocket protocol message types
- [x] **WS-1** [feature][L] gobwas/ws + epoll server
- [x] **WS-2** [feature][M] Connection manager (built with WS-1)
- [x] **WS-3** [feature][M] Redis session creation
- [x] **WS-4** [feature][M] Message dispatcher
- [x] **WS-5** [feature][M] Message router (built with WS-4)
- [x] **WS-6** [feature][S] Ping/pong heartbeat
- [x] **WS-8** [feature][S] /health endpoint
- [x] **WS-9** [feature][M] NATS integration
- [x] **WS-10** [feature][M] Redis session CRUD (built with WS-3)
- [x] **FE-1** [feature][M] WebSocket client service
- [x] **FE-2** [feature][M] Landing page with interest selection
- [x] **MATCH-1** [feature][M] Redis matching queue data structures (Sprint 4)
- [x] **MATCH-2** [feature][M] Tier 1 exact match algorithm (Sprint 4)
- [x] **MATCH-3** [feature][M] Tier 2 overlap match algorithm (Sprint 4)
- [x] **MATCH-9** [feature][S] Stale queue entry cleanup (Sprint 4)
- [x] **MATCH-5** [feature][M] Background matcher worker (Sprint 4)
- [x] **MATCH-8** [feature][S] Publish match results via NATS (Sprint 4)
- [x] **MATCH-7** [feature][M] Accept/decline flow — 15s deadline (Sprint 5)
- [x] **MATCH-6** [feature][S] Match timeout handling — 30s (Sprint 5)
- [x] **CHAT-1** [feature][M] NATS-based chat message routing (Sprint 5)
- [x] **CHAT-6** [feature][S] Chat session in Redis on match acceptance (Sprint 5)
- [x] **CHAT-2** [feature][M] Real-time message delivery via NATS → WebSocket (Sprint 5)
- [x] **CHAT-3** [feature][S] Typing indicator relay (Sprint 5)
- [x] **CHAT-7** [feature][S] Message validation (Sprint 5)
- [x] **CHAT-4** [feature][M] End chat and disconnect flow (Sprint 6)
- [x] **CHAT-5** [feature][S] Partner disconnect notification (Sprint 6)
- [x] **FE-7** [feature][M] Client-side state management (Sprint 6)
- [x] **FE-3** [feature][M] Matching/searching screen (Sprint 6)
- [x] **FE-4** [feature][M] Match found screen (Sprint 6)
- [x] **FE-5** [feature][L] Chat screen (Sprint 6)
- [x] **FE-8** [feature][S] Partner left / chat ended UI states (Sprint 6)
- [x] **FE-10** [feature][M] Responsive mobile-first CSS (Sprint 6)
- [x] **BUG-1** [bug][M] WebSocket false `partner_left` — three root causes fixed (Sprint 6)
  - Fix 1: Replaced `wsutil.ReadClientData` with `wsutil.NextReader` for per-frame control frame handling
  - Fix 2: Cleared stale `WriteDeadline` after `SendMessage` to prevent heartbeat ping write failures
  - Fix 3: Treated read timeouts from stale epoll dispatches as harmless (return, don't `RemoveConnection`)
  - Also: Bypassed Vite proxy via `VITE_WS_URL`, improved Vite proxy config, bumped Dockerfiles to Go 1.24
- [x] **SETUP-11** [infra][S] PostgreSQL migrations — abuse_reports table (Sprint 7)
- [x] **ABUSE-10** [security][S] WebSocket frame size limit — 4KB max enforcement (Sprint 7)
- [x] **ABUSE-1** [security][M] Rate limiting middleware — Redis INCR+EXPIRE, 5msg/10s, 10match/min (Sprint 7)
- [x] **ABUSE-2** [security][M] Keyword blocklist filter — ~120 terms, leetspeak detection, < 0.005ms (Sprint 7)
- [x] **ABUSE-3** [security][S] Regex spam detection — URLs, phone numbers, char/word flooding (Sprint 7)
- [x] **ABUSE-4** [security][M] FingerprintJS integration — frontend + set_fingerprint protocol (Sprint 7)
- [x] **ABUSE-5** [security][S] Fingerprint-based ban check — reject banned users on connect (Sprint 7)
- [x] **ABUSE-6** [security][M] Escalating ban system — 15min → 1h → 24h, auto-ban at 3 reports (Sprint 7)
- [x] **ABUSE-7** [security][M] User reporting flow — PostgreSQL storage, report handler with message context (Sprint 8)
- [x] **ABUSE-8** [security][S] Auto-ban trigger — Redis + PostgreSQL dual-layer detection, 3 reports in 24h (Sprint 8)
- [x] **MOD-1** [feature][S] Moderation service scaffold — NATS + Redis + content filter (Sprint 8)
- [x] **MOD-2** [feature][M] NATS async moderation — publish to moderation.check, subscribe to results, content_warning on flag (Sprint 8)
- [x] **MOD-6** [feature][S] In-memory message ring buffer — 5 msgs per chat, goroutine-safe (Sprint 8)
- [x] **FE-6** [feature][S] Report dialog — 4 reason options, submit confirmation, auto-close (Sprint 8)
- [x] **FE-9** [feature][S] Banned screen with countdown + rate-limit toast (Sprint 8)
- [x] **FE-12** [feature][S] Terms of service / community guidelines on landing page (Sprint 8)
- [x] **MATCH-4** [feature][M] Tier 3/4 matching — single-interest fallback (20-25s) + random matching (25-30s) (Sprint 9)
- [x] **FE-11** [feature][S] "Users online" counter — polls /api/online, green dot with count (Sprint 9)
- [x] **SETUP-8** [infra][M] Prometheus + Grafana — 6 metrics, pre-configured dashboard, Docker Compose (Sprint 9)
- [x] **SETUP-9** [infra][S] Kernel tuning scripts — sysctl.conf, limits.conf, tune-kernel.sh for 1M connections (Sprint 9)
- [x] **SETUP-5** [infra][S] Frontend Dockerfile — multi-stage node + nginx, SPA routing, gzip, security headers (Sprint 9)
- [x] **WS-7** [feature][M] Graceful shutdown — drain flag, 30s timeout, partner notifications, phased close (Sprint 9)
- [x] **LOAD-1** [infra][S] Custom Go load test client — gobwas/ws based, reusable client + stats packages (Sprint 10)
- [x] **LOAD-5** [infra][S] Kernel tuning for load test client — sysctl, limits, tune-client.sh (Sprint 10)
- [x] **LOAD-2** [infra][M] Connection saturation test — ramp-up, hold, progress reporting, signal handling (Sprint 10)
- [x] **LOAD-6** [infra][M] Metrics collection — Prometheus scraper, Grafana load test dashboard (Sprint 10)
- [x] **LOAD-3** [infra][M] Matching flow load test — connect all, batch find_match, accept_match, latency tracking (Sprint 10)
- [x] **LOAD-4** [infra][L] Full chat lifecycle load test — connect → match → chat messages → end_chat (Sprint 10)
- [x] **LOAD-7** [infra][L] Tiered benchmark scripts — run-tier.sh, thresholds.yaml, compare.sh for 10K→1M (Sprint 11)
- [x] **LOAD-8** [infra][M] Performance tuning playbook — bottleneck analysis, tuning parameters, scaling strategies (Sprint 11)
- [x] **RELEASE-1** [infra][S] Frontend Dockerfile hardened — non-root nginx, healthcheck, CSP, rate limiting (Sprint 12)
- [x] **RELEASE-2** [infra][M] Production Docker Compose — resource limits, dual networks, multi-instance wsserver, JSON logging (Sprint 12)
- [x] **RELEASE-3** [infra][M] Production HAProxy — SSL termination, WebSocket routing, rate limiting, connection draining (Sprint 12)
- [x] **RELEASE-4** [infra][S] Environment docs — .env.example + .env.production.example with all variables (Sprint 12)
- [x] **RELEASE-5** [infra][M] E2E integration test — 7 scenarios covering health, connect, match, chat, end, rate limit, content filter (Sprint 12)

---

## Dependency Graph (Critical Path)

```
SETUP-1 ──┬── SETUP-4 ── (Dockerfiles ready)
           ├── SETUP-10 ──┐
           └── WS-1 ──────┤
                           ├── WS-4 ── WS-5 ── (message routing ready)
SETUP-3 ──┬── SETUP-6     │
           ├── WS-1 ───── WS-2 ── WS-3 ── WS-10
           ├── WS-9 ──────┤
           └── MATCH-1 ───┤
                           ├── MATCH-2 ── MATCH-7 ── CHAT-6
                           ├── MATCH-3    │
                           └── MATCH-5 ── MATCH-6
                                          │
SETUP-2 ── FE-1 ── FE-7 ── FE-3 ── FE-4 ── FE-5 ── FE-10
                                                      │
WS-9 ── CHAT-1 ── CHAT-2 ── CHAT-7                   │
                   │                                   │
                   CHAT-4 ── CHAT-5       (MVP COMPLETE)
```

**Critical path to MVP**: SETUP-1 -> WS-1 -> WS-2 -> WS-3 -> WS-10 -> CHAT-6 (backend) | SETUP-2 -> FE-1 -> FE-7 -> FE-3 -> FE-4 -> FE-5 (frontend)

---

## Sprint Suggestions

### Sprint 1: Foundation (5 tasks, ~3-4 sessions)
| Task | Size | Description |
|------|------|-------------|
| SETUP-1 | S | Initialize Go module with project structure |
| SETUP-2 | S | Scaffold SvelteKit 2 project with SPA adapter |
| SETUP-3 | M | Create Docker Compose (Redis, NATS, PostgreSQL, HAProxy) |
| SETUP-7 | S | Add Makefile with dev commands |
| SETUP-10 | S | Define WebSocket protocol message types |

**Sprint 1 goal**: Development environment fully operational, `make docker-up` brings up all backing services, Go and Svelte projects compile.

### Sprint 2: WebSocket Core (5 tasks, ~5-6 sessions)
| Task | Size | Description |
|------|------|-------------|
| WS-1 | L | gobwas/ws server with epoll event loop |
| WS-4 | M | JSON message parser and protocol handler |
| WS-8 | S | /health endpoint |
| WS-6 | S | Ping/pong heartbeat |
| SETUP-4 | S | Go service Dockerfiles |

**Sprint 2 goal**: WebSocket server accepts connections, parses messages, and responds to pings inside Docker.

### Sprint 3: Connections + Frontend Start (5 tasks, ~5-6 sessions)
| Task | Size | Description |
|------|------|-------------|
| WS-2 | M | Connection manager |
| WS-3 | M | Session creation (Redis) |
| WS-9 | M | NATS integration |
| FE-1 | M | WebSocket client service |
| FE-2 | M | Landing page with interest selection |

**Sprint 3 goal**: Frontend connects to backend via WebSocket, sessions are created in Redis, NATS pub/sub is wired up.

### Sprint 4: Matching System (6 tasks, ~6-7 sessions)
| Task | Size | Description |
|------|------|-------------|
| MATCH-1 | M | Redis matching queue data structures |
| MATCH-2 | M | Tier 1 exact match algorithm |
| MATCH-3 | M | Tier 2 overlap match algorithm |
| MATCH-9 | S | Stale queue entry cleanup |
| MATCH-5 | M | Background matcher worker (2s polling, tier escalation) |
| MATCH-8 | S | Publish match results via NATS |

**Sprint 4 goal**: Matching service pairs users via tiered algorithm, results published via NATS.

### Sprint 5: Match Flow + Chat Backend (7 tasks, ~6-7 sessions)
| Task | Size | Description |
|------|------|-------------|
| MATCH-7 | M | Accept/decline flow (15s deadline) |
| MATCH-6 | S | Match timeout handling (30s) |
| CHAT-1 | M | NATS-based chat message routing |
| CHAT-6 | S | Chat session in Redis on match acceptance |
| CHAT-2 | M | Real-time message delivery via NATS → WebSocket |
| CHAT-3 | S | Typing indicator relay |
| CHAT-7 | S | Message validation (empty, size, ownership) |

**Sprint 5 goal**: Full match → accept → chat → typing lifecycle works end-to-end on the backend.

### Sprint 6: Frontend Screens + Chat Lifecycle (8 tasks, ~7-8 sessions)
| Task | Size | Description |
|------|------|-------------|
| CHAT-4 | M | End chat and disconnect flow |
| CHAT-5 | S | Partner disconnect notification |
| FE-7 | M | Client-side state management (app state machine) |
| FE-3 | M | Matching/searching screen (spinner, countdown, cancel) |
| FE-4 | M | Match found screen (shared interests, accept/decline, timer) |
| FE-5 | L | Chat screen (messages, input, typing indicator, end chat) |
| FE-8 | S | "Partner left" and "chat ended" UI states |
| FE-10 | M | Responsive mobile-first CSS across all screens |

**Sprint 6 goal**: MVP complete — full user journey in the frontend: select interests → match → accept → chat → end/disconnect, mobile-friendly.

### Sprint 7: Abuse Prevention Core (8 tasks, ~6-7 sessions)
| Task | Size | Description |
|------|------|-------------|
| SETUP-11 | S | PostgreSQL migrations (abuse_reports table) |
| ABUSE-1 | M | Rate limiting middleware (Redis INCR+EXPIRE) |
| ABUSE-2 | M | Keyword blocklist filter (in-memory, < 0.1ms) |
| ABUSE-3 | S | Regex spam detection (URLs, phone numbers, flooding) |
| ABUSE-10 | S | WebSocket frame size limit (4KB max) |
| ABUSE-4 | M | FingerprintJS integration on frontend |
| ABUSE-5 | S | Fingerprint-based ban check on connect |
| ABUSE-6 | M | Escalating ban system (15min → 1h → 24h) |

**Sprint 7 goal**: Core safety layer — rate limiting, content filtering, fingerprinting, and ban enforcement all active.

### Sprint 8: Reporting + Moderation + Frontend Polish (8 tasks, ~6-7 sessions)
| Task | Size | Description |
|------|------|-------------|
| ABUSE-7 | M | User reporting flow (PostgreSQL storage, message buffer) |
| ABUSE-8 | S | Auto-ban trigger (3 reports in 24h → ban) |
| MOD-1 | S | Scaffold moderation service |
| MOD-2 | M | NATS subscription for flagged messages |
| MOD-6 | S | In-memory message buffer per chat (ring buffer, 5 msgs) |
| FE-6 | S | Report dialog UI |
| FE-9 | S | Banned/rate-limited UI feedback |
| FE-12 | S | Terms of service display on landing page |

**Sprint 8 goal**: Users can report abuse, auto-ban kicks in, moderation service processes flagged messages, all safety UI in place.

### Sprint 9: Matching Polish + Monitoring (6 tasks, ~5-6 sessions)
| Task | Size | Description |
|------|------|-------------|
| MATCH-4 | M | Tier 3/4 matching (single-interest fallback + random) |
| FE-11 | S | "Users online" counter on landing page |
| SETUP-8 | M | Prometheus + Grafana in Docker Compose |
| SETUP-9 | S | Kernel tuning script for high connections |
| SETUP-5 | S | Frontend Dockerfile (nginx static server) |
| WS-7 | M | Graceful shutdown with connection draining |

**Sprint 9 goal**: Full 4-tier matching, monitoring dashboards operational, infrastructure production-ready.

### Sprint 10: Load Testing (6 tasks, ~5-6 sessions)
| Task | Size | Description |
|------|------|-------------|
| LOAD-1 | S | Research and select load testing tools |
| LOAD-5 | S | Kernel tuning for load test client |
| LOAD-2 | M | Connection saturation test |
| LOAD-3 | M | Matching flow load test |
| LOAD-4 | L | Full chat lifecycle load test |
| LOAD-6 | M | Metrics collection during load tests |

**Sprint 10 goal**: Load test suite ready, validated at 10K-100K connections locally.

### Sprint 11: Scale Testing + Tuning (2 tasks, ~3-4 sessions)
| Task | Size | Description |
|------|------|-------------|
| LOAD-7 | L | Tiered benchmarks (10K → 100K → 500K → 1M) |
| LOAD-8 | M | Analyze results, document bottlenecks, tune |

**Sprint 11 goal**: 1M connection benchmark completed, bottlenecks identified and resolved, performance documented.

### Sprint 12: Release (5 tasks, ~3-4 sessions)
| Task | Size | Description |
|------|------|-------------|
| RELEASE-1 | S | Frontend production Dockerfile |
| RELEASE-2 | M | Production Docker Compose (all services) |
| RELEASE-3 | M | Production HAProxy config (SSL, draining) |
| RELEASE-5 | M | End-to-end integration test |
| RELEASE-4 | S | Environment docs + .env.example |

**Sprint 12 goal**: Production-ready Docker Compose, full integration test passes.

### Sprint 13: Ship It (3 tasks, ~2-3 sessions)
| Task | Size | Description |
|------|------|-------------|
| RELEASE-6 | S | deploy.sh script with health checks |
| RELEASE-7 | S | DEPLOYMENT.md with ops runbook |
| — | — | Deploy to production server |

**Sprint 13 goal**: v1.0 shipped. Whisper is live.

---

## Release Summary

```
Sprints 1-3:   Foundation       ✅ DONE (17 tasks)
Sprint 4:      Matching         ✅ DONE (6 tasks)
Sprint 5:      Chat backend     ✅ DONE (7 tasks)
Sprint 6:      Frontend + MVP   ✅ DONE (8 tasks)
Sprint 7:      Abuse prevention ✅ DONE (8 tasks)
Sprint 8:      Reporting + mod  ✅ DONE (8 tasks)
Sprint 9:      Polish + monitor ✅ DONE (6 tasks)
Sprint 10:     Load testing     ✅ DONE (6 tasks)
Sprint 11:     Scale testing    ✅ DONE (2 tasks)
Sprint 12:     Release prep     ✅ DONE (5 tasks)
Sprint 13:     Ship it           3 tasks
────────────────────────────────────────
Total:         13 sprints, 76 tasks (+ 4 skipped for v2)
               73 done, 3 remaining
```

**Tasks deferred to v2.0** (not needed for first release):
- ABUSE-9: Behavioral analysis engine (complex, not critical for launch)
- MOD-3: Advanced keyword matching (leetspeak normalization)
- MOD-4: Perspective API integration
- MOD-5: Automated toxicity warn/kick/ban
