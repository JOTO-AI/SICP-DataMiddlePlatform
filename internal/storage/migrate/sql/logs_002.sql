CREATE INDEX events_failed_task_attempts ON logs.events
    ((payload #>> '{event,identity,task_id}'), occurred_at DESC, event_id DESC)
    WHERE kind='attempt' AND phase='finished'
      AND payload #>> '{event,outcome}'='failed';
