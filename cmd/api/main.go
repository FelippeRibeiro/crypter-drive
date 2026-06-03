package main

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/FelippeRibeiro/crypter-drive/internal/auth"
	"github.com/FelippeRibeiro/crypter-drive/internal/config"
	"github.com/FelippeRibeiro/crypter-drive/internal/db"
	"github.com/FelippeRibeiro/crypter-drive/internal/drive"
	httpserver "github.com/FelippeRibeiro/crypter-drive/internal/http"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	sqlDB, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("database open error: %v", err)
	}
	defer sqlDB.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if err := sqlDB.PingContext(ctx); err != nil {
		log.Fatalf("database ping error: %v", err)
	}

	if err := db.ApplyMigrations(ctx, sqlDB, "migrations"); err != nil {
		log.Fatalf("migration error: %v", err)
	}

	driveSvc, err := drive.NewService(cfg.GoogleCredentials, cfg.GoogleTokenFile, cfg.GoogleDriveRootName)
	if err != nil {
		log.Fatalf("google drive setup error: %v", err)
	}
	if _, err := driveSvc.EnsureRootFolder(ctx); err != nil {
		log.Fatalf("failed to ensure google drive folder: %v", err)
	}

	authSvc := auth.NewService(cfg.JWTSecret)
	server := httpserver.New(sqlDB, authSvc, driveSvc, cfg.MasterKey, "web")

	httpSrv := &http.Server{
		Addr:              ":" + cfg.HTTPPort,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Printf("server running on :%s", cfg.HTTPPort)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("http server error: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		log.Printf("graceful shutdown error: %v", err)
	}
}
