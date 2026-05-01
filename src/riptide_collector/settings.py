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
    config_path: Path = Field(
        default=Path("openshift/collector/riptide.json"),
        description="Path to riptide.json. The default points at the "
        "in-repo sample for `uv run` from the repo root; production overrides "
        "via RIPTIDE_CONFIG_PATH to a Secret-/ConfigMap-mounted location.",
    )
    team_keys_path: Path = Field(
        default=Path("openshift/collector/team-keys.json"),
        description="Path to team-keys.json (sha256 hashes of per-team bearer "
        "tokens). Default points at the in-repo dev sample; production "
        "overrides via RIPTIDE_TEAM_KEYS_PATH to a Secret mount.",
    )
    log_level: str = Field(default="INFO")
    config_reload_seconds: float = Field(
        default=30.0,
        description="How often to mtime-check the config for hot reload.",
    )


def load_settings() -> Settings:
    return Settings()
