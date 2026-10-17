-- name: GetNotifyTransport :one
SELECT * FROM practice_notify_transport WHERE practice_id = @practice_id AND kind = @kind;

-- name: ListNotifyTransports :many
SELECT * FROM practice_notify_transport WHERE practice_id = @practice_id ORDER BY kind;

-- name: UpsertNotifyTransport :exec
-- updated_at (horloge de la base, à la microseconde) sert de clé de cache du worker : toute modification invalide le sender.
INSERT INTO practice_notify_transport (practice_id, kind, mode, driver, settings, secrets, updated_at)
VALUES (@practice_id, @kind, @mode, sqlc.narg('driver'), @settings, sqlc.narg('secrets'), clock_timestamp())
ON CONFLICT (practice_id, kind) DO UPDATE SET
    mode = EXCLUDED.mode, driver = EXCLUDED.driver, settings = EXCLUDED.settings,
    secrets = EXCLUDED.secrets, updated_at = clock_timestamp();

-- name: SetPracticeNotificationChannels :exec
-- Repli des réglages par événement quand un canal est désactivé (T086).
UPDATE practice SET
    notify_confirmation_channel = @notify_confirmation_channel,
    notify_reminder_channel = @notify_reminder_channel,
    notify_change_channel = @notify_change_channel,
    updated_at = now()
WHERE id = @id;
