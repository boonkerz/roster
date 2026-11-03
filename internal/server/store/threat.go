package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// ThreatFeedRow ist der gespeicherte Stand einer Blockliste.
type ThreatFeedRow struct {
	Feed      string
	Data      string
	FetchedAt time.Time
	Entries   int
	Error     string
}

// ThreatFeed liest den gespeicherten Stand eines Feeds (nil, wenn noch nie geladen).
func (s *Store) ThreatFeed(ctx context.Context, id string) (*ThreatFeedRow, error) {
	var r ThreatFeedRow
	err := s.db.QueryRowContext(ctx, s.rebind(`SELECT feed, data, fetched_at, entries, error FROM threat_feeds WHERE feed=?`), id).
		Scan(&r.Feed, &r.Data, &r.FetchedAt, &r.Entries, &r.Error)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// SetThreatFeed speichert einen frisch geladenen Feed (Fehler leer).
func (s *Store) SetThreatFeed(ctx context.Context, id, data string, entries int) error {
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx, s.rebind(`UPDATE threat_feeds SET data=?, fetched_at=?, entries=?, error='' WHERE feed=?`), data, now, entries, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		_, err = s.db.ExecContext(ctx, s.rebind(`INSERT INTO threat_feeds (feed, data, fetched_at, entries, error) VALUES (?, ?, ?, ?, '')`), id, data, now, entries)
	}
	return err
}

// SetThreatFeedError vermerkt einen fehlgeschlagenen Abruf; der alte Stand bleibt stehen.
func (s *Store) SetThreatFeedError(ctx context.Context, id, msg string) error {
	res, err := s.db.ExecContext(ctx, s.rebind(`UPDATE threat_feeds SET error=? WHERE feed=?`), msg, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		_, err = s.db.ExecContext(ctx, s.rebind(`INSERT INTO threat_feeds (feed, data, fetched_at, entries, error) VALUES (?, '', ?, 0, ?)`), id, time.Time{}.UTC(), msg)
	}
	return err
}

// ThreatFeeds liefert alle gespeicherten Feeds (ohne Daten).
func (s *Store) ThreatFeeds(ctx context.Context) (map[string]ThreatFeedRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT feed, fetched_at, entries, error FROM threat_feeds`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]ThreatFeedRow{}
	for rows.Next() {
		var r ThreatFeedRow
		if err := rows.Scan(&r.Feed, &r.FetchedAt, &r.Entries, &r.Error); err != nil {
			return nil, err
		}
		out[r.Feed] = r
	}
	return out, rows.Err()
}
