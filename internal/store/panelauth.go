package store

import (
	"database/sql"

	fcrypto "github.com/kacperkwapisz/fob/internal/crypto"
	"github.com/kacperkwapisz/fob/internal/db"
)

type PanelAuthState struct {
	Seeded    bool
	MustReset bool
}

type PanelAuth struct {
	db *db.DB
}

func NewPanelAuth(d *db.DB) *PanelAuth { return &PanelAuth{db: d} }

func (p *PanelAuth) EnsureSeed() (seed string, state PanelAuthState, err error) {
	row := p.row()
	if row != nil {
		return "", PanelAuthState{Seeded: true, MustReset: row.mustReset}, nil
	}
	seed = fcrypto.GenerateSeedPassword()
	hash, err := fcrypto.HashPassword(seed)
	if err != nil {
		return "", state, err
	}
	_, err = p.db.SQL.Exec(`INSERT INTO panel_auth(id, password_hash, must_reset, created_at) VALUES (1, ?, 1, ?)`, hash, nowMs())
	if err != nil {
		return "", state, err
	}
	return seed, PanelAuthState{Seeded: true, MustReset: true}, nil
}

func (p *PanelAuth) Verify(password string) (ok bool, mustReset bool) {
	row := p.row()
	if row == nil {
		return false, false
	}
	if !fcrypto.VerifyPassword(password, row.hash) {
		return false, false
	}
	return true, row.mustReset
}

func (p *PanelAuth) Reset(oldPassword, newPassword string) (ok bool, reason string) {
	if len(newPassword) < 8 {
		return false, "new password must be at least 8 characters"
	}
	ok, _ = p.Verify(oldPassword)
	if !ok {
		return false, "old password is wrong"
	}
	hash, err := fcrypto.HashPassword(newPassword)
	if err != nil {
		return false, err.Error()
	}
	_, err = p.db.SQL.Exec(`UPDATE panel_auth SET password_hash = ?, must_reset = 0 WHERE id = 1`, hash)
	if err != nil {
		return false, err.Error()
	}
	return true, ""
}

func (p *PanelAuth) State() PanelAuthState {
	row := p.row()
	if row == nil {
		return PanelAuthState{Seeded: false, MustReset: true}
	}
	return PanelAuthState{Seeded: true, MustReset: row.mustReset}
}

type panelRow struct {
	hash      string
	mustReset bool
}

func (p *PanelAuth) row() *panelRow {
	var hash string
	var must int
	err := p.db.SQL.QueryRow(`SELECT password_hash, must_reset FROM panel_auth WHERE id = 1`).Scan(&hash, &must)
	if err == sql.ErrNoRows || err != nil {
		return nil
	}
	return &panelRow{hash: hash, mustReset: must == 1}
}
