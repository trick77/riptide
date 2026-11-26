coalesce(max(e.author_display_name) FILTER (WHERE e.event_type = 'pr:opened'),
           max(e.author_display_name) FILTER (WHERE e.event_type LIKE 'pr:%' AND e.event_type NOT IN (
             'pr:comment:added', 'pr:reviewer:approved', 'pr:reviewer:needs_work', 'pr:reviewer:updated',
             'pr:reviewer:unapproved', 'pr:ready_for_review', 'pr:back_to_draft')))
