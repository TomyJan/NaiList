package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"log/slog"
	"nailist/internal/groups"
	"nailist/internal/logging"
	"nailist/internal/server"
	"nailist/migrations"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func main() {
	version := applicationVersion()
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Println("nailist " + version)
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Stderr); err != nil {
		os.Exit(1)
	}
}

func run(ctx context.Context, output io.Writer) (err error) {
	logger, _ := logging.New(output, "info")
	slog.SetDefault(logger)
	stage := "environment"
	defer func() {
		if err != nil {
			slog.Error("服务退出", "stage", stage, "error", err)
		}
	}()
	if err = loadEnvironment(); err != nil {
		return errors.New("加载 .env 失败，请检查文件权限和格式")
	}
	stage = "logging_config"
	logger, err = logging.New(output, env("LOG_LEVEL", "info"), databaseURL(), os.Getenv("QQ_APP_KEY"), os.Getenv("OIDC_CLIENT_SECRET"))
	if err != nil {
		return err
	}
	slog.SetDefault(logger)
	version := applicationVersion()
	slog.Info("服务启动", "version", version, "log_level", strings.ToLower(env("LOG_LEVEL", "info")))
	stage = "config"
	dsn := databaseURL()
	if dsn == "" {
		return errors.New("请在 .env 或环境变量中配置 DATABASE_URL")
	}
	publicURL := strings.TrimRight(env("PUBLIC_URL", "http://localhost:8080"), "/")
	u, parseErr := url.Parse(publicURL)
	if parseErr != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return errors.New("PUBLIC_URL 必须是站点源地址")
	}
	port, parseErr := strconv.Atoi(env("PORT", "8080"))
	if parseErr != nil || port < 1 || port > 65535 {
		return errors.New("PORT 必须为 1～65535 的整数")
	}
	cfg := server.Config{PublicURL: publicURL, QQAppID: os.Getenv("QQ_APP_ID"), QQAppKey: os.Getenv("QQ_APP_KEY")}
	cfg.Version = version
	cfg.UmamiScriptURL = os.Getenv("UMAMI_SCRIPT_URL")
	cfg.UmamiWebsiteID = os.Getenv("UMAMI_WEBSITE_ID")
	if err = server.ValidateWebConfig(cfg); err != nil {
		return err
	}
	unionID, err := strconv.ParseBool(env("QQ_USE_UNIONID", "false"))
	if err != nil {
		return errors.New("QQ_USE_UNIONID 必须为 true 或 false")
	}
	cfg.QQUseUnionID = unionID
	cfg.OIDC = server.OIDCConfig{ID: os.Getenv("OIDC_ID"), Name: os.Getenv("OIDC_NAME"), Icon: os.Getenv("OIDC_ICON"), Issuer: os.Getenv("OIDC_ISSUER"), ClientID: os.Getenv("OIDC_CLIENT_ID"), ClientSecret: os.Getenv("OIDC_CLIENT_SECRET"), Scopes: os.Getenv("OIDC_SCOPES")}
	if err = server.ValidateAuthConfig(cfg); err != nil {
		return err
	}
	slog.Info("配置已加载", "public_url", publicURL, "qq_enabled", cfg.QQAppID != "" && cfg.QQAppKey != "", "qq_unionid", cfg.QQUseUnionID, "oidc_enabled", cfg.OIDC.ID != "", "umami_enabled", cfg.UmamiScriptURL != "")
	stage = "http_bind"
	listener, err := net.Listen("tcp", listenAddr())
	if err != nil {
		return fmt.Errorf("监听 %s 失败: %w", listenAddr(), err)
	}
	defer listener.Close()
	slog.Info("端口已绑定，正在初始化", "address", listener.Addr().String())
	stage = "database_connect"
	slog.Info("正在连接数据库")
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	startup, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	started := time.Now()
	if err = db.Ping(startup); err != nil {
		return err
	}
	slog.Info("数据库已连接", "duration_ms", time.Since(started).Milliseconds())
	stage = "database_migrate"
	slog.Info("开始数据库迁移")
	started = time.Now()
	if err = migrate(startup, db); err != nil {
		return err
	}
	slog.Info("数据库迁移完成", "duration_ms", time.Since(started).Milliseconds())
	s := server.New(cfg, db, groups.Store{DB: db})
	httpServer := &http.Server{Addr: listenAddr(), Handler: s.Routes(), ErrorLog: slog.NewLogLogger(logger.Handler(), slog.LevelError), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	stage = "http_serve"
	slog.Info("HTTP 服务就绪", "address", listener.Addr().String(), "public_url", publicURL, "version", version)
	return serveHTTP(ctx, httpServer, listener)
}

// Wait for active requests before allowing run to close the database pool.
func serveHTTP(ctx context.Context, server *http.Server, listener net.Listener) error {
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	select {
	case err := <-served:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		slog.Info("收到停止信号，等待请求完成")
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			slog.Error("优雅停机失败，强制关闭连接", "error", err)
			_ = server.Close()
			<-served
			return err
		}
		err := <-served
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		slog.Info("HTTP 服务已停止")
		return nil
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
