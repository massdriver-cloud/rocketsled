// Package sqlite persists Rocketsled state in a single SQLite file. SCM tokens are
// encrypted at rest with a key derived from the server's session secret.
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

// Open creates or migrates the database at path. secret encrypts stored tokens.
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
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (name, applied_at) VALUES (?, ?)`, name, now()); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }

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

func (s *Store) SaveCredential(ctx context.Context, userID, token string) error {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	sealed := s.aead.Seal(nonce, nonce, []byte(token), []byte(userID))
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO credentials (user_id, token_enc, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET token_enc = excluded.token_enc, updated_at = excluded.updated_at`,
		userID, sealed, now())
	return err
}

func (s *Store) GetCredential(ctx context.Context, userID string) (string, error) {
	var sealed []byte
	if err := s.db.QueryRowContext(ctx, `SELECT token_enc FROM credentials WHERE user_id = ?`, userID).Scan(&sealed); err != nil {
		return "", notFound(err, "credential for "+userID)
	}
	n := s.aead.NonceSize()
	if len(sealed) < n {
		return "", errors.New("credential is corrupt")
	}
	plain, err := s.aead.Open(nil, sealed[:n], sealed[n:], []byte(userID))
	if err != nil {
		return "", fmt.Errorf("decrypt credential: %w", err)
	}
	return string(plain), nil
}

// --- stacks ------------------------------------------------------------------

const stackColumns = `id, name, repo_provider, repo_external_id, repo_full_name, repo_url, repo_default_branch, repo_private,
	path, project_id, webhook_id, deploy_token_hash, created_by, created_at`

func scanStack(row interface{ Scan(...any) error }) (*domain.Stack, error) {
	var st domain.Stack
	var created string
	var private int
	err := row.Scan(&st.ID, &st.Name, &st.Repo.Provider, &st.Repo.ExternalID, &st.Repo.FullName, &st.Repo.URL,
		&st.Repo.DefaultBranch, &private, &st.Path, &st.ProjectID, &st.WebhookID, &st.DeployTokenHash, &st.CreatedBy, &created)
	if err != nil {
		return nil, err
	}
	st.Repo.Private = private == 1
	st.CreatedAt = parseTime(created)
	return &st, nil
}

