"""Quick "did I wire it up correctly?" diagnostic.

Usage: uv run python scripts/check_onboarding.py <service-id>

Reports whether each of the three sources has produced events for the given
service in the last hour. Exits non-zero if any source is missing data.
"""

from __future__ import annotations

import asyncio
import sys
from datetime import UTC, datetime, timedelta

from sqlalchemy import func, select
from sqlalchemy.ext.asyncio import create_async_engine

from riptide_collector.models import ArgoCDEvent, BitbucketEvent, JenkinsEvent
from riptide_collector.settings import load_settings


async def main(service_id: str) -> int:
    settings = load_settings()
    engine = create_async_engine(settings.db_url, future=True)
    cutoff = datetime.now(UTC) - timedelta(hours=1)

    sources = {
        "bitbucket_events": BitbucketEvent,
        "jenkins_events": JenkinsEvent,
        "argocd_events": ArgoCDEvent,
    }

    missing: list[str] = []
    async with engine.connect() as conn:
        for label, model in sources.items():
            stmt = (
                select(func.count())
                .select_from(model)
                .where(model.service == service_id)
                .where(model.created_at >= cutoff)
            )
            count = (await conn.execute(stmt)).scalar_one()
            status = "OK" if count > 0 else "MISSING"
            print(f"{status:>8}  {label:<20}  {count} events in the last hour")
            if count == 0:
                missing.append(label)

    await engine.dispose()
    if missing:
        print(f"\nNo events from: {', '.join(missing)}")
        return 1
    print("\nAll three sources reporting.")
    return 0


if __name__ == "__main__":
    if len(sys.argv) != 2:
        print(f"usage: {sys.argv[0]} <service-id>", file=sys.stderr)
        sys.exit(2)
    sys.exit(asyncio.run(main(sys.argv[1])))
