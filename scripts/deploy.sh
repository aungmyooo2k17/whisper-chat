#!/usr/bin/env bash
# =============================================================================
# Whisper — Production Deployment Script
# =============================================================================
# Single-command deployment for the full Whisper production stack with
# health validation, graceful shutdown, and operational subcommands.
#
# Usage:
#   ./scripts/deploy.sh [subcommand] [options]
#
# Subcommands:
#   up        Full deploy: build, start, health-check (default)
#   down      Graceful shutdown with connection draining
#   status    Check health of all services
#   logs      Follow logs (optionally for a specific service)
#   restart   Restart a specific service
#   build     Build images without deploying
#
# Options:
#   --no-build      Skip the build phase (use existing images)
#   --timeout N     Health check timeout in seconds (default: 60)
#   --dry-run       Print commands without executing
#   -h, --help      Show this help message
# =============================================================================

set -euo pipefail

# ---------------------------------------------------------------------------
# Project root — resolve relative to this script's location
# ---------------------------------------------------------------------------
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
COMPOSE_FILE="$PROJECT_ROOT/docker-compose.prod.yml"

# ---------------------------------------------------------------------------
# Defaults
# ---------------------------------------------------------------------------
SUBCOMMAND="up"
NO_BUILD=false
HEALTH_TIMEOUT=60
DRY_RUN=false
LOG_SERVICE=""
RESTART_SERVICE=""
DEPLOY_START_TIME=""

# ---------------------------------------------------------------------------
# All services in the stack, grouped by tier
# ---------------------------------------------------------------------------
INFRA_SERVICES=(redis nats postgres)
APP_SERVICES=(wsserver-1 wsserver-2 matcher moderator)
EDGE_SERVICES=(frontend haproxy)
OBSERVABILITY_SERVICES=(prometheus grafana)
ALL_SERVICES=("${INFRA_SERVICES[@]}" "${APP_SERVICES[@]}" "${EDGE_SERVICES[@]}" "${OBSERVABILITY_SERVICES[@]}")

# ---------------------------------------------------------------------------
# Color output — auto-detect TTY; strip colors for non-interactive use
# ---------------------------------------------------------------------------
if [[ -t 1 ]]; then
    RED='\033[0;31m'
    GREEN='\033[0;32m'
    YELLOW='\033[0;33m'
    BLUE='\033[0;34m'
    CYAN='\033[0;36m'
    BOLD='\033[1m'
    DIM='\033[2m'
    RESET='\033[0m'
else
    RED='' GREEN='' YELLOW='' BLUE='' CYAN='' BOLD='' DIM='' RESET=''
fi

# ---------------------------------------------------------------------------
# Logging helpers
# ---------------------------------------------------------------------------
info()  { printf "${BLUE}[INFO]${RESET}  %s\n" "$*"; }
ok()    { printf "${GREEN}[OK]${RESET}    %s\n" "$*"; }
warn()  { printf "${YELLOW}[WARN]${RESET}  %s\n" "$*"; }
fail()  { printf "${RED}[FAIL]${RESET}  %s\n" "$*"; }
step()  { printf "\n${BOLD}${CYAN}==> %s${RESET}\n" "$*"; }

# ---------------------------------------------------------------------------
# Dry-run wrapper — prints commands instead of executing when --dry-run is set
# ---------------------------------------------------------------------------
run() {
    if [[ "$DRY_RUN" == true ]]; then
        printf "${DIM}[dry-run] %s${RESET}\n" "$*"
        return 0
    fi
    "$@"
}

# ---------------------------------------------------------------------------
# Docker Compose shorthand
# ---------------------------------------------------------------------------
dc() {
    docker compose -f "$COMPOSE_FILE" "$@"
}

dc_run() {
    run docker compose -f "$COMPOSE_FILE" "$@"
}

