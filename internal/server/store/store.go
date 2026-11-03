// Package store kapselt den DB-Zugriff. Er unterstützt SQLite (modernc, CGo-frei)
// und PostgreSQL (pgx) über denselben Repository-Layer.
package store

import (
	"database/sql"
	"embed"
	"fmt"
	"strings"

	"github.com/pressly/goose/v3"

	_ "github.com/jackc/pgx/v5/stdlib" // Treiber "pgx"
	_ "modernc.org/sqlite"             // Treiber "sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Dialect unterscheidet die beiden unterstützten Datenbanken.
type Dialect int

const (
	DialectSQLite Dialect = iota
	DialectPostgres
)

// Store hält den DB-Pool und den erkannten Dialekt.
type Store struct {
	db      *sql.DB
	dialect Dialect
}

// Open verbindet anhand der URL und führt ausstehende Migrationen aus.
//
//	sqlite://./inventory.db        -> lokale Datei
//	postgres://user:pw@host/db     -> PostgreSQL
func Open(databaseURL string) (*Store, error) {
	driver, dsn, dialect, err := parseURL(databaseURL)
	if err != nil {
		return nil, err
	}

	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, fmt.Errorf("db öffnen: %w", err)
	}
	if dialect == DialectSQLite {
		// WAL erlaubt beliebig viele Leser neben einem Schreiber. Mit nur einer Verbindung
		// warteten Oberfläche und Checkins hintereinander auf jede einzelne Abfrage.
		// Transaktionen starten mit BEGIN IMMEDIATE (_txlock), damit Schreiber sofort die
		// Sperre nehmen und über busy_timeout warten statt in einen Deadlock zu laufen.
		db.SetMaxOpenConns(4)
		db.SetMaxIdleConns(4)
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("db erreichen: %w", err)
	}

	s := &Store{db: db, dialect: dialect}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	return s, nil
}

// DB gibt den darunterliegenden Pool zurück (für Tests).
func (s *Store) DB() *sql.DB { return s.db }

// Close schließt den Pool.
func (s *Store) Close() error { return s.db.Close() }

func parseURL(u string) (driver, dsn string, d Dialect, err error) {
	switch {
	case strings.HasPrefix(u, "sqlite://"):
		path := strings.TrimPrefix(u, "sqlite://")
		if path == "" {
			return "", "", 0, fmt.Errorf("sqlite-pfad fehlt in %q", u)
		}
		// Foreign Keys aktivieren + WAL für bessere Nebenläufigkeit.
		dsn = path + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(15000)&_pragma=synchronous(NORMAL)&_txlock=immediate"
		return "sqlite", dsn, DialectSQLite, nil
	case strings.HasPrefix(u, "postgres://"), strings.HasPrefix(u, "postgresql://"):
		return "pgx", u, DialectPostgres, nil
	default:
		return "", "", 0, fmt.Errorf("unbekanntes db-schema in %q (erwartet sqlite:// oder postgres://)", u)
	}
}

func (s *Store) migrate() error {
	goose.SetBaseFS(migrationsFS)
	dialect := "sqlite3"
	if s.dialect == DialectPostgres {
		dialect = "postgres"
	}
	if err := goose.SetDialect(dialect); err != nil {
		return fmt.Errorf("goose dialekt: %w", err)
	}
	if err := goose.Up(s.db, "migrations"); err != nil {
		return fmt.Errorf("migrationen: %w", err)
	}
	return nil
}

// rebind wandelt `?`-Platzhalter für PostgreSQL in `$1, $2, ...` um.
// Für SQLite bleibt die Query unverändert.
func (s *Store) rebind(query string) string {
	if s.dialect != DialectPostgres {
		return query
	}
	var b strings.Builder
	n := 0
	for _, r := range query {
		if r == '?' {
			n++
			b.WriteByte('$')
			b.WriteString(fmt.Sprint(n))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