func (s *Store) CreateStack(ctx context.Context, st *domain.Stack) error {
	private := 0
	if st.Repo.Private {
		private = 1
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO stacks (`+stackColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		st.ID, st.Name, st.Repo.Provider, st.Repo.ExternalID, st.Repo.FullName, st.Repo.URL, st.Repo.DefaultBranch, private,
		st.Path, st.ProjectID, st.WebhookID, st.DeployTokenHash, st.CreatedBy, fmtTime(st.CreatedAt))
	if isUnique(err) {
		return fmt.Errorf("%w: stack %s", domain.ErrConflict, st.ID)
	}
	return err
}

func (s *Store) GetStack(ctx context.Context, id string) (*domain.Stack, error) {
	st, err := scanStack(s.db.QueryRowContext(ctx, `SELECT `+stackColumns+` FROM stacks WHERE id = ?`, id))
	if err != nil {
		return nil, notFound(err, "stack "+id)
	}
	return st, nil
}

func (s *Store) ListStacks(ctx context.Context) ([]domain.Stack, error) {
	return s.queryStacks(ctx, `SELECT `+stackColumns+` FROM stacks ORDER BY created_at`)
}

func (s *Store) FindStacksByRepo(ctx context.Context, provider domain.Provider, fullName string) ([]domain.Stack, error) {
	return s.queryStacks(ctx, `SELECT `+stackColumns+` FROM stacks WHERE repo_provider = ? AND repo_full_name = ? ORDER BY created_at`, provider, fullName)
}

func (s *Store) queryStacks(ctx context.Context, query string, args ...any) ([]domain.Stack, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Stack{}
	for rows.Next() {
		st, err := scanStack(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *st)
	}
	return out, rows.Err()
}

func (s *Store) DeleteStack(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM stacks WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("%w: stack %s", domain.ErrNotFound, id)
	}
	return nil
}

// --- instances ---------------------------------------------------------------

const instanceColumns = `id, stack_id, slug, name, branch, environment_id, presets, state, created_by, created_at, updated_at`

func scanInstance(row interface{ Scan(...any) error }) (*domain.Instance, error) {
	var i domain.Instance
	var presets, created, updated string
	if err := row.Scan(&i.ID, &i.StackID, &i.Slug, &i.Name, &i.Branch, &i.EnvironmentID, &presets, &i.State, &i.CreatedBy, &created, &updated); err != nil {
		return nil, err
	}
	i.Presets = map[string]string{}
	if err := json.Unmarshal([]byte(presets), &i.Presets); err != nil {
		return nil, fmt.Errorf("decode presets: %w", err)
	}
	i.CreatedAt = parseTime(created)
	i.UpdatedAt = parseTime(updated)
	return &i, nil
}

func (s *Store) CreateInstance(ctx context.Context, i *domain.Instance) error {
	presets, err := json.Marshal(orEmpty(i.Presets))
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO instances (`+instanceColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		i.ID, i.StackID, i.Slug, i.Name, i.Branch, i.EnvironmentID, string(presets), i.State, i.CreatedBy, fmtTime(i.CreatedAt), fmtTime(i.UpdatedAt))
	if isUnique(err) {
		return fmt.Errorf("%w: instance %s", domain.ErrConflict, i.Slug)
	}
	return err
}

func orEmpty(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

func (s *Store) GetInstance(ctx context.Context, id string) (*domain.Instance, error) {
	i, err := scanInstance(s.db.QueryRowContext(ctx, `SELECT `+instanceColumns+` FROM instances WHERE id = ?`, id))
	if err != nil {
		return nil, notFound(err, "instance "+id)
	}
	return i, nil
}

func (s *Store) ListInstances(ctx context.Context, stackID string) ([]domain.Instance, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+instanceColumns+` FROM instances WHERE stack_id = ? ORDER BY created_at`, stackID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Instance{}
	for rows.Next() {
		i, err := scanInstance(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *i)
	}
	return out, rows.Err()
}

func (s *Store) UpdateInstance(ctx context.Context, i *domain.Instance) error {
	presets, err := json.Marshal(orEmpty(i.Presets))
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `UPDATE instances SET name = ?, branch = ?, environment_id = ?, presets = ?, state = ?, updated_at = ? WHERE id = ?`,
		i.Name, i.Branch, i.EnvironmentID, string(presets), i.State, fmtTime(i.UpdatedAt), i.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("%w: instance %s", domain.ErrNotFound, i.ID)
	}
	return nil
}

func (s *Store) DeleteInstance(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM instances WHERE id = ?`, id)
	return err
}

// --- deploys -----------------------------------------------------------------

const deployColumns = `id, instance_id, number, trigger_kind, ref, sha, image_tag, image, status, message, log, platform_ids, requested_by, created_at, updated_at`

func scanDeploy(row interface{ Scan(...any) error }) (*domain.Deploy, error) {
	var d domain.Deploy
	var ids, created, updated string
	if err := row.Scan(&d.ID, &d.InstanceID, &d.Number, &d.Trigger, &d.Ref, &d.SHA, &d.ImageTag, &d.Image, &d.Status, &d.Message, &d.Log, &ids, &d.RequestedBy, &created, &updated); err != nil {
		return nil, err
	}
	d.PlatformDeploymentIDs = []string{}
	if err := json.Unmarshal([]byte(ids), &d.PlatformDeploymentIDs); err != nil {
		return nil, fmt.Errorf("decode platform ids: %w", err)
	}
	d.CreatedAt = parseTime(created)
	d.UpdatedAt = parseTime(updated)
	return &d, nil
}

func (s *Store) CreateDeploy(ctx context.Context, d *domain.Deploy) error {
	ids, err := json.Marshal(orEmptySlice(d.PlatformDeploymentIDs))
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO deploys (`+deployColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		d.ID, d.InstanceID, d.Number, d.Trigger, d.Ref, d.SHA, d.ImageTag, d.Image, d.Status, d.Message, d.Log, string(ids), d.RequestedBy, fmtTime(d.CreatedAt), fmtTime(d.UpdatedAt))
	return err
}

func orEmptySlice(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func (s *Store) UpdateDeploy(ctx context.Context, d *domain.Deploy) error {
	ids, err := json.Marshal(orEmptySlice(d.PlatformDeploymentIDs))
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `UPDATE deploys SET status = ?, message = ?, image = ?, log = ?, platform_ids = ?, updated_at = ? WHERE id = ?`,
		d.Status, d.Message, d.Image, d.Log, string(ids), fmtTime(d.UpdatedAt), d.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("%w: deploy %s", domain.ErrNotFound, d.ID)
	}
	return nil
}

func (s *Store) ListDeploys(ctx context.Context, instanceID string, limit int) ([]domain.Deploy, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+deployColumns+` FROM deploys WHERE instance_id = ? ORDER BY number DESC LIMIT ?`, instanceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Deploy{}
	for rows.Next() {
		d, err := scanDeploy(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}
