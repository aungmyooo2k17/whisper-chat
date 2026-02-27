# Anonymous Real-Time Chat Application -- System Architecture

**Project Codename**: Whisper
**Author**: Architect (AI)
**Date**: 2026-02-27
**Scale Target**: 1,000,000 concurrent users

---

## Table of Contents

1. [Technology Decisions](#1-technology-decisions)
2. [System Architecture](#2-system-architecture)
3. [Database & Storage Design](#3-database--storage-design)
4. [Infrastructure & Scale Strategy](#4-infrastructure--scale-strategy)
5. [Matching Algorithm Design](#5-matching-algorithm-design)
6. [Abuse Prevention](#6-abuse-prevention)
7. [Risks & Mitigations](#7-risks--mitigations)

---

## 1. Technology Decisions

Every recommendation below is backed by benchmark data and real-world case studies.

### 1.1 Backend Language: Go

#### Comparison Table

| Criteria                  | Go                  | Elixir/BEAM         | Node.js             | Rust                |
|---------------------------|---------------------|----------------------|---------------------|---------------------|
| **1M WS Connections**     | Proven (600MB RAM)  | Proven (higher RAM)  | Fails at ~60K       | Proven (lowest RAM) |
| **Latency Consistency**   | Good (slight drift at 70K+) | Excellent (near-constant) | Degrades severely | Excellent |
| **Memory per Connection** | ~5KB (with epoll)   | ~10-20KB per process | ~30-50KB            | ~2-3KB              |
| **Sustained Throughput**  | Good                | Best (2x Go in some tests) | Moderate       | Best                |
| **Ecosystem Maturity**    | Excellent            | Good                 | Excellent           | Growing             |
| **Deployment Simplicity** | Single binary        | OTP release          | Node runtime + deps | Single binary       |
| **Hiring / Team Ramp-up** | Easy                 | Hard                 | Easiest             | Hardest             |
| **Concurrency Model**     | Goroutines + epoll   | Actor model (BEAM)   | Event loop (single) | async/await + tokio |

#### Why Go Wins

**Primary evidence**: The Mail.ru engineering team demonstrated handling [1 million WebSocket connections in Go](https://www.freecodecamp.org/news/million-websockets-and-go-cc58418460bb/) using `gobwas/ws` with epoll, achieving ~600MB total RAM on a single server -- a 97% memory reduction over naive implementations.

**Supporting evidence**:
- [Stressgrid benchmarks](https://stressgrid.com/blog/benchmarking_go_vs_node_vs_elixir/) show Go reaching 100K connections where Node.js collapsed at ~60K
- [C1000K benchmark](https://github.com/smallnest/C1000K-Servers) confirmed Go handling 1.2M active WebSocket connections
- Go uses roughly half the memory of Elixir at equivalent connection counts per [Hashrocket shootout](https://hashrocket.com/blog/posts/websocket-shootout)
- Pusher handles 1-2 million concurrent connections per cluster using Go

**Why not Elixir?** Elixir's BEAM VM excels at sustained throughput and fault tolerance, and actually [outperforms Go 2x in sustained message throughput](https://medium.com/beamworld/the-ultimate-websocket-battle-elixir-vs-go-performance-showdown-5edf2). However, Go's memory advantage (5KB vs 10-20KB per connection) saves ~10-15GB RAM at 1M connections. For a free, unfunded project, infrastructure cost matters. Go also deploys as a single binary with no runtime dependencies.

**Why not Rust?** Rust would yield the lowest memory footprint (~2-3KB/conn) but at a significant development velocity cost. For a greenfield project with no existing team, Go's simpler concurrency model and faster iteration speed are more pragmatic.

**Why not Node.js?** The [Stressgrid benchmark](https://stressgrid.com/blog/benchmarking_go_vs_node_vs_elixir/) is definitive: Node.js becomes severely overloaded at ~60K connections, with header response times degrading from milliseconds to 1.5 seconds. Reaching 1M would require 17+ Node processes just for connections, adding enormous operational complexity.

---

### 1.2 WebSocket Library: gobwas/ws + epoll (not Gorilla)

#### Comparison Table

| Library          | Memory at 1M Conns | Goroutines at 1M | Zero-copy Upgrade | Maintained |
|------------------|--------------------|-------------------|--------------------|------------|
| **gobwas/ws**    | ~600MB             | ~thousands (epoll)| Yes                | Yes        |
| gorilla/websocket| ~15GB+             | ~2M (2 per conn)  | No                 | Archived*  |
| nhooyr/websocket | ~8GB+              | ~1M               | No                 | Yes (coder/websocket) |

*gorilla/websocket was archived but has since been revived by the community.

#### Why gobwas/ws

The critical insight from the [Mail.ru million WebSocket article](https://medium.com/@gobwas/million-websockets-and-go-cc58418460bb/):

- Standard approach (gorilla): 2 goroutines per connection (read + write) = 2M goroutines at 1M connections, each consuming ~4KB stack = **~8GB just for goroutine stacks**
- gobwas/ws + epoll: delegate I/O waiting to the OS via epoll, use a **small worker pool** of goroutines to handle ready connections = **99% fewer goroutines, 97% less memory**

The [epoll approach](https://slides.com/jalex-chang/epoll-websocket-go/fullscreen) monitors file descriptors at the OS level and only allocates goroutines when data is ready to read, yielding ~5KB per connection total.

---

### 1.3 Message Broker: NATS

#### Comparison Table

| Criteria              | NATS                | Redis Pub/Sub       | Kafka               |
|-----------------------|---------------------|----------------------|---------------------|
| **Latency**           | ~0.1-0.4ms          | ~0.5-1.2ms           | ~5-50ms             |
| **Throughput**        | 10M+ msg/sec        | High                 | Highest (batched)   |
| **Persistence**       | Optional (JetStream)| No (Pub/Sub)         | Yes (mandatory)     |
| **Operational Weight**| Minimal (single binary) | Moderate (already needed) | Heavy (ZK/KRaft + brokers) |
| **Memory Footprint**  | ~20-50MB            | Shared with cache    | ~1GB+ per broker    |
| **Go Integration**    | Native (written in Go) | Good (go-redis)   | Good (confluent-kafka-go) |
| **At-most-once**      | Native              | Native               | Requires config     |
| **Clustering**        | Built-in            | Redis Cluster        | Built-in            |

#### Why NATS

Per [Brave New Geek's latency benchmarks](https://bravenewgeek.com/benchmarking-message-queue-latency/), NATS delivers 0.1-0.4ms latency -- the fastest in the comparison. For ephemeral chat messages that require no persistence, NATS's fire-and-forget pub/sub is ideal.

From [BuildShift's 2025 analysis](https://medium.com/@BuildShift/kafka-is-old-redpanda-is-fast-pulsar-is-weird-nats-is-tiny-which-message-broker-should-you-32ce61d8aa9f): "NATS is tiny" -- a single Go binary with minimal operational overhead, perfect for our unfunded project.

Production pattern: [Building a live chat with Go, NATS, Redis and Websockets](https://ribice.medium.com/building-a-live-chat-with-go-nats-redis-and-websockets-cb3edbc940ca) demonstrates exactly our architecture -- NATS handles message fanout between WebSocket server instances, Redis handles state.

**Why not Redis Pub/Sub?** We already use Redis for matching/sessions. Adding pub/sub to the same Redis cluster creates a single point of failure and contention. NATS separates concerns cleanly with lower latency.

**Why not Kafka?** Massive overkill. Kafka's strengths (durability, replay, ordering guarantees) are liabilities here -- we want ephemeral, fire-and-forget messaging with minimal latency.

---

### 1.4 Database/Cache: Redis (Primary) + PostgreSQL (Reports Only)

#### Comparison Table

| Data Type           | Redis               | PostgreSQL           | MongoDB             |
|---------------------|---------------------|----------------------|---------------------|
| **Matching Queue**  | Sorted sets O(log N)| B-tree index O(log N)| B-tree O(log N)     |
| **Session State**   | Hash with TTL       | Row with cron cleanup| Document with TTL index |
| **Rate Limiting**   | Native (INCR+EXPIRE)| Manual implementation| Manual              |
| **Latency**         | ~0.1-0.5ms          | ~1-5ms               | ~1-5ms              |
| **TTL Support**     | Native per key      | No native TTL        | TTL index (delayed) |
| **Ephemeral Data**  | Purpose-built       | Overkill             | Acceptable          |
| **Sorted Set Ops**  | O(log N) native     | Requires indexing    | Requires indexing   |

#### Why Redis

Almost all data in this system is ephemeral:
- Matching queue entries live for seconds to minutes
- Active sessions live for the duration of a chat (minutes to hours)
- Rate limiting counters live for seconds to minutes

Redis is purpose-built for this. [Netflix's Timestone](https://www.infoq.com/news/2022/10/netflix-timestone-priority-queue/) uses Redis sorted sets for production priority queuing, validating the pattern. Per [oneuptime's matchmaking guide](https://oneuptime.com/blog/post/2026-01-21-redis-matchmaking-systems/view), Redis sorted sets enable efficient skill-based matching with sub-millisecond operations.

**PostgreSQL role**: Only for abuse reports that need to survive beyond a session (storing reporter fingerprint, reported fingerprint, chat excerpt, timestamp). This is low-write, low-read traffic -- a single small PostgreSQL instance suffices.

---

### 1.5 Frontend: SvelteKit (SPA mode)

#### Comparison Table

| Criteria                | SvelteKit           | Next.js (React)      | React + Vite        |
|-------------------------|---------------------|----------------------|---------------------|
| **Runtime Size**        | 1.6KB               | 42KB+ (React+ReactDOM) | 42KB+ (React+ReactDOM) |
| **Bundle Size (typical)** | Smallest          | Largest              | Medium              |
| **WebSocket Support**   | Native (new in 2025)| Manual / Socket.IO   | Manual / Socket.IO  |
| **SSR Needed?**         | Optional (SPA mode) | Default (overkill)   | No SSR              |
| **Reactivity Model**    | Compile-time (runes)| Runtime (Virtual DOM)| Runtime (Virtual DOM)|
| **Learning Curve**      | Low                 | Medium               | Medium              |
| **Mobile Performance**  | Best (smallest bundle) | Good              | Good                |

#### Why SvelteKit

Per [Better Stack's comparison](https://betterstack.com/community/guides/scaling-nodejs/sveltekit-vs-nextjs/), SvelteKit's 1.6KB runtime vs React's 42KB means faster load times on mobile browsers -- critical for a chat app where users expect instant interactivity.

From [dev.to's 2026 deep dive](https://dev.to/paulthedev/sveltekit-vs-nextjs-in-2026-why-the-underdog-is-winning-a-developers-deep-dive-155b): real-world tests on production apps with 50+ routes showed SvelteKit "wasn't just faster, it was a game-changer."

**Key factors for this project**:
- No SEO needed (anonymous chat, no indexable content)
- No complex data fetching (just WebSocket messages)
- SPA mode eliminates SSR overhead entirely
- Svelte 5's runes provide fine-grained reactivity without Virtual DOM diffing
- [Native WebSocket support](https://medium.com/@vinay.s.khanagavi/implementing-websockets-in-a-sveltekit-version-5-1d6c6041e9ca) now available in SvelteKit

**Why not Next.js?** Next.js's strengths (SSR, ISR, API routes, image optimization) are irrelevant for a real-time chat SPA. It would ship 25x more runtime JavaScript for no benefit.

---

### 1.6 Technology Stack Summary

```
Frontend:       SvelteKit 2 (SPA mode, Svelte 5)
Backend:        Go 1.23+
WebSocket:      gobwas/ws + epoll
Message Broker: NATS
Cache/State:    Redis 7+ (Cluster mode)
Database:       PostgreSQL 16 (reports only)
Load Balancer:  HAProxy 3.0
CDN:            Cloudflare (free tier)
Containers:     Docker + Kubernetes
CI/CD:          GitHub Actions
Monitoring:     Prometheus + Grafana
```

---

## 2. System Architecture

### 2.1 Architecture Diagram

```
                                    Internet
                                       |
                                       v
                              +------------------+
                              |   Cloudflare CDN |  <-- Static assets (SvelteKit build)
                              |   (Free Tier)    |  <-- DDoS protection
                              +------------------+
                                       |
                                       v
                              +------------------+
                              |    HAProxy LB    |  <-- WebSocket-aware load balancing
                              |  (Sticky Sessions)|  <-- Cookie-based affinity
                              +------------------+
                                    /    |    \
                                   v     v     v
                         +--------+ +--------+ +--------+
                         |  WS    | |  WS    | |  WS    |   <-- Go WebSocket Servers
                         |Server 1| |Server 2| |Server N|       (gobwas/ws + epoll)
                         +--------+ +--------+ +--------+       Each handles ~50-100K conns
                            |  |       |  |       |  |
                     +------+  +-------+--+-------+  +------+
                     |                 |                     |
                     v                 v                     v
              +------------+   +-------------+   +------------------+
              | NATS Cluster|   |Redis Cluster|   |Matching Service  |
              | (3 nodes)   |   |(3+ masters) |   |(Go, N replicas)  |
              +------------+   +-------------+   +------------------+
                                      |                     |
                                      v                     v
                              +-------------+        +-------------+
                              | PostgreSQL  |        | Redis       |
                              | (Reports)   |        | (Match Queue|
                              +-------------+        |  Sessions)  |
                                                     +-------------+

                    +--------------------------------------------------+
                    |              Supporting Services                  |
                    |                                                   |
                    |  +---------------+  +---------------------------+ |
                    |  | Moderation    |  | Metrics                   | |
                    |  | Service (Go)  |  | (Prometheus + Grafana)    | |
                    |  +---------------+  +---------------------------+ |
                    +--------------------------------------------------+
```

### 2.2 Component Descriptions

#### WebSocket Server (Go)
- Accepts and manages WebSocket connections using gobwas/ws with epoll
- Each instance handles 50,000-100,000 concurrent connections
- Publishes/subscribes to NATS for cross-instance message delivery
- Manages session state in Redis
- Stateless -- any instance can serve any user (with sticky session for connection persistence)

#### Matching Service (Go)
- Dedicated service running N replicas
- Consumes from Redis matching queue
- Executes the interest-based matching algorithm
- Notifies matched users via NATS
- Operates on a tight loop with <100ms matching target

#### NATS Cluster
- 3-node cluster for high availability
- Handles message routing between WebSocket servers
- Channel per active chat session: `chat.<session_id>`
- Fire-and-forget delivery (at-most-once) -- acceptable for ephemeral chat

#### Redis Cluster
- 3+ master nodes with replicas
- Stores: matching queue, active sessions, rate limit counters, user fingerprints
- All keys have TTLs -- data auto-expires
- Sorted sets for matching, hashes for sessions, strings for rate limits

#### HAProxy
- Layer 7 load balancing with WebSocket awareness
- Cookie-based sticky sessions (`SERVERID` cookie)
- Health checks on WebSocket endpoints
- Connection draining for graceful deployments

#### Moderation Service (Go)
- Receives flagged messages asynchronously via NATS
- Runs keyword/pattern matching locally (zero latency)
- Optionally calls Perspective API for toxicity scoring on escalated content
- Updates Redis ban/throttle records

### 2.3 Data Flow: Complete User Journey

#### Flow 1: Matching

```
User A opens app
    |
    v
[1] Browser connects to WSS endpoint via HAProxy
    HAProxy assigns sticky session cookie, routes to WS Server 2
    |
    v
[2] WS Server 2 generates session_id (UUID v4), stores in Redis:
    SET session:<sid> {status: "idle", interests: [], server: "ws-2"} EX 3600
    |
    v
[3] User A selects interests: ["music", "gaming", "anime"]
    Sends: {type: "find_match", interests: ["music", "gaming", "anime"]}
    |
    v
[4] WS Server 2 publishes to Matching Service via NATS:
    Subject: match.request
    Payload: {session_id: "abc", interests: ["music","gaming","anime"]}
    |
    v
[5] Matching Service receives request:
    a) Compute interest hash: sort(interests) -> "anime|gaming|music" -> hash
    b) Check Redis SET "match:exact:<hash>" for waiting users
    c) If found: POP one, create match
    d) If not: Check individual interest sets (match:interest:music, etc.)
    e) If found: Score by overlap count, pick best match
    f) If not: Add user to all relevant sets, set 30s timeout
    |
    v
[6] Match found! User A matched with User B.
    Matching Service:
    a) Creates chat session in Redis:
       HSET chat:<chat_id> {user_a: "abc", user_b: "def", created: <ts>} EX 7200
    b) Removes both users from all matching queues
    c) Publishes via NATS:
       Subject: match.found.<session_id_a> -> {chat_id, partner: "def"}
       Subject: match.found.<session_id_b> -> {chat_id, partner: "abc"}
    |
    v
[7] Both WS Servers receive match notification, push to clients:
    {type: "match_found", chat_id: "xyz", accept_deadline: 15}
    |
    v
[8] Both users accept within 15 seconds:
    {type: "accept_match", chat_id: "xyz"}
    |
    v
[9] WS Servers subscribe to NATS subject: chat.xyz
    Both users enter chat state.
```

#### Flow 2: Chatting

```
User A types message: "Hello!"
    |
    v
[1] Browser sends via WebSocket:
    {type: "message", chat_id: "xyz", text: "Hello!"}
    |
    v
[2] WS Server 2 receives message:
    a) Validate: session exists, user is in this chat, rate limit check
    b) Run local keyword filter (< 0.1ms)
    c) Publish to NATS subject: chat.xyz
       Payload: {from: "abc", text: "Hello!", ts: 1709042400}
    |
    v
[3] NATS delivers to ALL subscribers of chat.xyz
    (WS Server 2 for User A, WS Server 5 for User B)
    |
    v
[4] WS Server 5 pushes to User B's WebSocket:
    {type: "message", from: "partner", text: "Hello!", ts: 1709042400}
    |
    v
[5] Message is NOT stored anywhere. It existed only in transit.
```

#### Flow 3: Disconnecting

```
User A clicks "End Chat" or closes browser
    |
    v
[1] WS Server 2 detects disconnect (WebSocket close or TCP timeout)
    |
    v
[2] WS Server 2:
    a) Publishes to NATS: chat.xyz -> {type: "partner_disconnected"}
    b) Unsubscribes from NATS subject: chat.xyz
    c) Deletes Redis keys:
       DEL chat:xyz
       DEL session:abc (or let TTL expire)
    |
    v
[3] WS Server 5 receives partner_disconnected:
    a) Pushes to User B: {type: "partner_left"}
    b) Unsubscribes from chat.xyz
    c) User B can choose to find a new match or leave
    |
    v
[4] All traces of the conversation are gone.
    No messages were ever stored. Chat session metadata deleted.
```

---

## 3. Database & Storage Design

### 3.1 Data Inventory

| Data Type          | Store    | Lifetime         | Why                                    |
|--------------------|----------|------------------|----------------------------------------|
| Session state      | Redis    | 1 hour TTL       | Track user connection and status       |
| Matching queue     | Redis    | 30-60 second TTL | Interest-based user matching           |
| Active chat state  | Redis    | 2 hour TTL       | Track which users are paired           |
| Rate limit counters| Redis    | 1-60 second TTL  | Prevent message flooding               |
| User fingerprints  | Redis    | 24 hour TTL      | Abuse prevention (browser fingerprint) |
| Ban records        | Redis    | 1-24 hour TTL    | Temporary bans for abusive users       |
| Abuse reports      | PostgreSQL| 30 days         | Review and pattern analysis            |
| Chat messages      | NOWHERE  | In-transit only  | Ephemeral by design                    |

### 3.2 Redis Schema

```
# ==========================================
# SESSION MANAGEMENT
# ==========================================

# User session (created on WebSocket connect)
# Key:   session:<session_id>
# Type:  Hash
# TTL:   3600 seconds (1 hour)
# Example:
HSET session:a1b2c3d4 \
    status       "chatting"       \  # idle | matching | chatting
    chat_id      "x9y8z7"        \  # null if not in chat
    server       "ws-server-2"   \  # which WS server holds the connection
    interests    "music,gaming"  \  # comma-separated
    fingerprint  "fp_hash_abc"   \  # browser fingerprint hash
    created_at   "1709042400"    \
    last_active  "1709042450"

EXPIRE session:a1b2c3d4 3600


# ==========================================
# MATCHING QUEUE
# ==========================================

# Exact-match bucket (users with identical interest sets)
# Key:   match:exact:<interests_hash>
# Type:  Set
# TTL:   60 seconds (refreshed on add)
# Example: Users who selected exactly ["anime", "gaming", "music"]
SADD match:exact:sha256_of_anime_gaming_music "session_id_1" "session_id_2"
EXPIRE match:exact:sha256_of_anime_gaming_music 60

# Per-interest waiting pool
# Key:   match:interest:<tag>
# Type:  Set
# TTL:   60 seconds (refreshed on add)
# Example:
SADD match:interest:music "session_id_1" "session_id_3" "session_id_7"
EXPIRE match:interest:music 60

# Global matching queue (ordered by wait time for fairness)
# Key:   match:queue
# Type:  Sorted Set (score = timestamp of entry)
# TTL:   None (members expire via matching service cleanup)
ZADD match:queue 1709042400 "session_id_1"
ZADD match:queue 1709042401 "session_id_2"


# ==========================================
# ACTIVE CHAT SESSIONS
# ==========================================

# Chat session
# Key:   chat:<chat_id>
# Type:  Hash
# TTL:   7200 seconds (2 hours)
HSET chat:x9y8z7 \
    user_a       "session_id_1"  \
    user_b       "session_id_2"  \
    status       "active"        \  # pending_accept | active | ended
    created_at   "1709042400"    \
    accept_deadline "1709042415"    # 15 seconds from creation

EXPIRE chat:x9y8z7 7200


# ==========================================
# RATE LIMITING
# ==========================================

# Message rate limit (sliding window)
# Key:   rate:msg:<session_id>
# Type:  String (counter)
# TTL:   10 seconds
# Rule:  Max 5 messages per 10 seconds
INCR rate:msg:a1b2c3d4
EXPIRE rate:msg:a1b2c3d4 10      # Only set on first INCR

# Match request rate limit
# Key:   rate:match:<fingerprint>
# Type:  String (counter)
# TTL:   60 seconds
# Rule:  Max 10 match requests per minute
INCR rate:match:fp_hash_abc
EXPIRE rate:match:fp_hash_abc 60

# Connection rate limit
# Key:   rate:conn:<ip_hash>
# Type:  String (counter)
# TTL:   60 seconds
# Rule:  Max 5 connections per minute per IP
INCR rate:conn:ip_hash_xyz
EXPIRE rate:conn:ip_hash_xyz 60


# ==========================================
# ABUSE PREVENTION
# ==========================================

# Temporary ban
# Key:   ban:<fingerprint>
# Type:  String (ban reason)
# TTL:   Varies (900 = 15min, 3600 = 1hr, 86400 = 24hr)
SET ban:fp_hash_abc "toxicity:repeated" EX 3600

# Report counter (escalating bans)
# Key:   reports:<fingerprint>
# Type:  String (counter)
# TTL:   86400 seconds (24 hours)
INCR reports:fp_hash_abc
EXPIRE reports:fp_hash_abc 86400
```

### 3.3 PostgreSQL Schema (Reports Only)

```sql
-- Only table in PostgreSQL. Low write volume.
CREATE TABLE abuse_reports (
    id              BIGSERIAL PRIMARY KEY,
    reporter_fp     VARCHAR(64) NOT NULL,   -- fingerprint hash of reporter
    reported_fp     VARCHAR(64) NOT NULL,   -- fingerprint hash of reported user
    chat_id         VARCHAR(36) NOT NULL,   -- UUID of the chat session
    reason          VARCHAR(50) NOT NULL,   -- 'harassment', 'spam', 'explicit', 'other'
    message_excerpt TEXT,                   -- optional: last few messages for context
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- Index for pattern detection queries
    -- "Has this fingerprint been reported multiple times?"
    INDEX idx_reported_fp (reported_fp),
    INDEX idx_created_at (created_at)
);

-- Auto-cleanup: delete reports older than 30 days
-- Run via pg_cron or application-level cron
DELETE FROM abuse_reports WHERE created_at < NOW() - INTERVAL '30 days';
```

### 3.4 Caching Strategy

There is no separate caching layer. Redis IS the primary data store for all ephemeral data. This is by design:

- **No cache invalidation problem**: Data has TTLs and is authoritative in Redis
- **No cache-aside pattern**: Redis is not caching a database; it IS the database for ephemeral state
- **Static assets**: Cached at Cloudflare CDN edge (HTML, JS, CSS, images)
- **No API responses to cache**: All communication is WebSocket (stateful, not cacheable)

---

## 4. Infrastructure & Scale Strategy

### 4.1 Handling 1M Concurrent WebSocket Connections

#### Capacity Planning

```
Target:          1,000,000 concurrent WebSocket connections
Per-server cap:  50,000 connections (conservative, proven safe)
Servers needed:  20 WebSocket server instances (with 25% headroom)

Memory per server:
  - 50,000 connections x 5KB (gobwas+epoll) = 250MB for connections
  - Application overhead                     = 250MB
  - Total per server                         = ~512MB

CPU per server:
  - epoll is I/O efficient; CPU mostly idle for idle connections
  - Message processing: ~10% CPU at typical chat load
  - Recommended: 4 vCPU per instance

Server spec:   4 vCPU, 1GB RAM (e.g., AWS c6g.medium or equivalent)
Total servers: 20-25 WebSocket instances
```

This is validated by the [1M WebSocket Go demonstration](https://www.bomberbot.com/golang/scaling-to-a-million-websockets-with-go/) showing that with epoll optimizations, a single 8-core server with 16GB RAM can handle 1M connections at ~600MB. We choose to distribute across 20 smaller instances for fault isolation.

#### Kernel Tuning (per WebSocket server)

```bash
# /etc/sysctl.d/99-websocket.conf

# Max file descriptors (each connection = 1 fd)
fs.file-max = 200000
fs.nr_open = 200000

# TCP tuning
net.core.somaxconn = 65535
net.ipv4.tcp_max_syn_backlog = 65535
net.core.netdev_max_backlog = 65535

# Ephemeral port range (for outbound connections)
net.ipv4.ip_local_port_range = 1024 65535

# TCP keepalive (detect dead connections faster)
net.ipv4.tcp_keepalive_time = 60
net.ipv4.tcp_keepalive_intvl = 10
net.ipv4.tcp_keepalive_probes = 6

# Reduce TIME_WAIT
net.ipv4.tcp_fin_timeout = 15
net.ipv4.tcp_tw_reuse = 1

# Memory buffers (reduced for WebSocket -- small messages)
net.core.rmem_max = 16384
net.core.wmem_max = 16384
```

```bash
# /etc/security/limits.d/99-websocket.conf
*    soft    nofile    200000
*    hard    nofile    200000
```

### 4.2 Horizontal Scaling Approach

```
                    Component Scaling Strategy
  +------------------------------------------------------------------+
  | Component          | Scaling    | Min | Max  | Trigger            |
  |--------------------|------------|-----|------|--------------------|
  | WS Servers         | Horizontal | 5   | 30   | Connections > 40K  |
  | Matching Service   | Horizontal | 2   | 10   | Queue depth > 1000 |
  | NATS               | Cluster    | 3   | 5    | Manual             |
  | Redis              | Cluster    | 3M  | 6M   | Memory > 70%       |
  | PostgreSQL         | Single     | 1   | 1    | N/A (low traffic)  |
  | HAProxy            | Active-Passive| 2 | 2   | Failover           |
  +------------------------------------------------------------------+
  M = master nodes with replicas
```

**Scaling triggers** (Kubernetes HPA):
- WebSocket Servers: scale when avg connections per pod > 40,000
- Matching Service: scale when Redis `match:queue` sorted set cardinality > 1,000

### 4.3 Load Balancing for WebSocket

#### HAProxy Configuration (Key Excerpts)

```haproxy
global
    maxconn 1000000
    # Increase file descriptor limit
    ulimit-n 2200000

defaults
    mode http
    timeout connect 5s
    timeout client  3600s    # 1 hour -- long-lived WebSocket
    timeout server  3600s
    timeout tunnel  3600s    # Critical for WebSocket tunneling

frontend ws_frontend
    bind *:443 ssl crt /etc/haproxy/certs/whisper.pem

    # Detect WebSocket upgrade
    acl is_websocket hdr(Upgrade) -i websocket

    # Route WebSocket to WS backend
    use_backend ws_servers if is_websocket

    # Route static/API to separate backend
    default_backend api_servers

backend ws_servers
    balance leastconn              # Distribute by fewest connections
    cookie SERVERID insert indirect nocache httponly secure

    option httpchk GET /health
    http-check expect status 200

    server ws1 ws-server-1:8080 check cookie ws1 maxconn 60000
    server ws2 ws-server-2:8080 check cookie ws2 maxconn 60000
    server ws3 ws-server-3:8080 check cookie ws3 maxconn 60000
    # ... dynamically managed by Kubernetes
```

Per [HAProxy's WebSocket documentation](https://www.haproxy.com/blog/websockets-load-balancing-with-haproxy), cookie-based sticky sessions are the most reliable method for WebSocket affinity. The `leastconn` algorithm ensures even distribution of new connections.

**Why sticky sessions?** WebSocket connections are stateful and long-lived. If a client reconnects (e.g., network blip), the cookie routes them back to the same server where their session context exists in memory, avoiding a full re-handshake.

### 4.4 CDN for Static Assets

**Cloudflare Free Tier** provides:
- Global CDN for SvelteKit static build (~200KB gzipped)
- DDoS protection (critical for anonymous apps attracting abuse)
- SSL termination
- WebSocket proxying (Cloudflare supports WebSocket passthrough on free tier)
- Rate limiting rules (additional layer before HAProxy)

Cache strategy:
```
/*.html          -> Cache 1 hour, stale-while-revalidate
/*.js, /*.css    -> Cache 1 year (content-hashed filenames)
/favicon.ico     -> Cache 1 week
/ws              -> No cache (WebSocket upgrade)
```

### 4.5 Deployment Strategy

#### Why Kubernetes (not simpler alternatives)

At 20+ WebSocket server instances + supporting services, manual orchestration becomes untenable. Kubernetes provides:

1. **HPA** (Horizontal Pod Autoscaler) -- auto-scale WebSocket servers based on custom metrics (connection count)
2. **Rolling updates** -- zero-downtime deploys with connection draining
3. **Service discovery** -- NATS and Redis endpoints resolved automatically
4. **Resource limits** -- prevent one service from starving others
5. **Self-healing** -- restart crashed pods automatically

For an unfunded project, **managed Kubernetes** (e.g., DigitalOcean DOKS, Linode LKE, or k3s on bare metal) keeps operational overhead low.

#### Deployment Topology

```yaml
# Kubernetes namespace: whisper
#
# Deployments:
#   - ws-server       (20 replicas, 4 vCPU, 1GB RAM each)
#   - match-service   (3 replicas, 2 vCPU, 512MB RAM each)
#   - mod-service     (2 replicas, 1 vCPU, 256MB RAM each)
#
# StatefulSets:
#   - nats            (3 replicas, 2 vCPU, 512MB RAM each)
#   - redis           (3 masters + 3 replicas, 2 vCPU, 2GB RAM each)
#   - postgresql      (1 primary + 1 replica, 2 vCPU, 1GB RAM)
#
# Services:
#   - ws-service      (ClusterIP, headless for HAProxy discovery)
#   - match-service   (ClusterIP)
#   - nats            (ClusterIP)
#   - redis           (ClusterIP)
#   - postgresql      (ClusterIP)
#
# Ingress:
#   - HAProxy Ingress Controller (DaemonSet on edge nodes)
```

#### Build & Deploy Pipeline

```
GitHub Push -> GitHub Actions:
  1. Run tests (Go: go test, Svelte: vitest)
  2. Build Go binaries (multi-stage Docker: scratch base)
  3. Build SvelteKit (npm run build, output: static)
  4. Push Docker images to registry
  5. Upload static assets to Cloudflare Pages
  6. kubectl rolling update (canary: 1 pod first, then all)
```

Docker images are minimal:
```dockerfile
# Go services: ~15MB (scratch + static binary)
FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o server .

FROM scratch
COPY --from=builder /app/server /server
ENTRYPOINT ["/server"]
```

---

## 5. Matching Algorithm Design

### 5.1 Algorithm Overview

The matching algorithm uses a **tiered approach** with three levels, each progressively relaxing match quality to minimize wait time.

```
Tier 1: EXACT MATCH     (identical interest sets)    -> <1 second
Tier 2: OVERLAP MATCH   (most shared interests)      -> 1-10 seconds
Tier 3: SINGLE MATCH    (at least one shared interest)-> 10-20 seconds
Tier 4: RANDOM MATCH    (fallback, no shared interests)-> 20-30 seconds
Tier 5: TIMEOUT         (no match available)          -> 30 seconds
```

### 5.2 Data Structures

```
Redis structures used by the Matching Service:

1. match:queue (Sorted Set)
   - Score: Unix timestamp (entry time)
   - Member: session_id
   - Purpose: Global queue ordered by wait time for fairness

2. match:exact:<hash> (Set)
   - Members: session_ids with identical interest sets
   - Hash: SHA-256(sorted, lowercased, pipe-joined interests)
   - Purpose: O(1) exact-match lookup

3. match:interest:<tag> (Set)
   - Members: session_ids interested in this tag
   - Purpose: Find partial overlaps

4. match:session:<session_id> (Hash)
   - Fields: interests (comma-sep), entered_at, tier
   - Purpose: Track individual user's match state
```

### 5.3 Algorithm Pseudocode

```go
func MatchUser(sessionID string, interests []string) {
    // Step 0: Rate limit check
    if isRateLimited(sessionID) {
        return ErrRateLimited
    }

    // Step 1: Compute exact-match hash
    sort.Strings(interests)
    exactHash := sha256(strings.Join(interests, "|"))

    // Step 2: Try Tier 1 -- Exact Match (O(1))
    candidate := redis.SPop("match:exact:" + exactHash)
    if candidate != "" && isStillWaiting(candidate) {
        createMatch(sessionID, candidate, interests)
        return
    }

    // Step 3: Try Tier 2 -- Best Overlap Match (O(K * M))
    // K = number of interests, M = avg pool size per interest
    overlapScores := map[string]int{}
    for _, interest := range interests {
        members := redis.SMembers("match:interest:" + interest)
        for _, member := range members {
            if member != sessionID {
                overlapScores[member]++
            }
        }
    }
    if bestMatch := findBestCandidate(overlapScores, len(interests)); bestMatch != "" {
        createMatch(sessionID, bestMatch, interests)
        return
    }

    // Step 4: No immediate match -- enter queues and wait
    redis.SAdd("match:exact:"+exactHash, sessionID)
    redis.Expire("match:exact:"+exactHash, 60)
    for _, interest := range interests {
        redis.SAdd("match:interest:"+interest, sessionID)
        redis.Expire("match:interest:"+interest, 60)
    }
    redis.ZAdd("match:queue", time.Now().Unix(), sessionID)

    // Step 5: Background matcher will re-check periodically
    // If still no match after 30s, notify user of timeout
}

func findBestCandidate(scores map[string]int, maxInterests int) string {
    bestID := ""
    bestScore := 0
    for id, score := range scores {
        if score > bestScore && isStillWaiting(id) {
            bestScore = score
            bestID = id
        }
    }
    // Only return if overlap is meaningful
    if bestScore >= 1 {
        return bestID
    }
    return ""
}

// Background worker: runs every 2 seconds
func BackgroundMatcher() {
    for {
        // Get users waiting > 20 seconds (Tier 4: random match)
        longWaiters := redis.ZRangeByScore("match:queue", 0, time.Now().Unix()-20)

        // Pair them with each other regardless of interests
        for i := 0; i+1 < len(longWaiters); i += 2 {
            createMatch(longWaiters[i], longWaiters[i+1], nil)
        }

        // Get users waiting > 30 seconds (Tier 5: timeout)
        expired := redis.ZRangeByScore("match:queue", 0, time.Now().Unix()-30)
        for _, sid := range expired {
            notifyTimeout(sid)
            removeFromAllQueues(sid)
        }

        time.Sleep(2 * time.Second)
    }
}
```

### 5.4 Latency Analysis

| Operation                            | Complexity       | Expected Latency |
|--------------------------------------|------------------|------------------|
| Exact-match lookup (SPOP)            | O(1)             | < 0.5ms          |
| Per-interest member scan (SMEMBERS)  | O(M) per interest| < 2ms for M=1000 |
| Overlap scoring (K interests)        | O(K * M)         | < 10ms           |
| Full match cycle (all tiers)         | O(K * M)         | < 15ms           |
| Background random matching           | O(N)             | < 50ms for N=100 |

At 1M concurrent users with ~200K in matching queue and 50 interests available:
- Average pool size per interest: ~12,000 users (200K / ~16 avg interests)
- K (interests per user): 3-5
- Overlap scoring: 5 * 12,000 = 60,000 comparisons = ~5ms in Go

### 5.5 When No Match Is Found

```
0-1s:   Try exact match (Tier 1)
1-10s:  Re-run overlap matching every 2s (Tier 2)
10-20s: Accept single-interest overlap (Tier 3)
20-30s: Accept random match with anyone waiting (Tier 4)
30s:    Notify user: "No match found. Try different interests or try again."
        Remove from all queues.
        User can retry immediately.
```

The UI shows a searching animation with a countdown. The accept/decline phase adds 15 seconds after a match is found (inspired by HeyMandi).

### 5.6 Accept/Decline Flow

```
Match found (both users notified)
    |
    +-- Both accept within 15s --> Chat starts
    |
    +-- One declines --> Declined user returns to queue position
    |                    Decliner gets 5s cooldown before re-queue
    |
    +-- Timeout (15s) --> Both users return to queue
                          Re-match attempted immediately
```

---

## 6. Abuse Prevention

Without user accounts, abuse prevention relies on **layered defenses**:

### 6.1 Defense Layers

```
Layer 1: Rate Limiting (Redis)
    - 5 messages per 10 seconds per session
    - 10 match requests per minute per fingerprint
    - 5 WebSocket connections per minute per IP
    - Sliding window algorithm via Redis INCR + EXPIRE

Layer 2: Content Filtering (Local, in-process)
    - Keyword blocklist (~500 terms, loaded at startup)
    - Regex patterns for common spam (URLs, phone numbers)
    - Zero added latency (in-memory string matching)
    - Action: message blocked, user warned

Layer 3: Behavioral Analysis (Matching Service)
    - Track: messages-per-chat, avg-chat-duration, skip-rate
    - Flag users who: send 50+ messages in first 30 seconds,
      skip 10+ matches in 5 minutes, get reported 3+ times in 24h
    - Action: temporary cooldown (5 minutes)

Layer 4: Browser Fingerprinting (Client-side)
    - FingerprintJS open-source library
    - Canvas fingerprint + WebGL + timezone + language + screen
    - Purpose: identify returning abusers after session reset
    - NOT for tracking users -- only for ban enforcement
    - Hash stored in Redis with TTL, never in PostgreSQL

Layer 5: Perspective API (Async, escalated only)
    - Only called for messages flagged by Layer 2 or 3
    - Toxicity score > 0.85 -> auto-warn
    - 3 warns in one session -> auto-kick + 15min ban on fingerprint
    - Free tier: 1 QPS (sufficient for escalated-only usage)

Layer 6: User Reporting
    - One-click "Report" button during chat
    - Captures: reporter fingerprint, reported fingerprint, reason,
      last 5 messages (from in-memory buffer, NOT from storage)
    - Stored in PostgreSQL for 30 days
    - 3 reports against same fingerprint in 24h -> auto-ban 1 hour

Layer 7: Connection-Level Protection
    - Cloudflare DDoS protection (free tier)
    - HAProxy connection rate limiting
    - WebSocket frame size limit (4KB max per message)
    - Ping/pong heartbeat (30s interval, 10s timeout)
```

### 6.2 Escalating Ban Durations

```
1st offense:   15-minute ban (fingerprint-based)
2nd offense:   1-hour ban
3rd offense:   24-hour ban
4th+ offense:  24-hour ban (no permanent bans -- fingerprints rotate)

Ban record: SET ban:<fingerprint> "reason" EX <duration>
On connect: CHECK EXISTS ban:<fingerprint>
```

---

## 7. Risks & Mitigations

### 7.1 Technical Risks

| Risk | Severity | Likelihood | Mitigation |
|------|----------|------------|------------|
| **Single server failure drops 50K users** | High | Medium | Graceful connection draining on shutdown. Client auto-reconnect with exponential backoff. HAProxy health checks remove unhealthy nodes in <5s. |
| **Redis cluster failure** | Critical | Low | Redis Cluster with 3 masters + 3 replicas. AOF persistence disabled (ephemeral data). If Redis dies, users can reconnect -- no data loss by design. |
| **NATS cluster failure** | High | Low | 3-node NATS cluster with auto-failover. If NATS dies, messages stop flowing but connections survive. Users see "connection issue" and retry. |
| **Matching queue grows unbounded** | Medium | Medium | 30-second TTL on all queue entries. Background cleaner removes stale entries every 5s. Max queue size enforced (reject with "try again later" at 500K). |
| **Memory leak in long-lived connections** | Medium | Medium | Go's garbage collector handles this well. Per-connection memory tracked via Prometheus. Alert at >10KB/conn average. Weekly rolling restart of WS pods. |
| **WebSocket connection storms after deploy** | High | High | Rolling deploy (1 pod at a time). Connection draining period of 30s. Client reconnects with jitter (random 0-5s delay). HAProxy rate limits new connections. |

### 7.2 Product Risks

| Risk | Severity | Likelihood | Mitigation |
|------|----------|------------|------------|
| **Platform used for illegal activity** | Critical | High | Content filtering, Perspective API, reporting system, IP-based rate limiting. Terms of service displayed on landing page. Cooperation with law enforcement if legally required (though minimal data to provide). |
| **Bots flood the matching queue** | High | High | Browser fingerprinting, connection rate limiting, CAPTCHA challenge after suspicious behavior (e.g., 10+ match requests in 1 minute). |
| **Low user count makes matching slow** | Medium | High (at launch) | Tier 4 random matching after 20s. Reduce interest granularity (fewer categories). Show "X users online" to set expectations. Consider "lobby" mode where users chat in a group until 1-on-1 match is available. |
| **Users circumvent bans easily** | Medium | High | Fingerprinting is imperfect -- accept this. Layer defenses so that circumventing one layer (clearing cookies) still leaves others (fingerprint, IP rate limit). The goal is to make abuse annoying, not impossible. |
| **Cost spirals at scale** | Medium | Medium | All infrastructure choices optimize for cost: Go's low memory = fewer servers, NATS's low overhead = small instances, Cloudflare free tier for CDN/DDoS, Redis for everything ephemeral (no expensive DB). Estimated monthly cost at 1M concurrent: ~$2,000-4,000 on budget cloud providers. |

### 7.3 Scaling Risk Matrix

```
Users         WS Servers  Redis Nodes  NATS Nodes  Est. Monthly Cost
------------- ----------- ------------ ----------- ------------------
10K           2           1 (single)   1 (single)  $50-100
100K          5           3 (cluster)  3 (cluster) $300-600
500K          12          3 (cluster)  3 (cluster) $1,000-2,000
1,000,000     25          6 (cluster)  3 (cluster) $2,500-4,000
2,000,000     50          9 (cluster)  5 (cluster) $5,000-8,000
```

---

## Appendix A: Protocol Specification (WebSocket Messages)

```jsonc
// Client -> Server
{"type": "find_match", "interests": ["music", "gaming", "anime"]}
{"type": "cancel_match"}
{"type": "accept_match", "chat_id": "uuid"}
{"type": "decline_match", "chat_id": "uuid"}
{"type": "message", "chat_id": "uuid", "text": "Hello!"}
{"type": "typing", "chat_id": "uuid", "is_typing": true}
{"type": "end_chat", "chat_id": "uuid"}
{"type": "report", "chat_id": "uuid", "reason": "harassment"}
{"type": "ping"}

// Server -> Client
{"type": "session_created", "session_id": "uuid"}
{"type": "matching_started", "timeout": 30}
{"type": "match_found", "chat_id": "uuid", "shared_interests": ["music", "gaming"], "accept_deadline": 15}
{"type": "match_accepted", "chat_id": "uuid"}
{"type": "match_declined"}
{"type": "match_timeout"}
{"type": "message", "from": "partner", "text": "Hello!", "ts": 1709042400}
{"type": "typing", "is_typing": true}
{"type": "partner_left"}
{"type": "rate_limited", "retry_after": 5}
{"type": "banned", "duration": 900, "reason": "policy_violation"}
{"type": "error", "code": "invalid_message", "message": "Message too long"}
{"type": "pong"}
```

## Appendix B: Interest Tags (Initial Set)

```
Categories (8 groups, ~50 tags total):

Technology:  programming, gaming, ai, crypto, gadgets, cybersecurity
Entertainment: movies, music, anime, books, tv-shows, podcasts, comics
Lifestyle:   fitness, cooking, travel, fashion, photography, gardening
Sports:      football, basketball, cricket, esports, f1, mma
Creative:    art, writing, design, filmmaking, music-production
Academic:    science, philosophy, history, psychology, mathematics, languages
Social:      relationships, career, mental-health, parenting, pets
Random:      memes, conspiracy-theories, shower-thoughts, unpopular-opinions, debate
```

Users select 1-5 interests from this list. The list can be expanded over time without algorithm changes.

---

## Appendix C: Key Research Sources

- [A Million WebSockets and Go (Mail.ru)](https://www.freecodecamp.org/news/million-websockets-and-go-cc58418460bb/)
- [Stressgrid: Benchmarking Go vs Node vs Elixir](https://stressgrid.com/blog/benchmarking_go_vs_node_vs_elixir/)
- [Hashrocket WebSocket Shootout](https://hashrocket.com/blog/posts/websocket-shootout)
- [Elixir vs Go WebSocket Battle](https://medium.com/beamworld/the-ultimate-websocket-battle-elixir-vs-go-performance-showdown-5edf2)
- [C1000K Servers Benchmark](https://github.com/smallnest/C1000K-Servers)
- [Brave New Geek: Message Queue Latency](https://bravenewgeek.com/benchmarking-message-queue-latency/)
- [NATS vs Redis vs Kafka Comparison](https://www.index.dev/skill-vs-skill/nats-vs-redis-vs-kafka)
- [Message Broker Comparison 2025](https://medium.com/@BuildShift/kafka-is-old-redpanda-is-fast-pulsar-is-weird-nats-is-tiny-which-message-broker-should-you-32ce61d8aa9f)
- [Building Live Chat with Go, NATS, Redis](https://ribice.medium.com/building-a-live-chat-with-go-nats-redis-and-websockets-cb3edbc940ca)
- [SvelteKit vs Next.js (Better Stack)](https://betterstack.com/community/guides/scaling-nodejs/sveltekit-vs-nextjs/)
- [SvelteKit vs Next.js 2026](https://dev.to/paulthedev/sveltekit-vs-nextjs-in-2026-why-the-underdog-is-winning-a-developers-deep-dive-155b)
- [HAProxy WebSocket Load Balancing](https://www.haproxy.com/blog/websockets-load-balancing-with-haproxy)
- [WebSocket Scaling Techniques](https://tsh.io/blog/how-to-scale-websocket)
- [Redis Matchmaking Systems](https://oneuptime.com/blog/post/2026-01-21-redis-matchmaking-systems/view)
- [Abuse Prevention Without Accounts](https://dev.to/vibetalk_51a1a0b171d67095/how-we-designed-abuse-prevention-without-user-accounts-in-an-anonymous-chat-app-5gp5)
- [Epoll: Hero Behind 1M WebSocket Connections](https://slides.com/jalex-chang/epoll-websocket-go/fullscreen)
- [Netflix Timestone Priority Queue](https://www.infoq.com/news/2022/10/netflix-timestone-priority-queue/)
- [Go WebSocket Benchmark](https://dev.to/lxzan/go-websocket-benchmark-31f4)
- [Socket.IO vs uWebSockets Benchmark](https://github.com/ezioda004/benchmark-socketio-uwebsockets)
- [Perspective API](https://perspectiveapi.com/how-it-works/)
