import pytest
from httpx import ASGITransport, AsyncClient

from auth_service.app import app


@pytest.mark.asyncio
async def test_health():
    transport = ASGITransport(app=app)
    async with AsyncClient(transport=transport, base_url="http://test") as ac:
        resp = await ac.get("/health")
        assert resp.status_code == 200
        data = resp.json()
        assert data["status"] == "healthy"
        assert data["service"] == "auth-service"


@pytest.mark.asyncio
async def test_login_success():
    transport = ASGITransport(app=app)
    async with AsyncClient(transport=transport, base_url="http://test") as ac:
        resp = await ac.post(
            "/api/v1/auth/login",
            json={"username": "snyder", "password": "glitch123"},
        )
        assert resp.status_code == 200
        data = resp.json()
        assert data["success"] is True
        assert "token" in data
        assert data["user"]["username"] == "snyder"
        assert data["user"]["role"] == "DevOps Engineer"


@pytest.mark.asyncio
async def test_login_invalid():
    transport = ASGITransport(app=app)
    async with AsyncClient(transport=transport, base_url="http://test") as ac:
        resp = await ac.post(
            "/api/v1/auth/login",
            json={"username": "snyder", "password": "wrongpassword"},
        )
        assert resp.status_code == 401


@pytest.mark.asyncio
async def test_register_and_profile():
    transport = ASGITransport(app=app)
    async with AsyncClient(transport=transport, base_url="http://test") as ac:
        # Register new operator
        reg_resp = await ac.post(
            "/api/v1/auth/register",
            json={
                "username": "morgan",
                "password": "morgansupersecret",
                "role": "Security Lead",
            },
        )
        assert reg_resp.status_code == 201
        token = reg_resp.json()["token"]

        # Fetch profile using Bearer token
        profile_resp = await ac.get(
            "/api/v1/auth/user",
            headers={"Authorization": f"Bearer {token}"},
        )
        assert profile_resp.status_code == 200
        pdata = profile_resp.json()
        assert pdata["username"] == "morgan"
        assert pdata["role"] == "Security Lead"
        assert pdata["status"] == "Active"

        # Verify token endpoint
        verify_resp = await ac.get(
            "/api/v1/auth/verify",
            headers={"Authorization": f"Bearer {token}"},
        )
        assert verify_resp.status_code == 200
        assert verify_resp.json()["valid"] is True
