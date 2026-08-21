package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// schema is the DDL that the cosfaction postgres adapter requires.
// Both tables are created with IF NOT EXISTS so Migrate is safe to call
// on every application startup without checking whether migration has
// already run.
const schema = `
CREATE TABLE IF NOT EXISTS faction_dispositions (
    entity_id    TEXT        NOT NULL,
    faction_id   TEXT        NOT NULL,
    value        BIGINT      NOT NULL DEFAULT 0,
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (entity_id, faction_id)
);

CREATE TABLE IF NOT EXISTS faction_disposition_history (
    id                BIGSERIAL   PRIMARY KEY,
    entity_id         TEXT        NOT NULL,
    faction_id        TEXT        NOT NULL,
    previous_value    BIGINT      NOT NULL,
    new_value         BIGINT      NOT NULL,
    delta             BIGINT      NOT NULL,
    propagation_order INT         NOT NULL DEFAULT 0,
    reason            TEXT,
    source            TEXT,
    occurred_at       BIGINT      NOT NULL
);

CREATE INDEX IF NOT EXISTS faction_disposition_history_lookup
    ON faction_disposition_history (entity_id, faction_id, occurred_at DESC);
`

// Migrate creates the faction_dispositions and faction_disposition_history
// tables in the provided database if they do not already exist. It is safe
// to call on every application startup — all statements use IF NOT EXISTS.
//
// The pool provided must have permission to CREATE TABLE and CREATE INDEX
// in the target schema.
func Migrate(ctx context.Context, db *pgxpool.Pool) error {
	_, err := db.Exec(ctx, schema)
	if err != nil {
		return fmt.Errorf("cosfaction postgres: migration failed: %w", err)
	}
	return nil
}
