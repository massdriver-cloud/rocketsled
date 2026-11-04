// Package memory is an in-memory domain.Store used by tests.
package memory

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/massdriver-cloud/rocketsled/internal/domain"
)

// Store keeps everything in maps guarded by one mutex.
type Store struct {
	mu           sync.RWMutex
	users        map[string]domain.User
	accounts     map[string]domain.Account
	credentials  map[string]domain.Credential
	states       map[string][]byte
	environments map[string]domain.Environment
	runs         map[string]domain.Run
}

var _ domain.Store = (*Store)(nil)

// New returns an empty Store.
func New() *Store {
	return &Store{
		users:        map[string]domain.User{},
		accounts:     map[string]domain.Account{},
		credentials:  map[string]domain.Credential{},
		states:       map[string][]byte{},
		environments: map[string]domain.Environment{},
		runs:         map[string]domain.Run{},
	}
}

func (s *Store) UpsertUser(_ context.Context, u *domain.User) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.users[u.ID]; ok {
		u.CreatedAt = existing.CreatedAt
	}
	s.users[u.ID] = *u
	return nil
}

func (s *Store) GetUser(_ context.Context, id string) (*domain.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.users[id]
	if !ok {
		return nil, fmt.Errorf("%w: user %s", domain.ErrNotFound, id)
	}
	return &u, nil
}

func (s *Store) CreateAccount(_ context.Context, a *domain.Account, credential domain.Credential) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.accounts {
		if existing.OwnerUserID == a.OwnerUserID {
			return fmt.Errorf("%w: user already has an account", domain.ErrConflict)
		}
	}
	s.accounts[a.ID] = cloneAccount(*a)
	s.credentials[a.ID] = credential
	return nil
}

func cloneAccount(a domain.Account) domain.Account {
	p := make(map[string]string, len(a.Platforms))
	for k, v := range a.Platforms {
		p[k] = v
	}
	a.Platforms = p
	return a
}

func (s *Store) GetAccount(_ context.Context, id string) (*domain.Account, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	a, ok := s.accounts[id]
	if !ok {
		return nil, fmt.Errorf("%w: account %s", domain.ErrNotFound, id)
	}
	a = cloneAccount(a)
	return &a, nil
}

func (s *Store) GetAccountByOwner(_ context.Context, userID string) (*domain.Account, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, a := range s.accounts {
		if a.OwnerUserID == userID {
			a = cloneAccount(a)
			return &a, nil
		}
	}
	return nil, fmt.Errorf("%w: no account for %s", domain.ErrNotFound, userID)
}

func (s *Store) UpdateAccount(_ context.Context, a *domain.Account) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.accounts[a.ID]; !ok {
		return fmt.Errorf("%w: account %s", domain.ErrNotFound, a.ID)
	}
	s.accounts[a.ID] = cloneAccount(*a)
	return nil
}

func (s *Store) GetAccountCredential(_ context.Context, accountID string) (domain.Credential, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.credentials[accountID]
	if !ok {
		return domain.Credential{}, fmt.Errorf("%w: credential for account %s", domain.ErrNotFound, accountID)
	}
	return c, nil
}

func (s *Store) SaveBootstrapState(_ context.Context, accountID string, state []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.states[accountID] = append([]byte(nil), state...)
	return nil
}

func (s *Store) GetBootstrapState(_ context.Context, accountID string) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st, ok := s.states[accountID]
	if !ok {
		return nil, fmt.Errorf("%w: state for account %s", domain.ErrNotFound, accountID)
	}
	return append([]byte(nil), st...), nil
}

func (s *Store) CreateEnvironment(_ context.Context, e *domain.Environment) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.environments {
		if existing.AccountID == e.AccountID && existing.Slug == e.Slug {
			return fmt.Errorf("%w: environment %s", domain.ErrConflict, e.Slug)
		}
	}
	s.environments[e.ID] = *e
	return nil
}

func (s *Store) GetEnvironment(_ context.Context, id string) (*domain.Environment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.environments[id]
	if !ok {
		return nil, fmt.Errorf("%w: environment %s", domain.ErrNotFound, id)
	}
	return &e, nil
}

func (s *Store) ListEnvironments(_ context.Context, accountID string) ([]domain.Environment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []domain.Environment{}
	for _, e := range s.environments {
		if e.AccountID == accountID {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].CreatedAt.Before(out[b].CreatedAt) })
	return out, nil
}

func (s *Store) UpdateEnvironment(_ context.Context, e *domain.Environment) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.environments[e.ID]; !ok {
		return fmt.Errorf("%w: environment %s", domain.ErrNotFound, e.ID)
	}
	s.environments[e.ID] = *e
	return nil
}

func (s *Store) DeleteEnvironment(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.environments, id)
	for rid, r := range s.runs {
		if r.EnvironmentID == id {
			delete(s.runs, rid)
		}
	}
	return nil
}

func (s *Store) CreateRun(_ context.Context, r *domain.Run) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runs[r.ID] = *r
	return nil
}

func (s *Store) UpdateRun(_ context.Context, r *domain.Run) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.runs[r.ID]; !ok {
		return fmt.Errorf("%w: run %s", domain.ErrNotFound, r.ID)
	}
	s.runs[r.ID] = *r
	return nil
}

// ListRuns returns the newest runs first.
func (s *Store) ListRuns(_ context.Context, environmentID string, limit int) ([]domain.Run, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []domain.Run{}
	for _, r := range s.runs {
		if r.EnvironmentID == environmentID {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Number > out[b].Number })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
