package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"log"
	"nailist/internal/groups"
	"nailist/internal/server"
	"nailist/migrations"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func env(k, fallback string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return fallback
}
func main() {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Println("nailist " + Version)
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	publicURL := strings.TrimRight(env("PUBLIC_URL", "http://localhost:8080"), "/")
	u, err := url.Parse(publicURL)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.Path != "" {
		log.Fatal("PUBLIC_URL 必须是站点源地址")
	}
	db, err := pgxpool.New(ctx, env("DATABASE_URL", "postgres://nailist:nailist@localhost:5432/nailist?sslmode=disable"))
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	startup, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err = db.Ping(startup); err != nil {
		log.Fatal("连接数据库失败：", err)
	}
	if err = migrate(startup, db); err != nil {
		log.Fatal("迁移失败：", err)
	}
	cfg := server.Config{PublicURL: publicURL, QQAppID: os.Getenv("QQ_APP_ID"), QQAppKey: os.Getenv("QQ_APP_KEY"), WebDir: env("WEB_DIR", "web/dist")}
	unionID, err := strconv.ParseBool(env("QQ_USE_UNIONID", "false"))
	if err != nil {
		log.Fatal("QQ_USE_UNIONID 必须为 true 或 false")
	}
	cfg.QQUseUnionID = unionID
	cfg.OIDC = server.OIDCConfig{ID: os.Getenv("OIDC_ID"), Name: os.Getenv("OIDC_NAME"), Icon: os.Getenv("OIDC_ICON"), Issuer: os.Getenv("OIDC_ISSUER"), ClientID: os.Getenv("OIDC_CLIENT_ID"), ClientSecret: os.Getenv("OIDC_CLIENT_SECRET"), Scopes: os.Getenv("OIDC_SCOPES")}
	if err = server.ValidateAuthConfig(cfg); err != nil {
		log.Fatal(err)
	}
	s := server.New(cfg, db, groups.Store{DB: db})
	httpServer := &http.Server{Addr: env("LISTEN_ADDR", ":8080"), Handler: s.Routes(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdown)
	}()
	log.Printf("奶龙群列表 v%s：%s", Version, publicURL)
	if err = httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
func migrate(ctx context.Context, db *pgxpool.Pool) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(884212)`); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, migrations.SQL); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
