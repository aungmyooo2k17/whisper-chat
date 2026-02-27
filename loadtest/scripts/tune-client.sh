#!/usr/bin/env bash
# =============================================================================
# Whisper Load Test Client - Kernel Tuning Script
# =============================================================================
# Applies sysctl and ulimit settings required to generate 100K-1M outbound
# WebSocket connections from a single load test client machine.
#
# Usage:
#   sudo ./loadtest/scripts/tune-client.sh           # Apply all settings
#   sudo ./loadtest/scripts/tune-client.sh --dry-run  # Show what would change
#
# This script is idempotent -- safe to run multiple times. It overwrites
# the previous /etc/security/limits.d/loadtest.conf each time.
#
# COMPANION SCRIPTS (server-side):
#   scripts/tune-kernel.sh        - Server kernel tuning
#   scripts/sysctl-whisper.conf   - Server sysctl settings
#   scripts/limits-whisper.conf   - Server FD/process limits
#
# NOTE (Docker): These settings must be applied on the Docker HOST, not
# inside a container. Containers share the host kernel.
# =============================================================================
set -euo pipefail

# ---------------------------------------------------------------------------
# Constants
# ---------------------------------------------------------------------------
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SYSCTL_SRC="$SCRIPT_DIR/sysctl-loadtest.conf"
LIMITS_SRC="$SCRIPT_DIR/limits-loadtest.conf"
LIMITS_DST="/etc/security/limits.d/loadtest.conf"
DRY_RUN=false

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
info()  { echo -e "\033[1;34m[INFO]\033[0m  $*"; }
ok()    { echo -e "\033[1;32m[OK]\033[0m    $*"; }
warn()  { echo -e "\033[1;33m[WARN]\033[0m  $*"; }
err()   { echo -e "\033[1;31m[ERROR]\033[0m $*" >&2; }

usage() {
    echo "Usage: sudo $0 [--dry-run]"
    echo ""
    echo "  --dry-run   Show what would be changed without applying anything."
    exit 0
}

# ---------------------------------------------------------------------------
# Argument parsing
# ---------------------------------------------------------------------------
for arg in "$@"; do
    case "$arg" in
        --dry-run) DRY_RUN=true ;;
        --help|-h) usage ;;
        *) err "Unknown argument: $arg"; usage ;;
    esac
done

# ---------------------------------------------------------------------------
# Pre-flight checks
# ---------------------------------------------------------------------------
echo "============================================================"
echo " Whisper Load Test Client - Kernel Tuning"
echo " Target: 100K-1M outbound WebSocket connections"
echo "============================================================"
echo ""

# Must run as root (or via sudo)
if [[ $EUID -ne 0 ]]; then
    err "This script must be run as root (use sudo)."
    exit 1
fi

# Verify config files exist
if [[ ! -f "$SYSCTL_SRC" ]]; then
    err "Missing sysctl config: $SYSCTL_SRC"
    exit 1
fi
if [[ ! -f "$LIMITS_SRC" ]]; then
    err "Missing limits config: $LIMITS_SRC"
    exit 1
fi

# Detect if we are inside a container (best-effort)
if [[ -f /.dockerenv ]] || grep -qsF 'docker\|lxc\|containerd' /proc/1/cgroup 2>/dev/null; then
    warn "Running inside a container. Sysctl changes may be restricted."
    warn "Apply these settings on the Docker HOST instead."
    echo ""
fi

# ---------------------------------------------------------------------------
# Step 1: Snapshot current values
# ---------------------------------------------------------------------------
info "Current kernel settings (before):"
echo "  fs.file-max               = $(sysctl -n fs.file-max 2>/dev/null || echo 'N/A')"
echo "  fs.nr_open                = $(sysctl -n fs.nr_open 2>/dev/null || echo 'N/A')"
echo "  net.ipv4.ip_local_port_range = $(sysctl -n net.ipv4.ip_local_port_range 2>/dev/null || echo 'N/A')"
echo "  net.ipv4.tcp_tw_reuse     = $(sysctl -n net.ipv4.tcp_tw_reuse 2>/dev/null || echo 'N/A')"
echo "  net.ipv4.tcp_fin_timeout  = $(sysctl -n net.ipv4.tcp_fin_timeout 2>/dev/null || echo 'N/A')"
echo "  net.ipv4.tcp_syn_retries  = $(sysctl -n net.ipv4.tcp_syn_retries 2>/dev/null || echo 'N/A')"
echo "  net.core.somaxconn        = $(sysctl -n net.core.somaxconn 2>/dev/null || echo 'N/A')"
echo "  net.ipv4.tcp_max_orphans  = $(sysctl -n net.ipv4.tcp_max_orphans 2>/dev/null || echo 'N/A')"
echo "  ulimit -n (this shell)    = $(ulimit -n)"
echo ""

if $DRY_RUN; then
    info "[DRY RUN] Would apply sysctl settings from: $SYSCTL_SRC"
    info "[DRY RUN] Would install limits config to:   $LIMITS_DST"
    echo ""
    info "Sysctl settings that would be applied:"
    grep -v '^\s*#' "$SYSCTL_SRC" | grep -v '^\s*$' | sed 's/^/  /'
    echo ""
    info "Limits config that would be installed:"
    grep -v '^\s*#' "$LIMITS_SRC" | grep -v '^\s*$' | sed 's/^/  /'
    echo ""
    ok "Dry run complete. No changes were made."
    exit 0
fi

# ---------------------------------------------------------------------------
# Step 2: Apply sysctl settings
# ---------------------------------------------------------------------------
info "[1/3] Applying sysctl settings from $SYSCTL_SRC ..."

