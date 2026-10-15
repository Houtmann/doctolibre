-- +goose Up
-- Métriques /metrics : OutboxCountByStatus (GROUP BY status) devient un
-- index-only scan au lieu d'un parcours de la table.
CREATE INDEX outbox_message_status_idx ON outbox_message (status);

-- +goose Down
DROP INDEX outbox_message_status_idx;