# ---------------------------------------------------------------------------
# Usage / help
# ---------------------------------------------------------------------------
usage() {
    cat <<'USAGE'
Whisper Production Deployment

Usage:
  deploy.sh [subcommand] [options]

Subcommands:
  up              Full deploy: build, start, health-check (default)
  down            Graceful shutdown with connection draining
  status          Check health of all services
  logs [service]  Follow logs (optionally for a specific service)
  restart service Restart a specific service
  build           Build images without deploying

Options:
  --no-build      Skip the build phase during 'up'
  --timeout N     Health check timeout in seconds (default: 60)
  --dry-run       Print commands without executing
  -h, --help      Show this help message

Examples:
  deploy.sh                     # Full deploy (same as 'up')
  deploy.sh up --no-build       # Deploy using existing images
  deploy.sh down                # Graceful shutdown
  deploy.sh status              # Health check all services
  deploy.sh logs wsserver-1     # Follow wsserver-1 logs
  deploy.sh restart matcher     # Restart matcher service
  deploy.sh build               # Build images only
  deploy.sh up --dry-run        # Preview deploy commands
USAGE
}

# ---------------------------------------------------------------------------
# Argument parsing
# ---------------------------------------------------------------------------
parse_args() {
    while [[ $# -gt 0 ]]; do
        case "$1" in
            up|down|status|build)
                SUBCOMMAND="$1"
                shift
                ;;
            logs)
                SUBCOMMAND="logs"
                shift
                if [[ $# -gt 0 && ! "$1" =~ ^-- ]]; then
                    LOG_SERVICE="$1"
                    shift
                fi
                ;;
            restart)
                SUBCOMMAND="restart"
                shift
                if [[ $# -gt 0 && ! "$1" =~ ^-- ]]; then
                    RESTART_SERVICE="$1"
                    shift
                else
                    fail "restart requires a service name"
                    usage
                    exit 1
                fi
                ;;
            --no-build)
                NO_BUILD=true
                shift
                ;;
            --timeout)
                shift
                if [[ $# -eq 0 || ! "$1" =~ ^[0-9]+$ ]]; then
                    fail "--timeout requires a numeric argument"
                    exit 1
                fi
                HEALTH_TIMEOUT="$1"
                shift
                ;;
            --dry-run)
                DRY_RUN=true
                shift
                ;;
            -h|--help)
                usage
                exit 0
                ;;
            *)
                fail "Unknown argument: $1"
                usage
                exit 1
                ;;
        esac
    done
}

