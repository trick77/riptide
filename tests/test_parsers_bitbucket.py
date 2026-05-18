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


class TestExtractPrDeclined:
    def test_pr_declined_fixture_returns_draft_with_lowercased_join_keys(self) -> None:
        # Given
        body = _load("bitbucket_pr_declined.json")

        # When
        result = extract_event(
            body,
            x_event_key="pr:declined",
            x_request_uuid="req-decl",
            x_hook_uuid=None,
        )

        # Then — declined-PRs share the merged payload shape; the parser
        # must persist them with full join keys so they show up in
        # downstream rollups (e.g. noergler outcome='declined' joins).
        assert isinstance(result, BitbucketEventDraft)
        assert result.delivery_id == "req-decl"
        assert result.event_type == "pr:declined"
        assert result.repo_full_name == "acme/payments-api"
        assert result.pr_id == 42
        assert result.commit_sha == "abc1234567890abc1234567890abc1234567890a"
        assert result.branch_name == "feature/abc-123-retries"
        assert result.author == "alice"
        assert result.is_revert is False
        assert result.change_type == "feature"
        assert "ABC-123" in result.jira_keys
        assert "PROJ-9" in result.jira_keys
        assert result.payload is body


class TestExtractPrModified:
    def test_draft_to_ready_flip_emits_synthetic_ready_for_review(self) -> None:
        # Given — pr:modified payload with previousDraft=true, draft=false.
        # This is the DX Core 4 pickup-time start signal for PRs that were
        # opened as drafts.
        body = _load("bitbucket_pr_modified_draft_to_ready.json")

        # When
        result = extract_event(
            body,
            x_event_key="pr:modified",
            x_request_uuid="req-modified-flip",
            x_hook_uuid=None,
        )

        # Then — re-typed to the synthetic event so downstream queries
        # don't have to dig into previousDraft on the raw payload.
        assert isinstance(result, BitbucketEventDraft)
        assert result.event_type == "pr:ready_for_review"
        assert result.pr_id == 42
        assert result.repo_full_name == "acme/payments-api"
        # Raw payload preserves the original eventKey for traceability.
        assert result.payload["eventKey"] == "pr:modified"
        assert result.payload["previousDraft"] is True

    def test_draft_to_ready_uses_actor_as_author(self) -> None:
        # Given — a maintainer (not the PR author) flips the draft switch.
        # Pickup-time attribution should follow whoever performed the
        # action, same rule as reviewer-activity events.
        body = _load("bitbucket_pr_modified_draft_to_ready.json")
        body["actor"] = {"name": "carol", "displayName": "Carol Maintainer", "slug": "carol"}

        # When
        result = extract_event(
            body, x_event_key="pr:modified", x_request_uuid="r", x_hook_uuid=None
        )

        # Then
        assert isinstance(result, BitbucketEventDraft)
        assert result.event_type == "pr:ready_for_review"
        # carol flipped it, even though alice originally opened the PR.
        assert result.author == "carol"

    def test_title_only_modify_returns_skip(self) -> None:
        # Given — pr:modified with previousDraft=false (i.e. just a title /
        # description / target change). Carries no metric we track today,
        # so the parser drops it rather than bloating the table.
        body = _load("bitbucket_pr_modified_title_only.json")

        # When
        result = extract_event(
            body, x_event_key="pr:modified", x_request_uuid="r", x_hook_uuid=None
        )

        # Then
        assert isinstance(result, BitbucketSkip)
        assert result.reason == "pr:modified without draft→ready flip"
        # event_type on the skip reflects what BBS sent, not the synthetic
        # rename (we never emitted a synthetic for this delivery).
        assert result.event_type == "pr:modified"
        assert result.repo_full_name == "acme/payments-api"

    def test_ready_to_draft_returns_skip(self) -> None:
        # Given — the reverse flip (ready → draft). Not a pickup signal;
        # if we tracked "PR rework rate" later this would be useful, but
        # for v1 it carries no metric so we skip it.
        body = _load("bitbucket_pr_modified_draft_to_ready.json")
        body["previousDraft"] = False
        body["pullRequest"]["draft"] = True

        # When
        result = extract_event(
            body, x_event_key="pr:modified", x_request_uuid="r", x_hook_uuid=None
        )

        # Then
        assert isinstance(result, BitbucketSkip)
        assert result.reason == "pr:modified without draft→ready flip"

    def test_pr_opened_as_draft_passes_through_unchanged(self) -> None:
        # Regression guard: an opened-as-draft PR must still produce a
        # regular pr:opened row — the pickup-time SQL inspects
        # payload->'pullRequest'->>'draft' on that row to decide whether
        # it counts as a clock-start. If the parser ever started filtering
        # or re-typing opened-as-draft, the SQL would silently break.
        body = _load("bitbucket_pr_merged.json")
        body["eventKey"] = "pr:opened"
        body["pullRequest"]["state"] = "OPEN"
        body["pullRequest"]["draft"] = True

        # When
        result = extract_event(body, x_event_key="pr:opened", x_request_uuid="r", x_hook_uuid=None)

        # Then
        assert isinstance(result, BitbucketEventDraft)
        assert result.event_type == "pr:opened"
        assert result.payload["pullRequest"]["draft"] is True

    def test_modified_without_draft_fields_returns_skip(self) -> None:
        # Given — a BBS DC version (or payload) without draft tracking at
        # all: no previousDraft, no pullRequest.draft. The parser must
        # not emit a synthetic ready-for-review row from absence-of-data.
        body = _load("bitbucket_pr_modified_title_only.json")
        del body["previousDraft"]
        del body["pullRequest"]["draft"]

        # When
        result = extract_event(
            body, x_event_key="pr:modified", x_request_uuid="r", x_hook_uuid=None
        )

        # Then
        assert isinstance(result, BitbucketSkip)


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

    def test_reviewer_event_uses_actor_not_pr_author(self) -> None:
        # Given — pr:reviewer:approved fixture has pullRequest.author=alice
        # and top-level actor=bob (the reviewer doing the approval).
        body = _load("bitbucket_pr_reviewer_approved.json")

        # When
        result = extract_event(
            body,
            x_event_key="pr:reviewer:approved",
            x_request_uuid="r",
            x_hook_uuid=None,
        )

        # Then — for review-pickup analytics we want the reviewer's handle,
        # not the PR opener's; the parser must prefer actor for these events.
        assert isinstance(result, BitbucketEventDraft)
        assert result.author == "bob"
        # pr_id still extracted from the payload so feedback joins back
        # to the opener via bitbucket_events rows for the same pr_id.
        assert result.pr_id == 42

    def test_comment_added_uses_actor_not_pr_author(self) -> None:
        # Same rule applies to pr:comment:added — the commenter is the
        # signal, not the PR opener. Dedicated fixture (not the reviewer
        # fixture with a swapped eventKey) so the shape stays honest.
        body = _load("bitbucket_pr_comment_added.json")

        result = extract_event(
            body,
            x_event_key="pr:comment:added",
            x_request_uuid="r",
            x_hook_uuid=None,
        )

        assert isinstance(result, BitbucketEventDraft)
        assert result.author == "bob"

    def test_reviewer_unapproved_uses_actor_not_pr_author(self) -> None:
        # 'pr:reviewer:unapproved' (retracted approval) is also reviewer
        # engagement and feeds the pickup-time metric. Same actor-wins rule.
        body = _load("bitbucket_pr_reviewer_approved.json")

        result = extract_event(
            body,
            x_event_key="pr:reviewer:unapproved",
            x_request_uuid="r",
            x_hook_uuid=None,
        )

        assert isinstance(result, BitbucketEventDraft)
        assert result.author == "bob"
        assert result.event_type == "pr:reviewer:unapproved"

    def test_reviewer_event_falls_back_to_actor_when_pr_author_missing(self) -> None:
        # Edge case: a Bitbucket DC payload without pullRequest.author (or
        # with a broken/empty author block). The reviewer-activity path
        # already prefers actor; this confirms the regular actor fallback
        # at the end of extract_event still works as a backstop and we
        # don't end up with author=None.
        body = _load("bitbucket_pr_reviewer_approved.json")
        body["pullRequest"]["author"] = {}

        result = extract_event(
            body,
            x_event_key="pr:reviewer:approved",
            x_request_uuid="r",
            x_hook_uuid=None,
        )

        assert isinstance(result, BitbucketEventDraft)
        assert result.author == "bob"

    def test_pr_opened_keeps_pr_author_not_actor(self) -> None:
        # Regression guard: for PR-lifecycle events (opened / merged /
        # from_ref_updated / deleted) we still want the PR author, even
        # if a maintainer (different actor) triggered the event.
        body = _load("bitbucket_pr_merged.json")
        body["actor"] = {"name": "carol", "slug": "carol"}

        result = extract_event(body, x_event_key="pr:merged", x_request_uuid="r", x_hook_uuid=None)

        assert isinstance(result, BitbucketEventDraft)
        # alice opened the PR; carol merged it. The historical row should
        # still attribute the PR to alice.
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
