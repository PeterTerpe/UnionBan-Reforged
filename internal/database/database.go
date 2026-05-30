package database

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

type Database struct {
	db *sql.DB
}

func Open(path string) (*Database, error) {
	// Create the parent directory if it does not exist.
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}

	// Open the SQLite database file.
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}

	database := &Database{
		db: db,
	}

	// Configure SQLite for local daemon usage.
	if err := database.configure(); err != nil {
		db.Close()
		return nil, err
	}

	return database, nil
}

func (d *Database) configure() error {
	// WAL mode improves behavior for long-running applications.
	if _, err := d.db.Exec("PRAGMA journal_mode=WAL;"); err != nil {
		return err
	}

	// Foreign keys are disabled by default in SQLite, so enable them explicitly.
	if _, err := d.db.Exec("PRAGMA foreign_keys=ON;"); err != nil {
		return err
	}

	// Wait up to 5 seconds when the database is busy.
	if _, err := d.db.Exec("PRAGMA busy_timeout=5000;"); err != nil {
		return err
	}

	return nil
}

func (d *Database) Migrate(ctx context.Context) error {
	query := `
CREATE TABLE IF NOT EXISTS metadata (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

INSERT OR IGNORE INTO metadata (key, value)
VALUES ('schema_version', '1');

CREATE TABLE IF NOT EXISTS local_identity (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    node_id TEXT NOT NULL,
    public_key TEXT NOT NULL,
    private_key TEXT NOT NULL,
    certificate TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS banlist (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    player_uuid TEXT NOT NULL,
    player_name TEXT NOT NULL DEFAULT '',
    reason TEXT NOT NULL,
    source_node_id TEXT NOT NULL,
    uuid_source TEXT NOT NULL DEFAULT '',
    signature TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_banlist_player_uuid
ON banlist(player_uuid);

CREATE INDEX IF NOT EXISTS idx_banlist_source_node_id
ON banlist(source_node_id);

CREATE TABLE IF NOT EXISTS player_decision_cache (
    server_id TEXT NOT NULL,
    player_uuid TEXT NOT NULL,
    player_name TEXT NOT NULL DEFAULT '',
    decision TEXT NOT NULL,
    reason TEXT NOT NULL DEFAULT '',
    policy_met TEXT NOT NULL DEFAULT '',
    banlist_version TEXT NOT NULL,
    updated_at INTEGER NOT NULL,
    PRIMARY KEY (server_id, player_uuid)
);

CREATE INDEX IF NOT EXISTS idx_player_decision_cache_updated_at
ON player_decision_cache(updated_at);
`

	if _, err := d.db.ExecContext(ctx, query); err != nil {
		return err
	}

	if err := d.ensureColumn(ctx, "banlist", "player_name", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}

	if err := d.ensureColumn(ctx, "banlist", "uuid_source", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}

	return nil
}

func (d *Database) ensureColumn(ctx context.Context, table string, column string, definition string) error {
	rows, err := d.db.QueryContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name string
		var columnType string
		var notNull int
		var defaultValue sql.NullString
		var pk int

		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			return err
		}

		if name == column {
			return nil
		}
	}

	if err := rows.Err(); err != nil {
		return err
	}

	_, err = d.db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, definition))
	return err
}

func (d *Database) Ping(ctx context.Context) error {
	// Check whether the database connection is alive.
	return d.db.PingContext(ctx)
}

func (d *Database) Close() error {
	return d.db.Close()
}

type DebugInfo struct {
	OK            bool
	SchemaVersion string
	Message       string
}

func (d *Database) DebugInfo(ctx context.Context) DebugInfo {
	// Check whether the database is reachable.
	if err := d.db.PingContext(ctx); err != nil {
		return DebugInfo{
			OK:      false,
			Message: err.Error(),
		}
	}

	var schemaVersion string

	// Read the current schema version from the metadata table.
	err := d.db.QueryRowContext(
		ctx,
		"SELECT value FROM metadata WHERE key = 'schema_version'",
	).Scan(&schemaVersion)

	if err != nil {
		return DebugInfo{
			OK:      false,
			Message: err.Error(),
		}
	}

	return DebugInfo{
		OK:            true,
		SchemaVersion: schemaVersion,
		Message:       "database is reachable",
	}
}
