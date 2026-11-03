// Package maintenance manages consistent ChimePort backups and safe restores.
// Restore is intentionally restricted to an EMPTY data directory, so switching
// a Docker bind mount never overwrites the previous installation.
package maintenance

import (
	"archive/tar"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const (
	FormatVersion          = 1
	SupportedSchema        = 2
	maxDatabaseBytes int64 = 64 << 30
	maxManifestBytes int64 = 64 << 10
)

type FileInfo struct {
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type Manifest struct {
	Format        int                 `json:"format"`
	CreatedAt     string              `json:"created_at"`
	InstanceID    string              `json:"instance_id"`
	SchemaVersion int                 `json:"schema_version"`
	ImageRef      string              `json:"image_ref,omitempty"`
	Files         map[string]FileInfo `json:"files"`
}

func ensureRegular(path string) error {
	fi, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("%s must be a regular file", path)
	}
	return nil
}

func hashFile(path string) (FileInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return FileInfo{}, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return FileInfo{}, err
	}
	return FileInfo{Size: n, SHA256: hex.EncodeToString(h.Sum(nil))}, nil
}

func openDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err = db.Exec("PRAGMA busy_timeout=5000"); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func verifyDatabase(path string, key []byte) (string, int, error) {
	if len(key) != 32 {
		return "", 0, errors.New("token-key must be 32 bytes")
	}
	if err := ensureRegular(path); err != nil {
		return "", 0, err
	}
	db, err := openDB(path)
	if err != nil {
		return "", 0, err
	}
	defer db.Close()
	var result string
	if err = db.QueryRow("PRAGMA integrity_check").Scan(&result); err != nil {
		return "", 0, fmt.Errorf("SQLite integrity check: %w", err)
	}
	if result != "ok" {
		return "", 0, fmt.Errorf("SQLite integrity check: %s", result)
	}
	rows, err := db.Query("PRAGMA foreign_key_check")
	if err != nil {
		return "", 0, err
	}
	hasFKError := rows.Next()
	rowError := rows.Err()
	rows.Close()
	if rowError != nil {
		return "", 0, rowError
	}
	if hasFKError {
		return "", 0, errors.New("SQLite foreign key check failed")
	}
	var version int
	if err = db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return "", 0, err
	}
	if version < 1 || version > SupportedSchema {
		return "", 0, fmt.Errorf("unsupported schema version %d; supported range 1-%d", version, SupportedSchema)
	}
	var instanceID string
	if err = db.QueryRow("SELECT value FROM instance WHERE key='instance_id'").Scan(&instanceID); err != nil {
		return "", 0, fmt.Errorf("missing stable instance identity: %w", err)
	}
	if instanceID == "" {
		return "", 0, errors.New("empty instance ID")
	}
	if version == 2 {
		var ciphertext string
		err = db.QueryRow("SELECT secret_encrypted FROM source_tokens WHERE secret_encrypted<>'' LIMIT 1").Scan(&ciphertext)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return "", 0, fmt.Errorf("token key compatibility check: %w", err)
		}
		if err == nil {
			raw, decodeErr := base64.RawURLEncoding.DecodeString(ciphertext)
			if decodeErr != nil {
				return "", 0, errors.New("encrypted token cannot be decoded with backup key")
			}
			block, cipherErr := aes.NewCipher(key)
			if cipherErr != nil {
				return "", 0, cipherErr
			}
			gcm, cipherErr := cipher.NewGCM(block)
			if cipherErr != nil {
				return "", 0, cipherErr
			}
			if len(raw) < gcm.NonceSize() {
				return "", 0, errors.New("encrypted token is truncated")
			}
			if _, cipherErr = gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], nil); cipherErr != nil {
				return "", 0, errors.New("token-key does not decrypt stored tokens")
			}
		}
	}
	return instanceID, version, nil
}

func copyFile(dst, src string) error {
	input, err := os.Open(src)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	syncErr := output.Sync()
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}

