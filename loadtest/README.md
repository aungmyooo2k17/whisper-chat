# Whisper Load Testing

Custom Go-based load testing infrastructure for the Whisper anonymous chat application.

## Tool Choice Rationale

We use a **custom Go client** built with `gobwas/ws` instead of off-the-shelf tools like k6 or Artillery:

| Consideration | Custom Go Client | k6 / Artillery |
|---|---|---|
| WebSocket library | Same `gobwas/ws` as the server — zero protocol mismatches | Generic WebSocket clients; subtle framing differences possible |
| Protocol support | Full control over the multi-step handshake (connect, set_fingerprint, find_match, accept_match, message, end_chat) | Requires complex scripting to model stateful flows |
| Concurrency model | Go goroutines: 100K+ connections with minimal memory overhead (~8KB stack per goroutine) | k6 uses a JS event loop; Artillery spawns Node processes |
| Code reuse | Can share protocol constants and message types with the server codebase | No code reuse; message formats duplicated in JS/YAML |
| Metrics | Full control over what to measure and when | Limited to what the tool exposes |

## Test Commands

### Connection Saturation (`saturate`)
Opens N idle WebSocket connections to find the server's connection capacity ceiling.

```bash
go run ./cmd/loadtest saturate \
  -url ws://localhost:8080/ws \
  -connections 10000 \
  -ramp 30s \
  -hold 60s
```

### Matching Flow (`match`)
Creates pairs of users who connect, enter the matching queue, and accept matches.

```bash
go run ./cmd/loadtest match \
  -url ws://localhost:8080/ws \
  -pairs 500 \
  -ramp 10s \
  -match-timeout 30s \
  -interests "music,gaming"
```

### Full Chat Lifecycle (`chat`)
End-to-end test: connect, match, exchange messages, and end chat.

```bash
go run ./cmd/loadtest chat \
  -url ws://localhost:8080/ws \
  -pairs 100 \
  -ramp 10s \
  -chat-duration 30s \
  -msg-interval 2s \
  -msg-size 128
```

## Building

```bash
cd loadtest
go build -o loadtest ./cmd/loadtest
./loadtest <command> [options]
```

## Resource Requirements

| Test | Connections | Estimated Client Memory | Estimated Client CPU |
|---|---|---|---|
| saturate (1K) | 1,000 | ~50 MB | 1 core |
| saturate (10K) | 10,000 | ~500 MB | 2 cores |
| saturate (100K) | 100,000 | ~4 GB | 4 cores |
| match (500 pairs) | 1,000 | ~100 MB | 2 cores |
| chat (100 pairs) | 200 | ~50 MB | 1 core |

### Kernel Tuning (for 10K+ connections)

The load test client machine needs increased file descriptor limits:

```bash
# Temporary (current session)
ulimit -n 200000

# Persistent (/etc/security/limits.conf)
* soft nofile 200000
* hard nofile 200000
```

For 100K+ connections, also increase the local port range:

```bash
sysctl -w net.ipv4.ip_local_port_range="1024 65535"
```

## Architecture

```
loadtest/
├── cmd/loadtest/       # CLI entry point with subcommands
│   ├── main.go         # Subcommand router
│   ├── saturate.go     # Connection saturation test (LOAD-2)
│   ├── match.go        # Matching flow test (LOAD-3)
│   └── chat.go         # Full chat lifecycle test (LOAD-4)
├── client/             # Reusable WebSocket load test client
│   └── client.go       # Connection management and protocol handling
└── stats/              # Metrics collection and reporting
    └── stats.go        # Goroutine-safe percentile stats
```
