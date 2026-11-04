package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/mirivlad/ChimePort/server/internal/app"
	"github.com/mirivlad/ChimePort/server/internal/maintenance"
)

func main() {
	dataDir := getenv("CHIMEPORT_DATA_DIR", "./data")
	command := "serve"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}
	switch command {
	case "maintenance":
		if len(os.Args) < 3 {
			log.Fatal("usage: chimeport maintenance [backup|verify|restore] [flags]")
		}
		switch os.Args[2] {
		case "backup":
			f := flag.NewFlagSet("maintenance backup", flag.ExitOnError)
			output := f.String("output", "", "New backup archive path, outside /data")
			directory := f.String("data-dir", dataDir, "Source SQLite volume")
			_ = f.Parse(os.Args[3:])
			if *output == "" || f.NArg() != 0 {
				log.Fatal("usage: chimeport maintenance backup --output /backups/name.tar.gz [--data-dir /data]")
			}
			manifest, err := maintenance.Backup(*directory, *output)
			if err != nil {
				log.Fatal(err)
			}
			fmt.Printf("Backup verified: %s (instance=%s schema=%d)\n", *output, manifest.InstanceID, manifest.SchemaVersion)
		case "verify":
			f := flag.NewFlagSet("maintenance verify", flag.ExitOnError)
			archive := f.String("archive", "", "Backup archive path")
			_ = f.Parse(os.Args[3:])
			if *archive == "" || f.NArg() != 0 {
				log.Fatal("usage: chimeport maintenance verify --archive /backups/name.tar.gz")
			}
			manifest, err := maintenance.Verify(*archive)
			if err != nil {
				log.Fatal(err)
			}
			fmt.Printf("Backup valid: instance=%s schema=%d created=%s image=%s\n",
				manifest.InstanceID, manifest.SchemaVersion, manifest.CreatedAt, manifest.ImageRef)
		case "restore":
			f := flag.NewFlagSet("maintenance restore", flag.ExitOnError)
			archive := f.String("archive", "", "Backup archive path")
			directory := f.String("data-dir", dataDir, "EMPTY destination directory")
			expected := f.String("expect-instance", "", "Instance ID printed by verify; required")
			_ = f.Parse(os.Args[3:])
			if *archive == "" || *expected == "" || f.NArg() != 0 {
				log.Fatal("usage: chimeport maintenance restore --archive /backups/name.tar.gz --data-dir /data --expect-instance ID")
			}
			manifest, err := maintenance.Restore(*archive, *directory, *expected)
			if err != nil {
				log.Fatal(err)
			}
			fmt.Printf("Restored into EMPTY directory: instance=%s schema=%d\n", manifest.InstanceID, manifest.SchemaVersion)
		default:
			log.Fatalf("unknown maintenance subcommand %q", os.Args[2])
		}
	case "bootstrap":
		f := flag.NewFlagSet("bootstrap", flag.ExitOnError)
		username := f.String("username", "admin", "First administrator username")
		passwordFile := f.String("password-file", "", "Path to file containing initial password")
		_ = f.Parse(os.Args[2:])
		if *passwordFile == "" {
			log.Fatal("--password-file is required; do not put passwords on the command line")
		}
		password, err := os.ReadFile(*passwordFile)
		if err != nil {
			log.Fatal(err)
		}
		store, err := app.OpenStore(dataDir)
		if err != nil {
			log.Fatal(err)
		}
		defer store.DB.Close()
		if err := store.Bootstrap(*username, strings.TrimRight(string(password), "\r\n")); err != nil {
			log.Fatal(err)
		}
		fmt.Println("Administrator created. Bootstrap is now disabled.")
	case "serve":
		if len(os.Args) > 2 {
			log.Fatal("usage: chimeport [serve|bootstrap]")
		}
		store, err := app.OpenStore(dataDir)
		if err != nil {
			log.Fatal(err)
		}
		defer store.DB.Close()
		publicURL := os.Getenv("CHIMEPORT_PUBLIC_URL")
		if publicURL == "" {
			log.Fatal("CHIMEPORT_PUBLIC_URL is required (HTTPS origin)")
		}
		api, err := app.New(store, publicURL)
		if err != nil {
			log.Fatal(err)
		}
		api.PublishRatePerMinute = nonNegativeEnv("CHIMEPORT_PUBLISH_RATE_PER_MINUTE", 120, 100000)
		retentionDays := nonNegativeEnv("CHIMEPORT_RETENTION_DAYS", 30, 36500)
		server := &http.Server{Addr: getenv("CHIMEPORT_LISTEN_ADDR", ":8080"), Handler: api.Handler(), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 20 * time.Second, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 16 * 1024}
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
		defer stop()
		if retentionDays > 0 {
			go retentionWorker(ctx, store, retentionDays)
		}
		go func() {
			<-ctx.Done()
			stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = server.Shutdown(stopCtx)
		}()
		slog.Info("ChimePort starting", "address", server.Addr, "instance_id", store.InstanceID)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	case "healthcheck":
		client := http.Client{Timeout: 3 * time.Second}
		resp, err := client.Get("http://127.0.0.1:8080/health/live")
		if err != nil {
			log.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			os.Exit(1)
		}
	default:
		log.Fatalf("unknown command %q", command)
	}
}
func getenv(key, otherwise string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return otherwise
}

// nonNegativeEnv rejects bad configuration rather than silently disabling safeguards.
func nonNegativeEnv(name string, fallback, max int) int {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 || n > max {
		log.Fatalf("%s must be an integer from 0 to %d", name, max)
	}
	return n
}

// Small transactions allow API writes between cleanup batches. The cleanup also
// runs at startup, without delaying readiness or requiring another container.
func retentionWorker(ctx context.Context, store *app.Store, days int) {
	run := func() {
		total := 0
		for {
			if ctx.Err() != nil {
				return
			}
			n, err := store.PruneExpired(ctx, days, 200)
			if err != nil {
				if ctx.Err() == nil {
					slog.Error("Retention cleanup failed", "error", err)
				}
				return
			}
			total += n
			if n < 200 {
				break
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(20 * time.Millisecond):
			}
		}
		if total > 0 {
			slog.Info("Expired messages pruned", "deleted", total, "retention_days", days)
		}
	}
	run()
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}
