package store

import (
	"time"

	"github.com/kacperkwapisz/fob/internal/db"
	"github.com/kacperkwapisz/fob/internal/domain"
)

const retainMS = 90 * 24 * 60 * 60 * 1000

type UsageTotals struct {
	Requests         int64
	PromptTokens     int64
	CompletionTokens int64
	CacheReadTokens  int64
	CacheWriteTokens int64
	USD              float64
	Errors           int64
}

type UsageBreakdown struct {
	Key string
	UsageTotals
}

type UsageStore struct {
	db *db.DB
}

func NewUsageStore(d *db.DB) *UsageStore { return &UsageStore{db: d} }

func (s *UsageStore) Record(event domain.UsageEvent) error {
	var keyID any
	if event.KeyID != "" {
		keyID = event.KeyID
	}
	_, err := s.db.SQL.Exec(
		`INSERT INTO usage_events(ts, key_id, provider, model, inbound, prompt_tokens, completion_tokens, cache_read_tokens, cache_write_tokens, latency_ms, status, usd)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.TS, keyID, string(event.Provider), event.Model, string(event.Inbound),
		event.PromptTokens, event.CompletionTokens, event.CacheReadTokens, event.CacheWriteTokens,
		event.LatencyMs, event.Status, event.USD,
	)
	return err
}

func (s *UsageStore) Purge() (int64, error) {
	cutoff := nowMs() - retainMS
	res, err := s.db.SQL.Exec(`DELETE FROM usage_events WHERE ts < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *UsageStore) Since(msAgo int64) (UsageTotals, error) {
	from := nowMs() - msAgo
	row := s.db.SQL.QueryRow(
		`SELECT COUNT(*) AS requests,
		        COALESCE(SUM(prompt_tokens),0) AS prompt,
		        COALESCE(SUM(completion_tokens),0) AS completion,
		        COALESCE(SUM(cache_read_tokens),0) AS cache_read,
		        COALESCE(SUM(cache_write_tokens),0) AS cache_write,
		        COALESCE(SUM(usd),0) AS usd,
		        COALESCE(SUM(CASE WHEN status = 'error' THEN 1 ELSE 0 END),0) AS errors
		 FROM usage_events WHERE ts >= ?`, from)
	return scanTotals(row)
}

func (s *UsageStore) GroupBy(msAgo int64, field string) ([]UsageBreakdown, error) {
	from := nowMs() - msAgo
	col := field
	if field == "key_id" {
		col = "COALESCE(key_id, 'unknown')"
	}
	rows, err := s.db.SQL.Query(
		`SELECT `+col+` AS key,
		        COUNT(*) AS requests,
		        COALESCE(SUM(prompt_tokens),0) AS prompt,
		        COALESCE(SUM(completion_tokens),0) AS completion,
		        COALESCE(SUM(cache_read_tokens),0) AS cache_read,
		        COALESCE(SUM(cache_write_tokens),0) AS cache_write,
		        COALESCE(SUM(usd),0) AS usd,
		        COALESCE(SUM(CASE WHEN status = 'error' THEN 1 ELSE 0 END),0) AS errors
		 FROM usage_events WHERE ts >= ?
		 GROUP BY `+col+`
		 ORDER BY usd DESC, requests DESC`, from)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UsageBreakdown
	for rows.Next() {
		var b UsageBreakdown
		if err := rows.Scan(&b.Key, &b.Requests, &b.PromptTokens, &b.CompletionTokens, &b.CacheReadTokens, &b.CacheWriteTokens, &b.USD, &b.Errors); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *UsageStore) TodayTokensForKey(keyID string) (int64, error) {
	start := startOfUTCDay(nowMs())
	var n int64
	err := s.db.SQL.QueryRow(
		`SELECT COALESCE(SUM(prompt_tokens + completion_tokens),0) FROM usage_events WHERE key_id = ? AND ts >= ?`,
		keyID, start,
	).Scan(&n)
	return n, err
}

func scanTotals(row rowScanner) (UsageTotals, error) {
	var t UsageTotals
	err := row.Scan(&t.Requests, &t.PromptTokens, &t.CompletionTokens, &t.CacheReadTokens, &t.CacheWriteTokens, &t.USD, &t.Errors)
	return t, err
}

func startOfUTCDay(ts int64) int64 {
	t := time.UnixMilli(ts).UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC).UnixMilli()
}
