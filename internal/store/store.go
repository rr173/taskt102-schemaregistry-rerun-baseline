// Package store is the SQLite persistence layer for the schema registry. It
// stores subjects (with their compatibility mode and next-version counter),
// schema versions (definition, fingerprint, creation time) and a small key/
// value meta table (used for the global default compatibility mode). All
// mutating operations that must stay atomic (registering a version, deleting a
// subject, setting a meta key) run inside a single SQL transaction.
package store

import (
	"database/sql"
	"errors"
	"fmt"

	_ "modernc.org/sqlite"
)

// ErrNotFound is returned when a subject or version does not exist.
var ErrNotFound = errors.New("not found")

// SubjectRow is the persisted state of a subject.
type SubjectRow struct {
	Name          string
	Compatibility string
	NextVersion   int
	CreatedAt     string
}

// SchemaRow is a persisted schema version.
type SchemaRow struct {
	Subject     string
	Version     int
	Definition  string
	Fingerprint string
	CreatedAt   string
}

// MetaKeyDefaultCompat is the meta key for the global default compatibility.
const MetaKeyDefaultCompat = "default_compatibility"

// Store wraps a database/sql connection to SQLite.
type Store struct {
	db *sql.DB
}

// Open opens (or creates) the SQLite database at path and ensures the schema.
// An empty path or ":memory:" uses an in-memory database.
func Open(path string) (*Store, error) {
	dsn := path
	if dsn == "" {
		dsn = ":memory:"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// A single connection serializes all access, which is safe and simple for
	// SQLite; the registry also serializes writes in-process.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA busy_timeout=5000;`); err != nil {
		db.Close()
		return nil, fmt.Errorf("set busy_timeout: %w", err)
	}
	if _, err := db.Exec(`PRAGMA journal_mode=WAL;`); err != nil {
		db.Close()
		return nil, fmt.Errorf("set journal_mode: %w", err)
	}
	s := &Store{db: db}
	if err := s.ensureSchema(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) ensureSchema() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS subjects (
	name          TEXT PRIMARY KEY,
	compatibility TEXT NOT NULL DEFAULT 'BACKWARD',
	next_version  INTEGER NOT NULL DEFAULT 1,
	created_at    TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS schemas (
	subject     TEXT NOT NULL,
	version     INTEGER NOT NULL,
	definition  TEXT NOT NULL,
	fingerprint TEXT NOT NULL,
	created_at  TEXT NOT NULL,
	PRIMARY KEY (subject, version)
);
CREATE INDEX IF NOT EXISTS idx_schemas_subject ON schemas(subject);
CREATE TABLE IF NOT EXISTS meta (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS audit (
	id      INTEGER PRIMARY KEY AUTOINCREMENT,
	subject TEXT NOT NULL,
	action  TEXT NOT NULL,
	version INTEGER NOT NULL DEFAULT 0,
	detail  TEXT NOT NULL DEFAULT '',
	at      TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_audit_subject ON audit(subject, id);
`)
	if err != nil {
		return fmt.Errorf("ensure schema: %w", err)
	}
	return nil
}

// Close closes the underlying database connection.
func (s *Store) Close() error {
	if s.db == nil {
		return nil
	}
	return s.db.Close()
}

// EnsureSubject creates a subject with the given compatibility if it does not
// exist. If it already exists, this is a no-op.
func (s *Store) EnsureSubject(name, compat, createdAt string) error {
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO subjects(name, compatibility, next_version, created_at) VALUES(?, ?, 1, ?)`,
		name, compat, createdAt)
	return err
}

// GetSubject returns a subject row or ErrNotFound.
func (s *Store) GetSubject(name string) (*SubjectRow, error) {
	row := s.db.QueryRow(
		`SELECT name, compatibility, next_version, created_at FROM subjects WHERE name=?`, name)
	var r SubjectRow
	if err := row.Scan(&r.Name, &r.Compatibility, &r.NextVersion, &r.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &r, nil
}

// ListSubjects returns all subjects ordered by name.
func (s *Store) ListSubjects() ([]SubjectRow, error) {
	rows, err := s.db.Query(`SELECT name, compatibility, next_version, created_at FROM subjects ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]SubjectRow, 0)
	for rows.Next() {
		var r SubjectRow
		if err := rows.Scan(&r.Name, &r.Compatibility, &r.NextVersion, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SetCompatibility ensures the subject exists (created with the given mode)
// and sets its compatibility mode.
func (s *Store) SetCompatibility(name, mode, createdAt string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(
		`INSERT OR IGNORE INTO subjects(name, compatibility, next_version, created_at) VALUES(?, ?, 1, ?)`,
		name, mode, createdAt); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE subjects SET compatibility=? WHERE name=?`, mode, name); err != nil {
		return err
	}
	return tx.Commit()
}

// GetSchema returns a schema version or ErrNotFound.
func (s *Store) GetSchema(subject string, version int) (*SchemaRow, error) {
	row := s.db.QueryRow(
		`SELECT subject, version, definition, fingerprint, created_at FROM schemas WHERE subject=? AND version=?`,
		subject, version)
	var r SchemaRow
	if err := row.Scan(&r.Subject, &r.Version, &r.Definition, &r.Fingerprint, &r.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &r, nil
}

// ListVersions returns all version numbers of a subject in ascending order.
// A subject with no versions returns an empty slice (no error).
func (s *Store) ListVersions(subject string) ([]int, error) {
	rows, err := s.db.Query(`SELECT version FROM schemas WHERE subject=? ORDER BY version`, subject)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]int, 0)
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// LatestVersion returns the maximum version number for a subject, or 0 if none.
func (s *Store) LatestVersion(subject string) (int, error) {
	var max sql.NullInt64
	err := s.db.QueryRow(`SELECT MAX(version) FROM schemas WHERE subject=?`, subject).Scan(&max)
	if err != nil {
		return 0, err
	}
	if !max.Valid {
		return 0, nil
	}
	return int(max.Int64), nil
}

// LatestSchema returns the schema row with the greatest version for a subject,
// or ErrNotFound if the subject has no versions.
func (s *Store) LatestSchema(subject string) (*SchemaRow, error) {
	row := s.db.QueryRow(
		`SELECT subject, version, definition, fingerprint, created_at FROM schemas WHERE subject=? ORDER BY version DESC LIMIT 1`,
		subject)
	var r SchemaRow
	if err := row.Scan(&r.Subject, &r.Version, &r.Definition, &r.Fingerprint, &r.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &r, nil
}

// RegisterVersion atomically assigns the next version number, stores the schema
// definition and fingerprint, and advances the subject's next-version counter.
// If the subject does not yet exist it is created with the given compatibility.
// It returns the assigned version number.
func (s *Store) RegisterVersion(subject, compat, definition, fingerprint, createdAt string) (int, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(
		`INSERT OR IGNORE INTO subjects(name, compatibility, next_version, created_at) VALUES(?, ?, 1, ?)`,
		subject, compat, createdAt); err != nil {
		return 0, err
	}
	var nextVersion int
	if err := tx.QueryRow(`SELECT next_version FROM subjects WHERE name=?`, subject).Scan(&nextVersion); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(
		`INSERT INTO schemas(subject, version, definition, fingerprint, created_at) VALUES(?, ?, ?, ?, ?)`,
		subject, nextVersion, definition, fingerprint, createdAt); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(`UPDATE subjects SET next_version=? WHERE name=?`, nextVersion+1, subject); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return nextVersion, nil
}

// DeleteSubject deletes a subject and all its versions. It reports ErrNotFound
// if the subject does not exist.
func (s *Store) DeleteSubject(subject string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`DELETE FROM subjects WHERE name=?`, subject)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	if _, err := tx.Exec(`DELETE FROM schemas WHERE subject=?`, subject); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteVersion deletes a single schema version. It reports ErrNotFound if the
// version does not exist.
func (s *Store) DeleteVersion(subject string, version int) error {
	res, err := s.db.Exec(`DELETE FROM schemas WHERE subject=? AND version=?`, subject, version)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// GetMeta returns the value for a meta key, or "" with a nil error if the key
// is unset.
func (s *Store) GetMeta(key string) (string, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM meta WHERE key=?`, key).Scan(&v)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	return v, nil
}

// SetMeta upserts a meta key/value pair.
func (s *Store) SetMeta(key, value string) error {
	_, err := s.db.Exec(`INSERT INTO meta(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

// AuditRow is one audit-log entry.
type AuditRow struct {
	ID      int64  `json:"id"`
	Subject string `json:"subject"`
	Action  string `json:"action"`
	Version int    `json:"version,omitempty"`
	Detail  string `json:"detail,omitempty"`
	At      string `json:"at"`
}

// AppendAudit records an audit event. version is 0 when not applicable.
func (s *Store) AppendAudit(subject, action string, version int, detail, at string) error {
	_, err := s.db.Exec(
		`INSERT INTO audit(subject, action, version, detail, at) VALUES(?, ?, ?, ?, ?)`,
		subject, action, version, detail, at)
	return err
}

// ListAudit returns audit entries for a subject, newest first, up to limit.
// A limit <= 0 means a default cap of 100.
func (s *Store) ListAudit(subject string, limit int) ([]AuditRow, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.Query(
		`SELECT id, subject, action, version, detail, at FROM audit WHERE subject=? ORDER BY id DESC LIMIT ?`,
		subject, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]AuditRow, 0)
	for rows.Next() {
		var r AuditRow
		if err := rows.Scan(&r.ID, &r.Subject, &r.Action, &r.Version, &r.Detail, &r.At); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// AllSchemas returns every schema version of a subject in ascending version
// order, used for transitive checks and exports.
func (s *Store) AllSchemas(subject string) ([]SchemaRow, error) {
	rows, err := s.db.Query(
		`SELECT subject, version, definition, fingerprint, created_at FROM schemas WHERE subject=? ORDER BY version`,
		subject)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]SchemaRow, 0)
	for rows.Next() {
		var r SchemaRow
		if err := rows.Scan(&r.Subject, &r.Version, &r.Definition, &r.Fingerprint, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
