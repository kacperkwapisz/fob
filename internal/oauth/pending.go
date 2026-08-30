package oauth

import (
	"sync"
	"time"

	"github.com/kacperkwapisz/fob/internal/domain"
)

const pendingTTL = 15 * time.Minute

var (
	pendingMu sync.Mutex
	pending   = map[string]PendingLogin{}
)

func PutPending(login PendingLogin) {
	pendingMu.Lock()
	defer pendingMu.Unlock()
	pending[login.State] = login
}

func TakePending(state string) *PendingLogin {
	pendingMu.Lock()
	defer pendingMu.Unlock()
	v, ok := pending[state]
	delete(pending, state)
	if !ok {
		return nil
	}
	if time.Since(time.UnixMilli(v.CreatedAt)) > pendingTTL {
		return nil
	}
	return &v
}

func TakePendingForProvider(provider domain.ProviderID) *PendingLogin {
	pendingMu.Lock()
	defer pendingMu.Unlock()
	var match *PendingLogin
	for state, login := range pending {
		if login.Provider != provider {
			continue
		}
		if time.Since(time.UnixMilli(login.CreatedAt)) > pendingTTL {
			delete(pending, state)
			continue
		}
		if match == nil || login.CreatedAt > match.CreatedAt {
			cp := login
			match = &cp
		}
	}
	if match != nil {
		delete(pending, match.State)
	}
	return match
}

func PeekPending(state string) *PendingLogin {
	pendingMu.Lock()
	defer pendingMu.Unlock()
	v, ok := pending[state]
	if !ok {
		return nil
	}
	if time.Since(time.UnixMilli(v.CreatedAt)) > pendingTTL {
		delete(pending, state)
		return nil
	}
	return &v
}