# =============================================================================
# Pre-flight checks
# =============================================================================
preflight() {
    step "Pre-flight checks"

    # -- Docker --
    if ! command -v docker &>/dev/null; then
        fail "Docker is not installed. Install Docker Engine: https://docs.docker.com/engine/install/"
        exit 1
    fi
    ok "Docker found: $(docker --version)"

    # -- Docker Compose (v2 plugin or standalone) --
    if docker compose version &>/dev/null; then
        ok "Docker Compose found: $(docker compose version --short)"
    elif command -v docker-compose &>/dev/null; then
        fail "docker-compose (v1) detected. Whisper requires Docker Compose v2 plugin."
        exit 1
    else
        fail "Docker Compose is not installed. Install the Compose plugin: https://docs.docker.com/compose/install/"
        exit 1
    fi

    # -- .env file --
    if [[ ! -f "$PROJECT_ROOT/.env" ]]; then
        fail ".env file not found at $PROJECT_ROOT/.env"
        info "Create it from the template:"
        info "  cp $PROJECT_ROOT/.env.production.example $PROJECT_ROOT/.env"
        info "Then fill in all CHANGE_ME_* values."
        exit 1
    fi
    ok ".env file found"

    # Check for unfilled placeholders
    if grep -q 'CHANGE_ME_' "$PROJECT_ROOT/.env"; then
        warn ".env contains CHANGE_ME_* placeholders — make sure all values are set before going live"
    fi

    # -- Required config files --
    local required_configs=(
        "haproxy/haproxy.prod.cfg"
        "config/redis.conf"
        "config/nats.conf"
    )
    for cfg in "${required_configs[@]}"; do
        if [[ ! -f "$PROJECT_ROOT/$cfg" ]]; then
            fail "Required config file missing: $cfg"
            exit 1
        fi
        ok "Config found: $cfg"
    done

    # -- SSL certificate (warn, not fatal — HTTP redirect still works) --
    if [[ ! -f "$PROJECT_ROOT/haproxy/certs/whisper.pem" ]]; then
        warn "SSL certificate not found at haproxy/certs/whisper.pem"
        warn "HTTPS will not work until a certificate is provided."
    else
        ok "SSL certificate found: haproxy/certs/whisper.pem"
    fi

    # -- Port conflicts --
    local ports=(80 443 3000)
    local port_conflict=false
    for port in "${ports[@]}"; do
        if ss -tlnp 2>/dev/null | grep -q ":${port} " || \
           lsof -iTCP:"$port" -sTCP:LISTEN -P -n 2>/dev/null | grep -q LISTEN; then
            # Check if it is our own stack occupying the port
            local container_id
            container_id=$(docker ps --filter "publish=${port}" --format '{{.ID}}' 2>/dev/null || true)
            if [[ -n "$container_id" ]]; then
                warn "Port $port is in use by an existing Docker container ($container_id) — will be replaced"
            else
                fail "Port $port is already in use by a non-Docker process"
                port_conflict=true
            fi
        fi
    done
    if [[ "$port_conflict" == true ]]; then
        fail "Resolve the port conflicts above before deploying."
        exit 1
    fi
    ok "Required ports (80, 443, 3000) are available"

    # -- Compose file exists --
    if [[ ! -f "$COMPOSE_FILE" ]]; then
        fail "Compose file not found: $COMPOSE_FILE"
        exit 1
    fi
    ok "Compose file found: docker-compose.prod.yml"
}

# =============================================================================
# Build phase
# =============================================================================
build_images() {
    step "Building images"
    dc_run build --progress=plain
    local rc=$?
    if [[ $rc -ne 0 ]]; then
        fail "Image build failed (exit code $rc)"
        exit 1
    fi
    ok "All images built successfully"
}

# =============================================================================
# Deploy phase
# =============================================================================
deploy_up() {
    step "Starting services"
    dc_run up -d
    local rc=$?
    if [[ $rc -ne 0 ]]; then
        fail "docker compose up failed (exit code $rc)"
        exit 1
    fi
    ok "All containers started"
}

# =============================================================================
# Health validation
# =============================================================================

# Check a single service's Docker health status.
# Returns 0 if healthy, 1 otherwise.
check_container_health() {
    local service="$1"
    local status
    status=$(dc ps --format '{{.Health}}' "$service" 2>/dev/null || echo "unknown")

    # Docker Compose may return "healthy", "Health: healthy", etc.
    if [[ "$status" == *"healthy"* ]]; then
        return 0
    fi

    # Fallback: check if the container is at least running
    local state
    state=$(dc ps --format '{{.State}}' "$service" 2>/dev/null || echo "unknown")
    if [[ "$state" == "running" ]]; then
        # For services without a healthcheck definition, running is acceptable
        return 0
    fi

    return 1
}

