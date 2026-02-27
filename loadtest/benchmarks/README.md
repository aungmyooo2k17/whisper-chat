# Whisper Tiered Benchmarks

Automated benchmark runner for the Whisper anonymous chat application. Executes
connection saturation, matching throughput, and full chat lifecycle tests at
four predefined scale tiers.

## Prerequisites

1. **Build the load test binary**:
   ```bash
   cd loadtest
   go build -o loadtest ./cmd/loadtest
   ```

2. **Start the Whisper server** (must be reachable from the benchmark client):
   ```bash
   go run ./cmd/whisper
   ```

3. **Apply kernel tuning** (required for 10k+ connections):
   ```bash
   # Server-side
   sudo ./scripts/tune-kernel.sh

   # Client-side (the machine running benchmarks)
   sudo ./loadtest/scripts/tune-client.sh
   ```

4. **Increase file descriptor limit** in the current shell:
   ```bash
   ulimit -n 1100000   # For 1M tier; adjust to match your target tier
   ```

## Running a Benchmark

```bash
cd loadtest/benchmarks

# Run the full 10k tier (saturate + match + chat)
./run-tier.sh 10k

# Run with a custom server URL
./run-tier.sh 100k -url ws://10.0.1.5:8080/ws

# Skip specific phases
./run-tier.sh 500k -skip-saturate -skip-match

# Preview commands without executing
./run-tier.sh 1m --dry-run
```

### Available Tiers

| Tier | Saturate | Match Pairs | Chat Pairs | Concurrency | Est. Duration |
|------|----------|-------------|------------|-------------|---------------|
| 10k  | 10,000   | 2,000       | 500        | 50          | ~5 min        |
| 100k | 100,000  | 20,000      | 5,000      | 200         | ~15 min       |
| 500k | 500,000  | 50,000      | 10,000     | 500         | ~30 min       |
| 1m   | 1,000,000| 100,000     | 20,000     | 1,000       | ~60 min       |

### Options

| Flag              | Description                              |
|-------------------|------------------------------------------|
| `-url <url>`      | Override WebSocket server URL             |
| `-skip-saturate`  | Skip connection saturation phase         |
| `-skip-match`     | Skip matching throughput phase           |
| `-skip-chat`      | Skip full chat lifecycle phase           |
| `--dry-run`       | Print commands without executing them    |

## Interpreting Results

Each benchmark run produces a log file in `results/<tier>-<timestamp>.log`.
The log contains output from all three phases. Look for these key sections:

### Load Test Results

```
=== Load Test Results ===
Duration:     2m30s
Connections:  10000
Errors:       12
Error rate:   0.12%
```

- **Connections**: Total successful WebSocket connections established.
- **Errors**: Total errors across all phases (connect failures, timeouts, etc.).
- **Error rate**: Errors as a percentage of attempted connections.

### Latency Distributions

```
--- Connect Latency ---
  avg: 1.234ms  p50: 1.1ms  p95: 2.3ms  p99: 5.6ms  max: 10ms  (n=10000)

--- Message Latency ---
  avg: 45.6ms  p50: 40ms  p95: 80ms  p99: 120ms  max: 250ms  (n=5000)
```

- **p50**: Median latency (half of requests are faster).
- **p95**: 95th percentile (only 5% of requests are slower).
- **p99**: 99th percentile (the "tail" latency most users never see).
- **max**: The single worst latency observed.

### Server Metrics (Prometheus)

```
--- Server Metrics (Prometheus) ---
  Metric            Initial      Final      Delta       Peak
  Connections          0       10000      10000      10000
  Active Chats         0         500        500        500
```

These are scraped from the server's `/metrics` endpoint during the test.

### Expected Thresholds

See `thresholds.yaml` for per-tier performance targets. Key indicators of a
healthy run:

| Tier | Connect p99 | Msg p99 | Error Rate |
|------|------------|---------|------------|
| 10k  | < 50ms     | < 100ms | < 1%       |
| 100k | < 100ms    | < 200ms | < 2%       |
| 500k | < 250ms    | < 500ms | < 3%       |
| 1m   | < 500ms    | < 1s    | < 5%       |

## Comparing Runs

Use `compare.sh` to diff two benchmark results:

```bash
./compare.sh results/10k-20260215-120000.log results/10k-20260228-143000.log
```

Output shows a side-by-side comparison with delta percentages:

```
--- Connect Latency ---
                          Baseline       Candidate        Delta
  avg                       1.23ms         1.45ms       +17.9% [!!REGRESSION!!]
  p50                       1.10ms         1.12ms        +1.8%
  p95                       2.30ms         2.50ms        +8.7%
  p99                       5.60ms         5.80ms        +3.6%
```

- **[!!REGRESSION!!]**: The metric degraded by more than 10%.
- **[improvement]**: The metric improved by more than 10%.
- No marker: Within normal variance.

## Resource Requirements

### Client Machine (running benchmarks)

| Tier | RAM    | CPU Cores | Source IPs | ulimit -n |
|------|--------|-----------|------------|-----------|
| 10k  | 1 GB   | 2         | 1          | 12,000    |
| 100k | 6 GB   | 4         | 2          | 110,000   |
| 500k | 20 GB  | 8         | 8          | 510,000   |
| 1m   | 40 GB  | 16        | 16         | 1,100,000 |

### Server Machine

| Tier | RAM    | CPU Cores |
|------|--------|-----------|
| 10k  | 2 GB   | 2         |
| 100k | 8 GB   | 4         |
| 500k | 24 GB  | 8         |
| 1m   | 48 GB  | 16        |

For 100k+ connections, the client machine needs multiple source IPs to overcome
the ~64K ephemeral port limit per source IP. Add virtual IPs:

```bash
sudo ip addr add 10.0.0.11/24 dev eth0
sudo ip addr add 10.0.0.12/24 dev eth0
# ... repeat as needed
```

## File Structure

```
loadtest/benchmarks/
  run-tier.sh        # Main benchmark runner script
  compare.sh         # Result comparison tool
  thresholds.yaml    # Expected performance thresholds per tier
  README.md          # This file
  results/           # Benchmark log output (gitignored except .gitkeep)
    .gitkeep
    10k-20260228-143000.log
    100k-20260301-091500.log
```
