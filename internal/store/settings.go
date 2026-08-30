package store

import (
	"database/sql"
	"encoding/json"

	"github.com/kacperkwapisz/fob/internal/db"
)

type SettingsStore struct {
	db *db.DB
}

func NewSettingsStore(d *db.DB) *SettingsStore { return &SettingsStore{db: d} }

func (s *SettingsStore) Get(key string) (string, bool) {
	var value string
	err := s.db.SQL.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
	if err == sql.ErrNoRows || err != nil {
		return "", false
	}
	return value, true
}

func (s *SettingsStore) Set(key, value string) error {
	_, err := s.db.SQL.Exec(
		`INSERT INTO settings(key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, value,
	)
	return err
}

func (s *SettingsStore) GetJSON(key string, dest any) error {
	raw, ok := s.Get(key)
	if !ok {
		return sql.ErrNoRows
	}
	return json.Unmarshal([]byte(raw), dest)
}

func (s *SettingsStore) SetJSON(key string, value any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return s.Set(key, string(b))
}

func (s *SettingsStore) All() (map[string]string, error) {
	rows, err := s.db.SQL.Query(`SELECT key, value FROM settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}
