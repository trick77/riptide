-- The signal once called flaky (a failure rate up against the previous
-- period) is failing_more now: flaky is a run red, then green on the same
-- commit, and never a signal kind. Stored analyses read under today's names
-- once: the latest one's signals still resolve (matched by ID), the page
-- words their kind, the report's entries name the same IDs.
--
-- The assessment and the report are machine data: an ID stands as a JSON
-- string of its own ("flaky:…", the report's missed IDs), behind resolved:
-- or in the report's "card …" / "focus …" entries; the kind is a JSON value
-- of its own, and jsonb's text form is canonical, so "kind": "flaky" has
-- exactly that spacing. The output is the model's prose: only its card's
-- "signal" and the {{facts}} in its text are IDs, any other "flaky:" stays.
UPDATE team_analyses
SET assessment = regexp_replace(
        regexp_replace(assessment::text, '("|resolved:)flaky:', '\1failing_more:', 'g'),
        '"kind": "flaky"', '"kind": "failing_more"', 'g')::jsonb,
    output = regexp_replace(output::text, '(\{\{|"signal": "|resolved:)flaky:', '\1failing_more:', 'g')::jsonb,
    report = regexp_replace(report::text, '("|resolved:|card |focus )flaky:', '\1failing_more:', 'g')::jsonb
WHERE assessment::text LIKE '%flaky%' OR output::text LIKE '%flaky%' OR report::text LIKE '%flaky%';
