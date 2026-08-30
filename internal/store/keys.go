package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"

	fcrypto "github.com/kacperkwapisz/fob/internal/crypto"
	"github.com/kacperkwapisz/fob/internal/db"
	"github.com/kacperkwapisz/fob/internal/domain"
)

type CreatedKey struct {
	Key    domain.LocalKey
	Secret string
}

type KeyStore struct {
	db *db.DB
}

func NewKeyStore(d *db.DB) *KeyStore { return &KeyStore{db: d} }

func (s *KeyStore) Create(name string, providers []domain.ProviderID, models []string, dailyCap *int64) (CreatedKey, error) {
	id := fcrypto.RandomID(12)
	raw := make([]byte, 24)
	if _, err := io.ReadFull(rand.Reader, raw); err != nil {
		return CreatedKey{}, err
	}
	secret := "sk-fob-" + hex.EncodeToString(raw)
	prefix := secret[:12]
	hash := fcrypto.SHA256Hex(secret)
	createdAt := nowMs()
	var providersJSON, modelsJSON any
	if len(providers) > 0 {
		b, _ := json.Marshal(providers)
		providersJSON = string(b)
	}
	if len(models) > 0 {
		b, _ := json.Marshal(models)
		modelsJSON = string(b)
	}
	_, err := s.db.SQL.Exec(
		`INSERT INTO local_keys(id, name, prefix, hash, providers, models, daily_cap, revoked, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, 0, ?)`,
		id, name, prefix, hash, providersJSON, modelsJSON, dailyCap, createdAt,
	)
	if err != nil {
		return CreatedKey{}, err
	}
	return CreatedKey{
		Secret: secret,
		Key: domain.LocalKey{
			ID:        id,
			Name:      name,
			Prefix:    prefix,
			Providers: providers,
			Models:    models,
			DailyCap:  dailyCap,
			Revoked:   false,
			CreatedAt: createdAt,
		},
	}, nil
}

func (s *KeyStore) List() ([]domain.LocalKey, error) {
	rows, err := s.db.SQL.Query(`SELECT id, name, prefix, providers, models, daily_cap, revoked, created_at FROM local_keys ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.LocalKey
	for rows.Next() {
		k, err := scanKey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func (s *KeyStore) Get(id string) (*domain.LocalKey, error) {
	row := s.db.SQL.QueryRow(`SELECT id, name, prefix, providers, models, daily_cap, revoked, created_at FROM local_keys WHERE id = ?`, id)
	k, err := scanKey(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &k, nil
}

func (s *KeyStore) Verify(secret string) (*domain.LocalKey, error) {
	if len(secret) < 7 || secret[:7] != "sk-fob-" {
		return nil, nil
	}
	hash := fcrypto.SHA256Hex(secret)
	row := s.db.SQL.QueryRow(`SELECT id, name, prefix, providers, models, daily_cap, revoked, created_at FROM local_keys WHERE hash = ?`, hash)
	k, err := scanKey(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if k.Revoked {
		return nil, nil
	}
	return &k, nil
}

func (s *KeyStore) Revoke(id string) (bool, error) {
	res, err := s.db.SQL.Exec(`UPDATE local_keys SET revoked = 1 WHERE id = ? AND revoked = 0`, id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s *KeyStore) Allows(key domain.LocalKey, provider domain.ProviderID, model string) error {
	if len(key.Providers) > 0 {
		ok := false
		for _, p := range key.Providers {
			if p == provider {
				ok = true
				break
			}
		}
		if !ok {
			return fmt.Errorf("key is not scoped to provider %s", provider)
		}
	}
	if len(key.Models) > 0 {
		ok := false
		for _, m := range key.Models {
			if m == model {
				ok = true
				break
			}
		}
		if !ok {
			return fmt.Errorf("key is not scoped to model %s", model)
		}
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanKey(row rowScanner) (domain.LocalKey, error) {
	var k domain.LocalKey
	var providers, models sql.NullString
	var daily sql.NullInt64
	var revoked int
	err := row.Scan(&k.ID, &k.Name, &k.Prefix, &providers, &models, &daily, &revoked, &k.CreatedAt)
	if err != nil {
		return k, err
	}
	k.Revoked = revoked == 1
	if daily.Valid {
		v := daily.Int64
		k.DailyCap = &v
	}
	if providers.Valid {
		var list []string
		if json.Unmarshal([]byte(providers.String), &list) == nil {
			for _, p := range list {
				if domain.IsProviderID(p) {
					k.Providers = append(k.Providers, domain.ProviderID(p))
				}
			}
		}
	}
	if models.Valid {
		_ = json.Unmarshal([]byte(models.String), &k.Models)
	}
	return k, nil
}
