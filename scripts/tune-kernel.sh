#!/usr/bin/env bash
# =============================================================================
# Whisper Kernel Tuning Script
# =============================================================================
# Applies sysctl and ulimit settings required to handle 1,000,000 concurrent
# WebSocket connections on a single Linux host.
#
# Usage:
#   sudo ./scripts/tune-kernel.sh           # Apply all settings
#   sudo ./scripts/tune-kernel.sh --dry-run # Show what would be changed
#
# This script is idempotent -- safe to run multiple times. It overwrites
# the previous /etc/security/limits.d/whisper.conf each time.
#
# NOTE (Docker): These settings must be applied on the Docker HOST, not
# inside a container. Containers share the host kernel. For per-container
# ulimits, use --ulimit flags in docker run or the ulimits key in
# docker-compose.yml.
# =============================================================================
set -euo pipefail

# ---------------------------------------------------------------------------
# Constants
# ---------------------------------------------------------------------------
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SYSCTL_SRC="$SCRIPT_DIR/sysctl-whisper.conf"
LIMITS_SRC="$SCRIPT_DIR/limits-whisper.conf"
LIMITS_DST="/etc/security/limits.d/whisper.conf"
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
echo " Whisper Kernel Tuning for 1M WebSocket Connections"
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
echo "  fs.file-max            = $(sysctl -n fs.file-max 2>/dev/null || echo 'N/A')"
echo "  fs.nr_open             = $(sysctl -n fs.nr_open 2>/dev/null || echo 'N/A')"
echo "  net.core.somaxconn     = $(sysctl -n net.core.somaxconn 2>/dev/null || echo 'N/A')"
echo "  net.ipv4.tcp_tw_reuse  = $(sysctl -n net.ipv4.tcp_tw_reuse 2>/dev/null || echo 'N/A')"
echo "  net.ipv4.tcp_fin_timeout = $(sysctl -n net.ipv4.tcp_fin_timeout 2>/dev/null || echo 'N/A')"
echo "  net.ipv4.ip_local_port_range = $(sysctl -n net.ipv4.ip_local_port_range 2>/dev/null || echo 'N/A')"
echo "  ulimit -n (this shell) = $(ulimit -n)"
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

# Apply settings. sysctl -p prints each setting as it is applied.
# We capture failures individually so one unsupported setting does not
# block the rest (e.g., fs.epoll.max_user_watches may not exist on
# older kernels).
FAILED=0
while IFS= read -r line; do
    # Skip comments and blank lines
    [[ -z "$line" || "$line" =~ ^[[:space:]]*# ]] && continue
    key="${line%%=*}"
    key="$(echo "$key" | xargs)"  # trim whitespace
    if sysctl -w "$line" > /dev/null 2>&1; then
        ok "  $line"
    else
        warn "  SKIPPED (not supported on this kernel): $key"
        FAILED=$((FAILED + 1))
    fi
done < "$SYSCTL_SRC"

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
echo "  fs.file-max            = $(sysctl -n fs.file-max)"
echo "  fs.nr_open             = $(sysctl -n fs.nr_open)"
echo "  net.core.somaxconn     = $(sysctl -n net.core.somaxconn)"
echo "  net.core.netdev_max_backlog = $(sysctl -n net.core.netdev_max_backlog)"
echo "  net.ipv4.tcp_rmem      = $(sysctl -n net.ipv4.tcp_rmem)"
echo "  net.ipv4.tcp_wmem      = $(sysctl -n net.ipv4.tcp_wmem)"
echo "  net.ipv4.tcp_fastopen  = $(sysctl -n net.ipv4.tcp_fastopen)"
echo "  net.ipv4.tcp_tw_reuse  = $(sysctl -n net.ipv4.tcp_tw_reuse)"
echo "  net.ipv4.tcp_fin_timeout = $(sysctl -n net.ipv4.tcp_fin_timeout)"
echo "  net.ipv4.tcp_max_orphans = $(sysctl -n net.ipv4.tcp_max_orphans)"
echo "  net.ipv4.tcp_max_syn_backlog = $(sysctl -n net.ipv4.tcp_max_syn_backlog)"
echo "  net.ipv4.ip_local_port_range = $(sysctl -n net.ipv4.ip_local_port_range)"
echo "  net.ipv4.tcp_keepalive_time  = $(sysctl -n net.ipv4.tcp_keepalive_time)"
echo "  ulimit -n (this shell) = $(ulimit -n)"
echo ""

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
echo "============================================================"
ok "Kernel tuning applied successfully."
echo ""
echo "  IMPORTANT:"
echo "    - ulimit changes require a NEW shell session to take effect."
echo "    - For systemd services, also set LimitNOFILE=1048576 in the"
echo "      [Service] section of your unit file."
echo "    - To persist sysctl settings across reboots, copy the conf:"
echo "        sudo cp $SYSCTL_SRC /etc/sysctl.d/99-whisper.conf"
echo "    - Docker: these settings apply to the HOST kernel. For"
echo "      per-container limits, use --ulimit in docker run or the"
echo "      ulimits key in docker-compose.yml."
echo "============================================================"
