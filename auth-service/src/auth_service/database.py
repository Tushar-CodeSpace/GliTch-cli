import logging
from datetime import datetime, timezone
from typing import Any, Dict, Optional
import asyncpg

from auth_service.config import settings
from auth_service.security import hash_password

logger = logging.getLogger("auth_service.database")


class DatabaseRepository:
    def __init__(self) -> None:
        self.pool: Optional[asyncpg.Pool] = None
        self.is_postgres: bool = False
        self._mem_users: Dict[str, Dict[str, Any]] = {}
        self._seed_default_memory_user()

    def _seed_default_memory_user(self) -> None:
        now = datetime.now(timezone.utc)
        self._mem_users["snyder"] = {
            "id": 1,
            "username": "snyder",
            "password_hash": hash_password("glitch123"),
            "role": "DevOps Engineer",
            "created_at": now,
            "updated_at": now,
        }

    async def connect(self) -> None:
        try:
            self.pool = await asyncpg.create_pool(
                host=settings.db_host,
                port=settings.db_port,
                user=settings.db_user,
                password=settings.db_password,
                database=settings.db_name,
                min_size=1,
                max_size=10,
                timeout=3.0,
            )
            # Test ping
            async with self.pool.acquire() as conn:
                await conn.execute("SELECT 1")
            self.is_postgres = True
            logger.info("Connected to PostgreSQL at %s:%s/%s", settings.db_host, settings.db_port, settings.db_name)
            await self._setup_schema()
        except Exception as e:
            logger.warning("PostgreSQL not immediately reachable (%s). Using in-memory fallback store.", e)
            self.is_postgres = False

    async def disconnect(self) -> None:
        if self.pool:
            await self.pool.close()
            logger.info("Closed PostgreSQL connection pool.")

    async def _setup_schema(self) -> None:
        if not self.pool:
            return

        schema_sql = """
        CREATE TABLE IF NOT EXISTS users (
            id SERIAL PRIMARY KEY,
            username VARCHAR(64) UNIQUE NOT NULL,
            password_hash VARCHAR(255) NOT NULL,
            role VARCHAR(64) NOT NULL DEFAULT 'DevOps Engineer',
            created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
            updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
        );

        CREATE TABLE IF NOT EXISTS login_audits (
            id SERIAL PRIMARY KEY,
            username VARCHAR(64) NOT NULL,
            ip_address VARCHAR(45) NOT NULL,
            success BOOLEAN NOT NULL,
            timestamp TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
        );
        """
        async with self.pool.acquire() as conn:
            await conn.execute(schema_sql)
            # Seed default admin user snyder if not present
            row = await conn.fetchrow("SELECT id FROM users WHERE username = $1", "snyder")
            if not row:
                pw_hash = hash_password("glitch123")
                await conn.execute(
                    "INSERT INTO users (username, password_hash, role) VALUES ($1, $2, $3)",
                    "snyder",
                    pw_hash,
                    "DevOps Engineer",
                )
                logger.info("Seeded default admin user 'snyder' into PostgreSQL.")

    async def get_user_by_username(self, username: str) -> Optional[Dict[str, Any]]:
        if self.is_postgres and self.pool:
            try:
                async with self.pool.acquire() as conn:
                    row = await conn.fetchrow(
                        "SELECT id, username, password_hash, role, created_at, updated_at FROM users WHERE username = $1",
                        username,
                    )
                    if row:
                        return dict(row)
            except Exception as e:
                logger.error("PostgreSQL query failed, falling back to memory: %s", e)

        return self._mem_users.get(username)

    async def create_user(self, username: str, password: str, role: str = "DevOps Engineer") -> Dict[str, Any]:
        pw_hash = hash_password(password)
        now = datetime.now(timezone.utc)

        if self.is_postgres and self.pool:
            try:
                async with self.pool.acquire() as conn:
                    row = await conn.fetchrow(
                        """
                        INSERT INTO users (username, password_hash, role, created_at, updated_at)
                        VALUES ($1, $2, $3, $4, $5)
                        RETURNING id, username, password_hash, role, created_at, updated_at
                        """,
                        username,
                        pw_hash,
                        role,
                        now,
                        now,
                    )
                    return dict(row)
            except Exception as e:
                logger.error("PostgreSQL create user failed, using memory store: %s", e)

        if username in self._mem_users:
            raise ValueError(f"User '{username}' already exists")

        user_data = {
            "id": len(self._mem_users) + 1,
            "username": username,
            "password_hash": pw_hash,
            "role": role,
            "created_at": now,
            "updated_at": now,
        }
        self._mem_users[username] = user_data
        return user_data

    async def record_audit(self, username: str, ip_address: str, success: bool) -> None:
        if self.is_postgres and self.pool:
            try:
                async with self.pool.acquire() as conn:
                    await conn.execute(
                        "INSERT INTO login_audits (username, ip_address, success) VALUES ($1, $2, $3)",
                        username,
                        ip_address,
                        success,
                    )
            except Exception as e:
                logger.warning("Failed to record audit log: %s", e)

    async def check_health(self) -> str:
        if self.is_postgres and self.pool:
            try:
                async with self.pool.acquire() as conn:
                    await conn.execute("SELECT 1")
                return "connected (PostgreSQL)"
            except Exception:
                return "degraded (PostgreSQL ping failed)"
        return "in-memory-fallback"


repo = DatabaseRepository()
