# Whisper Deployment Guide and Operations Runbook

Complete guide for deploying, operating, and troubleshooting the Whisper anonymous
real-time chat application.

---

## Table of Contents

1. [Prerequisites](#1-prerequisites)
2. [Quick Start](#2-quick-start)
3. [Configuration](#3-configuration)
4. [Deployment](#4-deployment)
5. [Operations Runbook](#5-operations-runbook)
6. [Monitoring](#6-monitoring)
7. [Troubleshooting](#7-troubleshooting)
8. [Security Checklist](#8-security-checklist)
9. [Architecture Reference](#9-architecture-reference)

---

## 1. Prerequisites

### 1.1 System Requirements

| Resource | Minimum (10K connections) | Recommended (100K connections) | High Scale (1M connections) |
|----------|---------------------------|--------------------------------|-----------------------------|
| OS       | Linux (kernel 5.x+)       | Linux (kernel 5.x+)           | Linux (kernel 5.x+)        |
| CPU      | 2 cores                   | 4-8 cores                     | 16 cores                   |
| RAM      | 4 GB                      | 16 GB                         | 64 GB                      |
| Disk     | 20 GB SSD                 | 50 GB SSD                     | 100 GB SSD                 |

Disk space breakdown:
- Docker images: ~2 GB
- PostgreSQL data: ~1 GB (abuse reports only)
- Prometheus data: ~10-20 GB (15-day retention)
- Grafana data: ~500 MB
- Log files: ~5 GB (with rotation enabled)
- NATS JetStream data: ~1 GB

### 1.2 Software Requirements

| Software         | Version   | Purpose                                 |
|------------------|-----------|-----------------------------------------|
| Docker Engine    | 24.0+     | Container runtime                       |
| Docker Compose   | v2.20+    | Service orchestration                   |
| Git              | 2.x+      | Repository clone                        |
| openssl          | 1.1+      | SSL certificate generation (self-signed)|
| certbot          | (latest)  | Let's Encrypt certificate automation    |
| make             | (any)     | Build automation (optional, for dev)    |

Verify installed versions:

```bash
docker --version
docker compose version
git --version
openssl version
```

### 1.3 Network Requirements

#### Ports

| Port | Protocol | Direction | Purpose                        |
|------|----------|-----------|--------------------------------|
| 80   | TCP      | Inbound   | HTTP (redirects to HTTPS)      |
| 443  | TCP      | Inbound   | HTTPS / WSS (SSL termination)  |
| 3000 | TCP      | Inbound   | Frontend direct access (nginx) |
| 8404 | TCP      | Internal  | HAProxy stats dashboard        |

All other ports (Redis 6379, NATS 4222/8222, PostgreSQL 5432, wsserver 8080,
Prometheus 9090, Grafana 3000) are internal to the Docker network and are
**not** exposed to the host in production.

#### Firewall Rules

```bash
# Allow HTTP and HTTPS
sudo ufw allow 80/tcp
sudo ufw allow 443/tcp

# Allow frontend direct access (optional; HAProxy routes to it)
sudo ufw allow 3000/tcp

# Deny everything else from external sources
sudo ufw default deny incoming
sudo ufw default allow outgoing
sudo ufw enable
```

#### DNS

Point your domain (e.g., `chat.example.com`) to the server's public IP address
with an A record.

---

## 2. Quick Start

### 2.1 Development Mode

Development mode runs all services with a single wsserver instance, exposed
ports for direct access, and no SSL.

```bash
# 1. Clone the repository
git clone <repository-url> whisper
cd whisper

# 2. Start all services (infrastructure + app)
docker compose up -d

# 3. Verify all containers are running
docker compose ps

# 4. Open the frontend
#    Frontend: http://localhost:3000
#    wsserver: http://localhost:8080/health
#    HAProxy:  http://localhost:8088
#    Grafana:  http://localhost:3001 (admin / whisper)
#    Prometheus: http://localhost:9090
#    NATS monitoring: http://localhost:8222
```

To run backend services locally outside Docker (for live-reload development):

```bash
# Start only infrastructure containers
make docker-up

# Run backend services in separate terminals
make run             # wsserver on :8080
make run-matcher     # matcher service
make run-moderator   # moderator service

# Start frontend dev server (Vite with hot-reload)
make fe-install
make fe-dev          # dev server on :5173
```

### 2.2 Production Mode

Production mode runs two wsserver instances behind HAProxy with SSL termination,
network isolation, resource limits, and log rotation.

```bash
# 1. Clone the repository
git clone <repository-url> whisper
cd whisper

# 2. Create and configure the environment file
cp .env.production.example .env
# Edit .env and replace ALL "CHANGE_ME_*" placeholders with real values
# (see Section 3 for details on each variable)

# 3. Generate SSL certificates (see Section 3.2 for details)
mkdir -p haproxy/certs
# For testing with self-signed certs:
openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
  -keyout haproxy/certs/whisper.key \
  -out haproxy/certs/whisper.crt \
  -subj "/CN=chat.example.com"
cat haproxy/certs/whisper.crt haproxy/certs/whisper.key > haproxy/certs/whisper.pem

# 4. Copy the production HAProxy config into place
cp haproxy/haproxy.prod.cfg haproxy/haproxy.cfg

# 5. (Optional) Apply kernel tuning for high connection counts
sudo ./scripts/tune-kernel.sh

# 6. Build and start all services
docker compose -f docker-compose.prod.yml up -d --build

# 7. Run database migrations
docker compose -f docker-compose.prod.yml exec wsserver-1 \
  /wsserver --migrate  2>/dev/null || \
  docker compose -f docker-compose.prod.yml exec postgres \
  psql -U "${POSTGRES_USER}" -d "${POSTGRES_DB}" \
  -f /migrations/001_create_abuse_reports.up.sql

# 8. Verify deployment health
docker compose -f docker-compose.prod.yml ps
curl -k https://localhost/health      # via HAProxy (returns 200 OK)
curl http://localhost:3000             # frontend direct access
```

---

## 3. Configuration

### 3.1 Environment Variables

Copy `.env.production.example` to `.env` and configure all values. Never commit
the filled-in `.env` file to version control.

#### PostgreSQL

| Variable            | Default              | Description                                        |
|---------------------|----------------------|----------------------------------------------------|
| `POSTGRES_DB`       | `whisper`            | Database name                                      |
| `POSTGRES_USER`     | `whisper_prod`       | Database user                                      |
| `POSTGRES_PASSWORD` | `CHANGE_ME_*`        | Database password. Use a strong random string (32+ characters). |

#### Redis

| Variable     | Default       | Description                          |
|--------------|---------------|--------------------------------------|
| `REDIS_ADDR` | `redis:6379`  | Redis connection address (host:port) |

#### NATS

| Variable   | Default              | Description                   |
|------------|----------------------|-------------------------------|
| `NATS_URL` | `nats://nats:4222`   | NATS server connection URL    |

#### WebSocket Server (wsserver)

| Variable           | Default   | Description                                                                 |
|--------------------|-----------|-----------------------------------------------------------------------------|
| `LISTEN_ADDR`      | `:8080`   | Address the wsserver listens on inside the container                        |
| `DATABASE_URL`     | (see below) | PostgreSQL connection string. Must match POSTGRES_USER/PASSWORD.          |
| `SERVER_NAME`      | `ws-prod-1` | Unique name per wsserver instance. Used for session namespacing.          |
| `WORKER_POOL_SIZE` | `512`     | Max concurrent goroutines for WebSocket frame processing                    |
| `MAX_CONNECTIONS`  | `100000`  | Hard cap on accepted WebSocket connections per instance                     |
| `READ_TIMEOUT`     | `10s`     | Deadline on WebSocket frame reads                                           |
| `WRITE_TIMEOUT`    | `10s`     | Deadline on WebSocket frame writes                                          |

The `DATABASE_URL` format:

```
postgres://<POSTGRES_USER>:<POSTGRES_PASSWORD>@postgres:5432/<POSTGRES_DB>?sslmode=disable
```

#### Frontend (Build-time)

| Variable       | Example                        | Description                                 |
|----------------|--------------------------------|---------------------------------------------|
| `VITE_WS_URL`  | `wss://chat.example.com/ws`    | WebSocket URL the frontend connects to      |
| `VITE_API_URL` | `https://chat.example.com`     | API base URL for REST endpoints             |

These values are baked into the frontend at build time. Changing them requires
rebuilding the frontend image.

#### Grafana

| Variable                     | Default     | Description                        |
|------------------------------|-------------|------------------------------------|
| `GF_SECURITY_ADMIN_PASSWORD` | `CHANGE_ME_*` | Grafana admin password           |
| `GF_USERS_ALLOW_SIGN_UP`    | `false`     | Disable public user registration   |

### 3.2 SSL Certificate Setup

HAProxy expects a PEM file at `haproxy/certs/whisper.pem` containing the
certificate chain and private key concatenated together.

#### Self-Signed Certificate (Testing Only)

```bash
mkdir -p haproxy/certs

# Generate a self-signed certificate valid for 365 days
openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
  -keyout haproxy/certs/whisper.key \
  -out haproxy/certs/whisper.crt \
  -subj "/C=US/ST=State/L=City/O=Whisper/CN=chat.example.com"

# Combine into a single PEM file for HAProxy
cat haproxy/certs/whisper.crt haproxy/certs/whisper.key > haproxy/certs/whisper.pem
chmod 600 haproxy/certs/whisper.pem
```

#### Let's Encrypt Certificate (Production)

```bash
# 1. Install certbot
sudo apt install certbot

# 2. Stop HAProxy temporarily to free port 80
docker compose -f docker-compose.prod.yml stop haproxy

# 3. Obtain the certificate (standalone mode)
sudo certbot certonly --standalone \
  -d chat.example.com \
  --agree-tos \
  --email admin@example.com

# 4. Combine into HAProxy PEM format
sudo cat /etc/letsencrypt/live/chat.example.com/fullchain.pem \
         /etc/letsencrypt/live/chat.example.com/privkey.pem \
  > haproxy/certs/whisper.pem
chmod 600 haproxy/certs/whisper.pem

# 5. Restart HAProxy
docker compose -f docker-compose.prod.yml start haproxy
```

**Certificate Renewal** (set up a cron job):

```bash
# /etc/cron.d/whisper-cert-renew
0 3 * * 1 root certbot renew --quiet --deploy-hook "\
  cat /etc/letsencrypt/live/chat.example.com/fullchain.pem \
      /etc/letsencrypt/live/chat.example.com/privkey.pem \
  > /path/to/whisper/haproxy/certs/whisper.pem && \
  docker compose -f /path/to/whisper/docker-compose.prod.yml restart haproxy"
```

---

## 4. Deployment

### 4.1 First-Time Deployment Procedure

1. **Prepare the server**: Install Docker, Docker Compose, and Git.

2. **Clone the repository**:
   ```bash
   git clone <repository-url> /opt/whisper
   cd /opt/whisper
   ```

3. **Configure environment**:
   ```bash
   cp .env.production.example .env
   # Edit .env -- replace every CHANGE_ME_* placeholder
   ```

4. **Set up SSL certificates** (see Section 3.2).

5. **Copy production HAProxy config**:
   ```bash
   cp haproxy/haproxy.prod.cfg haproxy/haproxy.cfg
   ```

6. **Apply kernel tuning** (recommended for >10K connections):
   ```bash
   sudo ./scripts/tune-kernel.sh
   # Persist across reboots:
   sudo cp scripts/sysctl-whisper.conf /etc/sysctl.d/99-whisper.conf
   ```

7. **Build and launch**:
   ```bash
   docker compose -f docker-compose.prod.yml up -d --build
   ```

8. **Run database migrations**:
   ```bash
   docker compose -f docker-compose.prod.yml exec postgres \
     psql -U whisper_prod -d whisper \
     -f /dev/stdin < migrations/001_create_abuse_reports.up.sql
   ```

9. **Verify health**:
   ```bash
   # All containers should be "Up" and "(healthy)" where applicable
   docker compose -f docker-compose.prod.yml ps

   # HAProxy health endpoint
   curl -k https://localhost/health

   # Frontend
   curl -s -o /dev/null -w "%{http_code}" http://localhost:3000
   ```

10. **Set up monitoring access** (optional):
    Grafana and Prometheus are on the internal backend network by default.
    To access them, either:
    - Add port mappings in a `docker-compose.override.yml`
    - Use SSH tunneling: `ssh -L 3001:localhost:3001 user@server`

### 4.2 Verifying Deployment Health

```bash
# Check all container statuses
docker compose -f docker-compose.prod.yml ps

# Check individual service health
docker compose -f docker-compose.prod.yml exec redis redis-cli ping
# Expected: PONG

docker compose -f docker-compose.prod.yml exec nats \
  wget -qO- http://localhost:8222/healthz
# Expected: ok

docker compose -f docker-compose.prod.yml exec postgres \
  pg_isready -U whisper_prod -d whisper
# Expected: accepting connections

# Test WebSocket connectivity through HAProxy
curl -k -i \
  -H "Connection: Upgrade" \
  -H "Upgrade: websocket" \
  -H "Sec-WebSocket-Version: 13" \
  -H "Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==" \
  https://localhost/ws
# Expected: HTTP/1.1 101 Switching Protocols
```

---

## 5. Operations Runbook

### 5.1 Starting and Stopping

```bash
# Start all services
docker compose -f docker-compose.prod.yml up -d

# Stop all services (containers are removed)
docker compose -f docker-compose.prod.yml down

# Stop all services and remove volumes (DATA LOSS -- use only for full reset)
docker compose -f docker-compose.prod.yml down -v

# Restart a specific service
docker compose -f docker-compose.prod.yml restart wsserver-1

# Stop a specific service without removing it
docker compose -f docker-compose.prod.yml stop wsserver-2
```

**Expected startup order** (enforced by `depends_on` with health checks):

1. `redis`, `nats`, `postgres` (infrastructure, start in parallel)
2. `wsserver-1`, `wsserver-2`, `matcher`, `moderator` (after infrastructure is healthy)
3. `haproxy` (after wsserver instances)
4. `frontend` (after wsserver instances)
5. `prometheus` (after wsserver instances)
6. `grafana` (after prometheus)

### 5.2 Scaling

#### Adding More wsserver Instances

1. **Add the new instance to `docker-compose.prod.yml`**:
   ```yaml
   wsserver-3:
     build:
       context: .
       dockerfile: cmd/wsserver/Dockerfile
     environment:
       LISTEN_ADDR: ${LISTEN_ADDR}
       REDIS_ADDR: ${REDIS_ADDR}
       NATS_URL: ${NATS_URL}
       DATABASE_URL: ${DATABASE_URL}
       SERVER_NAME: ws-prod-3
       WORKER_POOL_SIZE: ${WORKER_POOL_SIZE:-512}
       MAX_CONNECTIONS: ${MAX_CONNECTIONS:-100000}
       READ_TIMEOUT: ${READ_TIMEOUT:-10s}
       WRITE_TIMEOUT: ${WRITE_TIMEOUT:-10s}
     depends_on:
       redis:
         condition: service_healthy
       nats:
         condition: service_healthy
       postgres:
         condition: service_healthy
     restart: unless-stopped
     logging: *default-logging
     deploy:
       resources:
         limits:
           memory: 4g
           cpus: "4"
     networks:
       - whisper-frontend
       - whisper-backend
   ```

2. **Add the backend server to `haproxy/haproxy.cfg`** (or `haproxy.prod.cfg`):
   ```
   backend ws_servers
       # ... existing config ...
       server wsserver-3 wsserver-3:8080 check cookie wsserver-3 slowstart 30s
   ```

3. **Update `haproxy` depends_on in `docker-compose.prod.yml`**:
   ```yaml
   haproxy:
     depends_on:
       - wsserver-1
       - wsserver-2
       - wsserver-3
   ```

4. **Update Prometheus scrape targets** in `monitoring/prometheus/prometheus.yml`:
   ```yaml
   scrape_configs:
     - job_name: 'wsserver'
       static_configs:
         - targets: ['wsserver-1:8080', 'wsserver-2:8080', 'wsserver-3:8080']
   ```

5. **Rebuild and restart**:
   ```bash
   docker compose -f docker-compose.prod.yml up -d --build wsserver-3
   docker compose -f docker-compose.prod.yml restart haproxy
   docker compose -f docker-compose.prod.yml restart prometheus
   ```

No code changes are required. Redis and NATS handle cross-instance communication
transparently because session state is stored in Redis and chat messages are
routed through NATS subjects keyed by session ID.

### 5.3 Updating (Rolling Update)

Rolling updates allow zero-downtime deployments by restarting one wsserver
instance at a time. HAProxy automatically routes traffic away from servers
that fail health checks.

```bash
# 1. Pull the latest code
git pull origin main

# 2. Build new images (does not affect running containers)
docker compose -f docker-compose.prod.yml build

# 3. Drain and restart wsserver-1
#    HAProxy will detect the health check failure and redirect traffic to wsserver-2.
#    The wsserver gracefully shuts down with a 30-second drain period.
docker compose -f docker-compose.prod.yml up -d --no-deps wsserver-1

# 4. Wait for wsserver-1 to become healthy
docker compose -f docker-compose.prod.yml exec wsserver-1 wget -qO- http://localhost:8080/health
# Alternatively, watch HAProxy stats to confirm wsserver-1 is back UP

# 5. Repeat for wsserver-2
docker compose -f docker-compose.prod.yml up -d --no-deps wsserver-2

# 6. Update stateless services (no drain needed)
docker compose -f docker-compose.prod.yml up -d --no-deps matcher moderator frontend

# 7. Verify
docker compose -f docker-compose.prod.yml ps
curl -k https://localhost/health
```

### 5.4 Backup

#### What to Back Up

| Data                 | Location (Docker volume) | Priority | Notes                                    |
|----------------------|--------------------------|----------|------------------------------------------|
| PostgreSQL data      | `postgres-data`          | High     | Abuse reports; needed for ban enforcement |
| `.env` file          | Repository root          | High     | Contains secrets                         |
| SSL certificates     | `haproxy/certs/`         | High     | Required for HTTPS                       |
| Grafana dashboards   | `grafana-data`           | Medium   | Custom dashboards and alerts             |
| Prometheus data      | `prometheus-data`        | Low      | Historical metrics (can be regenerated)  |
| Redis data           | `redis-data`             | Low      | Ephemeral session data; persistence is disabled by default |
| NATS JetStream data  | `nats-data`              | Low      | Transient message streams                |

#### PostgreSQL Backup

```bash
# Create a SQL dump
docker compose -f docker-compose.prod.yml exec postgres \
  pg_dump -U whisper_prod -d whisper --format=custom \
  > backup/whisper_$(date +%Y%m%d_%H%M%S).dump

# Restore from a dump
docker compose -f docker-compose.prod.yml exec -T postgres \
  pg_restore -U whisper_prod -d whisper --clean \
  < backup/whisper_20260228_120000.dump
```

#### Automated Backup Script

```bash
#!/bin/bash
# /opt/whisper/scripts/backup.sh
BACKUP_DIR="/opt/whisper/backup"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
mkdir -p "$BACKUP_DIR"

# PostgreSQL
docker compose -f /opt/whisper/docker-compose.prod.yml exec -T postgres \
  pg_dump -U whisper_prod -d whisper --format=custom \
  > "$BACKUP_DIR/db_$TIMESTAMP.dump"

# Environment file
cp /opt/whisper/.env "$BACKUP_DIR/env_$TIMESTAMP"

# SSL certs
cp -r /opt/whisper/haproxy/certs "$BACKUP_DIR/certs_$TIMESTAMP"

# Retain last 30 days
find "$BACKUP_DIR" -type f -mtime +30 -delete
```

Add to cron:
```bash
0 2 * * * /opt/whisper/scripts/backup.sh >> /var/log/whisper-backup.log 2>&1
```

### 5.5 Log Management

#### Where Logs Are

All services use the Docker `json-file` logging driver with rotation configured
in `docker-compose.prod.yml`:

```yaml
logging:
  driver: json-file
  options:
    max-size: "10m"   # Rotate at 10 MB
    max-file: "5"     # Keep 5 rotated files
```

Maximum disk usage per service: 50 MB (10 MB x 5 files).
Maximum total log disk usage: ~500 MB (10 services x 50 MB).

#### Accessing Logs

```bash
# Follow all service logs
docker compose -f docker-compose.prod.yml logs -f

# Follow a specific service
docker compose -f docker-compose.prod.yml logs -f wsserver-1

# Last 100 lines from a service
docker compose -f docker-compose.prod.yml logs --tail=100 wsserver-1

# Logs from a specific time range
docker compose -f docker-compose.prod.yml logs --since="2026-02-28T10:00:00" wsserver-1

# View raw log files on disk
ls /var/lib/docker/containers/<container-id>/<container-id>-json.log
```

### 5.6 Database Migrations

Migrations are stored in the `migrations/` directory using numbered up/down SQL
files. The wsserver Docker image copies migrations to `/migrations/` inside the
container.

#### Running Migrations

```bash
# Apply the next migration
docker compose -f docker-compose.prod.yml exec postgres \
  psql -U whisper_prod -d whisper \
  -f /dev/stdin < migrations/001_create_abuse_reports.up.sql

# Roll back the last migration
docker compose -f docker-compose.prod.yml exec postgres \
  psql -U whisper_prod -d whisper \
  -f /dev/stdin < migrations/001_create_abuse_reports.down.sql
```

#### Checking Current Schema

```bash
# List all tables
docker compose -f docker-compose.prod.yml exec postgres \
  psql -U whisper_prod -d whisper -c "\dt"

# Describe a specific table
docker compose -f docker-compose.prod.yml exec postgres \
  psql -U whisper_prod -d whisper -c "\d abuse_reports"
```

---

## 6. Monitoring

### 6.1 Grafana Access

In production, Grafana is on the internal `whisper-backend` network and is not
exposed to the host by default.

**Access via SSH tunnel** (recommended):

```bash
# From your local machine
ssh -L 3001:localhost:3001 user@your-server

# Then open http://localhost:3001 in your browser
```

**Or add a port mapping** in a `docker-compose.override.yml`:

```yaml
services:
  grafana:
    ports:
      - "3001:3000"
```

**Default credentials**: `admin` / (value of `GF_SECURITY_ADMIN_PASSWORD` from `.env`)

In development mode, Grafana is accessible at `http://localhost:3001` with
username `admin` and password `whisper`.

### 6.2 Key Dashboards

Two dashboards are provisioned automatically:

| Dashboard            | File                                             | Purpose                           |
|----------------------|--------------------------------------------------|-----------------------------------|
| **Whisper**          | `monitoring/grafana/dashboards/whisper.json`     | Production operations monitoring  |
| **Whisper Loadtest** | `monitoring/grafana/dashboards/whisper-loadtest.json` | Load test analysis           |

Key panels to watch in the **Whisper** dashboard:

- **Active Connections**: Current total WebSocket connections (`whisper_connections_total`)
- **Message Throughput**: Messages sent/received per second
- **Match Queue Depth**: Users waiting for a match (`whisper_match_queue_size`)
- **Active Chats**: Currently paired conversations (`whisper_active_chats`)
- **Message Latency**: p50/p95/p99 processing latency

### 6.3 Prometheus Queries for Common Checks

Access Prometheus at `http://localhost:9090` (dev) or via SSH tunnel (prod).

**Connection health**:

```promql
# Current total connections
whisper_connections_total

# Connection rate (connections per second)
rate(whisper_connections_total[5m])
```

**Message throughput**:

```promql
# Messages sent per second
rate(whisper_messages_total{type="sent"}[5m])

# Messages blocked by moderation per second
rate(whisper_messages_total{type="blocked"}[5m])
```

**Latency percentiles**:

```promql
# Message processing latency p99
histogram_quantile(0.99, rate(whisper_message_latency_seconds_bucket[5m]))

# Match duration p95 (time to find a partner)
histogram_quantile(0.95, rate(whisper_match_duration_seconds_bucket[5m]))
```

**Resource usage**:

```promql
# Go heap memory in use
go_memstats_heap_inuse_bytes

# Goroutine count (should stay near WORKER_POOL_SIZE, not grow with connections)
go_goroutines

# GC pause duration p99
histogram_quantile(0.99, rate(go_gc_duration_seconds_bucket[5m]))
```

**Matching health**:

```promql
# Queue depth (should stay near zero when matcher is healthy)
whisper_match_queue_size

# Active chat pairs
whisper_active_chats
```

### 6.4 Alert Conditions Worth Monitoring

| Condition                  | Query / Check                                            | Threshold              | Severity |
|----------------------------|----------------------------------------------------------|------------------------|----------|
| Service down               | Container not running                                    | Any service unhealthy  | Critical |
| High memory usage          | `go_memstats_heap_inuse_bytes`                           | > 80% of container limit | Warning |
| Goroutine leak             | `go_goroutines`                                          | > 5,000                | Warning  |
| GC thrashing               | `rate(go_gc_duration_seconds_count[1m])`                 | > 10 cycles/sec        | Warning  |
| Match queue backing up     | `whisper_match_queue_size`                                | > 10,000 and growing   | Warning  |
| High message latency       | `histogram_quantile(0.99, rate(whisper_message_latency_seconds_bucket[5m]))` | > 500ms | Warning |
| Redis memory high          | `redis_used_memory_rss` (via NATS exporter or manual)    | > 80% of maxmemory     | Warning  |
| Connection stall           | `deriv(whisper_connections_total[5m]) < 1` during ramp   | Unexpected             | Critical |
| High error rate            | `rate(whisper_messages_total{type="blocked"}[1m]) / rate(whisper_messages_total[1m])` | > 5% | Warning |
| HAProxy backend down       | HAProxy stats page shows backend as DOWN                 | Any backend            | Critical |

---

## 7. Troubleshooting

### 7.1 Common Issues and Solutions

#### "Connection refused" on port 443

**Symptoms**: `curl: (7) Failed to connect to localhost port 443: Connection refused`

**Causes and fixes**:

1. **HAProxy is not running**:
   ```bash
   docker compose -f docker-compose.prod.yml ps haproxy
   docker compose -f docker-compose.prod.yml logs haproxy
   # If config error: check haproxy/haproxy.cfg syntax
   docker compose -f docker-compose.prod.yml exec haproxy haproxy -c -f /usr/local/etc/haproxy/haproxy.cfg
   ```

2. **Port conflict**: Another service is already using port 80 or 443.
   ```bash
   sudo ss -tlnp | grep -E ':80|:443'
   # Kill conflicting service or change HAProxy ports
   ```

3. **SSL certificate missing or invalid**:
   ```bash
   ls -la haproxy/certs/whisper.pem
   openssl x509 -in haproxy/certs/whisper.crt -text -noout | head -20
   ```

#### "WebSocket connection failed"

**Symptoms**: Frontend shows "Disconnected" or browser console shows
`WebSocket connection to 'wss://...' failed`.

**Causes and fixes**:

1. **Wrong VITE_WS_URL**: The frontend was built with an incorrect WebSocket
   URL. Check the `.env` value and rebuild the frontend:
   ```bash
   docker compose -f docker-compose.prod.yml build --no-cache frontend
   docker compose -f docker-compose.prod.yml up -d frontend
   ```

2. **HAProxy not routing WebSocket upgrades**: Verify the ACL rules in
   `haproxy/haproxy.cfg`:
   ```bash
   # Test WebSocket upgrade through HAProxy
   curl -k -i \
     -H "Connection: Upgrade" \
     -H "Upgrade: websocket" \
     -H "Sec-WebSocket-Version: 13" \
     -H "Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==" \
     https://localhost/ws
   ```

3. **Backend wsserver not healthy**:
   ```bash
   docker compose -f docker-compose.prod.yml logs wsserver-1
   docker compose -f docker-compose.prod.yml logs wsserver-2
   ```

4. **SSL certificate mismatch**: The domain in the certificate does not match
   the domain in `VITE_WS_URL`. Regenerate the certificate with the correct CN.

#### "Match timeout" (users not getting matched)

**Symptoms**: Users wait 30 seconds and receive a "no match found" message.

**Causes and fixes**:

1. **Matcher service not running**:
   ```bash
   docker compose -f docker-compose.prod.yml ps matcher
   docker compose -f docker-compose.prod.yml logs matcher
   ```

2. **Redis is down or unreachable**:
   ```bash
   docker compose -f docker-compose.prod.yml exec redis redis-cli ping
   docker compose -f docker-compose.prod.yml exec redis redis-cli info clients
   ```

3. **NATS is down** (matcher cannot publish match results):
   ```bash
   docker compose -f docker-compose.prod.yml exec nats \
     wget -qO- http://localhost:8222/healthz
   ```

4. **Only one user in the queue** (expected behavior with low traffic):
   ```bash
   docker compose -f docker-compose.prod.yml exec redis \
     redis-cli ZCARD match:queue
   ```

#### High Memory Usage

**Symptoms**: Container OOM-killed or host swapping.

**Diagnosis**:

```bash
# Check container memory usage
docker stats --no-stream

# Check Go heap
curl http://wsserver-1:8080/metrics | grep go_memstats_heap_inuse_bytes

# Check goroutine count (leak indicator)
curl http://wsserver-1:8080/metrics | grep go_goroutines
```

**Fixes**:

- **Connection leak**: If goroutine count grows proportionally to connections,
  there may be a subscription leak in NATS. Restart the affected wsserver.
- **GC tuning**: Set `GOGC=200` and `GOMEMLIMIT` to 80% of available memory.
- **Reduce container memory limit** to trigger faster GC pressure.

#### High Latency

**Symptoms**: Users report slow message delivery (>500ms).

**Diagnosis**:

```bash
# Check Redis slow queries
docker compose -f docker-compose.prod.yml exec redis \
  redis-cli SLOWLOG GET 20

# Check NATS slow consumers
docker compose -f docker-compose.prod.yml exec nats \
  wget -qO- 'http://localhost:8222/connz?subs=true' | \
  python3 -c "import sys,json; [print(c['name'],c.get('pending_bytes',0)) for c in json.load(sys.stdin)['connections']]"

# Check wsserver metrics
curl http://wsserver-1:8080/metrics | grep whisper_message_latency
```

**Fixes**:

- **Redis slow queries**: Enable `io-threads` in Redis config, increase pool
  size in the Go client, or optimize matching pipeline operations.
- **NATS slow consumers**: Increase `WORKER_POOL_SIZE` on the affected wsserver,
  or add more wsserver instances.
- **CPU saturation**: Check `docker stats`, consider scaling horizontally.

### 7.2 Diagnostic Commands

```bash
# View logs for a specific service
docker compose -f docker-compose.prod.yml logs -f <service>
# where <service> is: wsserver-1, wsserver-2, matcher, moderator, frontend,
#                      redis, nats, postgres, haproxy, prometheus, grafana

# Enter a container shell
docker compose -f docker-compose.prod.yml exec <service> sh

# Check Redis state
docker compose -f docker-compose.prod.yml exec redis redis-cli
# Useful Redis commands:
#   INFO                        -- server stats
#   INFO clients                -- connected clients
#   INFO memory                 -- memory usage
#   DBSIZE                      -- total key count
#   ZCARD match:queue           -- users waiting for match
#   KEYS session:*              -- list sessions (use SCAN in production)
#   HGETALL session:<id>        -- inspect a specific session
#   SLOWLOG GET 20              -- recent slow commands

# Check NATS state
docker compose -f docker-compose.prod.yml exec nats \
  wget -qO- http://localhost:8222/varz      # server stats
docker compose -f docker-compose.prod.yml exec nats \
  wget -qO- http://localhost:8222/connz     # connection details

# Check PostgreSQL
docker compose -f docker-compose.prod.yml exec postgres \
  psql -U whisper_prod -d whisper -c "SELECT COUNT(*) FROM abuse_reports;"

# Check HAProxy stats (if port 8404 is exposed)
curl http://localhost:8404/haproxy/stats
# Default credentials: admin / whisper_haproxy_stats

# Check container resource usage
docker stats --no-stream
```

---

## 8. Security Checklist

Complete all items before exposing the application to the internet.

### Secrets

- [ ] Change `POSTGRES_PASSWORD` from placeholder to a strong random value (32+ characters)
- [ ] Change `GF_SECURITY_ADMIN_PASSWORD` from placeholder to a strong random value
- [ ] Change HAProxy stats password in `haproxy.prod.cfg` (`stats auth admin:whisper_haproxy_stats`)
- [ ] Ensure `.env` file is not committed to version control (verify `.gitignore`)
- [ ] Set file permissions on `.env`: `chmod 600 .env`
- [ ] Set file permissions on SSL certificates: `chmod 600 haproxy/certs/whisper.pem`

### SSL/TLS

- [ ] Replace self-signed certificates with Let's Encrypt or a CA-signed certificate
- [ ] Verify HAProxy enforces TLS 1.2+ (`ssl-min-ver TLSv1.2` in haproxy.prod.cfg)
- [ ] Verify HSTS header is set (`Strict-Transport-Security` in haproxy.prod.cfg)
- [ ] Verify HTTP-to-HTTPS redirect is active (port 80 returns 301)
- [ ] Set up automated certificate renewal (see Section 3.2)

### Network

- [ ] Restrict exposed ports to only 80, 443, and 3000 (frontend)
- [ ] Verify `whisper-backend` network is set to `internal: true` (prevents external access)
- [ ] Configure host firewall (ufw/iptables) to deny all inbound except required ports
- [ ] Do not expose Redis (6379), NATS (4222), or PostgreSQL (5432) to the host

### Application

- [ ] Review HAProxy rate limiting settings (default: 100 connections per 10s per IP)
- [ ] Verify `POSTGRES_INITDB_ARGS` includes `--auth-host=scram-sha-256`
- [ ] Verify Redis `protected-mode` is acceptable (disabled in config because Redis
  is only accessible on the internal Docker network)
- [ ] Disable Grafana public sign-up (`GF_USERS_ALLOW_SIGN_UP=false`)
- [ ] Review `MAX_CONNECTIONS` setting to prevent resource exhaustion

### Container Security

- [ ] Frontend nginx runs as non-root user (`USER nginx` in Dockerfile)
- [ ] wsserver binary runs from `scratch` image (minimal attack surface)
- [ ] Container resource limits are set (memory and CPU in docker-compose.prod.yml)
- [ ] Docker socket is not mounted into any container

---

## 9. Architecture Reference

### 9.1 Service Dependency Diagram

```
                          Internet
                             |
                      [ Port 80/443 ]
                             |
                         HAProxy
                    (SSL termination,
                     load balancing,
                     rate limiting)
                      /          \
                     /            \
              wsserver-1       wsserver-2          frontend
             (Go, epoll)     (Go, epoll)        (nginx SPA)
                  |    \       /    |              [Port 3000]
                  |     \     /     |
                  |      \   /      |
                  |       \ /       |
                  |     Redis ------+-- matcher (Go)
                  |   (sessions,    |
                  |    queues,      |
                  |    bans)        |
                  |       |         |
                  +---- NATS -------+-- moderator (Go)
                  |  (pub/sub,      |
                  |   JetStream)    |
                  |                 |
                  +-- PostgreSQL ---+
                     (abuse reports)
                           |
                       Prometheus ----> Grafana
                      (metrics)       (dashboards)
```

### 9.2 Port Mapping Table

| Service      | Container Port | Host Port (Dev) | Host Port (Prod) | Protocol |
|--------------|----------------|-----------------|-------------------|----------|
| HAProxy      | 80             | 8088            | 80                | HTTP     |
| HAProxy      | 443            | --              | 443               | HTTPS    |
| HAProxy      | 8404           | --              | -- (internal)     | HTTP     |
| wsserver     | 8080           | 8080            | -- (internal)     | HTTP/WS  |
| frontend     | 8080           | 3000            | 3000              | HTTP     |
| Redis        | 6379           | 6379            | -- (internal)     | TCP      |
| NATS         | 4222           | 4222            | -- (internal)     | TCP      |
| NATS         | 8222           | 8222            | -- (internal)     | HTTP     |
| PostgreSQL   | 5432           | 5432            | -- (internal)     | TCP      |
| Prometheus   | 9090           | 9090            | -- (internal)     | HTTP     |
| Grafana      | 3000           | 3001            | -- (internal)     | HTTP     |

### 9.3 Volume Mapping Table

| Volume            | Mount Point (Container)         | Purpose                     | Persistence |
|-------------------|---------------------------------|-----------------------------|-------------|
| `redis-data`      | `/data`                         | Redis RDB/AOF (if enabled)  | Optional    |
| `nats-data`       | `/data`                         | NATS JetStream storage      | Optional    |
| `postgres-data`   | `/var/lib/postgresql/data`      | PostgreSQL database files   | Required    |
| `prometheus-data` | `/prometheus`                   | Prometheus TSDB             | Recommended |
| `grafana-data`    | `/var/lib/grafana`              | Grafana state and settings  | Recommended |

Config files mounted as read-only bind mounts:

| Host Path                                    | Container Path                            | Service     |
|----------------------------------------------|-------------------------------------------|-------------|
| `./config/redis.conf`                        | `/usr/local/etc/redis/redis.conf`         | redis       |
| `./config/nats.conf`                         | `/etc/nats/nats.conf`                     | nats        |
| `./haproxy/haproxy.cfg`                      | `/usr/local/etc/haproxy/haproxy.cfg`      | haproxy     |
| `./monitoring/prometheus/prometheus.yml`      | `/etc/prometheus/prometheus.yml`           | prometheus  |
| `./monitoring/grafana/provisioning/`         | `/etc/grafana/provisioning/`              | grafana     |
| `./monitoring/grafana/dashboards/`           | `/var/lib/grafana/dashboards/`            | grafana     |

### 9.4 Network Topology

Production uses two Docker bridge networks for isolation:

```
whisper-frontend (bridge)
  |-- haproxy
  |-- wsserver-1
  |-- wsserver-2
  |-- frontend

whisper-backend (bridge, internal: true)
  |-- haproxy
  |-- wsserver-1
  |-- wsserver-2
  |-- redis
  |-- nats
  |-- postgres
  |-- matcher
  |-- moderator
  |-- prometheus
  |-- grafana
```

The `whisper-backend` network is marked `internal: true`, which means containers
on this network cannot reach the internet and are not accessible from the host
unless ports are explicitly published. This ensures Redis, NATS, and PostgreSQL
are never accidentally exposed.

HAProxy and the wsserver instances are on both networks: they accept connections
from the frontend network and communicate with backend services on the backend
network.

### 9.5 Resource Limits (Production)

| Service      | Memory Limit | CPU Limit | Notes                                    |
|--------------|-------------|-----------|------------------------------------------|
| redis        | 2 GB        | 2 cores   | maxmemory set to 1800 MB in redis.conf   |
| nats         | 1 GB        | 2 cores   | JetStream max_mem 512 MB, max_file 1 GB  |
| postgres     | 1 GB        | 2 cores   | shared_buffers 256 MB                    |
| haproxy      | 1 GB        | 2 cores   | ~17 KB per connection                    |
| wsserver-1   | 4 GB        | 4 cores   | ~20 KB per connection (kernel + Go)      |
| wsserver-2   | 4 GB        | 4 cores   | ~20 KB per connection (kernel + Go)      |
| matcher      | 1 GB        | 2 cores   | Low baseline; spikes during queue scans  |
| moderator    | 512 MB      | 1 core    | Lightweight NATS subscriber              |
| frontend     | 256 MB      | 1 core    | Static file serving via nginx            |
| prometheus   | 1 GB        | 1 core    | Depends on metric cardinality + retention|
| grafana      | 512 MB      | 1 core    | Dashboard rendering                      |
| **Total**    | **~16 GB**  | **~22 cores** |                                     |