func writeMember(tw *tar.Writer, name, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		return err
	}
	if !stat.Mode().IsRegular() {
		return fmt.Errorf("non-regular archive input %s", name)
	}
	if err = tw.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: stat.Size(), Typeflag: tar.TypeReg}); err != nil {
		return err
	}
	_, err = io.Copy(tw, f)
	return err
}

func syncDirectory(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

func within(parent, candidate string) bool {
	a, errA := filepath.Abs(parent)
	b, errB := filepath.Abs(candidate)
	if errA != nil || errB != nil {
		return true
	}
	if resolved, err := filepath.EvalSymlinks(a); err == nil {
		a = resolved
	}
	if resolved, err := filepath.EvalSymlinks(filepath.Dir(b)); err == nil {
		b = filepath.Join(resolved, filepath.Base(b))
	}
	rel, err := filepath.Rel(a, b)
	return err != nil || rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// Backup uses SQLite VACUUM INTO, which creates a transactionally consistent
// snapshot of a live WAL database without copying transient -wal/-shm files.
// The token encryption key is checked before AND after the snapshot.
func Backup(dataDir, output string) (Manifest, error) {
	var empty Manifest
	if err := ensureRegular(filepath.Join(dataDir, "chimeport.db")); err != nil {
		return empty, fmt.Errorf("source database: %w", err)
	}
	keyFile := filepath.Join(dataDir, "token-key")
	if err := ensureRegular(keyFile); err != nil {
		return empty, fmt.Errorf("source encryption key: %w", err)
	}
	if within(dataDir, output) {
		return empty, errors.New("backup destination must be outside the live /data volume")
	}
	outputDir := filepath.Dir(output)
	if fi, err := os.Stat(outputDir); err != nil || !fi.IsDir() {
		return empty, errors.New("backup output directory must exist")
	}
	if _, err := os.Lstat(output); err == nil {
		return empty, errors.New("backup already exists; refusing to overwrite it")
	} else if !errors.Is(err, os.ErrNotExist) {
		return empty, err
	}
	keyBefore, err := os.ReadFile(keyFile)
	if err != nil {
		return empty, err
	}
	if len(keyBefore) != 32 {
		return empty, errors.New("invalid token-key length")
	}
	workDir, err := os.MkdirTemp(outputDir, ".chimeport-backup-work-*")
	if err != nil {
		return empty, err
	}
	defer os.RemoveAll(workDir)
	snapshot := filepath.Join(workDir, "chimeport.db")
	db, err := openDB(filepath.Join(dataDir, "chimeport.db"))
	if err != nil {
		return empty, err
	}
	_, snapshotErr := db.Exec("VACUUM main INTO ?", snapshot)
	closeErr := db.Close()
	if snapshotErr != nil {
		return empty, fmt.Errorf("SQLite online snapshot: %w", snapshotErr)
	}
	if closeErr != nil {
		return empty, closeErr
	}
	if err = ensureRegular(snapshot); err != nil {
		return empty, err
	}
	snapshotFile, err := os.Open(snapshot)
	if err != nil {
		return empty, err
	}
	if err = snapshotFile.Sync(); err != nil {
		snapshotFile.Close()
		return empty, err
	}
	snapshotFile.Close()
	keyAfter, err := os.ReadFile(keyFile)
	if err != nil {
		return empty, err
	}
	if len(keyAfter) != 32 || string(keyBefore) != string(keyAfter) {
		return empty, errors.New("token-key changed during backup; retry after stopping the service")
	}
	keySnapshot := filepath.Join(workDir, "token-key")
	if err := os.WriteFile(keySnapshot, keyBefore, 0600); err != nil {
		return empty, err
	}
	id, schema, err := verifyDatabase(snapshot, keyBefore)
	if err != nil {
		return empty, fmt.Errorf("snapshot validation: %w", err)
	}
	dbInfo, err := hashFile(snapshot)
	if err != nil {
		return empty, err
	}
	if dbInfo.Size > maxDatabaseBytes {
		return empty, errors.New("database exceeds configured archive size limit")
	}
	keyInfo, err := hashFile(keySnapshot)
	if err != nil {
		return empty, err
	}
	manifest := Manifest{
		Format:        FormatVersion,
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		InstanceID:    id,
		SchemaVersion: schema,
		ImageRef:      os.Getenv("CHIMEPORT_IMAGE_REF"),
		Files: map[string]FileInfo{
			"chimeport.db": dbInfo,
			"token-key":    keyInfo,
		},
	}
	info, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return empty, err
	}
	info = append(info, byte(10))
	temp, err := os.CreateTemp(outputDir, ".chimeport-backup-*.tmp")
	if err != nil {
		return empty, err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if err = temp.Chmod(0600); err != nil {
		temp.Close()
		return empty, err
	}
	gz := gzip.NewWriter(temp)
	tw := tar.NewWriter(gz)
	err = tw.WriteHeader(&tar.Header{Name: "manifest.json", Mode: 0600, Size: int64(len(info)), Typeflag: tar.TypeReg})
	if err == nil {
		_, err = tw.Write(info)
	}
	if err == nil {
		err = writeMember(tw, "chimeport.db", snapshot)
	}
	if err == nil {
		err = writeMember(tw, "token-key", keySnapshot)
	}
	closeTar := tw.Close()
	closeGzip := gz.Close()
	syncErr := temp.Sync()
	closeTemp := temp.Close()
	for _, failure := range []error{err, closeTar, closeGzip, syncErr, closeTemp} {
		if failure != nil {
			return empty, fmt.Errorf("write backup archive: %w", failure)
		}
	}
	// Verify the finished archive before advertising it as a usable backup.
	if _, err = Verify(tempName); err != nil {
		return empty, fmt.Errorf("self-check of backup: %w", err)
	}
	// Protect against a concurrent operator creating the same destination.
	if _, err := os.Lstat(output); err == nil {
		return empty, errors.New("backup destination appeared during creation")
	}
	if err = os.Link(tempName, output); err != nil {
		return empty, fmt.Errorf("atomically publish backup without overwrite: %w", err)
	}
	if err = syncDirectory(outputDir); err != nil {
		return empty, err
	}
	return manifest, nil
}

func readArchive(archivePath, workDir string) (Manifest, error) {
	var empty Manifest
	file, err := os.Open(archivePath)
	if err != nil {
		return empty, err
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return empty, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	seen := map[string]bool{}
	var manifest Manifest
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return empty, err
		}
		if h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeRegA {
			return empty, errors.New("backup contains a non-regular entry")
		}
		if h.Name != "manifest.json" && h.Name != "chimeport.db" && h.Name != "token-key" {
			return empty, fmt.Errorf("unexpected archive member %q", h.Name)
		}
		if seen[h.Name] {
			return empty, fmt.Errorf("duplicate archive member %q", h.Name)
		}
		seen[h.Name] = true
		sizeLimit := maxDatabaseBytes
		if h.Name == "manifest.json" {
			sizeLimit = maxManifestBytes
		} else if h.Name == "token-key" {
			sizeLimit = 32
		}
		if h.Size <= 0 || h.Size > sizeLimit {
			return empty, fmt.Errorf("invalid %s member size", h.Name)
		}
		out, err := os.OpenFile(filepath.Join(workDir, h.Name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return empty, err
		}
		n, copyErr := io.CopyN(out, tr, h.Size)
		closeErr := out.Close()
		if copyErr != nil || closeErr != nil || n != h.Size {
			return empty, fmt.Errorf("extract archive member %s: %v %v", h.Name, copyErr, closeErr)
		}
	}
	if len(seen) != 3 || !seen["manifest.json"] || !seen["chimeport.db"] || !seen["token-key"] {
		return empty, errors.New("backup is missing required members")
	}
	raw, err := os.ReadFile(filepath.Join(workDir, "manifest.json"))
	if err != nil {
		return empty, err
	}
	if err = json.Unmarshal(raw, &manifest); err != nil {
		return empty, fmt.Errorf("invalid backup manifest: %w", err)
	}
	if manifest.Format != FormatVersion {
		return empty, fmt.Errorf("unsupported backup format %d", manifest.Format)
	}
	if manifest.SchemaVersion < 1 || manifest.SchemaVersion > SupportedSchema {
		return empty, fmt.Errorf("backup schema %d is unsupported", manifest.SchemaVersion)
	}
	if len(manifest.Files) != 2 {
		return empty, errors.New("backup manifest must contain exactly database and token key")
	}
	for _, name := range []string{"chimeport.db", "token-key"} {
		expected, ok := manifest.Files[name]
		if !ok {
			return empty, fmt.Errorf("missing manifest entry %s", name)
		}
		actual, err := hashFile(filepath.Join(workDir, name))
		if err != nil {
			return empty, err
		}
		if expected != actual {
			return empty, fmt.Errorf("%s checksum or size mismatch", name)
		}
	}
	key, err := os.ReadFile(filepath.Join(workDir, "token-key"))
	if err != nil {
		return empty, err
	}
	id, schema, err := verifyDatabase(filepath.Join(workDir, "chimeport.db"), key)
	if err != nil {
		return empty, err
	}
	if manifest.InstanceID != id || manifest.SchemaVersion != schema {
		return empty, errors.New("backup manifest does not match SQLite identity or schema")
	}
	return manifest, nil
}

// Verify checks archived SHA-256 digests, SQLite integrity and foreign keys,
// instance ID and whether the key decrypts a stored encrypted API token.
func Verify(archivePath string) (Manifest, error) {
	var empty Manifest
	workDir, err := os.MkdirTemp("", "chimeport-verify-*")
	if err != nil {
		return empty, err
	}
	defer os.RemoveAll(workDir)
	return readArchive(archivePath, workDir)
}

// Restore NEVER overwrites a populated volume. Restore to a fresh host bind
// directory, then switch CHIMEPORT_DATA_MOUNT in Portainer after stopping the
// old container. The old named volume remains an instant rollback option.
func Restore(archivePath, emptyDataDir, confirmedInstanceID string) (Manifest, error) {
	var empty Manifest
	if confirmedInstanceID == "" {
		return empty, errors.New("--expect-instance is required; run verify-backup first")
	}
	stat, err := os.Lstat(emptyDataDir)
	if err != nil {
		return empty, fmt.Errorf("restore target must exist: %w", err)
	}
	if !stat.IsDir() || stat.Mode()&os.ModeSymlink != 0 {
		return empty, errors.New("restore target must be an ordinary directory")
	}
	members, err := os.ReadDir(emptyDataDir)
	if err != nil {
		return empty, err
	}
	if len(members) != 0 {
		return empty, errors.New("restore target is not EMPTY; use a new directory and preserve old data")
	}
	workDir, err := os.MkdirTemp(emptyDataDir, ".restore-stage-*")
	if err != nil {
		return empty, err
	}
	defer os.RemoveAll(workDir)
	manifest, err := readArchive(archivePath, workDir)
	if err != nil {
		return empty, err
	}
	if manifest.InstanceID != confirmedInstanceID {
		return empty, fmt.Errorf("instance mismatch: archive belongs to %s", manifest.InstanceID)
	}
	marker := filepath.Join(emptyDataDir, ".restore-in-progress")
	if err := os.WriteFile(marker, []byte(manifest.InstanceID+string(byte(10))), 0600); err != nil {
		return empty, err
	}
	// If interrupted, the marker forces OpenStore to fail rather than use a
	// half-restored DB and token key. A fresh directory is the safe retry.
	for _, name := range []string{"chimeport.db", "token-key"} {
		if err := os.Rename(filepath.Join(workDir, name), filepath.Join(emptyDataDir, name)); err != nil {
			return empty, fmt.Errorf("restore %s: %w (target left guarded)", name, err)
		}
	}
	if err := syncDirectory(emptyDataDir); err != nil {
		return empty, err
	}
	if err := os.Remove(marker); err != nil {
		return empty, err
	}
	if err := syncDirectory(emptyDataDir); err != nil {
		return empty, err
	}
	return manifest, nil
}
