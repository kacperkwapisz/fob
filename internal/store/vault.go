package store

import (
	"database/sql"
	"encoding/json"

	fcrypto "github.com/kacperkwapisz/fob/internal/crypto"
	"github.com/kacperkwapisz/fob/internal/db"
	"github.com/kacperkwapisz/fob/internal/domain"
)

type Vault struct {
	db  *db.DB
	key []byte
}

func NewVault(d *db.DB, key []byte) *Vault { return &Vault{db: d, key: key} }

type SaveCredential struct {
	ID        string
	Provider  domain.ProviderID
	Label     string
	Tokens    domain.CredentialTokens
	ExpiresAt *int64
}

func (v *Vault) Save(input SaveCredential) (domain.Credential, error) {
	now := nowMs()
	id := input.ID
	if id == "" {
		id = fcrypto.RandomID(12)
	}
	existing, _ := v.Get(id)
	createdAt := now
	if existing != nil {
		createdAt = existing.CreatedAt
	}
	raw, err := json.Marshal(tokenJSON(input.Tokens))
	if err != nil {
		return domain.Credential{}, err
	}
	blob, err := fcrypto.Encrypt(v.key, string(raw))
	if err != nil {
		return domain.Credential{}, err
	}
	var expires any
	if input.ExpiresAt != nil {
		expires = *input.ExpiresAt
	}
	_, err = v.db.SQL.Exec(
		`INSERT INTO credentials(id, provider, label, blob, expires_at, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   label = excluded.label,
		   blob = excluded.blob,
		   expires_at = excluded.expires_at,
		   updated_at = excluded.updated_at`,
		id, string(input.Provider), input.Label, blob, expires, createdAt, now,
	)
	if err != nil {
		return domain.Credential{}, err
	}
	return domain.Credential{
		ID:        id,
		Provider:  input.Provider,
		Label:     input.Label,
		Tokens:    input.Tokens,
		ExpiresAt: input.ExpiresAt,
		CreatedAt: createdAt,
		UpdatedAt: now,
	}, nil
}

func (v *Vault) Get(id string) (*domain.Credential, error) {
	row := v.db.SQL.QueryRow(`SELECT id, provider, label, blob, expires_at, created_at, updated_at FROM credentials WHERE id = ?`, id)
	c, err := v.scan(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (v *Vault) List(provider ...domain.ProviderID) ([]domain.Credential, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if len(provider) > 0 && provider[0] != "" {
		rows, err = v.db.SQL.Query(`SELECT id, provider, label, blob, expires_at, created_at, updated_at FROM credentials WHERE provider = ? ORDER BY created_at`, string(provider[0]))
	} else {
		rows, err = v.db.SQL.Query(`SELECT id, provider, label, blob, expires_at, created_at, updated_at FROM credentials ORDER BY created_at`)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Credential
	for rows.Next() {
		c, err := v.scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (v *Vault) Remove(id string) (bool, error) {
	res, err := v.db.SQL.Exec(`DELETE FROM credentials WHERE id = ?`, id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (v *Vault) Healthy(provider domain.ProviderID) ([]domain.Credential, error) {
	all, err := v.List(provider)
	if err != nil {
		return nil, err
	}
	now := nowMs()
	var out []domain.Credential
	for _, c := range all {
		if c.ExpiresAt == nil || *c.ExpiresAt > now-60_000 {
			out = append(out, c)
		}
	}
	return out, nil
}

func (v *Vault) scan(row rowScanner) (domain.Credential, error) {
	var c domain.Credential
	var provider, blob string
	var expires sql.NullInt64
	err := row.Scan(&c.ID, &provider, &c.Label, &blob, &expires, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return c, err
	}
	c.Provider = domain.ProviderID(provider)
	if expires.Valid {
		v := expires.Int64
		c.ExpiresAt = &v
	}
	plain, err := fcrypto.Decrypt(v.key, blob)
	if err != nil {
		return c, err
	}
	var tok tokenWire
	if err := json.Unmarshal([]byte(plain), &tok); err != nil {
		return c, err
	}
	c.Tokens = tok.toTokens()
	return c, nil
}

type tokenWire struct {
	AccessToken  string         `json:"accessToken"`
	RefreshToken *string        `json:"refreshToken"`
	AccountID    *string        `json:"accountId"`
	Email        *string        `json:"email"`
	Extra        map[string]any `json:"extra"`
}

func tokenJSON(t domain.CredentialTokens) tokenWire {
	w := tokenWire{AccessToken: t.AccessToken, Extra: t.Extra}
	if t.RefreshToken != "" {
		w.RefreshToken = &t.RefreshToken
	}
	if t.AccountID != "" {
		w.AccountID = &t.AccountID
	}
	if t.Email != "" {
		w.Email = &t.Email
	}
	if w.Extra == nil {
		w.Extra = map[string]any{}
	}
	return w
}

func (w tokenWire) toTokens() domain.CredentialTokens {
	t := domain.CredentialTokens{AccessToken: w.AccessToken, Extra: w.Extra}
	if w.RefreshToken != nil {
		t.RefreshToken = *w.RefreshToken
	}
	if w.AccountID != nil {
		t.AccountID = *w.AccountID
	}
	if w.Email != nil {
		t.Email = *w.Email
	}
	if t.Extra == nil {
		t.Extra = map[string]any{}
	}
	return t
}
