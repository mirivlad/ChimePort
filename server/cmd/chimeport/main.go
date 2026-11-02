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
	"strings"
	"syscall"
	"time"

	"github.com/mirivlad/ChimePort/server/internal/app"
)

func main() {
	dataDir := getenv("CHIMEPORT_DATA_DIR", "./data")
	command := "serve"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}
	switch command {
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
		server := &http.Server{Addr: getenv("CHIMEPORT_LISTEN_ADDR", ":8080"), Handler: api.Handler(), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 20 * time.Second, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 16 * 1024}
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
		defer stop()
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
