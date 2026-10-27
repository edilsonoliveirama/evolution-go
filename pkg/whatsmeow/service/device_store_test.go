package whatsmeow_service

import (
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/evolution-foundation/evolution-go/pkg/config"
)

// Every StartClient used to open a new device store (and *sql.DB pool) that
// was never closed, leaking idle Postgres connections on each reconnect.
func TestDeviceContainerIsShared(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "dbdata"), 0o755); err != nil {
		t.Fatal(err)
	}
	w := &whatsmeowService{config: &config.Config{}, exPath: dir, store: &sharedDeviceStore{}}

	first, err := w.deviceContainer()
	if err != nil {
		t.Fatalf("deviceContainer: %v", err)
	}
	t.Cleanup(func() { _ = first.Close() })

	// StartClient has a value receiver: the struct is copied on each call.
	copied := *w
	second, err := copied.deviceContainer()
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("each client start must reuse the same device store, not open a new pool")
	}
}
