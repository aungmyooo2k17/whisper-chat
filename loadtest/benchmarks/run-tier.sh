#!/usr/bin/env bash
# =============================================================================
# Whisper Tiered Benchmark Runner
# =============================================================================
# Executes a complete benchmark tier (saturate, match, chat) against the
# Whisper chat application. Each tier defines connection counts, ramp times,
# and concurrency levels appropriate for the target scale.
#
# Usage:
#   ./run-tier.sh <tier> [options]
#
# Tiers:
#   10k   — 10,000 connections   (development / single-machine validation)
#   100k  — 100,000 connections  (staging / pre-production baseline)
#   500k  — 500,000 connections  (production capacity test)
#   1m    — 1,000,000 connections (maximum capacity / stress test)
#
# Options:
#   -url <ws://host:port/ws>   Override the WebSocket server URL
#   -skip-saturate             Skip the connection saturation phase
#   -skip-match                Skip the matching throughput phase
#   -skip-chat                 Skip the full chat lifecycle phase
#   --dry-run                  Print commands without executing them
#   -h, --help                 Show this help message
#
# Output:
#   All output is tee'd to loadtest/benchmarks/results/<tier>-<timestamp>.log
#
# Prerequisites:
#   - Load test binary built:  cd loadtest && go build -o loadtest ./cmd/loadtest
#   - Server running and reachable at the target URL
#   - Kernel tuning applied:   sudo ./scripts/tune-kernel.sh (server)
#                               sudo ./loadtest/scripts/tune-client.sh (client)
#   - Sufficient file descriptors: ulimit -n >= 2 * connections
# =============================================================================
set -euo pipefail

# ---------------------------------------------------------------------------
# Resolve paths
# ---------------------------------------------------------------------------
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
LOADTEST_DIR="$REPO_ROOT/loadtest"
BINARY="$LOADTEST_DIR/loadtest"
RESULTS_DIR="$SCRIPT_DIR/results"

# ---------------------------------------------------------------------------
# Defaults
# ---------------------------------------------------------------------------
SERVER_URL="ws://localhost:8080/ws"
METRICS_URL="http://localhost:8080/metrics"
HEALTH_URL="http://localhost:8080/health"
SKIP_SATURATE=false
SKIP_MATCH=false
SKIP_CHAT=false
DRY_RUN=false
TIER=""

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
info()  { echo -e "\033[1;34m[INFO]\033[0m  $*"; }
ok()    { echo -e "\033[1;32m[OK]\033[0m    $*"; }
warn()  { echo -e "\033[1;33m[WARN]\033[0m  $*"; }
err()   { echo -e "\033[1;31m[ERROR]\033[0m $*" >&2; }

usage() {
    sed -n '2,/^# ====/{ /^# ====/d; s/^# \?//p }' "$0"
    exit 0
}

# ---------------------------------------------------------------------------
# Argument parsing
# ---------------------------------------------------------------------------
if [[ $# -lt 1 ]]; then
    err "Missing required argument: <tier>"
    echo "Usage: $0 <tier> [options]"
    echo "Tiers: 10k, 100k, 500k, 1m"
    echo "Run '$0 --help' for full usage."
    exit 1
fi

while [[ $# -gt 0 ]]; do
    case "$1" in
        10k|100k|500k|1m)
            TIER="$1"
            shift
            ;;
        -url)
            SERVER_URL="$2"
            # Derive metrics/health URLs from the server URL.
            URL_BASE="${SERVER_URL%%/ws}"
            URL_BASE="${URL_BASE/ws:\/\//http://}"
            URL_BASE="${URL_BASE/wss:\/\//https://}"
            METRICS_URL="$URL_BASE/metrics"
            HEALTH_URL="$URL_BASE/health"
            shift 2
            ;;
        -skip-saturate) SKIP_SATURATE=true; shift ;;
        -skip-match)    SKIP_MATCH=true;    shift ;;
        -skip-chat)     SKIP_CHAT=true;     shift ;;
        --dry-run)      DRY_RUN=true;       shift ;;
        -h|--help)      usage ;;
        *)
            err "Unknown argument: $1"
            echo "Run '$0 --help' for usage."
            exit 1
            ;;
    esac
