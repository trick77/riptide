import json
from pathlib import Path
from typing import Any

from riptide_collector.parsers_bitbucket import (
    BitbucketEventDraft,
    BitbucketSkip,
    extract_event,
)

FIXTURES = Path(__file__).parent / "fixtures"


def _load(name: str) -> dict[str, Any]:
    return json.loads((FIXTURES / name).read_text())


class TestExtractPrMerged:
    def test_pr_merged_fixture_returns_draft_with_lowercased_join_keys(self) -> None:
        # Given
        body = _load("bitbucket_pr_merged.json")

        # When
        result = extract_event(
            body,
            x_event_key="pr:merged",
            x_request_uuid="req-1",
            x_hook_uuid=None,
        )

        # Then
        assert isinstance(result, BitbucketEventDraft)
        assert result.delivery_id == "req-1"
        assert result.event_type == "pr:merged"
        assert result.repo_full_name == "acme/payments-api"
        assert result.pr_id == 42
        assert result.commit_sha == "abc1234567890abc1234567890abc1234567890a"
        assert result.branch_name == "feature/abc-123-retries"
        assert result.author == "alice"
        assert result.is_revert is False
        assert result.change_type == "feature"
        assert "ABC-123" in result.jira_keys
        assert "PROJ-9" in result.jira_keys
        # Raw payload preserved verbatim
        assert result.payload is body

    def test_revert_pr_title_marks_is_revert(self) -> None:
        # Given
        body = _load("bitbucket_pr_merged.json")
        body["pullRequest"]["title"] = 'Revert "Add payment retry"'

        # When
        result = extract_event(body, x_event_key="pr:merged", x_request_uuid="r", x_hook_uuid=None)

        # Then
        assert isinstance(result, BitbucketEventDraft)
        assert result.is_revert is True


class TestExtractRefsChanged:
    def test_branch_push_extracts_to_hash_and_branch(self) -> None:
        # Given
        body = _load("bitbucket_refs_changed.json")

        # When
        result = extract_event(
            body,
            x_event_key="repo:refs_changed",
            x_request_uuid="req-2",
            x_hook_uuid=None,
        )

        # Then
        assert isinstance(result, BitbucketEventDraft)
        assert result.repo_full_name == "acme/payments-api"
        assert result.branch_name == "master"
        assert result.commit_sha == "feedfacefeedfacefeedfacefeedfacefeedface"
        assert result.author == "alice"
        assert result.pr_id is None

    def test_tag_only_push_returns_skip(self) -> None:
        # Given
        body = _load("bitbucket_refs_changed.json")
        body["changes"] = [
            {
                "ref": {"id": "refs/tags/v1", "displayId": "v1", "type": "TAG"},
                "fromHash": "0" * 40,
                "toHash": "1" * 40,
                "type": "ADD",
            }
        ]

        # When
        result = extract_event(
            body, x_event_key="repo:refs_changed", x_request_uuid="r", x_hook_uuid=None
        )

        # Then
        assert isinstance(result, BitbucketSkip)
        assert result.reason == "no branch change in push"
        assert result.repo_full_name == "acme/payments-api"

    def test_delete_only_push_returns_skip(self) -> None:
        # Given
        body = _load("bitbucket_refs_changed.json")
        body["changes"][0]["type"] = "DELETE"

        # When
        result = extract_event(
            body, x_event_key="repo:refs_changed", x_request_uuid="r", x_hook_uuid=None
        )

        # Then
        assert isinstance(result, BitbucketSkip)
        assert result.reason == "no branch change in push"


class TestDeliveryIdSynthesis:
    def test_uses_request_uuid_when_present(self) -> None:
        # Given / When
        result = extract_event({}, x_event_key="x", x_request_uuid="uuid-1", x_hook_uuid="uuid-2")

        # Then
        assert isinstance(result, BitbucketEventDraft)
        assert result.delivery_id == "uuid-1"

    def test_falls_back_to_hook_uuid(self) -> None:
        # Given / When
        result = extract_event({}, x_event_key="x", x_request_uuid=None, x_hook_uuid="uuid-2")

        # Then
        assert isinstance(result, BitbucketEventDraft)
        assert result.delivery_id == "uuid-2"

    def test_synth_id_includes_to_hash_to_disambiguate_same_second_pushes(
        self,
    ) -> None:
        # Given — two pushes on the same repo, same second, different toHash.
        # Without toHash in the synth key these would dedup-collapse.
        base = _load("bitbucket_refs_changed.json")
        first = json.loads(json.dumps(base))
        second = json.loads(json.dumps(base))
        second["changes"][0]["toHash"] = "deadbeef" * 5

        # When
        first_result = extract_event(
            first,
            x_event_key="repo:refs_changed",
            x_request_uuid=None,
            x_hook_uuid=None,
        )
        second_result = extract_event(
            second,
            x_event_key="repo:refs_changed",
            x_request_uuid=None,
            x_hook_uuid=None,
        )

        # Then
        assert isinstance(first_result, BitbucketEventDraft)
        assert isinstance(second_result, BitbucketEventDraft)
        assert first_result.delivery_id != second_result.delivery_id


class TestAuthorFallbacks:
    def test_pr_author_user_name_preferred(self) -> None:
        # Given
        body = _load("bitbucket_pr_merged.json")
        body["pullRequest"]["author"]["user"] = {
            "name": "login-name",
            "slug": "slug-name",
            "displayName": "Display Name",
        }

        # When
        result = extract_event(body, x_event_key="pr:merged", x_request_uuid="r", x_hook_uuid=None)

        # Then
        assert isinstance(result, BitbucketEventDraft)
        assert result.author == "login-name"

    def test_falls_back_to_actor_when_pr_author_missing(self) -> None:
        # Given — push event has no pullRequest, only top-level actor
        body = _load("bitbucket_refs_changed.json")

        # When
        result = extract_event(
            body, x_event_key="repo:refs_changed", x_request_uuid="r", x_hook_uuid=None
        )

        # Then
        assert isinstance(result, BitbucketEventDraft)
        assert result.author == "alice"


class TestEventTypeAndOccurredAt:
    def test_event_type_defaults_to_unknown_when_header_missing(self) -> None:
        # Given / When
        result = extract_event(
            _load("bitbucket_refs_changed.json"),
            x_event_key=None,
            x_request_uuid="r",
            x_hook_uuid=None,
        )

        # Then
        assert isinstance(result, BitbucketEventDraft)
        assert result.event_type == "unknown"

    def test_occurred_at_parsed_from_iso_string(self) -> None:
        # Given
        body = _load("bitbucket_pr_merged.json")

        # When
        result = extract_event(body, x_event_key="pr:merged", x_request_uuid="r", x_hook_uuid=None)

        # Then
        assert isinstance(result, BitbucketEventDraft)
        assert result.occurred_at.year == 2026
        assert result.occurred_at.tzinfo is not None
