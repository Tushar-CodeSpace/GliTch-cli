import logging
from contextlib import asynccontextmanager
from datetime import datetime, timezone
from typing import Optional
from fastapi import FastAPI, Header, HTTPException, Query, Request, status
from fastapi.middleware.cors import CORSMiddleware
from fastapi.responses import JSONResponse

from auth_service.database import repo
from auth_service.models import (
    AuthResponse,
    HealthResponse,
    TokenVerifyResponse,
    UserCredentials,
    UserProfile,
    UserProfileResponse,
)
from auth_service.security import (
    create_access_token,
    decode_access_token,
    verify_password,
)

logging.basicConfig(level=logging.INFO, format="%(asctime)s [%(levelname)s] %(name)s: %(message)s")
logger = logging.getLogger("auth_service")


@asynccontextmanager
async def lifespan(app: FastAPI):
    logger.info("⚡ GliTch Auth Microservice initializing...")
    await repo.connect()
    yield
    await repo.disconnect()
    logger.info("GliTch Auth Microservice shutdown complete.")


app = FastAPI(
    title="GliTch Auth Microservice",
    version="1.0.0",
    description="Identity & Access Management service for GliTch monorepo",
    lifespan=lifespan,
)

app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)


@app.post(
    "/api/v1/auth/login",
    response_model=AuthResponse,
    summary="Authenticate user and issue JWT",
)
@app.post("/api/v1/login", response_model=AuthResponse, include_in_schema=False)
async def login(creds: UserCredentials, request: Request):
    client_ip = request.client.host if request.client else "unknown"
    user = await repo.get_user_by_username(creds.username)

    if not user or not verify_password(creds.password, user["password_hash"]):
        await repo.record_audit(creds.username, client_ip, False)
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED,
            detail="Invalid username or password",
        )

    token = create_access_token(user["id"], user["username"], user["role"])
    await repo.record_audit(creds.username, client_ip, True)

    profile = UserProfile(
        id=user["id"],
        username=user["username"],
        role=user["role"],
        created_at=user["created_at"],
        updated_at=user.get("updated_at"),
    )
    return AuthResponse(
        success=True,
        token=token,
        message="Authentication successful!",
        user=profile,
    )


@app.post(
    "/api/v1/auth/register",
    response_model=AuthResponse,
    status_code=status.HTTP_201_CREATED,
    summary="Register new operator account",
)
@app.post("/api/v1/register", response_model=AuthResponse, status_code=status.HTTP_201_CREATED, include_in_schema=False)
async def register(creds: UserCredentials):
    try:
        user = await repo.create_user(
            username=creds.username,
            password=creds.password,
            role=creds.role or "DevOps Engineer",
        )
    except ValueError as e:
        raise HTTPException(status_code=status.HTTP_409_CONFLICT, detail=str(e))

    token = create_access_token(user["id"], user["username"], user["role"])
    profile = UserProfile(
        id=user["id"],
        username=user["username"],
        role=user["role"],
        created_at=user["created_at"],
        updated_at=user.get("updated_at"),
    )
    return AuthResponse(
        success=True,
        token=token,
        message="User registered successfully!",
        user=profile,
    )


@app.get(
    "/api/v1/auth/user",
    response_model=UserProfileResponse,
    summary="Get authenticated user profile",
)
@app.get("/api/v1/user", response_model=UserProfileResponse, include_in_schema=False)
async def get_user_profile(authorization: Optional[str] = Header(None)):
    if not authorization:
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED,
            detail="Unauthorized: Missing authorization header",
        )

    try:
        payload = decode_access_token(authorization)
    except Exception as e:
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED,
            detail=f"Unauthorized: {str(e)}",
        )

    exp_ts = payload.get("exp")
    expires_str = datetime.fromtimestamp(exp_ts, tz=timezone.utc).isoformat() if exp_ts else None

    return UserProfileResponse(
        username=payload.get("username", "unknown"),
        role=payload.get("role", "Operator"),
        user_id=payload.get("user_id", 0),
        system="GliTch-cli v1.0",
        status="Active",
        expires=expires_str,
    )


@app.get(
    "/api/v1/auth/verify",
    response_model=TokenVerifyResponse,
    summary="Verify JWT validity",
)
@app.get("/api/v1/verify", response_model=TokenVerifyResponse, include_in_schema=False)
async def verify_token(
    authorization: Optional[str] = Header(None),
    token: Optional[str] = Query(None),
):
    token_str = authorization or token
    if not token_str:
        raise HTTPException(
            status_code=status.HTTP_400_BAD_REQUEST,
            detail="Token parameter or authorization header missing",
        )

    try:
        payload = decode_access_token(token_str)
        return TokenVerifyResponse(
            valid=True,
            username=payload.get("username"),
            role=payload.get("role"),
            user_id=payload.get("user_id"),
        )
    except Exception as e:
        return JSONResponse(
            status_code=status.HTTP_401_UNAUTHORIZED,
            content={"valid": False, "message": str(e)},
        )


@app.get("/health", response_model=HealthResponse, summary="Health status probe")
@app.get("/api/v1/auth/health", response_model=HealthResponse, include_in_schema=False)
async def health():
    db_status = await repo.check_health()
    return HealthResponse(
        status="healthy",
        service="auth-service",
        database=db_status,
        time=datetime.now(timezone.utc).isoformat(),
    )
