from riptide_collector.parsers import (
    extract_jira_keys,
    is_revert_commit,
    looks_bot_shaped,
    parse_change_type,
)


class TestParseChangeType:
    def test_feature_prefix_maps_to_feature(self) -> None:
        assert parse_change_type("feature/ABC-1-do-thing") == "feature"

    def test_feat_alias_maps_to_feature(self) -> None:
        assert parse_change_type("feat/short") == "feature"

    def test_bugfix_and_fix_aliases(self) -> None:
        assert parse_change_type("bugfix/x") == "bugfix"
        assert parse_change_type("fix/x") == "bugfix"

    def test_hotfix_maps_to_hotfix(self) -> None:
        assert parse_change_type("hotfix/page") == "hotfix"

    def test_unknown_prefix_is_other(self) -> None:
        assert parse_change_type("wip/x") == "other"

    def test_no_prefix_lowercases_whole_name(self) -> None:
        assert parse_change_type("HOTFIX") == "hotfix"

    def test_none_branch_returns_none(self) -> None:
        assert parse_change_type(None) is None

    def test_empty_string_returns_none(self) -> None:
        assert parse_change_type("") is None


class TestExtractJiraKeys:
    def test_single_key_in_title(self) -> None:
        assert extract_jira_keys("ABC-123: do thing") == ["ABC-123"]

    def test_keys_dedupe_across_sources(self) -> None:
        result = extract_jira_keys("ABC-1 fix", "ABC-1 also fix", "feature/ABC-2")
        assert result == ["ABC-1", "ABC-2"]

    def test_multiple_keys_in_one_string(self) -> None:
        result = extract_jira_keys("ABC-1 and ABC-2 in PROJ-99")
        assert result == ["ABC-1", "ABC-2", "PROJ-99"]

    def test_lowercase_key_not_matched(self) -> None:
        assert extract_jira_keys("abc-1 do thing") == []

    def test_none_sources_skipped(self) -> None:
        assert extract_jira_keys(None, None, "ABC-9") == ["ABC-9"]

    def test_empty_when_no_keys(self) -> None:
        assert extract_jira_keys("just some text") == []


class TestIsRevertCommit:
    def test_revert_commit_detected(self) -> None:
        assert is_revert_commit('Revert "fix bug"') is True

    def test_revert_case_insensitive(self) -> None:
        assert is_revert_commit('revert "thing"') is True

    def test_non_revert_message(self) -> None:
        assert is_revert_commit("Normal commit") is False

    def test_none_message(self) -> None:
        assert is_revert_commit(None) is False


class TestLooksBotShaped:
    def test_bot_suffix(self) -> None:
        assert looks_bot_shaped("renovate-bot") is True

    def test_bracket_bot(self) -> None:
        assert looks_bot_shaped("dependabot[bot]") is True

    def test_bot_prefix(self) -> None:
        assert looks_bot_shaped("bot-deploy") is True

    def test_human_username(self) -> None:
        assert looks_bot_shaped("alice") is False

    def test_none(self) -> None:
        assert looks_bot_shaped(None) is False
