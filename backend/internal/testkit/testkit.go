// Package testkit gives integration tests a real PostGIS database.
//
// One container per test binary (testcontainers, image TODAYZ_POSTGIS_IMAGE,
// default the digest-pinned PostGIS 16-3.4 build the local stack uses), or,
// when TODAYZ_TEST_PG_URL_FILE names a file holding a superuser URL, one
// shared server for every test binary (scripts/testpg.sh up starts it). The
// shared server saves a container per package, which is what keeps several
// worktrees testing at once inside the Docker VM's memory. Each binary still
// migrates its own template (worktrees carry different migrations) and every
// test still gets its own fresh database.
//
// The container mirrors the database role contract of the local stack
// (roles.sql, database.sql): a superuser creates the extensions and two
// roles, todayz_owner (owns the schema, runs migrations) and todayz_app
// (api, worker, seed: DML only, through default privileges). The first NewDB
// migrates a template database once, as todayz_owner; every NewDB after that
// is a fresh CREATE DATABASE … TEMPLATE copy, dropped when the test ends.
// DB.Pool connects as todayz_app, the role the services use; DB.OwnerPool
// connects as todayz_owner for the tests that need it.
//
// Packages with integration tests add
//
//	func TestMain(m *testing.M) { testkit.Main(m) }
//
// so the container is removed when the binary exits (Ryuk is the backstop).
// `go test -short` skips every test that calls NewDB.
package testkit

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	_ "embed" // role SQL
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpg "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/todayz/backend/internal/platform/migrate"
)

// Pinned PostGIS 16-3.4 images: the same refs as infra/local/lib.sh (D-86;
// digests in infra/local/README.md). postgis/postgis is amd64 only; arm64 uses
// the same docker-postgis build from imresamu/postgis. TODAYZ_POSTGIS_IMAGE
// (repo:tag or repo:tag@sha256:digest) overrides both.
const (
	ImageAMD64 = "postgis/postgis:16-3.4@sha256:44126d872ac91993766c341e369c539e8196614321765d36a6f1bab0419a5fa5"
	ImageARM64 = "imresamu/postgis:16-3.4@sha256:6da75969915039b7356058b4310d43fde88c275ada982c2dfee29da68445ff4d"
)

// DefaultImage is the PostGIS image used when TODAYZ_POSTGIS_IMAGE is unset.
func DefaultImage() string {
	if runtime.GOARCH == "arm64" {
		return ImageARM64
	}
	return ImageAMD64
}

// Role names (the local stack uses the same).
const (
	OwnerRole = "todayz_owner"
	AppRole   = "todayz_app"
)

// SharedURLFileEnv names the file holding the shared server's superuser URL.
const SharedURLFileEnv = "TODAYZ_TEST_PG_URL_FILE"

// Label marks every container testkit starts.
const Label = "in.todayz.testkit"

var (
	//go:embed roles.sql
	rolesSQL string
	//go:embed database.sql
	databaseSQL string
)

var (
	startOnce sync.Once
	startErr  error
	container *tcpg.PostgresContainer
	adminURL  string // superuser, postgres database
	ownerPW   string
	appPW     string

	tmplOnce sync.Once
	tmplErr  error

	// procTag keeps this binary's databases apart on a shared server.
	procTag    = randHex(4)
	shared     bool
	ownServer  bool
	templateDB = "tz_template"
	tmplMade   bool

	createMu sync.Mutex
	seq      atomic.Int64
)

// Image returns the PostGIS image to run.
func Image() string {
	if v := strings.TrimSpace(os.Getenv("TODAYZ_POSTGIS_IMAGE")); v != "" {
		return v
	}
	return DefaultImage()
}

// Main runs the tests and removes the container (or, on a shared server,
// this binary's template) afterwards.
func Main(m *testing.M) {
	code := m.Run()
	if container != nil {
		_ = testcontainers.TerminateContainer(container)
	}
	if shared && tmplMade {
		ctx := context.Background()
		_ = adminExec(ctx, "postgres", "ALTER DATABASE "+templateDB+" WITH IS_TEMPLATE false")
		_ = adminExec(ctx, "postgres", "DROP DATABASE IF EXISTS "+templateDB+" WITH (FORCE)")
	}
	os.Exit(code)
}

