package store

import (
	"time"

	"github.com/kacperkwapisz/fob/internal/db"
	"github.com/kacperkwapisz/fob/internal/domain"
)

const (
	dayMS    = 24 * 60 * 60 * 1000
	retainMS = 90 * dayMS
)

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

type UsageDay struct {
	Start int64
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

func (s *UsageStore) Daily(days int) ([]UsageDay, error) {
	if days < 1 {
		days = 1
	}
	if days > 90 {
		days = 90
	}
	now := time.UnixMilli(nowMs()).Local()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	from := today.AddDate(0, 0, -(days - 1))
	rows, err := s.db.SQL.Query(
		`SELECT strftime('%Y-%m-%d', ts / 1000, 'unixepoch', 'localtime') AS day,
		        COUNT(*) AS requests,
		        COALESCE(SUM(prompt_tokens),0) AS prompt,
		        COALESCE(SUM(completion_tokens),0) AS completion,
		        COALESCE(SUM(cache_read_tokens),0) AS cache_read,
		        COALESCE(SUM(cache_write_tokens),0) AS cache_write,
		        COALESCE(SUM(usd),0) AS usd,
		        COALESCE(SUM(CASE WHEN status = 'error' THEN 1 ELSE 0 END),0) AS errors
		 FROM usage_events WHERE ts >= ?
		 GROUP BY day
		 ORDER BY day`, from.UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byDay := map[string]UsageTotals{}
	for rows.Next() {
		var key string
		var t UsageTotals
		if err := rows.Scan(&key, &t.Requests, &t.PromptTokens, &t.CompletionTokens, &t.CacheReadTokens, &t.CacheWriteTokens, &t.USD, &t.Errors); err != nil {
			return nil, err
		}
		byDay[key] = t
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]UsageDay, days)
	for i := 0; i < days; i++ {
		d := from.AddDate(0, 0, i)
		out[i] = UsageDay{Start: d.UnixMilli(), UsageTotals: byDay[d.Format("2006-01-02")]}
	}
	return out, nil
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
