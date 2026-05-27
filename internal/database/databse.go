package database

import (
	"context"
	"database/sql"
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
	// Create a minimal metadata table for the first development version.
	query := `
CREATE TABLE IF NOT EXISTS metadata (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

INSERT OR IGNORE INTO metadata (key, value)
VALUES ('schema_version', '1');
`

	_, err := d.db.ExecContext(ctx, query)
	return err
}

func (d *Database) Ping(ctx context.Context) error {
	// Check whether the database connection is alive.
	return d.db.PingContext(ctx)
}

func (d *Database) Close() error {
	return d.db.Close()
}
