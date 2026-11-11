// Command sikat adalah binary tunggal aplikasi SIKAT.
//
//	sikat serve                 menjalankan server HTTP
//	sikat migrate up|down|status mengelola migrasi database
//	sikat create-admin          membuat akun Admin IT pertama (interaktif)
//	sikat version               menampilkan versi
package main

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata" // zona Asia/Makassar tersedia walau server tidak punya tzdata

	"golang.org/x/term"

	"sikat"
	"sikat/internal/clock"
	"sikat/internal/config"
	"sikat/internal/database"
	"sikat/internal/server"
	"sikat/internal/users"
)

// version diisi saat build: -ldflags "-X main.version=1.0.0"
var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() error {
	return errors.New(`penggunaan: sikat <perintah>

perintah:
  serve                    menjalankan server HTTP
  migrate up|down|status   mengelola migrasi database
  create-admin             membuat akun Admin IT (interaktif)
  version                  menampilkan versi`)
}

func run(args []string) error {
	if len(args) == 0 {
		return usage()
	}
	if args[0] == "version" {
		fmt.Println("sikat", version)
		return nil
	}

	cfg, err := config.Load(".env")
	if err != nil {
		return err
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	if !cfg.IsProduction() {
		log = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := database.Open(ctx, cfg.DBDSN)
	if err != nil {
		return err
	}
	defer db.Close()

	switch args[0] {
	case "serve":
		return serve(ctx, cfg, db, log)
	case "migrate":
		return migrate(ctx, db, args[1:])
	case "create-admin":
		return createAdmin(ctx, db)
	default:
		return usage()
	}
}

func migrationsFS() (fs.FS, error) {
	return fs.Sub(sikat.MigrationsFS, "migrations")
}

func migrate(ctx context.Context, db *sql.DB, args []string) error {
	if len(args) != 1 {
		return errors.New("penggunaan: sikat migrate up|down|status")
	}
	fsys, err := migrationsFS()
	if err != nil {
		return err
	}
	m, err := database.NewMigrator(db, fsys)
	if err != nil {
		return err
	}
	switch args[0] {
	case "up":
		applied, err := m.Up(ctx)
		for _, a := range applied {
			fmt.Println("diterapkan:", a)
		}
		if err != nil {
			return err
		}
		if len(applied) == 0 {
			fmt.Println("database sudah versi terbaru")
		}
	case "down":
		name, err := m.Down(ctx)
		if err != nil {
			return err
		}
		fmt.Println("dibatalkan:", name)
	case "status":
		lines, err := m.Status(ctx)
		if err != nil {
			return err
		}
		for _, l := range lines {
			fmt.Println(l)
		}
	default:
		return errors.New("penggunaan: sikat migrate up|down|status")
	}
	return nil
}

func serve(ctx context.Context, cfg config.Config, db *sql.DB, log *slog.Logger) error {
	app, err := server.New(ctx, server.Deps{
		Config: cfg, DB: db, Log: log, Clock: clock.Real{},
		Templates: sikat.TemplatesFS, Static: sikat.StaticFS,
		DevReloadDir: ".", SessionCleanup: 30 * time.Minute,
	})
	if err != nil {
		return err
	}
	go app.Worker.Run(ctx) // pekerjaan latar belakang (hanya berjalan di instance pemegang lock)
	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           app.Handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	errCh := make(chan error, 1)
	go func() {
		log.Info("SIKAT berjalan", "addr", cfg.Addr, "env", cfg.Env, "version", version)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}
	log.Info("menghentikan server...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

func prompt(r *bufio.Reader, label string) (string, error) {
	fmt.Print(label)
	s, err := r.ReadString('\n')
	if err != nil && s == "" {
		return "", err
	}
	return strings.TrimSpace(s), nil
}

func readPassword(r *bufio.Reader, label string) (string, error) {
	fmt.Print(label)
	if term.IsTerminal(int(os.Stdin.Fd())) {
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		return string(b), err
	}
	s, err := r.ReadString('\n')
	if err != nil && s == "" {
		return "", err
	}
	return strings.TrimRight(s, "\r\n"), nil
}

func createAdmin(ctx context.Context, db *sql.DB) error {
	r := bufio.NewReader(os.Stdin)
	username, err := prompt(r, "Username admin: ")
	if err != nil {
		return err
	}
	fullName, err := prompt(r, "Nama lengkap: ")
	if err != nil {
		return err
	}
	pw, err := readPassword(r, "Password (min. 10 karakter): ")
	if err != nil {
		return err
	}
	pw2, err := readPassword(r, "Ulangi password: ")
	if err != nil {
		return err
	}
	if pw != pw2 {
		return errors.New("password tidak sama")
	}
	id, err := users.NewService(db, clock.Real{}).CreateAdmin(ctx, username, fullName, pw)
	if err != nil {
		return err
	}
	fmt.Printf("Admin IT dibuat (id %d). Silakan login dengan username %q.\n", id, strings.ToLower(username))
	return nil
}
