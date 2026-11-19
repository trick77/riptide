jsonb_agg(jsonb_build_object(
             'at', e.occurred_at,
             'approved', e.event_type = 'pr:reviewer:approved',
             'author', coalesce(e.author, ''),
             'display_name', coalesce(e.author_display_name, ''))
           ORDER BY e.occurred_at, e.id)
