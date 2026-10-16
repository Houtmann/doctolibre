-- name: EnqueueOutboxMessage :one
-- Idempotent : un dedup_key déjà présent ne crée rien (ErrNoRows côté Go).
INSERT INTO outbox_message (practice_id, kind, template, payload, recipient, appointment_id, dedup_key, run_at)
VALUES (@practice_id, @kind, @template, @payload, @recipient, @appointment_id, @dedup_key, COALESCE(sqlc.narg('run_at')::timestamptz, now()))
ON CONFLICT (dedup_key) DO NOTHING
RETURNING id;

-- name: CancelOutboxForAppointment :execrows
UPDATE outbox_message
SET status = 'cancelled', updated_at = @now
WHERE appointment_id = @appointment_id AND status = 'pending';

-- name: ClaimOutboxMessages :many
-- Réserve un lot : pending échus -> sending (transaction courte, l'envoi se fait hors transaction).
WITH due AS (
    SELECT id FROM outbox_message
    WHERE status = 'pending' AND run_at <= @now
    ORDER BY run_at
    LIMIT @batch_size
    FOR UPDATE SKIP LOCKED
)
UPDATE outbox_message m
SET status = 'sending', attempts = m.attempts + 1, updated_at = @now
FROM due
WHERE m.id = due.id
RETURNING m.*;

-- name: MarkOutboxSent :execrows
-- Une fois envoyé, les secrets du message (lien de gestion, lien de réinitialisation,
-- lien de proposition de liste d'attente, code, texte du SMS qui les reprend) sont retirés tout de suite du payload.
UPDATE outbox_message
SET payload = payload - 'manage_url' - 'reset_url' - 'code' - 'body' - 'offer_url', status = 'sent', sent_at = @now::timestamptz, provider_ref = @provider_ref, last_error = NULL, updated_at = @now
WHERE id = @id AND status = 'sending' AND attempts = @attempts;

-- name: MarkOutboxRetry :execrows
UPDATE outbox_message
SET status = 'pending', run_at = @run_at, last_error = @last_error, updated_at = @now
WHERE id = @id AND status = 'sending' AND attempts = @attempts;

-- name: MarkOutboxFailed :execrows
UPDATE outbox_message
SET status = 'failed', last_error = @last_error, updated_at = @now
WHERE id = @id AND status = 'sending' AND attempts = @attempts;

-- name: ReleaseOutboxMessage :execrows
-- Rend un message réservé mais non envoyé (arrêt du worker) : la tentative n'est pas comptée.
UPDATE outbox_message
SET status = 'pending', attempts = attempts - 1, updated_at = @now
WHERE id = @id AND status = 'sending' AND attempts = @attempts;

-- name: RecoverStuckOutbox :execrows
-- Crash recovery : sending trop ancien -> pending.
UPDATE outbox_message
SET status = 'pending', updated_at = @now
WHERE status = 'sending' AND updated_at < @cutoff;

-- name: ResendOutboxMessage :execrows
-- Renvoi manuel d'un message failed/cancelled (bouton « renvoyer »).
UPDATE outbox_message
SET status = 'pending', attempts = 0, run_at = @now, last_error = NULL, updated_at = @now
WHERE id = @id AND practice_id = @practice_id AND status IN ('failed', 'cancelled') AND scrubbed_at IS NULL;

-- name: ListRecentOutboxMessages :many
SELECT * FROM outbox_message
WHERE practice_id = @practice_id
ORDER BY created_at DESC, id
LIMIT @max_rows;

-- name: OutboxCountByStatus :many
SELECT status, count(*)::bigint AS n FROM outbox_message GROUP BY status;

-- name: OutboxOldestPending :one
SELECT run_at FROM outbox_message WHERE status = 'pending' ORDER BY run_at LIMIT 1;

-- name: TouchOutboxMessage :execrows
-- Juste avant l'envoi : rafraîchit updated_at (évite la récupération par un autre worker)
-- et vérifie qu'on possède toujours le message (0 ligne => ne pas envoyer).
UPDATE outbox_message
SET updated_at = @now
WHERE id = @id AND status = 'sending' AND attempts = @attempts;
