#!/usr/bin/env bash
# =============================================================================
# Whisper Benchmark Comparison Tool
# =============================================================================
# Compares two benchmark result log files and highlights regressions.
#
# Usage:
#   ./compare.sh <baseline.log> <candidate.log>
#
# Example:
#   ./compare.sh results/10k-20260215-120000.log results/10k-20260228-143000.log
#
# The script parses the "=== Load Test Results ===" section from each log
# and compares:
#   - Connection count and error rate
#   - Connect latency (avg, p50, p95, p99, max)
#   - Message latency (avg, p50, p95, p99, max)
#
# Regressions (>10% degradation) are marked with [!!REGRESSION!!].
# Improvements (>10% better) are marked with [improvement].
# =============================================================================
set -euo pipefail

# ---------------------------------------------------------------------------
# Constants
# ---------------------------------------------------------------------------
REGRESSION_THRESHOLD=10  # percentage increase that triggers a warning

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
err()  { echo -e "\033[1;31m[ERROR]\033[0m $*" >&2; }
warn() { echo -e "\033[1;33m[!!REGRESSION!!]\033[0m $*"; }
good() { echo -e "\033[1;32m[improvement]\033[0m $*"; }

usage() {
    echo "Usage: $0 <baseline.log> <candidate.log>"
    echo ""
    echo "Compares two Whisper benchmark result log files."
    echo "Regressions (>10% degradation) are flagged with a warning."
    exit 1
}

