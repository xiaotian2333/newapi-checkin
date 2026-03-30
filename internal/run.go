package app

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"newapi-checkin/internal/auth"
	"newapi-checkin/internal/config"
	"newapi-checkin/internal/handler"
	"newapi-checkin/internal/store"
)

func Run() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	dbStore, err := store.New(context.Background(), cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("连接数据库失败: %v", err)
	}
	defer dbStore.Close()

	if err := dbStore.ValidateSchema(context.Background()); err != nil {
		log.Fatalf("数据库结构校验失败: %v", err)
	}

	authService := auth.NewService(cfg)
	webApp, err := handler.New(handler.Options{
		Config: cfg,
		Store:  dbStore,
		Auth:   authService,
	})
	if err != nil {
		log.Fatalf("初始化应用失败: %v", err)
	}

	server := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           webApp.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("服务已启动，监听 %s", cfg.ListenAddr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("启动 HTTP 服务失败: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("关闭服务失败: %v", err)
	}
}