// MainOwnServer is Main for a package that must not share a server even when
// TODAYZ_TEST_PG_URL_FILE is set: transaction ids and snapshots are
// cluster-wide, so another package's open transaction would hold back what
// this one measures (realtime's xmin cursors).
func MainOwnServer(m *testing.M) {
	ownServer = true
	Main(m)
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func start() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if f := strings.TrimSpace(os.Getenv(SharedURLFileEnv)); f != "" && !ownServer {
		startShared(ctx, f)
		return
	}
	// Per-run random passwords: nothing secret-looking lives in the repo.
	pw := randHex(16)
	ownerPW, appPW = randHex(16), randHex(16)
	c, err := tcpg.Run(ctx, Image(),
		tcpg.WithDatabase("postgres"),
		tcpg.WithUsername("postgres"),
		tcpg.WithPassword(pw),
		tcpg.BasicWaitStrategies(),
		testcontainers.WithLabels(map[string]string{Label: "1"}),
		testcontainers.WithTmpfs(map[string]string{"/var/lib/postgresql/data": "rw"}),
		testcontainers.WithCmdArgs("-c", "fsync=off", "-c", "synchronous_commit=off", "-c", "full_page_writes=off", "-c", "max_connections=300", "-c", "shared_preload_libraries=pg_stat_statements"),
	)
	if err != nil {
		startErr = fmt.Errorf("testkit: start %s (is Docker running? colima start): %w", Image(), err)
		if c != nil {
			_ = testcontainers.TerminateContainer(c)
		}
		return
	}
	container = c
	if adminURL, startErr = c.ConnectionString(ctx, "sslmode=disable"); startErr != nil {
		return
	}
	startErr = adminExec(ctx, "postgres", rolesSQL+
		fmt.Sprintf("\nALTER ROLE %s PASSWORD '%s';\nALTER ROLE %s PASSWORD '%s';", OwnerRole, ownerPW, AppRole, appPW))
	if startErr != nil {
		startErr = fmt.Errorf("testkit: roles: %w", startErr)
	}
}

// startShared uses the server whose superuser URL is in file. The role
// passwords derive from that URL, so every binary sets the same ones; the
// advisory lock keeps concurrent binaries from racing on CREATE ROLE.
func startShared(ctx context.Context, file string) {
	b, err := os.ReadFile(file) // #nosec G304 G703 -- a path the developer sets for tests
	if err != nil {
		startErr = fmt.Errorf("testkit: %s: %w (scripts/testpg.sh up)", SharedURLFileEnv, err)
		return
	}
	shared = true
	adminURL = strings.TrimSpace(string(b))
	templateDB = "tz_template_" + procTag
	ownerPW, appPW = derivePW(adminURL, OwnerRole), derivePW(adminURL, AppRole)
	startErr = adminExec(ctx, "postgres", "SELECT pg_advisory_xact_lock(7307);\n"+rolesSQL+
		fmt.Sprintf("\nALTER ROLE %s PASSWORD '%s';\nALTER ROLE %s PASSWORD '%s';", OwnerRole, ownerPW, AppRole, appPW))
	if startErr != nil {
		startErr = fmt.Errorf("testkit: shared server roles (scripts/testpg.sh up): %w", startErr)
	}
}

func derivePW(seed, role string) string {
	sum := sha256.Sum256([]byte(seed + "|" + role))
	return hex.EncodeToString(sum[:16])
}

// urlFor returns a URL for dbName as user (empty user: the superuser).
func urlFor(dbName, user string) string {
	u, err := url.Parse(adminURL)
	if err != nil {
		panic(err)
	}
	u.Path = "/" + dbName
	switch user {
	case OwnerRole:
		u.User = url.UserPassword(OwnerRole, ownerPW)
	case AppRole:
		u.User = url.UserPassword(AppRole, appPW)
	}
	return u.String()
}

func adminExec(ctx context.Context, dbName, sql string) error {
	conn, err := pgx.Connect(ctx, urlFor(dbName, ""))
	if err != nil {
		return err
	}
	defer conn.Close(context.WithoutCancel(ctx))
	_, err = conn.Exec(ctx, sql)
	return err
}

