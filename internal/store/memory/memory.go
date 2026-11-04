// Package memory is an in-memory domain.Store used by tests and local experiments.
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
	mu          sync.RWMutex
	users       map[string]domain.User
	credentials map[string]string
	stacks      map[string]domain.Stack
	instances   map[string]domain.Instance
	deploys     map[string]domain.Deploy
}

var _ domain.Store = (*Store)(nil)

// New returns an empty Store.
func New() *Store {
	return &Store{
		users:       map[string]domain.User{},
		credentials: map[string]string{},
		stacks:      map[string]domain.Stack{},
		instances:   map[string]domain.Instance{},
		deploys:     map[string]domain.Deploy{},
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

func (s *Store) SaveCredential(_ context.Context, userID, token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.credentials[userID] = token
	return nil
}

func (s *Store) GetCredential(_ context.Context, userID string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.credentials[userID]
	if !ok {
		return "", fmt.Errorf("%w: credential for %s", domain.ErrNotFound, userID)
	}
	return t, nil
}

func (s *Store) CreateStack(_ context.Context, st *domain.Stack) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.stacks[st.ID]; exists {
		return fmt.Errorf("%w: stack %s", domain.ErrConflict, st.ID)
	}
	s.stacks[st.ID] = *st
	return nil
}

func (s *Store) GetStack(_ context.Context, id string) (*domain.Stack, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st, ok := s.stacks[id]
	if !ok {
		return nil, fmt.Errorf("%w: stack %s", domain.ErrNotFound, id)
	}
	return &st, nil
}

func (s *Store) ListStacks(_ context.Context) ([]domain.Stack, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]domain.Stack, 0, len(s.stacks))
	for _, st := range s.stacks {
		out = append(out, st)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (s *Store) FindStacksByRepo(_ context.Context, provider domain.Provider, fullName string) ([]domain.Stack, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []domain.Stack{}
	for _, st := range s.stacks {
		if st.Repo.Provider == provider && st.Repo.FullName == fullName {
			out = append(out, st)
		}
	}
	return out, nil
}

func (s *Store) DeleteStack(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.stacks[id]; !ok {
		return fmt.Errorf("%w: stack %s", domain.ErrNotFound, id)
	}
	delete(s.stacks, id)
	return nil
}

func (s *Store) CreateInstance(_ context.Context, i *domain.Instance) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.instances {
		if existing.StackID == i.StackID && existing.Slug == i.Slug {
			return fmt.Errorf("%w: instance %s", domain.ErrConflict, i.Slug)
		}
	}
	s.instances[i.ID] = *i
	return nil
}

func (s *Store) GetInstance(_ context.Context, id string) (*domain.Instance, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	i, ok := s.instances[id]
	if !ok {
		return nil, fmt.Errorf("%w: instance %s", domain.ErrNotFound, id)
	}
	return &i, nil
}

func (s *Store) ListInstances(_ context.Context, stackID string) ([]domain.Instance, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []domain.Instance{}
	for _, i := range s.instances {
		if i.StackID == stackID {
			out = append(out, i)
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].CreatedAt.Before(out[b].CreatedAt) })
	return out, nil
}

func (s *Store) UpdateInstance(_ context.Context, i *domain.Instance) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.instances[i.ID]; !ok {
		return fmt.Errorf("%w: instance %s", domain.ErrNotFound, i.ID)
	}
	s.instances[i.ID] = *i
	return nil
}

func (s *Store) DeleteInstance(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.instances, id)
	for did, d := range s.deploys {
		if d.InstanceID == id {
			delete(s.deploys, did)
		}
	}
	return nil
}

func (s *Store) CreateDeploy(_ context.Context, d *domain.Deploy) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deploys[d.ID] = *d
	return nil
}

func (s *Store) UpdateDeploy(_ context.Context, d *domain.Deploy) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.deploys[d.ID]; !ok {
		return fmt.Errorf("%w: deploy %s", domain.ErrNotFound, d.ID)
	}
	s.deploys[d.ID] = *d
	return nil
}

// ListDeploys returns the newest runs first.
func (s *Store) ListDeploys(_ context.Context, instanceID string, limit int) ([]domain.Deploy, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []domain.Deploy{}
	for _, d := range s.deploys {
		if d.InstanceID == instanceID {
			out = append(out, d)
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Number > out[b].Number })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
