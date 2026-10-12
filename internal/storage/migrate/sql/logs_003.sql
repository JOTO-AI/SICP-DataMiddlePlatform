-- Preserve legacy call/attempt rows in place. New dual writers enforce routing
-- before opening a transaction; historical rows retain their original source.
ALTER TABLE logs.events ADD CONSTRAINT events_kind_allowed
    CHECK (kind IN ('request','task','step','audit','call','attempt'));
ALTER TABLE logs.events ADD CONSTRAINT events_phase_allowed
    CHECK (phase IN ('started','finished','transport'));