// createPrepared creates dbName owned by todayz_owner (from tmpl when set)
// and, for a fresh database, runs database.sql in it.
func createPrepared(ctx context.Context, dbName, tmpl string) error {
	createMu.Lock()
	err := adminExec(ctx, "postgres", "CREATE DATABASE "+dbName+" OWNER "+OwnerRole+tmpl)
	createMu.Unlock()
	if err != nil {
		return err
	}
	if tmpl != "" {
		// A template copy keeps the schema and its privileges but not the
		// database ACL, so the TEMPORARY revoke from database.sql is repeated.
		return adminExec(ctx, "postgres", "REVOKE TEMPORARY ON DATABASE "+dbName+" FROM PUBLIC")
	}
	return adminExec(ctx, dbName, databaseSQL)
}

func buildTemplate() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := createPrepared(ctx, templateDB, ""); err != nil {
		tmplErr = fmt.Errorf("testkit: template: %w", err)
		return
	}
	tmplMade = true
	pool, err := pgxpool.New(ctx, urlFor(templateDB, OwnerRole))
	if err != nil {
		tmplErr = err
		return
	}
	defer pool.Close()
	m, err := migrate.New(pool, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		tmplErr = err
		return
	}
	defer func() { _ = m.Close() }()
	if _, err := m.Up(ctx); err != nil {
		tmplErr = fmt.Errorf("testkit: migrate template: %w", err)
		return
	}
	// Like `todayz migrate up` with APP_ENV=test (dbenv.Record).
	if _, err := pool.Exec(ctx, `INSERT INTO platform_environment (id, env) VALUES (true, 'test')`); err != nil {
		tmplErr = fmt.Errorf("testkit: platform_environment: %w", err)
		return
	}
	pool.Close()
	tmplErr = adminExec(ctx, "postgres", "ALTER DATABASE "+templateDB+" WITH IS_TEMPLATE true")
}

// DB is one test's database.
type DB struct {
	// Pool connects as todayz_app: DML only, like the api and worker.
	Pool *pgxpool.Pool
	URL  string
	// OwnerPool connects as todayz_owner, the schema owner migrations run as.
	OwnerPool *pgxpool.Pool
	OwnerURL  string
	Name      string
}

func requireContainer(t testing.TB) {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test: skipped with -short")
	}
	startOnce.Do(start)
	if startErr != nil {
		t.Fatal(startErr)
	}
}

// NewDB returns a fresh, fully migrated database (settings seeded) for t.
func NewDB(t testing.TB) *DB {
	t.Helper()
	requireContainer(t)
	tmplOnce.Do(buildTemplate)
	if tmplErr != nil {
		t.Fatal(tmplErr)
	}
	return create(t, " TEMPLATE "+templateDB)
}

// NewEmptyDB returns a fresh database with the roles, extensions and default
// privileges in place and nothing migrated (for migration tests).
func NewEmptyDB(t testing.TB) *DB {
	t.Helper()
	requireContainer(t)
	return create(t, "")
}

func create(t testing.TB, tmpl string) *DB {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	name := fmt.Sprintf("tz_%s_t%d", procTag, seq.Add(1))
	if err := createPrepared(ctx, name, tmpl); err != nil {
		t.Fatalf("testkit: create database: %v", err)
	}
	d := &DB{Name: name, URL: urlFor(name, AppRole), OwnerURL: urlFor(name, OwnerRole)}
	var err error
	if d.Pool, err = pgxpool.New(ctx, d.URL); err != nil {
		t.Fatalf("testkit: connect: %v", err)
	}
	if d.OwnerPool, err = pgxpool.New(ctx, d.OwnerURL); err != nil {
		t.Fatalf("testkit: connect owner: %v", err)
	}
	t.Cleanup(func() {
		d.Pool.Close()
		d.OwnerPool.Close()
		_ = adminExec(context.Background(), "postgres", "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
	})
	return d
}

// AdminExec runs sql as the superuser in this database (for tests that must
// break the setup on purpose, such as dropping an extension).
func (d *DB) AdminExec(ctx context.Context, sql string) error {
	return adminExec(ctx, d.Name, sql)
}
