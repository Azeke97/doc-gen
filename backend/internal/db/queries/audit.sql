-- name: CreateAuditEntry :exec
INSERT INTO audit_log (actor, action, entity_type, entity_id, details)
VALUES ($1, $2, $3, $4, $5);
