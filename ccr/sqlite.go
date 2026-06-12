package ccr

import (
	"database/sql"
	"time"

	_ "modernc.org/sqlite"
)

// SqliteStore is a persistent CCR store backed by SQLite with WAL mode.
type SqliteStore struct {
	db  *sql.DB
	ttl time.Duration
}

// NewSqliteStore opens (or creates) a SQLite database at path and returns a store.
func NewSqliteStore(path string, ttl time.Duration) (*SqliteStore, error) {
	db, err := sql.Open("sqlite", path+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, err
	}
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS ccr (
		hash TEXT PRIMARY KEY,
		payload BLOB NOT NULL,
		inserted_at INTEGER NOT NULL
	)`)
	if err != nil {
		db.Close()
		return nil, err
	}
	return &SqliteStore{db: db, ttl: ttl}, nil
}

func (s *SqliteStore) Put(key string, value []byte) {
	now := time.Now().Unix()
	s.db.Exec(
		`INSERT INTO ccr (hash, payload, inserted_at) VALUES (?, ?, ?)
		 ON CONFLICT(hash) DO UPDATE SET payload=excluded.payload, inserted_at=excluded.inserted_at`,
		key, value, now,
	)
}

func (s *SqliteStore) Get(key string) ([]byte, bool) {
	cutoff := time.Now().Add(-s.ttl).Unix()
	var payload []byte
	err := s.db.QueryRow(
		`SELECT payload FROM ccr WHERE hash = ? AND inserted_at > ?`,
		key, cutoff,
	).Scan(&payload)
	if err != nil {
		return nil, false
	}
	return payload, true
}

func (s *SqliteStore) Len() int {
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM ccr`).Scan(&n)
	return n
}

// Close closes the underlying database connection.
func (s *SqliteStore) Close() error {
	return s.db.Close()
}