# HTTP health check via docker compose exec for wsserver instances.
check_wsserver_http() {
    local service="$1"
    local response
    response=$(dc exec -T "$service" wget -q -O - --timeout=5 http://localhost:8080/health 2>/dev/null || echo "")
    if [[ -n "$response" ]]; then
        return 0
    fi
    return 1
}

# Wait for a group of services to become healthy.
# Arguments: tier_name service1 service2 ...
wait_for_services() {
    local tier="$1"
    shift
    local services=("$@")

    step "Health checks — $tier"

    local max_retries=$(( HEALTH_TIMEOUT / 2 ))
    if [[ $max_retries -lt 1 ]]; then
        max_retries=1
    fi

    local failed_services=()

    for service in "${services[@]}"; do
        local attempt=0
        local healthy=false
        printf "  Waiting for %-14s " "$service"

        while [[ $attempt -lt $max_retries ]]; do
            if check_container_health "$service"; then
                # Extra HTTP check for wsserver instances
                if [[ "$service" == wsserver-* ]]; then
                    if check_wsserver_http "$service"; then
                        healthy=true
                        break
                    fi
                else
                    healthy=true
                    break
                fi
            fi
            attempt=$((attempt + 1))
            sleep 2
        done

        if [[ "$healthy" == true ]]; then
            printf "${GREEN}[OK]${RESET}\n"
        else
            printf "${RED}[FAIL]${RESET}\n"
            failed_services+=("$service")
        fi
    done

    if [[ ${#failed_services[@]} -gt 0 ]]; then
        return 1
    fi
    return 0
}

health_check_all() {
    local has_failure=false

    if ! wait_for_services "Infrastructure" "${INFRA_SERVICES[@]}"; then
        has_failure=true
    fi

    if ! wait_for_services "Application" "${APP_SERVICES[@]}"; then
        has_failure=true
    fi

    if ! wait_for_services "Edge / Frontend" "${EDGE_SERVICES[@]}"; then
        has_failure=true
    fi

    if ! wait_for_services "Observability" "${OBSERVABILITY_SERVICES[@]}"; then
        has_failure=true
    fi

    if [[ "$has_failure" == true ]]; then
        return 1
    fi
    return 0
}

# =============================================================================
# Post-deploy summary
# =============================================================================
print_summary() {
    local overall_status="$1"
    local deploy_end_time
    deploy_end_time=$(date +%s)
    local elapsed=$(( deploy_end_time - DEPLOY_START_TIME ))
    local minutes=$(( elapsed / 60 ))
    local seconds=$(( elapsed % 60 ))

    step "Deployment Summary"

    # Service status table
    printf "\n"
    printf "  ${BOLD}%-18s %-12s %-10s${RESET}\n" "SERVICE" "STATUS" "STATE"
    printf "  %-18s %-12s %-10s\n" "------------------" "------------" "----------"

    for service in "${ALL_SERVICES[@]}"; do
        local state health
        state=$(dc ps --format '{{.State}}' "$service" 2>/dev/null || echo "not found")
        health=$(dc ps --format '{{.Health}}' "$service" 2>/dev/null || echo "n/a")

        if [[ "$health" == *"healthy"* ]] || { [[ "$state" == "running" ]] && [[ -z "$health" || "$health" == "" ]]; }; then
            printf "  %-18s ${GREEN}%-12s${RESET} %-10s\n" "$service" "OK" "$state"
        elif [[ "$state" == "running" ]]; then
            printf "  %-18s ${YELLOW}%-12s${RESET} %-10s\n" "$service" "starting" "$state"
        else
            printf "  %-18s ${RED}%-12s${RESET} %-10s\n" "$service" "FAIL" "$state"
        fi
    done

    # Access URLs
    printf "\n  ${BOLD}Access URLs:${RESET}\n"
    printf "  %-22s %s\n" "Frontend (HTTP):"  "http://localhost:3000"
    printf "  %-22s %s\n" "Frontend (HTTPS):"  "https://localhost"
    printf "  %-22s %s\n" "HAProxy Stats:"    "http://localhost:8404/haproxy/stats"
    printf "  %-22s %s\n" "Prometheus:"        "(internal — via Grafana datasource)"
    printf "  %-22s %s\n" "Grafana:"           "(expose port in compose or access via proxy)"

    # Deployment time
    printf "\n  ${BOLD}Deployment time:${RESET} %dm %ds\n" "$minutes" "$seconds"

    if [[ "$overall_status" == "ok" ]]; then
        printf "\n  ${GREEN}${BOLD}Deployment completed successfully.${RESET}\n\n"
    else
        printf "\n  ${RED}${BOLD}Deployment completed with errors — check failing services above.${RESET}\n\n"
    fi
}

# =============================================================================
# Subcommand: status
# =============================================================================
cmd_status() {
    step "Service Status"
    health_check_all
    local rc=$?

    # Print a quick table even without full deploy timing
    printf "\n"
    printf "  ${BOLD}%-18s %-12s %-10s${RESET}\n" "SERVICE" "STATUS" "STATE"
    printf "  %-18s %-12s %-10s\n" "------------------" "------------" "----------"

    for service in "${ALL_SERVICES[@]}"; do
        local state health
        state=$(dc ps --format '{{.State}}' "$service" 2>/dev/null || echo "not found")
        health=$(dc ps --format '{{.Health}}' "$service" 2>/dev/null || echo "n/a")

        if [[ "$health" == *"healthy"* ]] || { [[ "$state" == "running" ]] && [[ -z "$health" || "$health" == "" ]]; }; then
            printf "  %-18s ${GREEN}%-12s${RESET} %-10s\n" "$service" "OK" "$state"
        elif [[ "$state" == "running" ]]; then
            printf "  %-18s ${YELLOW}%-12s${RESET} %-10s\n" "$service" "starting" "$state"
        else
            printf "  %-18s ${RED}%-12s${RESET} %-10s\n" "$service" "FAIL" "$state"
        fi
    done
    printf "\n"
    return $rc
}

# =============================================================================
# Subcommand: down (graceful shutdown with connection draining)
# =============================================================================
cmd_down() {
    step "Graceful shutdown"

    info "Sending SIGTERM to allow connection draining..."
    dc_run stop --timeout 30
    local rc=$?

    if [[ $rc -eq 0 ]]; then
        info "Removing containers and networks..."
        dc_run down --remove-orphans
        ok "Stack stopped and cleaned up"
    else
        warn "Graceful stop timed out — forcing removal"
        dc_run down --remove-orphans --timeout 10
    fi
}

# =============================================================================
# Subcommand: logs
# =============================================================================
cmd_logs() {
    if [[ -n "$LOG_SERVICE" ]]; then
        info "Following logs for: $LOG_SERVICE"
        dc logs -f "$LOG_SERVICE"
    else
        info "Following logs for all services"
        dc logs -f
    fi
}

# =============================================================================
# Subcommand: restart
# =============================================================================
cmd_restart() {
    local service="$RESTART_SERVICE"

    # Validate the service name
    local valid=false
    for s in "${ALL_SERVICES[@]}"; do
        if [[ "$s" == "$service" ]]; then
            valid=true
            break
        fi
    done
    if [[ "$valid" != true ]]; then
        fail "Unknown service: $service"
        info "Valid services: ${ALL_SERVICES[*]}"
        exit 1
    fi

    step "Restarting $service"
    dc_run restart "$service"
    info "Waiting for $service to become healthy..."
    wait_for_services "$service" "$service"
}

# =============================================================================
# Subcommand: build
# =============================================================================
cmd_build() {
    preflight
    build_images
}

# =============================================================================
# Subcommand: up (full deploy)
# =============================================================================
cmd_up() {
    DEPLOY_START_TIME=$(date +%s)

    preflight

    if [[ "$NO_BUILD" == false ]]; then
        build_images
    else
        info "Skipping build phase (--no-build)"
    fi

    deploy_up

    if [[ "$DRY_RUN" == true ]]; then
        info "Dry run complete — no health checks performed"
        return 0
    fi

    if health_check_all; then
        print_summary "ok"
    else
        print_summary "error"
        fail "Some services failed health checks. Inspect with:"
        info "  $0 logs <service>"
        info "  $0 status"
        exit 1
    fi
}

# =============================================================================
# Main
# =============================================================================
main() {
    parse_args "$@"

    # Ensure we run from the project root so relative paths in compose work
    cd "$PROJECT_ROOT"

    case "$SUBCOMMAND" in
        up)      cmd_up      ;;
        down)    cmd_down    ;;
        status)  cmd_status  ;;
        logs)    cmd_logs    ;;
        restart) cmd_restart ;;
        build)   cmd_build   ;;
        *)
            fail "Unknown subcommand: $SUBCOMMAND"
            usage
            exit 1
            ;;
    esac
}

main "$@"