done

if [[ -z "$TIER" ]]; then
    err "Missing required argument: <tier>"
    echo "Usage: $0 <tier> [options]"
    echo "Tiers: 10k, 100k, 500k, 1m"
    exit 1
fi

# ---------------------------------------------------------------------------
# Tier configurations
# ---------------------------------------------------------------------------
#   SATURATE_CONNS  — number of connections for the saturation test
#   SATURATE_RAMP   — ramp-up duration for saturation
#   SATURATE_HOLD   — hold duration after all connections are open
#   MATCH_PAIRS     — number of user pairs for the match test
#   CHAT_PAIRS      — number of user pairs for the chat test
#   CHAT_DURATION   — how long each chat pair exchanges messages
#   CONCURRENCY     — max simultaneous connection attempts
#   SCRAPE_INTERVAL — Prometheus scrape interval
#   MATCH_TIMEOUT   — timeout waiting for match completion
#   MSG_INTERVAL    — interval between messages per user in chat
#   MSG_SIZE        — size of each chat message payload in bytes

case "$TIER" in
    10k)
        SATURATE_CONNS=10000
        SATURATE_RAMP="30s"
        SATURATE_HOLD="60s"
        MATCH_PAIRS=2000
        CHAT_PAIRS=500
        CHAT_DURATION="30s"
        CONCURRENCY=50
        SCRAPE_INTERVAL="2s"
        MATCH_TIMEOUT="30s"
        MSG_INTERVAL="2s"
        MSG_SIZE=128
        ;;
    100k)
        SATURATE_CONNS=100000
        SATURATE_RAMP="120s"
        SATURATE_HOLD="120s"
        MATCH_PAIRS=20000
        CHAT_PAIRS=5000
        CHAT_DURATION="30s"
        CONCURRENCY=200
        SCRAPE_INTERVAL="2s"
        MATCH_TIMEOUT="60s"
        MSG_INTERVAL="2s"
        MSG_SIZE=128
        ;;
    500k)
        SATURATE_CONNS=500000
        SATURATE_RAMP="300s"
        SATURATE_HOLD="180s"
        MATCH_PAIRS=50000
        CHAT_PAIRS=10000
        CHAT_DURATION="30s"
        CONCURRENCY=500
        SCRAPE_INTERVAL="5s"
        MATCH_TIMEOUT="120s"
        MSG_INTERVAL="2s"
        MSG_SIZE=128
        ;;
    1m)
        SATURATE_CONNS=1000000
        SATURATE_RAMP="600s"
        SATURATE_HOLD="300s"
        MATCH_PAIRS=100000
        CHAT_PAIRS=20000
        CHAT_DURATION="30s"
        CONCURRENCY=1000
        SCRAPE_INTERVAL="5s"
        MATCH_TIMEOUT="180s"
        MSG_INTERVAL="2s"
        MSG_SIZE=128
        ;;
esac

# ---------------------------------------------------------------------------
# Output logging setup
# ---------------------------------------------------------------------------
TIMESTAMP="$(date +%Y%m%d-%H%M%S)"
LOG_FILE="$RESULTS_DIR/${TIER}-${TIMESTAMP}.log"
mkdir -p "$RESULTS_DIR"

