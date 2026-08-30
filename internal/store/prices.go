package store

import (
	"database/sql"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"time"

	"github.com/kacperkwapisz/fob/internal/db"
	"github.com/kacperkwapisz/fob/internal/domain"
	"github.com/kacperkwapisz/fob/internal/prices"
)

const (
	modelsDevURL = "https://models.dev/api.json"
	refreshMS    = 24 * 60 * 60 * 1000
)

type PriceCatalog struct {
	Models map[string]PriceModel `json:"models"`
}

type PriceModel struct {
	ID   string     `json:"id"`
	Name string     `json:"name"`
	Cost *PriceCost `json:"cost"`
}

type PriceCost struct {
	Input      *float64 `json:"input"`
	Output     *float64 `json:"output"`
	CacheRead  *float64 `json:"cache_read"`
	CacheWrite *float64 `json:"cache_write"`
}

type PriceStore struct {
	db        *db.DB
	lastFetch int64
}

func NewPriceStore(d *db.DB) (*PriceStore, error) {
	s := &PriceStore{db: d}
	if err := s.seedFromSnapshot(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *PriceStore) seedFromSnapshot() error {
	var n int
	if err := s.db.SQL.QueryRow(`SELECT COUNT(*) FROM prices`).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		merged := map[string]PriceCatalog{}
		if err := json.Unmarshal(prices.APIJSON, &merged); err != nil {
			return err
		}
		if err := s.upsertCatalog(merged, nowMs()); err != nil {
			return err
		}
	}
	return s.upsertCursorOverlay()
}

func (s *PriceStore) upsertCursorOverlay() error {
	cursor := map[string]PriceCatalog{}
	if err := json.Unmarshal(prices.CursorJSON, &cursor); err != nil {
		return err
	}
	if err := s.upsertCatalog(cursor, nowMs()); err != nil {
		return err
	}
	_, err := s.db.SQL.Exec(`DELETE FROM prices WHERE provider = 'cursor' AND model IN ('cursor-auto', 'auto', 'default')`)
	return err
}

func (s *PriceStore) Get(provider, model string) *domain.ModelPrice {
	if provider == "cursor" && isCursorAuto(model) {
		return nil
	}
	if p := s.lookup(provider, model); p != nil {
		return p
	}
	if provider == "cursor" {
		for _, alt := range []string{"anthropic", "openai", "xai"} {
			if p := s.lookup(alt, model); p != nil {
				return p
			}
		}
	}
	return nil
}

func (s *PriceStore) lookup(provider, model string) *domain.ModelPrice {
	row := s.db.SQL.QueryRow(`SELECT provider, model, input, output, cache_read, cache_write FROM prices WHERE provider = ? AND model = ?`, provider, model)
	if p, err := scanPrice(row); err == nil {
		return &p
	}
	row = s.db.SQL.QueryRow(
		`SELECT provider, model, input, output, cache_read, cache_write FROM prices WHERE provider = ? AND (model = ? OR ? LIKE model || '%') ORDER BY length(model) DESC LIMIT 1`,
		provider, model, model,
	)
	if p, err := scanPrice(row); err == nil {
		return &p
	}
	return nil
}

func (s *PriceStore) Estimate(provider, model string, prompt, completion, cacheRead, cacheWrite int64) float64 {
	p := s.Get(provider, model)
	if p == nil {
		return 0
	}
	perM := 1_000_000.0
	usd := 0.0
	if p.Input != nil {
		usd += *p.Input * float64(prompt) / perM
	}
	if p.Output != nil {
		usd += *p.Output * float64(completion) / perM
	}
	if p.CacheRead != nil {
		usd += *p.CacheRead * float64(cacheRead) / perM
	}
	if p.CacheWrite != nil {
		usd += *p.CacheWrite * float64(cacheWrite) / perM
	}
	return math.Round(usd*1_000_000) / 1_000_000
}

func (s *PriceStore) Refresh(force bool) string {
	now := nowMs()
	if !force && now-s.lastFetch < refreshMS {
		return "skipped"
	}
	client := &http.Client{Timeout: 15 * time.Second}
	res, err := client.Get(modelsDevURL)
	if err != nil {
		return "failed"
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "failed"
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return "failed"
	}
	var data map[string]PriceCatalog
	if err := json.Unmarshal(body, &data); err != nil {
		return "failed"
	}
	if err := s.upsertCatalog(data, now); err != nil {
		return "failed"
	}
	if err := s.upsertCursorOverlay(); err != nil {
		return "failed"
	}
	s.lastFetch = now
	return "ok"
}

func (s *PriceStore) upsertCatalog(data map[string]PriceCatalog, fetchedAt int64) error {
	tx, err := s.db.SQL.Begin()
	if err != nil {
		return err
	}
	stmt, err := tx.Prepare(
		`INSERT INTO prices(provider, model, input, output, cache_read, cache_write, fetched_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(provider, model) DO UPDATE SET
		   input = excluded.input,
		   output = excluded.output,
		   cache_read = excluded.cache_read,
		   cache_write = excluded.cache_write,
		   fetched_at = excluded.fetched_at`,
	)
	if err != nil {
		tx.Rollback()
		return err
	}
	defer stmt.Close()
	for provider, catalog := range data {
		for id, m := range catalog.Models {
			model := m.ID
			if model == "" {
				model = id
			}
			var in, out, cr, cw any
			if m.Cost != nil {
				in, out, cr, cw = m.Cost.Input, m.Cost.Output, m.Cost.CacheRead, m.Cost.CacheWrite
			}
			if _, err := stmt.Exec(provider, model, in, out, cr, cw, fetchedAt); err != nil {
				tx.Rollback()
				return err
			}
		}
	}
	return tx.Commit()
}

func isCursorAuto(model string) bool {
	switch model {
	case "cursor-auto", "auto", "default":
		return true
	default:
		return false
	}
}

func scanPrice(row rowScanner) (domain.ModelPrice, error) {
	var p domain.ModelPrice
	var in, out, cr, cw sql.NullFloat64
	err := row.Scan(&p.Provider, &p.Model, &in, &out, &cr, &cw)
	if err != nil {
		return p, err
	}
	if in.Valid {
		p.Input = &in.Float64
	}
	if out.Valid {
		p.Output = &out.Float64
	}
	if cr.Valid {
		p.CacheRead = &cr.Float64
	}
	if cw.Valid {
		p.CacheWrite = &cw.Float64
	}
	return p, nil
}
