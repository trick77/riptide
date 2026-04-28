from datetime import UTC, datetime
from typing import Any

from fastapi import APIRouter, Depends, Header, Request, status
from sqlalchemy.dialects.postgresql import insert as pg_insert
from sqlalchemy.ext.asyncio import async_sessionmaker

from riptide_collector.catalog import CatalogStore
from riptide_collector.logging_config import get_logger
from riptide_collector.models import BitbucketEvent
from riptide_collector.parsers import (
    extract_jira_keys,
    is_revert_commit,
    parse_change_type,
)

logger = get_logger(__name__)


def _as_dict(value: Any) -> dict[str, Any]:
    return value if isinstance(value, dict) else {}


def _as_list(value: Any) -> list[Any]:
    return value if isinstance(value, list) else []


def _safe_int(value: Any) -> int | None:
    if isinstance(value, bool):
        return None
    if isinstance(value, int):
        return value
    if isinstance(value, str) and value.isdigit():
        return int(value)
    return None


def _parse_dt(value: Any) -> datetime | None:
    if not isinstance(value, str):
        return None
    try:
        return datetime.fromisoformat(value.replace("Z", "+00:00"))
    except ValueError:
        return None


def _synth_delivery_id(event_key: str | None, body: dict[str, Any]) -> str:
    pr = _as_dict(body.get("pullrequest"))
    pr_id = pr.get("id")
    repo = _as_dict(body.get("repository")).get("full_name") or "unknown"
    when = body.get("date") or body.get("created_on") or "?"
    return f"{event_key or 'unknown'}#{repo}#{pr_id}#{when}"


def make_router(
    catalog: CatalogStore,
    session_factory: async_sessionmaker[Any],
    auth_dep: Any,
) -> APIRouter:
    router = APIRouter(prefix="/webhooks", tags=["webhooks"])

    @router.post(
        "/bitbucket",
        status_code=status.HTTP_202_ACCEPTED,
        dependencies=[Depends(auth_dep)],
        summary="Bitbucket webhook sink",
    )
    async def bitbucket_webhook(  # pyright: ignore[reportUnusedFunction]
        request: Request,
        x_event_key: str | None = Header(default=None),
        x_request_uuid: str | None = Header(default=None),
        x_hook_uuid: str | None = Header(default=None),
    ) -> dict[str, str]:
        body = await request.json()
        if not isinstance(body, dict):
            return {"status": "ignored", "reason": "non-object payload"}

        catalog.maybe_reload()

        delivery_id = x_request_uuid or x_hook_uuid or _synth_delivery_id(x_event_key, body)
        event_type = x_event_key or "unknown"

        repo_full_name_raw = _as_dict(body.get("repository")).get("full_name")
        repo_full_name = repo_full_name_raw if isinstance(repo_full_name_raw, str) else None

        pr = _as_dict(body.get("pullrequest"))
        push = _as_dict(body.get("push"))

        pr_id: int | None = pr.get("id") if isinstance(pr.get("id"), int) else None
        title = pr.get("title") if isinstance(pr.get("title"), str) else None
        description = pr.get("description") if isinstance(pr.get("description"), str) else None

        source = _as_dict(pr.get("source"))
        branch = _as_dict(source.get("branch"))
        commit = _as_dict(source.get("commit"))
        branch_name: str | None = (
            branch.get("name") if isinstance(branch.get("name"), str) else None
        )
        commit_sha: str | None = commit.get("hash") if isinstance(commit.get("hash"), str) else None

        author_section = _as_dict(pr.get("author"))
        author_candidate = (
            author_section.get("nickname")
            or author_section.get("username")
            or author_section.get("display_name")
        )
        author: str | None = author_candidate if isinstance(author_candidate, str) else None

        lines_added = _safe_int(pr.get("lines_added"))
        lines_removed = _safe_int(pr.get("lines_removed"))
        files_changed = _safe_int(pr.get("files_changed"))

        is_revert = False
        commit_messages: list[str] = []

        for change in _as_list(push.get("changes")):
            change_dict = _as_dict(change)
            new_section = _as_dict(change_dict.get("new"))
            target = _as_dict(new_section.get("target"))
            target_hash = target.get("hash")
            if not commit_sha and isinstance(target_hash, str):
                commit_sha = target_hash
            new_name = new_section.get("name")
            if not branch_name and isinstance(new_name, str):
                branch_name = new_name
            msg = target.get("message")
            if isinstance(msg, str):
                commit_messages.append(msg)
                if is_revert_commit(msg):
                    is_revert = True

        if not author and push:
            actor = _as_dict(body.get("actor"))
            for key in ("nickname", "username", "display_name"):
                value = actor.get(key)
                if isinstance(value, str):
                    author = value
                    break

        change_type = parse_change_type(branch_name)
        jira_keys = extract_jira_keys(title, description, branch_name, *commit_messages)
        automation_source = catalog.detect_automation_source(author, branch_name)

        resolution = catalog.resolve_bitbucket(repo_full_name)
        if resolution is None and repo_full_name:
            logger.warning("bitbucket_unknown_repo", repo=repo_full_name)

        occurred_at = (
            _parse_dt(body.get("date")) or _parse_dt(body.get("created_on")) or datetime.now(UTC)
        )

        async with session_factory() as session:
            stmt = (
                pg_insert(BitbucketEvent)
                .values(
                    delivery_id=delivery_id,
                    event_type=event_type,
                    repo_full_name=repo_full_name,
                    pr_id=pr_id,
                    commit_sha=commit_sha,
                    author=author,
                    branch_name=branch_name,
                    change_type=change_type,
                    jira_keys=jira_keys,
                    automation_source=automation_source,
                    lines_added=lines_added,
                    lines_removed=lines_removed,
                    files_changed=files_changed,
                    is_revert=is_revert,
                    occurred_at=occurred_at,
                    service=resolution.service_id if resolution else None,
                    team=resolution.team_name if resolution else None,
                    payload=body,
                )
                .on_conflict_do_nothing(index_elements=["delivery_id"])
            )
            await session.execute(stmt)
            await session.commit()

        logger.info(
            "bitbucket_event_received",
            delivery_id=delivery_id,
            event_type=event_type,
            repo=repo_full_name,
            service=resolution.service_id if resolution else None,
        )
        return {"status": "accepted"}

    return router