# ---------------------------------------------------------------------------
# Pre-flight checks
# ---------------------------------------------------------------------------
preflight() {
    echo "============================================================"
    echo " Whisper Benchmark — Tier: $TIER"
    echo " Timestamp: $TIMESTAMP"
    echo "============================================================"
    echo ""

    local failed=0

    # Check 1: Binary exists
    info "Checking load test binary..."
    if [[ -x "$BINARY" ]]; then
        ok "Binary found: $BINARY"
    else
        err "Binary not found or not executable: $BINARY"
        err "Build it with: cd $LOADTEST_DIR && go build -o loadtest ./cmd/loadtest"
        failed=1
    fi

    # Check 2: Server is reachable
    info "Checking server health at $HEALTH_URL ..."
    if curl -sf --connect-timeout 5 --max-time 10 "$HEALTH_URL" > /dev/null 2>&1; then
        ok "Server is reachable."
    else
        # Fall back to checking if the WebSocket port responds at all
        local ws_host="${SERVER_URL#*://}"
        ws_host="${ws_host%%/*}"
        local host="${ws_host%%:*}"
        local port="${ws_host##*:}"
        if curl -sf --connect-timeout 5 --max-time 10 "http://${host}:${port}/" > /dev/null 2>&1; then
            warn "Health endpoint not found, but server port is open."
        else
            err "Server is not reachable at $HEALTH_URL"
            err "Make sure the Whisper server is running."
            failed=1
        fi
    fi

    # Check 3: File descriptor limit
    info "Checking file descriptor limit..."
    local current_ulimit
    current_ulimit="$(ulimit -n)"
    local required_fds=$((SATURATE_CONNS + 1000))  # Some headroom

    if [[ "$current_ulimit" == "unlimited" ]] || [[ "$current_ulimit" -ge "$required_fds" ]]; then
        ok "ulimit -n = $current_ulimit (need >= $required_fds for tier $TIER)"
    else
        warn "ulimit -n = $current_ulimit (need >= $required_fds for tier $TIER)"
        warn "Run: ulimit -n $required_fds"
        warn "Or apply kernel tuning: sudo $LOADTEST_DIR/scripts/tune-client.sh"
        # Only fail for tiers that definitely need more
        if [[ "$current_ulimit" -lt "$((SATURATE_CONNS / 2))" ]]; then
            failed=1
        fi
    fi

    echo ""

    # Print configuration summary
    info "Tier configuration:"
    echo "  Server URL:       $SERVER_URL"
    echo "  Metrics URL:      $METRICS_URL"
    echo "  Concurrency:      $CONCURRENCY"
    echo "  Scrape interval:  $SCRAPE_INTERVAL"
    echo ""
    if ! $SKIP_SATURATE; then
        echo "  [Saturate]  connections=$SATURATE_CONNS  ramp=$SATURATE_RAMP  hold=$SATURATE_HOLD"
    else
        echo "  [Saturate]  SKIPPED"
    fi
    if ! $SKIP_MATCH; then
        echo "  [Match]     pairs=$MATCH_PAIRS  ramp=$SATURATE_RAMP  timeout=$MATCH_TIMEOUT"
    else
        echo "  [Match]     SKIPPED"
    fi
    if ! $SKIP_CHAT; then
        echo "  [Chat]      pairs=$CHAT_PAIRS  ramp=$SATURATE_RAMP  duration=$CHAT_DURATION  msg-interval=$MSG_INTERVAL  msg-size=$MSG_SIZE"
    else
        echo "  [Chat]      SKIPPED"
    fi
    echo ""
    echo "  Log file: $LOG_FILE"
    echo ""

    if [[ $failed -ne 0 ]]; then
        err "Pre-flight checks failed. Aborting."
        exit 1
    fi

    ok "All pre-flight checks passed."
    echo ""
}

# ---------------------------------------------------------------------------
# Phase runners
# ---------------------------------------------------------------------------
run_phase() {
    local phase_name="$1"
    shift
    local cmd=("$@")

    echo "============================================================"
    echo " Phase: $phase_name"
    echo " Command: ${cmd[*]}"
    echo " Started: $(date '+%Y-%m-%d %H:%M:%S %Z')"
    echo "============================================================"
    echo ""

    if $DRY_RUN; then
        info "[DRY RUN] Would execute: ${cmd[*]}"
        echo ""
        return 0
    fi

    local start_time
    start_time="$(date +%s)"

    # Run the command; capture exit code without exiting on failure.
    local exit_code=0
    "${cmd[@]}" || exit_code=$?

    local end_time
    end_time="$(date +%s)"
    local elapsed=$((end_time - start_time))

    echo ""
    if [[ $exit_code -eq 0 ]]; then
        ok "Phase '$phase_name' completed in ${elapsed}s (exit code 0)"
    else
        warn "Phase '$phase_name' finished in ${elapsed}s with exit code $exit_code"
    fi
    echo ""

    return $exit_code
}

