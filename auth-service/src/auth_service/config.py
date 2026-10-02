import os
from pydantic import BaseModel


class Settings(BaseModel):
    port: int = int(os.getenv("PORT", "8081"))
    host: str = os.getenv("HOST", "0.0.0.0")

    # Database
    db_host: str = os.getenv("DB_HOST", "localhost")
    db_port: int = int(os.getenv("DB_PORT", "5432"))
    db_user: str = os.getenv("DB_USER", "glitch_user")
    db_password: str = os.getenv("DB_PASSWORD", "glitch_pass")
    db_name: str = os.getenv("DB_NAME", "glitch_db")

    # Security
    jwt_secret: str = os.getenv("JWT_SECRET", "glitch-ultra-secure-monorepo-secret-key-32b")
    jwt_algorithm: str = "HS256"
    jwt_expiration_hours: int = 24


settings = Settings()