# ---------------------------------------------------------------------------
# Argument validation
# ---------------------------------------------------------------------------
if [[ $# -ne 2 ]]; then
    err "Expected exactly 2 arguments."
    usage
fi

BASELINE="$1"
CANDIDATE="$2"

if [[ ! -f "$BASELINE" ]]; then
    err "Baseline file not found: $BASELINE"
    exit 1
fi

if [[ ! -f "$CANDIDATE" ]]; then
    err "Candidate file not found: $CANDIDATE"
    exit 1
fi

# ---------------------------------------------------------------------------
# Parsing functions
# ---------------------------------------------------------------------------

# extract_value <file> <label>
# Extracts a numeric value following a label from the results section.
# Example: extract_value file.log "Connections:" -> "10000"
extract_value() {
    local file="$1"
    local label="$2"
    grep -A0 "$label" "$file" | head -1 | grep -oP "$label\s*\K[\d.]+" || echo "N/A"
}

# extract_error_rate <file>
# Extracts the error rate percentage.
extract_error_rate() {
    local file="$1"
    grep "Error rate:" "$file" | head -1 | grep -oP '[\d.]+(?=%)' || echo "N/A"
}

# extract_latency_line <file> <section>
# Extracts the latency summary line from a named section.
# Section is "Connect Latency" or "Message Latency".
extract_latency_line() {
    local file="$1"
    local section="$2"
    # The latency line follows the "--- <section> ---" header.
    grep -A1 -- "--- $section ---" "$file" | tail -1
}

# parse_latency_field <line> <field>
# Extracts a specific field (avg, p50, p95, p99, max) from a latency summary line.
# The format is: "  avg: 1.234ms  p50: 1.1ms  p95: 2.3ms  p99: 5.6ms  max: 10ms  (n=1000)"
parse_latency_field() {
    local line="$1"
    local field="$2"
    echo "$line" | grep -oP "${field}: \K[^\s]+" || echo "N/A"
}

# duration_to_us <duration_string>
# Converts a Go duration string (e.g., "1.234ms", "500us", "2.5s") to microseconds.
duration_to_us() {
    local d="$1"
    if [[ "$d" == "N/A" ]]; then
        echo "N/A"
        return
    fi

    # Remove any trailing whitespace
    d="$(echo "$d" | xargs)"

    if [[ "$d" =~ ^([0-9.]+)ms$ ]]; then
        echo "${BASH_REMATCH[1]}" | awk '{printf "%.0f", $1 * 1000}'
    elif [[ "$d" =~ ^([0-9.]+)µs$ ]] || [[ "$d" =~ ^([0-9.]+)us$ ]]; then
        echo "${BASH_REMATCH[1]}" | awk '{printf "%.0f", $1}'
    elif [[ "$d" =~ ^([0-9.]+)s$ ]]; then
        echo "${BASH_REMATCH[1]}" | awk '{printf "%.0f", $1 * 1000000}'
    elif [[ "$d" =~ ^([0-9.]+)m ]]; then
        echo "${BASH_REMATCH[1]}" | awk '{printf "%.0f", $1 * 60000000}'
    else
        # Try to parse as plain number (assume microseconds)
        echo "$d" | awk '{printf "%.0f", $1}' 2>/dev/null || echo "N/A"
    fi
}

# format_us <microseconds>
# Formats microseconds into a human-readable duration string.
format_us() {
    local us="$1"
    if [[ "$us" == "N/A" ]]; then
        echo "N/A"
        return
    fi
    if [[ "$us" -ge 1000000 ]]; then
        awk "BEGIN {printf \"%.2fs\", $us / 1000000}"
    elif [[ "$us" -ge 1000 ]]; then
        awk "BEGIN {printf \"%.2fms\", $us / 1000}"
    else
        echo "${us}us"
    fi
}

# compute_delta <baseline_val> <candidate_val>
# Computes the percentage change. Positive = regression (higher is worse).
# Returns: "delta_pct" or "N/A"
compute_delta() {
    local base="$1"
    local cand="$2"

    if [[ "$base" == "N/A" ]] || [[ "$cand" == "N/A" ]] || [[ "$base" == "0" ]]; then
        echo "N/A"
        return
    fi

    awk "BEGIN {printf \"%.1f\", (($cand - $base) / $base) * 100}"
}

# print_comparison_row <label> <base_val> <cand_val> <unit> <higher_is_worse>
# Prints a formatted comparison row with delta and regression flagging.
print_comparison_row() {
    local label="$1"
    local base_raw="$2"
    local cand_raw="$3"
    local unit="$4"
    local higher_is_worse="${5:-true}"

    local delta
    delta="$(compute_delta "$base_raw" "$cand_raw")"

    local base_display="$base_raw"
    local cand_display="$cand_raw"

    # Format microsecond values
    if [[ "$unit" == "latency" ]]; then
        base_display="$(format_us "$base_raw")"
        cand_display="$(format_us "$cand_raw")"
    fi

    local delta_display=""
    local flag=""

    if [[ "$delta" != "N/A" ]]; then
        local sign=""
        # Check for negative delta (improvement for higher-is-worse metrics)
        if awk "BEGIN {exit !($delta > 0)}" 2>/dev/null; then
            sign="+"
        fi
        delta_display="${sign}${delta}%"

        # Check for regression
        local abs_delta
        abs_delta="$(echo "$delta" | sed 's/^-//')"

        if awk "BEGIN {exit !($abs_delta > $REGRESSION_THRESHOLD)}" 2>/dev/null; then
            if [[ "$higher_is_worse" == "true" ]]; then
                if awk "BEGIN {exit !($delta > 0)}" 2>/dev/null; then
                    flag=" [!!REGRESSION!!]"
                else
                    flag=" [improvement]"
                fi
            else
                if awk "BEGIN {exit !($delta < 0)}" 2>/dev/null; then
                    flag=" [!!REGRESSION!!]"
                else
                    flag=" [improvement]"
                fi
            fi
        fi
    fi

    printf "  %-22s %15s %15s %12s%s\n" "$label" "$base_display" "$cand_display" "$delta_display" "$flag"
}

# ---------------------------------------------------------------------------
# Extract data from both files
# ---------------------------------------------------------------------------
echo "============================================================"
echo " Whisper Benchmark Comparison"
echo "============================================================"
echo ""
echo "  Baseline:  $BASELINE"
echo "  Candidate: $CANDIDATE"
echo ""

# --- Summary metrics ---
BASE_CONNECTIONS="$(extract_value "$BASELINE" "Connections:")"
CAND_CONNECTIONS="$(extract_value "$CANDIDATE" "Connections:")"

BASE_ERRORS="$(extract_value "$BASELINE" "Errors:")"
CAND_ERRORS="$(extract_value "$CANDIDATE" "Errors:")"

BASE_ERROR_RATE="$(extract_error_rate "$BASELINE")"
CAND_ERROR_RATE="$(extract_error_rate "$CANDIDATE")"

# --- Connect latency ---
BASE_CONNECT_LINE="$(extract_latency_line "$BASELINE" "Connect Latency")"
CAND_CONNECT_LINE="$(extract_latency_line "$CANDIDATE" "Connect Latency")"

BASE_CONN_AVG="$(duration_to_us "$(parse_latency_field "$BASE_CONNECT_LINE" "avg")")"
CAND_CONN_AVG="$(duration_to_us "$(parse_latency_field "$CAND_CONNECT_LINE" "avg")")"

BASE_CONN_P50="$(duration_to_us "$(parse_latency_field "$BASE_CONNECT_LINE" "p50")")"
CAND_CONN_P50="$(duration_to_us "$(parse_latency_field "$CAND_CONNECT_LINE" "p50")")"

BASE_CONN_P95="$(duration_to_us "$(parse_latency_field "$BASE_CONNECT_LINE" "p95")")"
CAND_CONN_P95="$(duration_to_us "$(parse_latency_field "$CAND_CONNECT_LINE" "p95")")"

BASE_CONN_P99="$(duration_to_us "$(parse_latency_field "$BASE_CONNECT_LINE" "p99")")"
CAND_CONN_P99="$(duration_to_us "$(parse_latency_field "$CAND_CONNECT_LINE" "p99")")"

BASE_CONN_MAX="$(duration_to_us "$(parse_latency_field "$BASE_CONNECT_LINE" "max")")"
CAND_CONN_MAX="$(duration_to_us "$(parse_latency_field "$CAND_CONNECT_LINE" "max")")"

# --- Message latency ---
BASE_MSG_LINE="$(extract_latency_line "$BASELINE" "Message Latency")"
CAND_MSG_LINE="$(extract_latency_line "$CANDIDATE" "Message Latency")"

BASE_MSG_AVG="$(duration_to_us "$(parse_latency_field "$BASE_MSG_LINE" "avg")")"
CAND_MSG_AVG="$(duration_to_us "$(parse_latency_field "$CAND_MSG_LINE" "avg")")"

BASE_MSG_P50="$(duration_to_us "$(parse_latency_field "$BASE_MSG_LINE" "p50")")"
CAND_MSG_P50="$(duration_to_us "$(parse_latency_field "$CAND_MSG_LINE" "p50")")"

BASE_MSG_P95="$(duration_to_us "$(parse_latency_field "$BASE_MSG_LINE" "p95")")"
CAND_MSG_P95="$(duration_to_us "$(parse_latency_field "$CAND_MSG_LINE" "p95")")"

BASE_MSG_P99="$(duration_to_us "$(parse_latency_field "$BASE_MSG_LINE" "p99")")"
CAND_MSG_P99="$(duration_to_us "$(parse_latency_field "$CAND_MSG_LINE" "p99")")"

BASE_MSG_MAX="$(duration_to_us "$(parse_latency_field "$BASE_MSG_LINE" "max")")"
CAND_MSG_MAX="$(duration_to_us "$(parse_latency_field "$CAND_MSG_LINE" "max")")"

# ---------------------------------------------------------------------------
# Print comparison
# ---------------------------------------------------------------------------
echo "--- Summary ---"
printf "  %-22s %15s %15s %12s\n" "" "Baseline" "Candidate" "Delta"
printf "  %-22s %15s %15s %12s\n" "----------------------" "---------------" "---------------" "------------"
print_comparison_row "Connections" "$BASE_CONNECTIONS" "$CAND_CONNECTIONS" "count" "false"
print_comparison_row "Errors" "$BASE_ERRORS" "$CAND_ERRORS" "count" "true"
print_comparison_row "Error rate (%)" "$BASE_ERROR_RATE" "$CAND_ERROR_RATE" "count" "true"

echo ""
echo "--- Connect Latency ---"
printf "  %-22s %15s %15s %12s\n" "" "Baseline" "Candidate" "Delta"
printf "  %-22s %15s %15s %12s\n" "----------------------" "---------------" "---------------" "------------"
print_comparison_row "avg" "$BASE_CONN_AVG" "$CAND_CONN_AVG" "latency" "true"
print_comparison_row "p50" "$BASE_CONN_P50" "$CAND_CONN_P50" "latency" "true"
print_comparison_row "p95" "$BASE_CONN_P95" "$CAND_CONN_P95" "latency" "true"
print_comparison_row "p99" "$BASE_CONN_P99" "$CAND_CONN_P99" "latency" "true"
print_comparison_row "max" "$BASE_CONN_MAX" "$CAND_CONN_MAX" "latency" "true"

echo ""
echo "--- Message Latency ---"
printf "  %-22s %15s %15s %12s\n" "" "Baseline" "Candidate" "Delta"
printf "  %-22s %15s %15s %12s\n" "----------------------" "---------------" "---------------" "------------"
print_comparison_row "avg" "$BASE_MSG_AVG" "$CAND_MSG_AVG" "latency" "true"
print_comparison_row "p50" "$BASE_MSG_P50" "$CAND_MSG_P50" "latency" "true"
print_comparison_row "p95" "$BASE_MSG_P95" "$CAND_MSG_P95" "latency" "true"
print_comparison_row "p99" "$BASE_MSG_P99" "$CAND_MSG_P99" "latency" "true"
print_comparison_row "max" "$BASE_MSG_MAX" "$CAND_MSG_MAX" "latency" "true"

echo ""
echo "============================================================"
echo " Regression threshold: >${REGRESSION_THRESHOLD}% degradation"
echo "============================================================"