# Apply settings one at a time. This allows us to skip individual parameters
# that are not supported on the running kernel (e.g., conntrack params when
# nf_conntrack is not loaded) without blocking the rest.
APPLIED=0
FAILED=0
while IFS= read -r line; do
    # Skip comments and blank lines
    [[ -z "$line" || "$line" =~ ^[[:space:]]*# ]] && continue
    key="${line%%=*}"
    key="$(echo "$key" | xargs)"  # trim whitespace
    if sysctl -w "$line" > /dev/null 2>&1; then
        ok "  $line"
        APPLIED=$((APPLIED + 1))
    else
        warn "  SKIPPED (not supported on this kernel): $key"
        FAILED=$((FAILED + 1))
    fi
done < "$SYSCTL_SRC"

ok "  Applied $APPLIED setting(s)."
if [[ $FAILED -gt 0 ]]; then
    warn "$FAILED setting(s) could not be applied (see above)."
fi
echo ""

# ---------------------------------------------------------------------------
# Step 3: Install limits.conf
# ---------------------------------------------------------------------------
info "[2/3] Installing limits configuration to $LIMITS_DST ..."

# Back up existing file if it differs
if [[ -f "$LIMITS_DST" ]]; then
    if ! diff -q "$LIMITS_SRC" "$LIMITS_DST" > /dev/null 2>&1; then
        BACKUP="${LIMITS_DST}.bak.$(date +%Y%m%d%H%M%S)"
        cp "$LIMITS_DST" "$BACKUP"
        ok "  Backed up existing file to $BACKUP"
    else
        ok "  $LIMITS_DST is already up to date."
    fi
fi

cp "$LIMITS_SRC" "$LIMITS_DST"
chmod 644 "$LIMITS_DST"
ok "  Installed $LIMITS_DST"
echo ""

# ---------------------------------------------------------------------------
# Step 4: Verify
# ---------------------------------------------------------------------------
info "[3/3] Verifying applied settings ..."
echo ""
echo "  fs.file-max               = $(sysctl -n fs.file-max)"
echo "  fs.nr_open                = $(sysctl -n fs.nr_open)"
echo "  net.ipv4.ip_local_port_range = $(sysctl -n net.ipv4.ip_local_port_range)"
echo "  net.ipv4.tcp_tw_reuse     = $(sysctl -n net.ipv4.tcp_tw_reuse)"
echo "  net.ipv4.tcp_fin_timeout  = $(sysctl -n net.ipv4.tcp_fin_timeout)"
echo "  net.ipv4.tcp_syn_retries  = $(sysctl -n net.ipv4.tcp_syn_retries)"
echo "  net.ipv4.tcp_synack_retries = $(sysctl -n net.ipv4.tcp_synack_retries)"
echo "  net.ipv4.tcp_fastopen     = $(sysctl -n net.ipv4.tcp_fastopen)"
echo "  net.core.somaxconn        = $(sysctl -n net.core.somaxconn)"
echo "  net.core.netdev_max_backlog = $(sysctl -n net.core.netdev_max_backlog)"
echo "  net.ipv4.tcp_rmem         = $(sysctl -n net.ipv4.tcp_rmem)"
echo "  net.ipv4.tcp_wmem         = $(sysctl -n net.ipv4.tcp_wmem)"
echo "  net.ipv4.tcp_max_orphans  = $(sysctl -n net.ipv4.tcp_max_orphans)"
echo "  net.ipv4.tcp_max_syn_backlog = $(sysctl -n net.ipv4.tcp_max_syn_backlog)"
echo "  ulimit -n (this shell)    = $(ulimit -n)"
echo ""

# Check conntrack status
if lsmod 2>/dev/null | grep -q nf_conntrack; then
    info "nf_conntrack module is loaded."
    echo "  nf_conntrack_max          = $(sysctl -n net.netfilter.nf_conntrack_max 2>/dev/null || echo 'N/A')"
    echo ""
    CONNTRACK_MAX=$(sysctl -n net.netfilter.nf_conntrack_max 2>/dev/null || echo 0)
    if [[ "$CONNTRACK_MAX" -lt 1000000 ]]; then
        warn "nf_conntrack_max ($CONNTRACK_MAX) is below 1M."
        warn "Uncomment conntrack settings in $SYSCTL_SRC if running behind NAT."
    fi
else
    info "nf_conntrack module is NOT loaded (conntrack tuning not needed)."
fi
echo ""

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
echo "============================================================"
ok "Load test client kernel tuning applied successfully."
echo ""
echo "  IMPORTANT:"
echo "    - ulimit changes require a NEW shell session to take effect."
echo "    - To persist sysctl settings across reboots, copy the conf:"
echo "        sudo cp $SYSCTL_SRC /etc/sysctl.d/99-loadtest.conf"
echo ""
echo "  SCALING BEYOND 64K CONNECTIONS:"
echo "    A single source IP can sustain at most ~64K connections to one"
echo "    destination (limited by the ephemeral port range). To reach"
echo "    100K+ connections, add virtual IP addresses to the load test"
echo "    client NIC:"
echo ""
echo "      sudo ip addr add 10.0.0.11/24 dev eth0"
echo "      sudo ip addr add 10.0.0.12/24 dev eth0"
echo "      sudo ip addr add 10.0.0.13/24 dev eth0"
echo ""
echo "    Then configure the load test tool to bind outbound connections"
echo "    across these IPs in round-robin fashion. With 16 source IPs"
echo "    you can sustain ~1M connections to a single server."
echo ""
echo "  COMPANION SCRIPTS (server-side):"
echo "    sudo ./scripts/tune-kernel.sh    # Tune the Whisper server"
echo "============================================================"
