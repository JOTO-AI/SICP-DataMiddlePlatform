-- Source failure summaries scan recent acquisition facts, never log payloads.
CREATE INDEX checkpoints_acquisition_recent ON control.checkpoints(recorded_at DESC,task_id DESC)
WHERE name='acquisition-progress';
