-- name: CreateAuditLog :exec
INSERT INTO audit_log (user_id, actor, action, entity, entity_id, meta)
VALUES (@user_id, @actor, @action, @entity, @entity_id, @meta);
