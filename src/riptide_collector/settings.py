from pathlib import Path

from pydantic import Field
from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    model_config = SettingsConfigDict(
        env_prefix="RIPTIDE_",
        env_file=".env",
        env_file_encoding="utf-8",
        extra="ignore",
    )

    db_url: str = Field(
        default="postgresql+asyncpg://riptide:riptide@localhost:5432/riptide",
        description="SQLAlchemy async DB URL.",
    )
    catalog_path: Path = Field(
        default=Path("config/service-catalog.json"),
        description="Path to service-catalog.json.",
    )
    team_keys_path: Path = Field(
        default=Path("config/team-keys.json"),
        description="Path to team-keys.json (sha256 hashes of per-team bearer tokens).",
    )
    log_level: str = Field(default="INFO")
    catalog_reload_seconds: float = Field(
        default=30.0,
        description="How often to mtime-check the catalog for hot reload.",
    )


def load_settings() -> Settings:
    return Settings()
