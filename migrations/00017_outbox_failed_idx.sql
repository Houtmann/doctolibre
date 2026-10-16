-- +goose Up
-- Compteurs de messages en échec (page /admin/status, alertes) : les failed sont rares,
-- l'index partiel reste minuscule.
CREATE INDEX outbox_message_failed_idx ON outbox_message (practice_id, updated_at) WHERE status = 'failed';

-- +goose Down
DROP INDEX outbox_message_failed_idx;
