from datetime import datetime
from typing import Optional
from pydantic import BaseModel, Field


class UserCredentials(BaseModel):
    username: str = Field(..., min_length=1, max_length=64)
    password: str = Field(..., min_length=1, max_length=128)
    role: Optional[str] = "DevOps Engineer"


class UserProfile(BaseModel):
    id: int
    username: str
    role: str
    created_at: datetime
    updated_at: Optional[datetime] = None


class AuthResponse(BaseModel):
    success: bool
    token: Optional[str] = None
    message: str
    user: Optional[UserProfile] = None


class UserProfileResponse(BaseModel):
    username: str
    role: str
    user_id: int
    system: str = "GliTch-cli v1.0"
    status: str = "Active"
    expires: Optional[str] = None


class TokenVerifyResponse(BaseModel):
    valid: bool
    username: Optional[str] = None
    role: Optional[str] = None
    user_id: Optional[int] = None
    message: Optional[str] = None


class HealthResponse(BaseModel):
    status: str
    service: str
    database: str
    time: str
