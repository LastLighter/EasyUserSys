package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"easyusersys/internal/config"
	"easyusersys/internal/db"
	httpapi "easyusersys/internal/http"
	"easyusersys/internal/services"
	"github.com/joho/godotenv"
)

func main() {
	if _, err := os.Stat(".env"); err == nil {
		if err := godotenv.Load(); err != nil {
			log.Printf("load .env failed: %v", err)
		}
	} else if err != nil && !os.IsNotExist(err) {
		log.Printf("stat .env failed: %v", err)
	}

	cfg := config.Load()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pool, err := db.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("db connect failed: %v", err)
	}
	defer pool.Close()

	svc := services.New(pool, cfg)
	if err := svc.EnsureDefaultPlans(ctx); err != nil {
		log.Fatalf("ensure plans failed: %v", err)
	}

	server := httpapi.NewServer(svc, cfg)
	httpServer := &http.Server{
		Addr:    cfg.ServerAddr,
		Handler: server.Routes(),
	}

	go func() {
		log.Printf("server listening on %s", cfg.ServerAddr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("server shutdown error: %v", err)
	}
}
