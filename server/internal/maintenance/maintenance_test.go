package maintenance_test

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mirivlad/ChimePort/server/internal/app"
	"github.com/mirivlad/ChimePort/server/internal/maintenance"
)

func createStore(t *testing.T, dir string) *app.Store {
	t.Helper()
	store, err := app.OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.DB.Close() })
	return store
}

func TestConsistentOnlineBackupAndRestoreWithoutLosingIdentity(t *testing.T) {
	data := t.TempDir()
	store := createStore(t, data)
	_, err := store.DB.Exec("INSERT INTO instance(key,value) VALUES ('sample','before')")
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "daily.tar.gz")
	backup, err := maintenance.Backup(data, out)
	if err != nil {
		t.Fatal(err)
	}
	if backup.InstanceID != store.InstanceID || backup.SchemaVersion != 5 {
		t.Fatalf("unexpected backup manifest: %+v", backup)
	}
	_, err = store.DB.Exec("UPDATE instance SET value='after' WHERE key='sample'")
	if err != nil {
		t.Fatal(err)
	}
	verified, err := maintenance.Verify(out)
	if err != nil || verified.InstanceID != backup.InstanceID {
		t.Fatalf("backup verification: %v %+v", err, verified)
	}
	restored := t.TempDir()
	result, err := maintenance.Restore(out, restored, backup.InstanceID)
	if err != nil {
		t.Fatal(err)
	}
	if result.InstanceID != store.InstanceID {
		t.Fatalf("server identity changed: %s", result.InstanceID)
	}
	reopened := createStore(t, restored)
	if reopened.InstanceID != store.InstanceID {
		t.Fatal("restored instance identity changed")
	}
	var value string
	if err := reopened.DB.QueryRow("SELECT value FROM instance WHERE key='sample'").Scan(&value); err != nil {
		t.Fatal(err)
	}
	if value != "before" {
		t.Fatalf("not a consistent prior snapshot: %q", value)
	}
	key1, _ := os.ReadFile(filepath.Join(data, "token-key"))
	key2, _ := os.ReadFile(filepath.Join(restored, "token-key"))
	if string(key1) != string(key2) {
		t.Fatal("restore did not preserve token encryption key")
	}
	if _, err := maintenance.Restore(out, restored, backup.InstanceID); err == nil {
		t.Fatal("second restore must refuse to overwrite populated target")
	}
	if _, err := maintenance.Backup(data, out); err == nil {
		t.Fatal("backup must never overwrite existing archive")
	}
}

func TestRejectWrongInstanceAndNonEmptyVolume(t *testing.T) {
	data := t.TempDir()
	_ = createStore(t, data)
	out := filepath.Join(t.TempDir(), "safe.tar.gz")
	manifest, err := maintenance.Backup(data, out)
	if err != nil {
		t.Fatal(err)
	}
	empty := t.TempDir()
	if _, err := maintenance.Restore(out, empty, "wrong-instance"); err == nil {
		t.Fatal("unexpected restore with wrong expected instance")
	}
	items, err := os.ReadDir(empty)
	if err != nil || len(items) != 0 {
		t.Fatalf("failed restore must leave target empty: %d %v", len(items), err)
	}
	if _, err := maintenance.Restore(out, data, manifest.InstanceID); err == nil {
		t.Fatal("must refuse to overwrite active /data")
	}
	if _, err := maintenance.Backup(data, filepath.Join(data, "inside.tar.gz")); err == nil {
		t.Fatal("backup archive inside live data volume must be rejected")
	}
}

func TestRejectIncompleteAndMaliciousArchives(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "evil.tar.gz")
	file, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(file)
	tarball := tar.NewWriter(gz)
	content := []byte("malicious")
	if err := tarball.WriteHeader(&tar.Header{
		Name: "../outside", Mode: 0600, Size: int64(len(content)), Typeflag: tar.TypeReg,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarball.Write(content); err != nil {
		t.Fatal(err)
	}
	tarball.Close()
	gz.Close()
	file.Close()
	if _, err := maintenance.Verify(archive); err == nil || !strings.Contains(err.Error(), "unexpected archive member") {
		t.Fatalf("path traversal entry was accepted: %v", err)
	}
	empty := t.TempDir()
	if _, err := maintenance.Restore(archive, empty, "some-id"); err == nil {
		t.Fatal("malicious archive was accepted")
	}
	if _, err := os.Stat(filepath.Join(empty, "outside")); !os.IsNotExist(err) {
		t.Fatal("archive created an unexpected file")
	}
}

func TestRejectMissingOrWrongEncryptionKey(t *testing.T) {
	data := t.TempDir()
	_ = createStore(t, data)
	keyPath := filepath.Join(data, "token-key")
	if err := os.WriteFile(keyPath, []byte("not a real AES key"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := maintenance.Backup(data, filepath.Join(t.TempDir(), "bad.tar.gz")); err == nil {
		t.Fatal("invalid encryption key must cause backup failure")
	}
}

func TestRefuseStartupOnInterruptedRestore(t *testing.T) {
	data := t.TempDir()
	if err := os.WriteFile(filepath.Join(data, ".restore-in-progress"), []byte("incomplete\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := app.OpenStore(data); err == nil {
		t.Fatal("server must not start with incomplete restoration marker")
	}
}

func TestRejectBackupThroughSymlinkIntoLiveVolume(t *testing.T) {
	data := t.TempDir()
	_ = createStore(t, data)
	inside := filepath.Join(data, "inside")
	if err := os.Mkdir(inside, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "out")
	if err := os.Symlink(inside, link); err != nil {
		t.Fatal(err)
	}
	if _, err := maintenance.Backup(data, filepath.Join(link, "bad.tar.gz")); err == nil {
		t.Fatal("backup output symlinked into source volume was accepted")
	}
}
