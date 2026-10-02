import uvicorn
from auth_service.config import settings


def main() -> None:
    uvicorn.run(
        "auth_service.app:app",
        host=settings.host,
        port=settings.port,
        reload=False,
    )


if __name__ == "__main__":
    main()
