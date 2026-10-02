# ⚡ GliTch: Distributed Microservices & TUI Command Center

[![Go Version](https://img.shields.io/badge/Go-1.27%2B-00ADD8?style=flat&logo=go)](https://go.dev/)
[![Docker Compose](https://img.shields.io/badge/Docker-Compose-2496ED?style=flat&logo=docker)](https://www.docker.com/)
[![PostgreSQL](https://img.shields.io/badge/PostgreSQL-16-336791?style=flat&logo=postgresql)](https://www.postgresql.org/)
[![Redis](https://img.shields.io/badge/Redis-7-DC382D?style=flat&logo=redis)](https://redis.io/)
[![Charm](https://img.shields.io/badge/TUI-Bubble%20Tea-FF5FAF)](https://charm.sh/)

**GliTch** is an enterprise-grade, distributed microservices monorepo paired with an interactive, full-screen Terminal User Interface (TUI) client written in Go. The platform serves as a secure, real-time command center and server telemetry monitor.

---

## 🏛️ Architecture Overview

```text
                                ┌────────────────────────────────┐
                                │   🖥️  GliTch TUI Client        │
                                │   (Charm Bubble Tea / Terminal)│
                                └───────────────┬────────────────┘
                                                │ HTTP / Bearer JWT
                                                ▼
┌────────────────────────────────────────────────────────────────────────────────────────┐
│ 🛡️  GliTch API Gateway (:8000)                                                         │
│ ├── Token Bucket Rate Limiting (30 RPS / 60 Burst)                                     │
│ ├── HMAC-SHA256 JWT Authentication & Identity Guard                                    │
│ └── Dynamic Reverse Proxy & Route Forwarding                                           │
└──────────────────┬──────────────────────────────────────────┬──────────────────────────┘
                   │                                          │
                   ▼                                          ▼
┌──────────────────────────────────────┐   ┌─────────────────────────────────────────────┐
│ 🔐  Auth Service (:8081)             │   │ ⚡  Telemetry Service (:8082)                │
│ ├── Identity & Role Management       │   │ ├── Host & Kernel Telemetry (gopsutil)      │
│ ├── bcrypt Password Hashing          │   │ ├── Multi-core CPU & Load Averages          │
│ ├── PostgreSQL Persistent Store      │   │ ├── RAM & Swap Consumption                  │
│ └── Login Auditing                   │   │ ├── Filesystem Mount Space (/)              │
└──────────────────┬───────────────────┘   │ ├── Network I/O & Packet Counters           │
                   │                       │ └── Non-blocking SSE Event Stream (/stream) │
                   ▼                       └─────────────────────────────────────────────┘
┌──────────────────────────────────────┐
│ 🐘  PostgreSQL 16 (:5432)            │
│ ├── users (credentials & roles)      │
│ └── login_audits (access logs)       │
└──────────────────────────────────────┘
```

---

## 📂 Repository Layout

```text
GliTch/
├── agent.md                    # Operational guidelines & architecture rules for glitch-ops
├── docker-compose.yml          # Full container orchestration (services, DB, cache, TUI)
├── go.work                     # Go multi-module workspace definition
├── Makefile                    # Automation tasks (build, test, run, clean)
├── api-gateway/                # Reverse proxy, rate limiter, JWT validation & cluster health
│   ├── Dockerfile
│   ├── go.mod
│   ├── go.sum
│   ├── main.go
│   └── main_test.go
├── auth-service/               # Identity provider, PostgreSQL integration, JWT issuance
│   ├── Dockerfile
│   ├── go.mod
│   ├── go.sum
│   ├── main.go
│   └── main_test.go
├── telemetry-service/          # Real-time host hardware collection via gopsutil & SSE
│   ├── Dockerfile
│   ├── go.mod
│   ├── go.sum
│   ├── main.go
│   └── main_test.go
└── tui-client/                 # Full-screen Bubble Tea terminal application
    ├── Dockerfile
    ├── go.mod
    ├── go.sum
    ├── main.go
    └── main_test.go
```

---

## 🚀 Quickstart

### Option 1: Docker Compose (Recommended)

Start the entire distributed cluster (PostgreSQL, Redis, Auth Service, Telemetry Service, API Gateway, and TUI Client):

```bash
# Build and launch all services in background
docker compose up --build -d

# Verify cluster status
docker compose ps

# Attach to the full-screen TUI command center
docker compose run --rm tui-client
```

### Option 2: Local Development with Go Workspace

GliTch uses a unified `go.work` multi-module workspace.

```bash
# 1. Build all binaries to bin/
make build

# 2. Run unit tests across all modules
make test

# 3. Launch TUI client locally
make run-tui
```

---

## 🔑 Default Credentials

GliTch seeds a default administrator on database initialization:

- **Username**: `snyder`
- **Password**: `glitch123`
- **Assigned Role**: `DevOps Engineer`

New users can also register dynamically via `POST /api/v1/auth/register`.

---

## 📡 API Reference

All external API interactions route through the **API Gateway** (`http://localhost:8000`).

### Public Routes
| Method | Path | Description |
| :--- | :--- | :--- |
| `GET` | `/health` | Gateway health check |
| `GET` | `/api/v1/cluster/health` | Aggregated cluster & downstream service health |
| `POST`| `/api/v1/auth/login` | Authenticate user and receive JWT token |
| `POST`| `/api/v1/auth/register` | Register new user account |

### Protected Routes (Requires `Authorization: Bearer <token>`)
| Method | Path | Description |
| :--- | :--- | :--- |
| `GET` | `/api/v1/auth/user` | Retrieve authenticated user profile and roles |
| `GET` | `/api/v1/auth/verify` | Verify token validity and claims |
| `GET` | `/api/v1/telemetry/stats` | JSON snapshot of hardware & system metrics |
| `GET` | `/api/v1/telemetry/stream`| Server-Sent Events (SSE) live telemetry stream |

---

## 🎮 TUI Command Center Guide

The `tui-client` provides a full-screen cyberpunk-styled dashboard powered by Charm's [Bubble Tea](https://github.com/charmbracelet/bubbletea) and [Lip Gloss](https://github.com/charmbracelet/lipgloss).

### Navigation & Keybindings
| Shortcut | Action |
| :--- | :--- |
| `[1]` | Switch to **[1] Overview & Dashboard** |
| `[2]` | Switch to **[2] Microservices Cluster** |
| `[3]` | Switch to **[3] Server Telemetry** |
| `[4]` | Switch to **[4] Audit & Activity Logs** |
| `[Tab]` / `[Right]` | Cycle to next tab |
| `[Shift+Tab]` / `[Left]` | Cycle to previous tab |
| `[r]` | Trigger immediate cluster probe & telemetry refresh |
| `[l]` | Log out current session and return to login screen |
| `[q]` / `[Ctrl+C]` | Quit application |

---

## ⚙️ Environment Variables

| Variable | Service | Default | Description |
| :--- | :--- | :--- | :--- |
| `PORT` | All | `8000` (GW), `8081` (Auth), `8082` (Telem) | Service listen port |
| `AUTH_SERVICE_URL` | `api-gateway` | `http://auth-service:8081` | Upstream auth service target |
| `TELEMETRY_SERVICE_URL`| `api-gateway` | `http://telemetry-service:8082`| Upstream telemetry service target |
| `REDIS_HOST` | `api-gateway` | `redis:6379` | Redis host for caching & probes |
| `JWT_SECRET` | Gateway & Auth| `glitch-ultra-secure-monorepo-secret-key-32b` | Signing key for HMAC-SHA256 JWTs |
| `RATE_LIMIT_RPS` | `api-gateway` | `30` | Allowed requests per second per IP |
| `RATE_LIMIT_BURST` | `api-gateway` | `60` | Maximum burst capacity per IP |
| `DB_HOST` | `auth-service`| `postgres` | PostgreSQL hostname |
| `DB_USER` | `auth-service`| `glitch_user` | PostgreSQL username |
| `DB_PASSWORD` | `auth-service`| `glitch_pass` | PostgreSQL password |
| `DB_NAME` | `auth-service`| `glitch_db` | PostgreSQL database name |
| `GATEWAY_URL` | `tui-client` | `http://localhost:8000` | Gateway endpoint for TUI client |

---

## 🛠️ Verification & Maintenance

Run the automated test suite across all modules:
```bash
make test
```

Rebuild all container images:
```bash
make up
```