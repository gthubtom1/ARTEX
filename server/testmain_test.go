package server

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Autumn-27/artex/db"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestMain acquires a PostgreSQL advisory lock (7337741002) for the entire
// server test suite so cross-package DELETE cleanup races with db/agent
// packages are avoided when running `go test ./...`.
func TestMain(m *testing.M) {
	dsn, _, err := db.DSN()
	if err != nil {
		os.Exit(m.Run())
	}
	conn, err := sql.Open("pgx", dsn)
	if err != nil || conn.Ping() != nil {
		os.Exit(m.Run())
	}
	defer conn.Close()
	if _, err := conn.Exec(`SELECT pg_advisory_lock(7337741002)`); err != nil {
		os.Exit(m.Run())
	}
	defer conn.Exec(`SELECT pg_advisory_unlock(7337741002)`) //nolint:errcheck
	os.Exit(m.Run())
}

// newTestServer gives every test its own server lifetime. Cancelling at cleanup
// stops the scheduler, notifier, archive worker and side-question writer before
// t.TempDir removes their directories; without it a leftover goroutine can
// recreate a file after the test ends and make TempDir cleanup fail on Windows.
// seedToolCache prevents every test server from launching a full PATH/tool
// discovery scan. Besides being slow, that scan produces millions of os.Stat
// entries in the test log, which makes `go test` spend minutes trying to hash
// the result for its cache (and can appear to hang on Windows). An empty cached
// list is sufficient: these tests exercise HTTP wiring, not tool discovery.
func seedToolCache(t *testing.T, dataDir string) {
	t.Helper()
	dir := filepath.Join(dataDir, "cache")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tools_cache.json"), []byte("[]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func newTestServer(t *testing.T, m *Manager, skillDir, dataDir, keyDir string) *Server {
	t.Helper()
	seedToolCache(t, dataDir)
	ctx, cancel := context.WithCancel(context.Background())
	// Start already cancelled: these route/HTTP tests do not need the scheduler,
	// notifier or restored task engines. Keeping those loops idle also prevents a
	// residual task in the shared dev DB from blocking startup on a network probe.
	cancel()
	t.Cleanup(func() {
		cancel()
		time.Sleep(100 * time.Millisecond)
	})
	return New(ctx, m, skillDir, dataDir, keyDir)
}
