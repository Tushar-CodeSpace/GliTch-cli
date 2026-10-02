# 🔐 GliTch Auth Microservice (Python + Astral uv)

Enterprise identity and access management microservice for the GliTch platform, built with **Python 3.12**, **FastAPI**, **Astral uv**, and **PostgreSQL**.

---

## ⚡ Features
- **Modern Python with `uv`**: Ultra-fast dependency resolution, project management, and lockfile synchronization.
- **Asynchronous Architecture**: Fully async FastAPI handlers powered by `uvicorn` and `asyncpg`.
- **Identity & Access Management**: Secure password hashing with `bcrypt` (10 rounds) and HMAC-SHA256 JWT tokens.
- **Relational Storage & Fallback**: Native PostgreSQL pool management with automatic schema setup (`users`, `login_audits`) and in-memory fallback for offline/standalone execution.
- **Seeded Default Admin**: Automatically provisions operator credentials (`snyder` / `glitch123`) on initialization.

---

## 🚀 Development & Usage

### Running Locally with uv
```bash
# Install dependencies
uv sync

# Run the service
uv run auth-service
```
Or start directly with uvicorn:
```bash
uv run uvicorn auth_service.app:app --host 0.0.0.0 --port 8081 --reload
```

### Running Tests
```bash
uv run pytest
```

---

## 📡 API Endpoints

| Method | Endpoint | Description |
| :--- | :--- | :--- |
| `POST` | `/api/v1/auth/login` | Authenticate user and receive JWT bearer token |
| `POST` | `/api/v1/auth/register` | Register a new user |
| `GET` | `/api/v1/auth/user` | Retrieve current user profile (requires `Authorization: Bearer <token>`) |
| `GET` | `/api/v1/auth/verify` | Verify token signature and claims |
| `GET` | `/health` | Service health probe and database connection status |

---

## ⚙️ Environment Configuration

| Variable | Default | Description |
| :--- | :--- | :--- |
| `PORT` | `8081` | Service listen port |
| `HOST` | `0.0.0.0` | Bind host address |
| `DB_HOST` | `localhost` | PostgreSQL host |
| `DB_PORT` | `5432` | PostgreSQL port |
| `DB_USER` | `glitch_user` | Database user |
| `DB_PASSWORD` | `glitch_pass` | Database password |
| `DB_NAME` | `glitch_db` | Database name |
| `JWT_SECRET` | `glitch-ultra-secure-monorepo-secret-key-32b` | Signing key for HMAC-SHA256 JWTs |
