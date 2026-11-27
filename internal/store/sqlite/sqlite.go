// Package sqlite persists Rocketsled state in a single SQLite file. Each account's
// Massdriver credential is encrypted at rest with a key derived from the server's
// session secret; its bootstrap state is stored alongside it.
package sqlite

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite" // database/sql driver

	"github.com/massdriver-cloud/rocketsled/internal/domain"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Store implements domain.Store on SQLite.
type Store struct {
	db   *sql.DB
	aead cipher.AEAD
}

var _ domain.Store = (*Store)(nil)

// Open creates or migrates the database at path. secret encrypts stored credentials.
func Open(path, secret string) (*Store, error) {
	dsn := "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
	if path == ":memory:" {
		dsn = "file::memory:?cache=shared&_pragma=foreign_keys(1)"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	key := sha256.Sum256([]byte(secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	s := &Store{db: db, aead: aead}
	if err := s.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Close releases the database.
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (name TEXT PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		return err
	}
	entries, err := fs.ReadDir(migrations, "migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	for _, name := range names {
		var applied int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE name = ?`, name).Scan(&applied); err != nil {
			return err
		}
		if applied > 0 {
			continue
		}
		body, err := migrations.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migration %s: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (name, applied_at) VALUES (?, ?)`, name, fmtTime(time.Now())); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func fmtTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func parseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}

func notFound(err error, what string) error {
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: %s", domain.ErrNotFound, what)
	}
	return err
}

func isUnique(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

func (s *Store) seal(plaintext, aad string) ([]byte, error) {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return s.aead.Seal(nonce, nonce, []byte(plaintext), []byte(aad)), nil
}

func (s *Store) open(sealed []byte, aad string) (string, error) {
	n := s.aead.NonceSize()
	if len(sealed) < n {
		return "", errors.New("credential is corrupt")
	}
	plain, err := s.aead.Open(nil, sealed[:n], sealed[n:], []byte(aad))
	if err != nil {
		return "", fmt.Errorf("decrypt credential: %w", err)
	}
	return string(plain), nil
}

// --- users -------------------------------------------------------------------

func (s *Store) UpsertUser(ctx context.Context, u *domain.User) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO users (id, provider, provider_user_id, login, name, email, avatar_url, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET login = excluded.login, name = excluded.name,
			email = excluded.email, avatar_url = excluded.avatar_url`,
		u.ID, u.Provider, u.ProviderUserID, u.Login, u.Name, u.Email, u.AvatarURL, fmtTime(u.CreatedAt))
	if err != nil {
		return err
	}
	var created string
	if err := s.db.QueryRowContext(ctx, `SELECT created_at FROM users WHERE id = ?`, u.ID).Scan(&created); err != nil {
		return err
	}
	u.CreatedAt = parseTime(created)
	return nil
}

func (s *Store) GetUser(ctx context.Context, id string) (*domain.User, error) {
	var u domain.User
	var created string
	err := s.db.QueryRowContext(ctx, `SELECT id, provider, provider_user_id, login, name, email, avatar_url, created_at FROM users WHERE id = ?`, id).
		Scan(&u.ID, &u.Provider, &u.ProviderUserID, &u.Login, &u.Name, &u.Email, &u.AvatarURL, &created)
	if err != nil {
		return nil, notFound(err, "user "+id)
	}
	u.CreatedAt = parseTime(created)
	return &u, nil
}

// --- accounts ----------------------------------------------------------------

const accountColumns = `id, name, owner_user_id, organization_id, organization_name, status, catalog_id, project_id,
	base_environment_id, platforms, install_log, install_error, created_at, updated_at`

func scanAccount(row interface{ Scan(...any) error }) (*domain.Account, error) {
	var a domain.Account
	var platforms, created, updated string
	err := row.Scan(&a.ID, &a.Name, &a.OwnerUserID, &a.OrganizationID, &a.OrganizationName, &a.Status, &a.CatalogID, &a.ProjectID,
		&a.BaseEnvironmentID, &platforms, &a.InstallLog, &a.InstallError, &created, &updated)
	if err != nil {
		return nil, err
	}
	a.Platforms = map[string]string{}
	if err := json.Unmarshal([]byte(platforms), &a.Platforms); err != nil {
		return nil, fmt.Errorf("decode platforms: %w", err)
	}
	a.CreatedAt = parseTime(created)
	a.UpdatedAt = parseTime(updated)
	return &a, nil
}

func (s *Store) CreateAccount(ctx context.Context, a *domain.Account, credential domain.Credential) error {
	platforms, err := json.Marshal(orEmpty(a.Platforms))
	if err != nil {
		return err
	}
	sealed, err := s.seal(credential.APIKey, a.ID)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO accounts (`+accountColumns+`, credential_url, credential_enc)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.Name, a.OwnerUserID, a.OrganizationID, a.OrganizationName, a.Status, a.CatalogID, a.ProjectID,
		a.BaseEnvironmentID, string(platforms), a.InstallLog, a.InstallError, fmtTime(a.CreatedAt), fmtTime(a.UpdatedAt),
		credential.URL, sealed)
	if isUnique(err) {
		return fmt.Errorf("%w: user already has an account", domain.ErrConflict)
	}
	return err
}

func orEmpty(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

func (s *Store) GetAccount(ctx context.Context, id string) (*domain.Account, error) {
	a, err := scanAccount(s.db.QueryRowContext(ctx, `SELECT `+accountColumns+` FROM accounts WHERE id = ?`, id))
	if err != nil {
		return nil, notFound(err, "account "+id)
	}
	return a, nil
}

func (s *Store) GetAccountByOwner(ctx context.Context, userID string) (*domain.Account, error) {
	a, err := scanAccount(s.db.QueryRowContext(ctx, `SELECT `+accountColumns+` FROM accounts WHERE owner_user_id = ?`, userID))
	if err != nil {
		return nil, notFound(err, "account for "+userID)
	}
	return a, nil
}

func (s *Store) ListAccounts(ctx context.Context, status domain.AccountStatus) ([]domain.Account, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+accountColumns+` FROM accounts WHERE status = ? ORDER BY created_at`, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Account{}
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

func (s *Store) UpdateAccount(ctx context.Context, a *domain.Account) error {
	platforms, err := json.Marshal(orEmpty(a.Platforms))
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `UPDATE accounts SET name = ?, organization_name = ?, status = ?, catalog_id = ?, project_id = ?,
		base_environment_id = ?, platforms = ?, install_log = ?, install_error = ?, updated_at = ? WHERE id = ?`,
		a.Name, a.OrganizationName, a.Status, a.CatalogID, a.ProjectID, a.BaseEnvironmentID, string(platforms), a.InstallLog, a.InstallError, fmtTime(a.UpdatedAt), a.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("%w: account %s", domain.ErrNotFound, a.ID)
	}
	return nil
}

func (s *Store) GetAccountCredential(ctx context.Context, accountID string) (domain.Credential, error) {
	var url, orgID string
	var sealed []byte
	if err := s.db.QueryRowContext(ctx, `SELECT credential_url, organization_id, credential_enc FROM accounts WHERE id = ?`, accountID).Scan(&url, &orgID, &sealed); err != nil {
		return domain.Credential{}, notFound(err, "account "+accountID)
	}
	key, err := s.open(sealed, accountID)
	if err != nil {
		return domain.Credential{}, err
	}
	return domain.Credential{URL: url, OrganizationID: orgID, APIKey: key}, nil
}

func (s *Store) SaveBootstrapState(ctx context.Context, accountID string, state []byte) error {
	res, err := s.db.ExecContext(ctx, `UPDATE accounts SET bootstrap_state = ? WHERE id = ?`, state, accountID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("%w: account %s", domain.ErrNotFound, accountID)
	}
	return nil
}

func (s *Store) GetBootstrapState(ctx context.Context, accountID string) ([]byte, error) {
	var state []byte
	if err := s.db.QueryRowContext(ctx, `SELECT bootstrap_state FROM accounts WHERE id = ?`, accountID).Scan(&state); err != nil {
		return nil, notFound(err, "account "+accountID)
	}
	if state == nil {
		return nil, fmt.Errorf("%w: no bootstrap state for %s", domain.ErrNotFound, accountID)
	}
	return state, nil
}

// --- environments ------------------------------------------------------------

const environmentColumns = `id, account_id, slug, name, environment_id, presets, state, created_by, created_at, updated_at, runtime, addons`

func scanEnvironment(row interface{ Scan(...any) error }) (*domain.Environment, error) {
	var e domain.Environment
	var presets, created, updated, addons string
	if err := row.Scan(&e.ID, &e.AccountID, &e.Slug, &e.Name, &e.EnvironmentID, &presets, &e.State, &e.CreatedBy, &created, &updated, &e.Runtime, &addons); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(addons), &e.Addons); err != nil {
		return nil, fmt.Errorf("decode addons: %w", err)
	}
	if len(e.Addons) == 0 {
		e.Addons = nil
	}
	e.Presets = map[string]string{}
	if err := json.Unmarshal([]byte(presets), &e.Presets); err != nil {
		return nil, fmt.Errorf("decode presets: %w", err)
	}
	e.CreatedAt = parseTime(created)
	e.UpdatedAt = parseTime(updated)
	return &e, nil
}

func (s *Store) CreateEnvironment(ctx context.Context, e *domain.Environment) error {
	presets, err := json.Marshal(orEmpty(e.Presets))
	if err != nil {
		return err
	}
	addons, err := json.Marshal(orEmptyList(e.Addons))
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO environments (`+environmentColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, e.AccountID, e.Slug, e.Name, e.EnvironmentID, string(presets), e.State, e.CreatedBy, fmtTime(e.CreatedAt), fmtTime(e.UpdatedAt), e.Runtime, string(addons))
	if isUnique(err) {
		return fmt.Errorf("%w: environment %s", domain.ErrConflict, e.Slug)
	}
	return err
}

func (s *Store) GetEnvironment(ctx context.Context, id string) (*domain.Environment, error) {
	e, err := scanEnvironment(s.db.QueryRowContext(ctx, `SELECT `+environmentColumns+` FROM environments WHERE id = ?`, id))
	if err != nil {
		return nil, notFound(err, "environment "+id)
	}
	return e, nil
}

func (s *Store) ListEnvironments(ctx context.Context, accountID string) ([]domain.Environment, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+environmentColumns+` FROM environments WHERE account_id = ? ORDER BY created_at`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Environment{}
	for rows.Next() {
		e, err := scanEnvironment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, rows.Err()
}

func (s *Store) UpdateEnvironment(ctx context.Context, e *domain.Environment) error {
	presets, err := json.Marshal(orEmpty(e.Presets))
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `UPDATE environments SET name = ?, environment_id = ?, presets = ?, state = ?, updated_at = ? WHERE id = ?`,
		e.Name, e.EnvironmentID, string(presets), e.State, fmtTime(e.UpdatedAt), e.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("%w: environment %s", domain.ErrNotFound, e.ID)
	}
	return nil
}

func (s *Store) DeleteEnvironment(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM environments WHERE id = ?`, id)
	return err
}

// --- runs --------------------------------------------------------------------

const runColumns = `id, environment_id, number, trigger_kind, status, message, requested_by, created_at, updated_at`

func scanRun(row interface{ Scan(...any) error }) (*domain.Run, error) {
	var r domain.Run
	var created, updated string
	if err := row.Scan(&r.ID, &r.EnvironmentID, &r.Number, &r.Trigger, &r.Status, &r.Message, &r.RequestedBy, &created, &updated); err != nil {
		return nil, err
	}
	r.CreatedAt = parseTime(created)
	r.UpdatedAt = parseTime(updated)
	return &r, nil
}

func (s *Store) CreateRun(ctx context.Context, r *domain.Run) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO runs (`+runColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.EnvironmentID, r.Number, r.Trigger, r.Status, r.Message, r.RequestedBy, fmtTime(r.CreatedAt), fmtTime(r.UpdatedAt))
	return err
}

func (s *Store) UpdateRun(ctx context.Context, r *domain.Run) error {
	res, err := s.db.ExecContext(ctx, `UPDATE runs SET status = ?, message = ?, updated_at = ? WHERE id = ?`, r.Status, r.Message, fmtTime(r.UpdatedAt), r.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("%w: run %s", domain.ErrNotFound, r.ID)
	}
	return nil
}

func (s *Store) ListRuns(ctx context.Context, environmentID string, limit int) ([]domain.Run, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+runColumns+` FROM runs WHERE environment_id = ? ORDER BY number DESC LIMIT ?`, environmentID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Run{}
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

func orEmptyList(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
