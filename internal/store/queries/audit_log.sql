-- name: CreateAuditLog :exec
-- practice_id : si absent, celui de l'utilisateur.
INSERT INTO audit_log (user_id, practice_id, actor, action, entity, entity_id, meta)
VALUES (
    @user_id,
    COALESCE(sqlc.narg('practice_id')::uuid, (SELECT practice_id FROM app_user WHERE id = @user_id::uuid)),
    @actor, @action, @entity, @entity_id, @meta
);
