-- name: CountFailedOutbox :one
-- Messages en échec du cabinet (index partiel outbox_message_failed_idx).
SELECT count(*)::bigint FROM outbox_message
WHERE practice_id = @practice_id AND status = 'failed';

-- name: CountFailedOutboxSince :one
-- Échecs récents hors alertes admin (une alerte en échec ne doit pas en déclencher une autre).
SELECT count(*)::bigint FROM outbox_message
WHERE practice_id = @practice_id AND status = 'failed' AND updated_at >= @since AND template <> 'admin_alert';

-- name: ListPracticeAdminEmails :many
SELECT id, email::text AS email FROM app_user
WHERE practice_id = @practice_id AND role = 'admin'
ORDER BY id;

-- name: DatabaseSize :one
SELECT pg_database_size(current_database())::bigint;

-- name: OutboxDedupExists :one
SELECT EXISTS (SELECT 1 FROM outbox_message WHERE dedup_key = @dedup_key);