run_saturate() {
    run_phase "Saturate (Connection Capacity)" \
        "$BINARY" saturate \
        -url "$SERVER_URL" \
        -connections "$SATURATE_CONNS" \
        -ramp "$SATURATE_RAMP" \
        -hold "$SATURATE_HOLD" \
        -concurrency "$CONCURRENCY"
}

run_match() {
    # Compute a ramp duration scaled to the number of match pairs.
    # Use half the saturate ramp since match has fewer total connections.
    local match_ramp
    case "$TIER" in
        10k)  match_ramp="20s" ;;
        100k) match_ramp="60s" ;;
        500k) match_ramp="120s" ;;
        1m)   match_ramp="240s" ;;
    esac

    run_phase "Match (Matching Throughput)" \
        "$BINARY" match \
        -url "$SERVER_URL" \
        -pairs "$MATCH_PAIRS" \
        -ramp "$match_ramp" \
        -match-timeout "$MATCH_TIMEOUT" \
        -concurrency "$CONCURRENCY" \
        -metrics-url "$METRICS_URL" \
        -scrape-interval "$SCRAPE_INTERVAL"
}

run_chat() {
    # Compute a ramp duration scaled to the number of chat pairs.
    local chat_ramp
    case "$TIER" in
        10k)  chat_ramp="10s" ;;
        100k) chat_ramp="30s" ;;
        500k) chat_ramp="60s" ;;
        1m)   chat_ramp="120s" ;;
    esac

    run_phase "Chat (Full Lifecycle)" \
        "$BINARY" chat \
        -url "$SERVER_URL" \
        -pairs "$CHAT_PAIRS" \
        -ramp "$chat_ramp" \
        -chat-duration "$CHAT_DURATION" \
        -msg-interval "$MSG_INTERVAL" \
        -msg-size "$MSG_SIZE" \
        -concurrency "$CONCURRENCY" \
        -metrics-url "$METRICS_URL" \
        -scrape-interval "$SCRAPE_INTERVAL"
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------
main() {
    preflight

    if $DRY_RUN; then
        info "[DRY RUN] Commands that would be executed:"
        echo ""
    fi

    local overall_start
    overall_start="$(date +%s)"
    local any_failed=0

    # Phase 1: Saturate
    if ! $SKIP_SATURATE; then
        run_saturate || any_failed=1

        # Brief pause between phases to let the server recover.
        if ! $DRY_RUN && { ! $SKIP_MATCH || ! $SKIP_CHAT; }; then
            info "Pausing 10s between phases for server recovery..."
            sleep 10
        fi
    fi

    # Phase 2: Match
    if ! $SKIP_MATCH; then
        run_match || any_failed=1

        # Brief pause between phases.
        if ! $DRY_RUN && ! $SKIP_CHAT; then
            info "Pausing 10s between phases for server recovery..."
            sleep 10
        fi
    fi

    # Phase 3: Chat
    if ! $SKIP_CHAT; then
        run_chat || any_failed=1
    fi

    local overall_end
    overall_end="$(date +%s)"
    local overall_elapsed=$((overall_end - overall_start))

    echo ""
    echo "============================================================"
    echo " Benchmark Complete — Tier: $TIER"
    echo " Total wall time: ${overall_elapsed}s"
    echo " Finished: $(date '+%Y-%m-%d %H:%M:%S %Z')"
    if ! $DRY_RUN; then
        echo " Log file: $LOG_FILE"
    fi
    echo "============================================================"

    if [[ $any_failed -ne 0 ]]; then
        warn "One or more phases reported non-zero exit codes."
        exit 1
    fi

    ok "All phases completed successfully."
}

# Execute main, teeing all output to the log file.
if $DRY_RUN; then
    # In dry-run mode, skip the tee to avoid creating empty log files.
    main
else
    main 2>&1 | tee "$LOG_FILE"
fi
