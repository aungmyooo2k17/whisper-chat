# Whisper Performance Tuning Playbook

Target: **1,000,000 concurrent WebSocket connections** on a single logical deployment.

This document covers bottleneck analysis methodology, tuning parameters, scaling
strategies, and expected resource profiles for the Whisper WebSocket chat server.

---

## Table of Contents

1. [Architecture Overview for Performance](#1-architecture-overview-for-performance)
2. [Bottleneck Identification Guide](#2-bottleneck-identification-guide)
3. [Tuning Parameters Reference](#3-tuning-parameters-reference)
4. [Scaling Strategies](#4-scaling-strategies)
5. [Monitoring During Load Tests](#5-monitoring-during-load-tests)
6. [Known Limitations](#6-known-limitations)

---

## 1. Architecture Overview for Performance

### 1.1 Connection Data Path

```
Client (browser)
  |
  v
HAProxy (leastconn + sticky SERVERID cookie)
  |
  v
wsserver (gobwas/ws upgrade, epoll registration)
  |
  +---> Epoll event loop (Wait -> dispatch to worker pool)
  +---> Redis (session state, matching queue, rate limiting, chat state)
  +---> NATS  (match events, chat messages, moderation)
  +---> PostgreSQL (abuse reports only, low QPS)
```

The wsserver process uses an **epoll-based event loop** (`internal/ws/epoll.go`)
rather than one-goroutine-per-connection. When epoll reports a file descriptor is
ready, a worker goroutine from a bounded pool (default 256) reads the frame,
processes it, and returns. This means only active connections consume goroutine
stack memory, while idle connections cost only their fd + kernel socket buffers +
in-memory Connection struct.

### 1.2 Per-Connection Memory Budget

Each WebSocket connection consumes memory across three layers:

| Component | Bytes per connection | Notes |
|---|---|---|
| **Kernel TCP buffers** | ~16 KB | `tcp_rmem` 8 KB + `tcp_wmem` 8 KB (from `sysctl-whisper.conf`) |
| **File descriptor** | ~1 KB | Kernel `struct socket` + `struct file` overhead |
| **Epoll registration** | ~160 B | `epoll_event` entry in the epoll interest list |
| **`Connection` struct** | ~200 B | `ID` (UUID string 36 B), `net.Conn` interface, `Fd` int, timestamps, `sync.Mutex`, `processing` flag |
| **`ConnectionManager` map entries** | ~128 B | Two Go map entries (`byID` + `byFd`) with pointer overhead |
| **Redis session hash** | ~300 B (remote) | 8 fields in `session:<id>` hash; memory is on the Redis server, not the Go process |
| **NATS subscriptions** | ~200 B per sub | Active only while matching or chatting (2-4 subs per active user) |
| **gobwas/ws overhead** | ~0 B steady-state | gobwas/ws is zero-alloc after the upgrade; no per-connection buffers are retained |
| **Buffer pool** | ~4 KB (shared) | `sync.Pool` of 4 KB read buffers, reused across all connections |

**Effective per-connection cost on the Go process: ~1.5 KB idle, ~5.5 KB active**
(active = matched in a chat with NATS subscriptions and occasional read buffer use).

**Kernel-side per-connection cost: ~17 KB** (TCP buffers + fd + epoll).

### 1.3 Memory Formula

```
Total Memory = base_overhead + (connections x per_conn_bytes)

Where:
  base_overhead    =  ~200 MB  (Go runtime, binary, static data, Redis/NATS clients)
  per_conn_bytes   =  ~20 KB   (kernel ~17 KB + Go process ~1.5-5.5 KB)

Examples:
  10K   connections:  200 MB + (10,000   x 20 KB) =   ~0.4 GB
  100K  connections:  200 MB + (100,000  x 20 KB) =   ~2.2 GB
  500K  connections:  200 MB + (500,000  x 20 KB) =  ~10.0 GB
  1M    connections:  200 MB + (1,000,000 x 20 KB) = ~20.0 GB
```

The benchmark thresholds in `loadtest/benchmarks/thresholds.yaml` allocate
additional headroom for GC overhead, Redis memory, and NATS buffers:

| Tier | Connections | Expected Server Memory | Expected CPU Cores |
|------|-------------|------------------------|--------------------|
| 10K | 10,000 | 2 GB | 2 |
| 100K | 100,000 | 8 GB | 4 |
| 500K | 500,000 | 24 GB | 8 |
| 1M | 1,000,000 | 48 GB | 16 |

The 48 GB at 1M includes: ~20 GB for connections, ~6-8 GB for Redis (collocated
or remote), ~4-6 GB Go heap with GC headroom, ~2-4 GB NATS, and OS/kernel
overhead.

### 1.4 CPU Profile Breakdown

At steady state with 1M connections where ~10% are actively chatting:

| CPU Consumer | Estimated Share | Description |
|---|---|---|
| **Epoll event loop** | 5-10% | `EpollWait` syscall + connection dispatch; very efficient |
| **Worker goroutines** (frame read/write) | 30-40% | `wsutil.NextReader`, `wsutil.WriteServerMessage`, JSON marshal/unmarshal |
| **Redis RTT** (session/chat/queue ops) | 15-25% | Pipelining reduces this; each message touches Redis 2-4 times |
| **NATS publish/subscribe** | 10-15% | Per-message NATS publish + subscription callback dispatch |
| **GC (garbage collection)** | 5-15% | Depends on `GOGC`/`GOMEMLIMIT`; allocations from JSON encoding and map operations |
| **Heartbeat cycle** | 2-5% | Every 30s, iterates all connections for ping/timeout check |
| **Kernel networking** | 5-10% | TCP stack, interrupt handling, socket operations |

---

## 2. Bottleneck Identification Guide

### 2.1 CPU Bottlenecks

#### How to Profile

```bash
# 1. Enable pprof endpoint (already available via /metrics handler in production).
#    For dedicated pprof, add to cmd/wsserver/main.go:
#    import _ "net/http/pprof"
#    go http.ListenAndServe(":6060", nil)

# 2. Capture a 30-second CPU profile during load test:
go tool pprof -http=:8081 http://wsserver:6060/debug/pprof/profile?seconds=30

# 3. Capture goroutine profile to detect contention:
go tool pprof http://wsserver:6060/debug/pprof/goroutine

# 4. Capture mutex contention profile:
go tool pprof http://wsserver:6060/debug/pprof/mutex

# 5. Capture blocking profile (time goroutines spend waiting):
go tool pprof http://wsserver:6060/debug/pprof/block
```

#### What to Look For

**GC Pressure**
- In the CPU flame graph, look for `runtime.gcBgMarkWorker`, `runtime.mallocgc`,
  and `runtime.scanobject`. If these collectively exceed 15% of total CPU, GC is
  a bottleneck.
- Check `go_gc_duration_seconds` Prometheus metric. If p99 GC pause > 10ms at
  scale, increase `GOGC` or set `GOMEMLIMIT`.
- Common allocation hotspots in Whisper:
  - `json.Marshal` / `json.Unmarshal` in message handlers
  - `make([]net.Conn, 0, n)` in `Epoll.Wait()` (called on every epoll cycle)
  - `make([]byte, header.Length)` in `handleConn` for each incoming frame

**Lock Contention**
- The `ConnectionManager` uses a single `sync.RWMutex` for `byID`/`byFd` maps.
  At 1M connections, if the write rate is high (many connects/disconnects per
  second), this can become a bottleneck. Look for `sync.(*RWMutex).Lock` or
  `sync.(*RWMutex).RLock` in the mutex profile.
- The `Epoll.mu` RWMutex protects the fd-to-connection map and is held during
  both `Add`/`Remove` (write) and `Wait` (read).
- Per-connection `writeMu` serializes writes; this is fine unless a connection
  receives high fan-out writes.

**Syscall Overhead**
- Look for `syscall.EpollWait`, `syscall.EpollCtl`, `syscall.Read`,
  `syscall.Write` in the profile. These are expected but should not dominate.
- If `EpollCtl` is a bottleneck, it indicates excessive connection churn
  (connect/disconnect rate > 10K/sec).

### 2.2 Memory Bottlenecks

#### How to Identify

```bash
# Live memory stats via pprof:
go tool pprof http://wsserver:6060/debug/pprof/heap

# Programmatic check via runtime.MemStats:
curl http://wsserver:8080/debug/memstats  # (if exposed)
```

#### Key `runtime.MemStats` Fields

| Field | What It Tells You |
|---|---|
| `HeapAlloc` | Bytes of allocated heap objects (the "live" heap) |
| `HeapSys` | Bytes of heap memory obtained from the OS |
| `HeapInuse` | Bytes in in-use heap spans (closest to actual RSS) |
| `HeapIdle` | Bytes in idle (unused) heap spans (can be returned to OS) |
| `StackInuse` | Bytes of goroutine stacks (grows with goroutine count) |
| `NumGC` | Total number of completed GC cycles |
| `PauseTotalNs` | Cumulative GC pause time |
| `GCSys` | Memory used by the GC itself |

#### Expected Heap Growth Patterns

1. **Ramp phase** (0 to target connections): Heap grows linearly with
   connection count. Each connection creates a `Connection` struct and two map
   entries. Expect ~1.5 KB heap per idle connection.

2. **Steady state** (all connections established): Heap stabilizes. GC should
   reclaim temporary allocations (JSON buffers, epoll event slices) each cycle.
   If heap continues to grow, suspect a goroutine leak (check `go_goroutines`
   metric).

3. **Chat activity phase**: Heap fluctuates as messages are processed. Each
   message allocates a `[]byte` for the frame payload and JSON marshal buffers.
   The `sync.Pool` for read buffers limits steady-state allocations.

4. **Churn phase** (high connect/disconnect): Heap spikes due to map resizing
   and deferred cleanup. The Go runtime does not shrink maps, so the
   `ConnectionManager` maps may retain capacity from peak connection count.

#### GC Tuning

```bash
# Reduce GC frequency (default GOGC=100 means GC when heap doubles).
# GOGC=200 means GC when heap triples the live set.
export GOGC=200

# Hard memory limit (Go 1.19+). Prevents OOM by forcing GC when approaching limit.
# Set to ~80% of available RAM to leave room for kernel buffers.
export GOMEMLIMIT=38GiB  # for a 48 GB machine at the 1M tier

# Combine both: high GOGC for fewer GC cycles, GOMEMLIMIT as safety net.
export GOGC=400
export GOMEMLIMIT=38GiB
```

**When to use `GOGC` vs `GOMEMLIMIT`:**
- `GOGC` alone: simple, but at high heap sizes the live-set-to-garbage ratio
  changes and GC can be either too frequent or too rare.
- `GOMEMLIMIT` alone (`GOGC=off`): GC only runs when approaching the limit.
  Risk: if allocation rate is very high, GC thrashing can occur near the limit.
- **Recommended**: Set both. `GOGC=200-400` for normal operations, `GOMEMLIMIT`
  as a ceiling. The runtime uses whichever triggers first.

### 2.3 Network Bottlenecks

#### File Descriptor Exhaustion

```bash
# Check current fd usage:
ls /proc/$(pgrep wsserver)/fd | wc -l

# Check system-wide:
cat /proc/sys/fs/file-nr
# Output: allocated   free   max

# Check per-process limits:
cat /proc/$(pgrep wsserver)/limits | grep "open files"
```

**Symptoms**: `accept` returns `EMFILE` or `ENFILE`. New connections fail with
"too many open files". The server logs `ws: epoll add failed`.

**Fix**: Ensure `scripts/sysctl-whisper.conf` is applied (`fs.file-max = 2097152`)
and `scripts/limits-whisper.conf` sets `nofile = 1048576`. For Docker, pass
`--ulimit nofile=1048576:1048576`.

#### TCP Buffer Tuning

The sysctl config (`scripts/sysctl-whisper.conf`) sets deliberately small default
TCP buffers to minimize per-connection kernel memory:

```
net.ipv4.tcp_rmem = 4096 8192 16384
net.ipv4.tcp_wmem = 4096 8192 16384
```

At 1M connections with 8 KB default buffers each way, kernel TCP buffer memory is:
`1,000,000 x 16 KB = ~16 GB`. This is the single largest memory consumer.

If messages are larger than expected or throughput is insufficient, increase the
default (second value), but be aware of the linear memory impact.

#### Ephemeral Port Exhaustion (Load Test Client)

Each load test client connection to a single server IP:port consumes one
ephemeral port on the client machine. The default Linux range is 32768-60999
(~28K ports).

```bash
# The sysctl config expands this:
net.ipv4.ip_local_port_range = 1024 65535  # ~64K ports

# For >64K connections to a single destination, use multiple source IPs:
# Each source IP provides ~64K additional ports.
# 1M connections requires 16+ source IPs.

# Check current ephemeral port usage:
ss -s
ss -tn | awk '{print $4}' | cut -d: -f1 | sort | uniq -c | sort -rn
```

### 2.4 Redis Bottlenecks

Redis is the most critical external dependency. Every connection lifecycle event
(create, update, delete) and every message (rate limit check, chat lookup)
touches Redis.

#### Connection Pooling

The `go-redis/v9` client uses a connection pool internally. Default pool size is
`10 * runtime.GOMAXPROCS`. For 16 cores, that is 160 connections.

```go
// In internal/session/store.go, the client is created with defaults:
redis.NewClient(&redis.Options{
    Addr: redisAddr,
    // PoolSize defaults to 10 * GOMAXPROCS
})
```

At high QPS, increase the pool size:

```go
redis.NewClient(&redis.Options{
    Addr:     redisAddr,
    PoolSize: 500,        // increase for 1M tier
    MinIdleConns: 100,    // keep warm connections ready
})
```

#### Pipeline Batching

The codebase already uses pipelines in critical paths:
- `session.Store.Create()` pipelines `HSET` + `EXPIRE`
- `matching.Queue.Enqueue()` pipelines sorted set, set, and hash operations

For the matching loop (`service.go:processQueue`), each iteration calls
`GetAllQueued` (ZRANGE) followed by per-user `IsQueued` + `GetEntry`. At scale
(100K+ queue), this becomes O(n) Redis round trips per 2-second tick.

**Optimization**: Batch `GetEntry` calls into a pipeline or use Lua scripts to
process multiple queue entries atomically.

#### SLOWLOG Analysis

```bash
# Enable slow log (commands taking > 10ms):
redis-cli CONFIG SET slowlog-log-slower-than 10000

# Check slow commands:
redis-cli SLOWLOG GET 20

# Common slow operations at scale:
# - ZRANGE match:queue 0 -1       (full queue scan)
# - SMEMBERS match:exact:<hash>   (large interest groups)
# - KEYS match:*                  (never use in production; use SCAN)
```

**Key Redis metrics to monitor**:
- `redis_connected_clients` (should stay below `maxclients`)
- `redis_used_memory` / `redis_used_memory_rss`
- `redis_instantaneous_ops_per_sec`
- `redis_keyspace_hits` / `redis_keyspace_misses`
- `redis_latest_fork_usec` (if persistence is enabled)

### 2.5 NATS Bottlenecks

NATS handles all inter-service messaging: match requests, match results, chat
messages, moderation events.

#### Slow Consumers

A slow consumer occurs when a subscriber cannot keep up with the message rate.
NATS will drop messages for the subscriber after the pending message/byte limit
is reached.

```bash
# Check for slow consumers via NATS monitoring endpoint:
curl http://nats:8222/connz?subs=true | jq '.connections[] | select(.slow_consumer == true)'

# Or via nats CLI:
nats server report connections --sort=pending
```

**Symptoms**: Users report missing messages. `nats_client_slow_consumer_count`
Prometheus metric increases.

**Fix**: Increase subscriber pending limits or add more subscriber instances.

#### Subject Fan-out

Whisper uses subject-per-entity patterns:
- `chat.<chat_id>` -- both participants subscribe
- `match.found.<session_id>` -- one subscriber per session
- `match.notify.<session_id>` -- one subscriber per session
- `moderation.result.<session_id>` -- one subscriber per session

At 500K active chats (1M connections), there are 500K distinct `chat.*` subjects.
NATS handles this efficiently (subjects are just trie lookups), but the total
subscription count matters:

```
Per actively-chatting user: ~4 NATS subscriptions
  (chat, match.found, match.notify, moderation.result)

1M connections, 50% chatting: ~2M active subscriptions
```

NATS can handle millions of subscriptions, but monitor `nats_subscriptions_total`.

#### Message Queue Depth

```bash
# Monitor pending messages per connection:
curl http://nats:8222/connz?subs=true | jq '.connections[] | {name, pending_bytes, pending_count}'

# Via Prometheus (if NATS exporter is configured):
# nats_server_total_msg_count
# nats_server_slow_consumers
```

---

## 3. Tuning Parameters Reference

### 3.1 Go Runtime

| Parameter | Default | Recommended (1M) | Effect | Risk |
|---|---|---|---|---|
| `GOGC` | `100` | `200-400` | Reduces GC frequency by allowing heap to grow 2-4x live set before collecting. Fewer GC cycles = less CPU spent on GC = lower tail latency. | Higher peak memory usage. Risk of OOM if not paired with `GOMEMLIMIT`. |
| `GOMEMLIMIT` | unlimited | `38GiB` (on 48 GB host) | Hard ceiling on Go heap. Prevents OOM by triggering aggressive GC near the limit. | GC thrashing if set too low relative to live heap size. Set to 75-80% of available RAM. |
| `GOMAXPROCS` | num CPU cores | Leave at default or set to num cores | Controls number of OS threads for goroutine scheduling. More threads = more parallelism for worker pool. | Over-setting wastes context-switch overhead. Under-setting starves the event loop. |

**Environment variable configuration** (set in Docker Compose or systemd unit):

```bash
GOGC=300
GOMEMLIMIT=38GiB
GOMAXPROCS=16
```

### 3.2 Application Configuration

| Parameter | Env Var | Default | Recommended (1M) | Effect | Risk |
|---|---|---|---|---|---|
| Worker pool size | `WORKER_POOL_SIZE` | `256` | `512-1024` | Max concurrent goroutines reading WebSocket frames from epoll-ready connections. Higher = more parallelism for message processing. | Too high: excessive goroutine scheduling overhead and memory from goroutine stacks (~2-8 KB each). Too low: epoll events queue up, increasing latency. |
| Max connections | `MAX_CONNECTIONS` | `100000` | `1000000` | Hard cap on accepted WebSocket connections. Server returns HTTP 503 when exceeded. | Must match kernel fd limits. Set equal to or slightly below `nofile` limit to leave room for non-socket fds. |
| Read timeout | `READ_TIMEOUT` | `10s` | `10s` | Deadline on WebSocket frame reads. Prevents stale epoll dispatch from blocking a worker forever. | Too short: kills connections during slow network conditions. Too long: ties up worker goroutines. |
| Write timeout | `WRITE_TIMEOUT` | `10s` | `10s` | Deadline on WebSocket frame writes. | Too short: drops messages to slow clients. Too long: accumulates blocked writers. |
| Max frame size | (hardcoded) | `4096` | `4096` | Rejects WebSocket frames larger than 4 KB. Prevents memory exhaustion from oversized messages. | Increase only if legitimate messages exceed 4 KB. |
| Heartbeat interval | (hardcoded) | `30s` | `30s` | How often the server pings all connections and checks for dead peers. | Shorter: faster dead peer detection, more CPU for ping iteration. Longer: slower detection, stale connections linger. |
| Heartbeat timeout | (hardcoded) | `10s` | `10s` | Grace period after heartbeat interval for activity before declaring a connection dead. | Total dead-peer detection time = interval + timeout = 40s. |

### 3.3 Kernel Parameters (from `scripts/sysctl-whisper.conf`)

| Parameter | Default (Linux) | Whisper Setting | Effect | Risk |
|---|---|---|---|---|
| `fs.file-max` | `~100K` | `2097152` | System-wide max open file descriptors. Must exceed total FDs across all processes. | Set too low: connections rejected with `ENFILE`. Virtually no risk setting high. |
| `fs.nr_open` | `1048576` | `2097152` | Per-process max that can be set via `ulimit`. Must be >= desired `nofile`. | Must match `fs.file-max`. |
| `fs.epoll.max_user_watches` | `~400K` | `2097152` | Max epoll watches per user. Each WebSocket in the epoll instance counts as one watch. | Set too low: `epoll_ctl(ADD)` returns `ENOSPC`. |
| `net.core.somaxconn` | `128` | `65535` | Listen backlog queue size. During reconnection storms, this absorbs the burst. | Set too low: connections dropped during bursts (SYN cookies may help). |
| `net.core.netdev_max_backlog` | `1000` | `65535` | Max packets queued when NIC receives faster than kernel processes. | Set too low: packets dropped under burst. |
| `net.ipv4.tcp_rmem` | `4096 131072 6291456` | `4096 8192 16384` | TCP receive buffer: min, default, max. Whisper uses small defaults to save memory at scale. | Small default reduces throughput for large transfers. Fine for Whisper's small chat messages (<4 KB). |
| `net.ipv4.tcp_wmem` | `4096 16384 4194304` | `4096 8192 16384` | TCP send buffer: min, default, max. Same rationale as rmem. | Same as above. |
| `net.ipv4.tcp_mem` | varies | `786432 1048576 1572864` | TCP memory in pages (4 KB each): min (~3 GB), pressure (~4 GB), max (~6 GB). | Set too low: kernel starts dropping connections when TCP memory is exhausted. Tune relative to available RAM. |
| `net.ipv4.tcp_tw_reuse` | `0` | `1` | Allow reuse of TIME_WAIT sockets for new outbound connections. Critical during rolling restarts. | Minor security consideration (recycled socket could receive stale packets). |
| `net.ipv4.tcp_fin_timeout` | `60` | `15` | How long to stay in FIN_WAIT2. Shorter = faster resource release. | Too short on lossy networks: may not complete graceful close. |
| `net.ipv4.tcp_max_orphans` | `~8192` | `262144` | Max TCP sockets not attached to a user file handle. | Set too low: kernel kills orphan sockets during network blips. |
| `net.ipv4.tcp_max_syn_backlog` | `128` | `65535` | Queue of half-open connections (SYN received, waiting for ACK). | Set too low: SYN flood or reconnection burst overwhelms the queue. |
| `net.ipv4.tcp_fastopen` | `0` | `3` | TCP Fast Open for client (1) + server (2). Saves one RTT on connection establishment. | Older middleboxes may drop TFO packets. Safe in modern environments. |
| `net.ipv4.tcp_keepalive_time` | `7200` | `300` | Seconds before first TCP keepalive probe. Lower = faster dead peer detection at the kernel level. | Complements the application-level WebSocket heartbeat. |
| `net.ipv4.tcp_keepalive_intvl` | `75` | `30` | Seconds between keepalive probes. | With `probes=5`: dead peer detected in 300 + (30*5) = 450s. |
| `net.ipv4.tcp_keepalive_probes` | `9` | `5` | Number of keepalive probes before declaring connection dead. | Fewer probes = faster detection but more false positives on lossy networks. |
| `net.ipv4.ip_local_port_range` | `32768 60999` | `1024 65535` | Ephemeral port range for outbound connections. Important for load test clients and server connections to Redis/NATS. | Using ports below 1024 may conflict with well-known services. |
| `net.ipv4.tcp_window_scaling` | `1` | `1` | Required for window sizes > 64 KB. Already default on modern kernels. | No risk leaving enabled. |

### 3.4 PAM Limits (from `scripts/limits-whisper.conf`)

| Parameter | Default | Whisper Setting | Effect | Risk |
|---|---|---|---|---|
| `nofile` (soft + hard) | `1024` | `1048576` | Per-process file descriptor limit. Must be >= target connection count. | Docker: must be set via `--ulimit` flag, not PAM. Systemd: must set `LimitNOFILE` in unit file. |
| `nproc` (soft + hard) | `~4096` | `65535` | Per-user process limit. Go uses goroutines, not OS processes, so this is mainly for helper processes. | Rarely a bottleneck for Go services. |

### 3.5 Redis Configuration

| Parameter | Default | Recommended (1M) | Effect | Risk |
|---|---|---|---|---|
| `maxmemory` | `0` (unlimited) | `8gb` | Hard memory cap for Redis. Prevents Redis from consuming all system RAM. | Eviction policy (`maxmemory-policy`) determines what happens at the limit. Use `allkeys-lru` for Whisper (session data is ephemeral). |
| `maxclients` | `10000` | `20000` | Max simultaneous client connections to Redis. Whisper connects from wsserver, matcher, moderator, and rate limiter. | Set too low: `ERR max number of clients reached`. Each Redis connection uses ~10 KB. |
| `tcp-backlog` | `511` | `65535` | Redis TCP listen backlog. Should match `net.core.somaxconn`. | Set too low: connections dropped during bursts of Redis reconnects. |
| `timeout` | `0` (disabled) | `300` | Seconds of idle time before Redis closes a client connection. | Set too low: kills pooled idle connections, causing reconnection storms. |
| `hz` | `10` | `100` | Redis internal timer frequency. Higher = more responsive expiry and eviction. | Higher CPU usage. At `100`, Redis checks expired keys 100 times/sec instead of 10. |
| `save` | `3600 1 ...` | `""` (disabled) | RDB snapshot persistence. For Whisper, all data is ephemeral (sessions expire in 1h). | Disabling persistence removes `fork()` overhead, which can cause latency spikes proportional to dataset size. |
| `appendonly` | `no` | `no` | AOF persistence. Same rationale as `save`. | Disable for pure-cache usage. |
| `io-threads` | `1` | `4` | Redis 6+ multi-threaded I/O. Helps with high connection counts. | Must also set `io-threads-do-reads yes`. Experimental; test before production. |

### 3.6 NATS Configuration

| Parameter | Default | Recommended (1M) | Effect | Risk |
|---|---|---|---|---|
| `max_connections` | `65536` | `100000` | Max client connections to the NATS server. | Set too low: new subscribers rejected. |
| `max_payload` | `1048576` (1 MB) | `65536` (64 KB) | Max message payload size. Whisper messages are < 4 KB. Lowering this rejects accidental large publishes. | Set too low: legitimate messages rejected. |
| `write_deadline` | `10s` | `10s` | Max time NATS waits for a slow client to accept a write. After this, the client is marked as a slow consumer. | Too short: messages dropped to temporarily slow subscribers. Too long: memory builds up for stalled clients. |
| `max_pending_size` | `67108864` (64 MB) | `134217728` (128 MB) | Max pending bytes per subscriber before slow consumer is triggered. | Higher = more buffering before message loss, but more memory per slow subscriber. |
| `max_subscriptions` | `0` (unlimited) | `0` (unlimited) | Max subscriptions per client connection. Whisper creates per-session subscriptions. | Set a limit only if you need to prevent subscription leaks. |
| JetStream (`--js`) | enabled | enabled | Provides at-least-once delivery for match requests. Used by Whisper's matcher service. | Adds disk I/O overhead. Ensure sufficient disk performance. |

### 3.7 HAProxy Configuration

| Parameter | Current | Recommended (1M) | Effect | Risk |
|---|---|---|---|---|
| `global maxconn` | `100000` | `1100000` | Max concurrent connections HAProxy will accept. | HAProxy uses ~17 KB per connection (similar to the backend). 1.1M x 17 KB = ~18 GB for HAProxy alone. |
| `timeout connect` | `5s` | `5s` | Max time to establish a connection to a backend server. | Too short: failures during backend GC pauses. |
| `timeout client` | `3600s` | `3600s` | Max idle time for a client connection. Set high for long-lived WebSockets. | Too short: HAProxy closes idle WebSocket connections. |
| `timeout server` | `3600s` | `3600s` | Max idle time for a backend connection. Must match `timeout client` for WebSockets. | Must be >= client timeout. |
| `timeout tunnel` | `3600s` | `3600s` | Timeout for tunneled connections (WebSocket upgrade). Governs the actual data transfer phase. | This is the timeout that matters for WebSocket data frames. |
| `timeout http-request` | `10s` | `10s` | Max time to receive a complete HTTP request (including the upgrade). | Too short: slow clients fail to complete the WebSocket handshake. |
| `balance` | `leastconn` | `leastconn` | Distributes connections to the backend with fewest active connections. Ideal for long-lived WebSocket connections. | -- |
| `cookie SERVERID insert` | enabled | enabled | Sticky sessions via cookie. Ensures reconnects go to the same backend. | Stickiness can cause imbalance if one server accumulates connections. |

---

## 4. Scaling Strategies

### 4.1 Horizontal Scaling (Multiple wsserver Instances)

#### Architecture

```
                    HAProxy (leastconn + sticky sessions)
                    /           |            \
                wsserver-1   wsserver-2   wsserver-N
                    \           |            /
                  Redis (shared session + queue state)
                          |
                        NATS (cross-instance messaging)
```

**How it works today**: The `server` field in the Redis session hash records
which wsserver instance owns the connection. NATS subjects include the session
ID, so match notifications and chat messages are routed to the correct instance
regardless of which server the partner is on.

#### Adding More Instances

1. Add wsserver instances to `docker-compose.yml` (or scale via orchestrator).
2. Add corresponding `server` entries to `haproxy.cfg`:

```
backend ws_servers
    balance leastconn
    cookie SERVERID insert indirect nocache
    server ws1 wsserver-1:8080 check cookie ws1
    server ws2 wsserver-2:8080 check cookie ws2
    server ws3 wsserver-3:8080 check cookie ws3
```

3. No code changes required -- NATS and Redis handle cross-instance
   communication transparently.

**Capacity per instance**: A single wsserver on a 16-core, 48 GB machine can
handle ~500K-1M connections. For 1M total with redundancy, use 3-4 instances at
~250K-333K each.

#### Sticky Session Considerations

- HAProxy's `SERVERID` cookie ensures a client reconnects to the same backend.
- If a backend goes down, the cookie is invalid and HAProxy routes to a new
  server. The client must re-establish its session.
- During rolling deploys, drain connections from one server at a time (the
  `server.Shutdown()` method handles this with a 30-second drain timeout).

### 4.2 Vertical Scaling

#### Memory Scaling Per Connection Tier

| Tier | Connections | Min Server RAM | Recommended RAM | CPU Cores |
|------|-------------|----------------|-----------------|-----------|
| 10K | 10,000 | 4 GB | 8 GB | 2-4 |
| 100K | 100,000 | 8 GB | 16 GB | 4-8 |
| 500K | 500,000 | 24 GB | 32 GB | 8-12 |
| 1M | 1,000,000 | 48 GB | 64 GB | 16 |

**Why recommended exceeds minimum**: GC headroom (2x live heap with `GOGC=100`),
Redis memory (if collocated), kernel TCP buffers (which are outside the Go
heap), and burst headroom during reconnection storms.

#### CPU Scaling

CPU scales sub-linearly with connections because idle connections consume no CPU:

- **10K connections, 10% active**: 2 cores sufficient
- **100K connections, 10% active**: 4 cores (10K active messages/sec)
- **1M connections, 10% active**: 16 cores (100K active messages/sec)

The bottleneck shifts from CPU to memory as connections increase. At 1M idle
connections, CPU usage may be only 5-10% (heartbeat + epoll overhead).

### 4.3 Redis Scaling

#### Single Instance (Current Architecture)

The current deployment uses a single Redis 7 instance. This is sufficient up to
~500K connections with:
- ~500K session hashes (~150 MB)
- ~50K matching queue entries (~15 MB)
- Rate limit keys (~20 MB)
- Chat session hashes (~50 MB)

Total Redis memory: ~250-500 MB for 500K connections.

#### When to Consider Redis Cluster

Switch to Redis Cluster when:

1. **Single-threaded CPU becomes a bottleneck**: Redis processes commands on a
   single thread. If `redis_instantaneous_ops_per_sec` exceeds ~100K and
   latency increases, a single instance is saturated.

2. **Memory exceeds single-node capacity**: At 1M connections, Redis needs
   ~500 MB-1 GB. This is well within single-node capacity. Memory is unlikely
   to drive the Cluster decision.

3. **Availability requirements**: Redis Cluster provides automatic failover. If
   Redis downtime is unacceptable, deploy a 3-node Cluster with 1 replica per
   shard.

**Key design consideration**: The matching queue (`match:queue` sorted set,
`match:exact:*` sets, `match:interest:*` sets) uses multi-key operations and Lua
scripts. In Redis Cluster, all keys in a transaction or Lua script must be on
the same shard. Use hash tags (e.g., `{match}:queue`, `{match}:exact:*`) to
ensure matching keys are co-located.

#### Redis Sentinel (Alternative)

For high availability without data sharding, deploy Redis Sentinel with 1 primary
+ 2 replicas + 3 sentinels. This provides automatic failover without the
complexity of Cluster hash slots. Sufficient for 1M connections since memory is
not the bottleneck.

---

## 5. Monitoring During Load Tests

### 5.1 Key Prometheus Queries

The wsserver exposes Prometheus metrics at `/metrics`. The following queries are
critical during load tests:

#### Connection Health

```promql
# Current connection count (should match target tier):
whisper_connections_total

# Connection rate (connections per second):
rate(whisper_connections_total[1m])

# Connection ramp slope (are we on track?):
deriv(whisper_connections_total[5m])
```

#### Message Throughput

```promql
# Messages sent per second:
rate(whisper_messages_total{type="sent"}[1m])

# Messages received per second:
rate(whisper_messages_total{type="received"}[1m])

# Messages blocked by moderation per second:
rate(whisper_messages_total{type="blocked"}[1m])

# Total error rate:
rate(whisper_messages_total{type="blocked"}[1m]) /
  (rate(whisper_messages_total{type="sent"}[1m]) + rate(whisper_messages_total{type="received"}[1m]))
```

#### Latency

```promql
# Message processing latency p50/p95/p99:
histogram_quantile(0.50, rate(whisper_message_latency_seconds_bucket[5m]))
histogram_quantile(0.95, rate(whisper_message_latency_seconds_bucket[5m]))
histogram_quantile(0.99, rate(whisper_message_latency_seconds_bucket[5m]))

# Match duration p50/p95/p99 (time to find a partner):
histogram_quantile(0.50, rate(whisper_match_duration_seconds_bucket[5m]))
histogram_quantile(0.95, rate(whisper_match_duration_seconds_bucket[5m]))
histogram_quantile(0.99, rate(whisper_match_duration_seconds_bucket[5m]))
```

#### Matching

```promql
# Current queue depth:
whisper_match_queue_size

# Active chat pairs:
whisper_active_chats
```

### 5.2 Go Runtime Metrics

These are automatically exposed by the Prometheus Go client library:

```promql
# Goroutine count (should be ~worker_pool_size + small constant, NOT one per connection):
go_goroutines

# Heap in use (live objects):
go_memstats_heap_inuse_bytes

# Total allocated (cumulative, always increasing):
go_memstats_alloc_bytes_total

# GC pause duration p99:
histogram_quantile(0.99, rate(go_gc_duration_seconds_bucket[5m]))

# GC cycles per second:
rate(go_gc_duration_seconds_count[1m])

# Stack memory (goroutine stacks):
go_memstats_stack_inuse_bytes

# Heap objects (count of live allocations):
go_memstats_heap_objects

# Allocation rate (bytes/sec -- indicates GC pressure):
rate(go_memstats_alloc_bytes_total[1m])
```

### 5.3 Grafana Dashboard Panels to Focus On

The load test dashboard (`monitoring/grafana/dashboards/whisper-loadtest.json`)
includes panels for connection ramp, message throughput, and latency. During a
load test, focus on these panels in order:

1. **Connection Ramp** (`whisper_connections_total`): Verify the ramp-up curve
   matches the expected rate. A plateau before the target indicates a bottleneck
   (usually fd exhaustion or client-side port exhaustion).

2. **Memory** (`go_memstats_heap_inuse_bytes` + `process_resident_memory_bytes`):
   Should grow linearly during ramp. A super-linear curve suggests a leak or
   excessive GC overhead. Compare heap inuse vs RSS to check for kernel buffer
   growth.

3. **Goroutine Count** (`go_goroutines`): Should stay roughly constant
   (worker pool size + heartbeat + NATS handlers). If it grows with connections,
   there is a goroutine leak (likely an unsubscribed NATS handler or unclosed
   context).

4. **GC Pause** (`go_gc_duration_seconds`): p99 should stay below 10ms. If it
   exceeds 50ms, increase `GOGC` or set `GOMEMLIMIT`.

5. **Message Latency** (`whisper_message_latency_seconds`): p99 should stay
   within the tier threshold (100ms for 10K, 1000ms for 1M).

6. **Match Queue Depth** (`whisper_match_queue_size`): Should fluctuate near
   zero when the matcher is keeping up. A growing queue indicates the matcher
   cannot process fast enough (likely Redis bottleneck in queue scanning).

### 5.4 Alert-Worthy Conditions During Load Tests

| Condition | Query | Threshold | Action |
|---|---|---|---|
| Connection stall | `deriv(whisper_connections_total[5m]) < 1` | During ramp phase | Check fd limits, client ports, HAProxy maxconn |
| Memory spike | `process_resident_memory_bytes > threshold_gb * 1e9` | Tier-dependent | Check for goroutine leak, reduce GOGC, set GOMEMLIMIT |
| GC thrashing | `rate(go_gc_duration_seconds_count[1m]) > 10` | >10 GC cycles/sec | Increase GOGC, check allocation hotspots |
| Goroutine leak | `go_goroutines > 5000` | Unexpected growth | Check NATS subscription cleanup |
| Match queue backup | `whisper_match_queue_size > 10000` | Growing trend | Scale matcher, optimize Redis queries |
| High error rate | `rate(whisper_messages_total{type="blocked"}[1m]) > 100` | Tier-dependent | Check rate limiter, content filter |

---

## 6. Known Limitations

### 6.1 Single-Server Epoll Limits

A single Linux process can watch up to `fs.epoll.max_user_watches` file
descriptors (set to 2,097,152 in our config). The practical limit for a single
wsserver is ~1M connections because:

- Each connection = 1 fd registered with `epoll_ctl(EPOLL_CTL_ADD)`
- The `Epoll.connections` map (`map[int]net.Conn`) uses ~128 bytes per entry
- At 1M entries, the map itself consumes ~128 MB and has O(1) lookup but
  resizing can cause latency spikes
- The `Epoll.Wait()` function allocates `[]net.Conn` on each call; the initial
  event buffer is 128 events, which is small for 1M connections with high
  activity

**Mitigation**: For beyond 1M, use multiple wsserver instances behind HAProxy.
The epoll event buffer size (128) could be increased for high-activity workloads
to reduce the number of `epoll_wait` syscalls per second.

### 6.2 Redis Single-Threaded Bottleneck

Redis processes all commands on a single thread. The matching queue scan
(`processQueue` in `internal/matching/service.go`) runs every 2 seconds and
issues O(n) Redis commands where n = queue size:

```
For each queued user:
  1. IsQueued     -> ZSCORE   (1 command)
  2. GetEntry     -> HGETALL  (1 command)
  3. TryExactMatch -> SMEMBERS (1 command) + HGETALL per candidate
  ...
```

At 10K queued users, this is ~30K-50K Redis commands every 2 seconds (~15K-25K
ops/sec sustained). Redis can handle 100K+ ops/sec, so this is manageable. At
100K queued users, it approaches the single-thread limit.

**Mitigations**:
- Pipeline batch the queue scan (read all entries in one pipeline call)
- Use Lua scripts to process multiple match candidates server-side
- Shard the matching queue by interest hash if using Redis Cluster
- Enable Redis `io-threads` (v6+) for I/O parallelism (does not help with
  command processing, but reduces I/O overhead)

### 6.3 NATS Message Ordering

NATS guarantees message ordering **per publisher, per subject**. This means:

- Messages on `chat.<chat_id>` from a single wsserver arrive in order
- If two wsserver instances publish to the same `chat.<chat_id>` subject (both
  partners on different servers), ordering is per-publisher only
- For the typical Whisper chat (2 participants, alternating messages), this is
  not an issue because each participant publishes from a single server

**Where ordering matters**:
- `match.found.<session_id>`: Only one publisher (the matcher), so ordering is
  guaranteed
- `chat.<chat_id>`: Two publishers (one per participant), but messages alternate
  so interleaving is natural
- `moderation.result.<session_id>`: One publisher (moderator), ordering
  guaranteed

**Where ordering can break**: If multiple matcher instances race to process the
same queue entry. The current design runs a single matcher, but horizontal
matcher scaling would need distributed locking or partitioned queues.

### 6.4 ConnectionManager Map Sizing

The Go `map` type does not shrink after growing. If the server handles a burst
of 1M connections and then drops to 100K, the `byID` and `byFd` maps in
`ConnectionManager` still occupy memory for 1M entries. This memory is not
released until the process restarts.

**Mitigation**: For servers with highly variable connection counts, consider
periodically rebuilding the maps (create new maps, copy live entries, replace).
This is not currently implemented.

### 6.5 Heartbeat Iteration Cost

The heartbeat check (`checkConnections` in `internal/ws/heartbeat.go`) iterates
over **all** connections every 30 seconds:

```go
for _, c := range server.Connections().All() {
    // Check timeout and send ping
}
```

At 1M connections, `All()` allocates a slice of 1M `*Connection` pointers
(~8 MB) and iterates the entire `byID` map under a read lock. The heartbeat
then sends a WebSocket ping frame to each connection.

Cost at 1M connections per heartbeat cycle:
- Allocation: ~8 MB for the slice
- Lock duration: milliseconds (RLock on map iteration)
- I/O: 1M ping frames = ~1M `syscall.Write` calls over 30s = ~33K writes/sec

**Mitigation**: Shard the heartbeat into batches (e.g., check 100K connections
every 3 seconds instead of 1M every 30 seconds). This spreads I/O load and
reduces peak allocation.

### 6.6 JSON Serialization Overhead

All WebSocket messages use `encoding/json` for serialization. At high message
throughput (100K msg/sec), JSON marshal/unmarshal becomes a significant CPU
consumer due to reflection-based field access and buffer allocation.

**Mitigations** (if profiling confirms this is a bottleneck):
- Switch to `github.com/json-iterator/go` for 2-3x faster JSON processing
- Pre-allocate message templates for common server messages (pong, error, etc.)
- Use `sync.Pool` for JSON encoder/decoder buffers
- Consider binary protocols (Protocol Buffers, MessagePack) for internal
  NATS messaging, keeping JSON only for the WebSocket client interface

### 6.7 Load Test Client Constraints

The load test client (`loadtest/client/client.go`) uses one goroutine per
connection for the read loop. At 1M simulated connections, this means 1M
goroutines on the client machine, each with a ~2 KB minimum stack = ~2 GB just
for goroutine stacks. Combined with client-side TCP buffers, the load test
client needs as much or more RAM than the server.

Client resource requirements from `loadtest/benchmarks/thresholds.yaml`:

| Tier | Client RAM | Client CPU | Source IPs | `ulimit -n` |
|------|------------|------------|------------|-------------|
| 10K | 1 GB | 2 cores | 1 | 12,000 |
| 100K | 6 GB | 4 cores | 2 | 110,000 |
| 500K | 20 GB | 8 cores | 8 | 510,000 |
| 1M | 40 GB | 16 cores | 16 | 1,100,000 |

For 1M connections, the client machine(s) need full kernel tuning
(`sysctl-whisper.conf` + `limits-whisper.conf`) applied identically to the
server.
