package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"paperqual/internal/api"
	"paperqual/internal/application"
	"paperqual/internal/store"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cfg, err := parseConfig(args)
	if err != nil {
		return err
	}
	if cfg.selfCheck {
		return runSelfCheck(cfg.address)
	}
	repo, err := store.Open(cfg.dataDir)
	if err != nil {
		return fmt.Errorf("打开持久化仓储失败: %w", err)
	}
	service := application.NewService(repo)
	if err := service.Ready(); err != nil {
		return fmt.Errorf("服务未就绪: %w", err)
	}
	listener, err := net.Listen("tcp", cfg.address)
	if err != nil {
		return fmt.Errorf("监听 %s 失败: %w", cfg.address, err)
	}
	httpServer := &http.Server{
		Handler:           api.NewServer(service),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    32 << 10,
	}
	errCh := make(chan error, 1)
	go func() { errCh <- httpServer.Serve(listener) }()
	log.Printf("纸安批次放行台监听 %s，数据目录 %s", cfg.address, cfg.dataDir)
	signalCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-signalCtx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("服务有界关闭失败: %w", err)
	}
	if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
