(SELECT split_part(x.repo_full_name, '/', 1)
            FROM (SELECT b.repo_full_name, b.occurred_at
                    FROM bitbucket_events b
                   WHERE b.commit_sha = r.revision AND b.repo_full_name IS NOT NULL
                  UNION ALL
                  SELECT b.repo_full_name, b.occurred_at
                    FROM bitbucket_events b
                   WHERE b.event_type = 'pr:merged' AND b.repo_full_name IS NOT NULL
                     AND lower(b.payload->'pullRequest'->'properties'->'mergeCommit'->>'id') = r.revision) x
           ORDER BY x.occurred_at
           LIMIT 1)
