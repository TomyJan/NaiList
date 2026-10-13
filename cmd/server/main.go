package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
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
	"runtime"
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
	logger = logger.With("service", "nailist", "version", applicationVersion(), "pid", os.Getpid())
	slog.SetDefault(logger)
	lifetime := time.Now()
	ready := false
	stage := "environment"
	defer func() {
		if err != nil {
			event := "app.startup.failed"
			message := "服务启动失败，未进入可用状态"
			level, outcome := slog.LevelError, "failed"
			if ready {
				event = "app.runtime.failed"
				message = "服务运行异常退出"
			}
			if !ready && errors.Is(err, context.Canceled) {
				level, outcome, event = slog.LevelInfo, "canceled", "app.startup.canceled"
				message = "启动期间收到停止信号，已释放初始化资源"
			}
			slog.Log(context.Background(), level, message, append([]any{"component", "app", "event", event, "stage", stage, "outcome", outcome, "uptime_ms", logging.DurationMS(lifetime), "listen_address", listenAddr(), "hint", startupHint(stage)}, logging.ErrorAttrs(err)...)...)
		} else {
			slog.Info("服务已正常退出，HTTP 与数据库资源已释放", "component", "app", "event", "app.stopped", "outcome", "success", "uptime_ms", logging.DurationMS(lifetime))
		}
	}()
	_, dotenvErr := os.Stat(".env")
	if err = loadEnvironment(); err != nil {
		return &environmentLoadError{cause: err}
	}
	stage = "logging_config"
	logger, err = logging.New(output, env("LOG_LEVEL", "info"), databaseURL(), os.Getenv("QQ_APP_KEY"), os.Getenv("OIDC_CLIENT_SECRET"), os.Getenv("UMAMI_SHARE_URL"))
	if err != nil {
		return err
	}
	logger = logger.With("service", "nailist", "version", applicationVersion(), "pid", os.Getpid())
	slog.SetDefault(logger)
	ctx = logging.WithLogger(ctx, logger)
	version := applicationVersion()
	slog.Info("服务开始启动，正在校验配置及运行依赖", "component", "app", "event", "app.starting", "log_level", strings.ToLower(env("LOG_LEVEL", "info")), "go_version", runtime.Version(), "os", runtime.GOOS, "arch", runtime.GOARCH, "cpu_count", runtime.NumCPU())
	slog.Debug("配置来源已确定，环境变量优先于 .env，未设置项使用程序默认值", "component", "config", "event", "config.source.loaded", "env_file", ".env", "env_file_present", dotenvErr == nil)
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
	cfg.TrustedProxyCIDRs, err = server.ParseTrustedProxyCIDRs(os.Getenv("TRUSTED_PROXY_CIDRS"))
	if err != nil {
		return err
	}
	cfg.AdminUserIDs, err = parseAdminUserIDs(os.Getenv("ADMIN_USER_IDS"))
	if err != nil {
		return err
	}
	cfg.Version = version
	cfg.UmamiScriptURL = os.Getenv("UMAMI_SCRIPT_URL")
	cfg.UmamiWebsiteID = os.Getenv("UMAMI_WEBSITE_ID")
	cfg.UmamiShareURL = strings.TrimSpace(os.Getenv("UMAMI_SHARE_URL"))
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
	slog.Info("配置校验通过，已确定站点与登录方式", "component", "config", "event", "config.validated", "public_url", publicURL, "listen_address", listenAddr(), "qq_enabled", cfg.QQAppID != "" && cfg.QQAppKey != "", "qq_unionid", cfg.QQUseUnionID, "oidc_enabled", cfg.OIDC.ID != "", "oidc_provider", cfg.OIDC.ID, "umami_enabled", cfg.UmamiScriptURL != "", "traffic_enabled", cfg.UmamiShareURL != "")
	if cfg.OIDC.ID != "" {
		slog.Debug("OIDC 登录配置已启用，授权时将发现并校验服务端点", "component", "config", "event", "config.oidc.enabled", "provider", cfg.OIDC.ID, "issuer", cfg.OIDC.Issuer, "callback_path", "/auth/"+cfg.OIDC.ID+"/callback", "extra_scopes", strings.Fields(cfg.OIDC.Scopes))
	}
	if (cfg.QQAppID != "" || cfg.QQAppKey != "") && (cfg.QQAppID == "" || cfg.QQAppKey == "") {
		slog.Warn("QQ 应用 ID 或密钥缺失，QQ 登录不会启用", "component", "config", "event", "config.qq.disabled", "reason", "incomplete_credentials", "app_id_present", cfg.QQAppID != "", "app_key_present", cfg.QQAppKey != "")
	}
	stage = "http_bind"
	listener, err := net.Listen("tcp", listenAddr())
	if err != nil {
		return fmt.Errorf("监听 %s 失败: %w", listenAddr(), err)
	}
	defer listener.Close()
	slog.Debug("HTTP 端口绑定成功，数据库初始化完成后才接受业务请求", "component", "http", "event", "http.listener.bound", "address", listener.Addr().String())
	stage = "database_connect"
	dbConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return fmt.Errorf("解析 DATABASE_URL: %w", err)
	}
	dbConfig.ConnConfig.Tracer = logging.DatabaseTracer{}
	slog.Info("正在连接数据库，验证 PostgreSQL 可达性与凭据", "component", "database", "event", "db.pool.starting", "db_host", dbConfig.ConnConfig.Host, "db_port", dbConfig.ConnConfig.Port, "database", dbConfig.ConnConfig.Database, "db_user", dbConfig.ConnConfig.User, "tls_enabled", dbConfig.ConnConfig.TLSConfig != nil, "startup_timeout_ms", 15000)
	slog.Debug("数据库连接池参数已确定", "component", "database", "event", "db.pool.configured", "pool_min", dbConfig.MinConns, "pool_max", dbConfig.MaxConns, "max_connection_lifetime_ms", dbConfig.MaxConnLifetime.Milliseconds(), "max_idle_time_ms", dbConfig.MaxConnIdleTime.Milliseconds(), "health_period_ms", dbConfig.HealthCheckPeriod.Milliseconds())
	db, err := pgxpool.NewWithConfig(ctx, dbConfig)
	if err != nil {
		return err
	}
	defer func() {
		started := time.Now()
		stat := db.Stat()
		slog.Debug("关闭数据库连接池，等待借出的连接归还", "component", "database", "event", "db.pool.closing", "pool_acquired", stat.AcquiredConns(), "pool_total", stat.TotalConns())
		db.Close()
		slog.Debug("数据库连接池已关闭", "component", "database", "event", "db.pool.closed", "duration_ms", logging.DurationMS(started))
	}()
	startup, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	started := time.Now()
	if err = db.Ping(logging.WithOperation(startup, "startup.database_ping")); err != nil {
		return err
	}
	slog.Info("数据库连接验证通过", "component", "database", "event", "db.pool.ready", "database", dbConfig.ConnConfig.Database, "duration_ms", logging.DurationMS(started))
	stage = "database_migrate"
	slog.Debug("开始幂等数据库迁移，将先获取事务级排他迁移锁", "component", "database", "event", "db.migration.started", "migration_count", migrations.Count)
	started = time.Now()
	if err = migrate(startup, db); err != nil {
		return err
	}
	slog.Info("数据库迁移已提交，账号及奶群表结构可用", "component", "database", "event", "db.migration.completed", "outcome", "committed", "duration_ms", logging.DurationMS(started))
	s := server.New(cfg, db, groups.Store{DB: db})
	httpServer := &http.Server{Addr: listenAddr(), Handler: s.Routes(), ErrorLog: slog.NewLogLogger(logger.Handler(), slog.LevelError), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	stage = "http_serve"
	slog.Debug("HTTP 请求限制已配置，登录发起与回调独立限流，可信代理之外的转发头不参与客户端识别", "component", "http", "event", "http.server.configured", "read_header_timeout_ms", 5000, "read_timeout_ms", 20000, "write_timeout_ms", 30000, "idle_timeout_ms", 60000, "request_timeout_ms", 20000, "write_concurrency", 32, "auth_start_concurrency", 8, "auth_callback_concurrency", 16, "auth_start_per_ip_minute", 20, "auth_start_global_minute", 120, "auth_callback_per_ip_minute", 60, "auth_callback_global_minute", 240, "user_writes_minute", 30, "user_writes_hour", 200, "trusted_proxy_cidrs", cfg.TrustedProxyCIDRs, "shutdown_timeout_ms", 10000)
	ready = true
	slog.Info("HTTP 服务就绪，可接受页面、奶群与登录请求", "component", "app", "event", "app.ready", "address", listener.Addr().String(), "public_url", publicURL, "startup_ms", logging.DurationMS(lifetime))
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
		started := time.Now()
		slog.Info("收到停止信号，停止接受新连接并等待请求完成", "component", "app", "event", "app.shutdown.started", "shutdown_timeout_ms", 10000, "reason", context.Cause(ctx))
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			slog.Warn("优雅停机等待超时或失败，强制关闭剩余连接", append([]any{"component", "http", "event", "http.shutdown.forced", "duration_ms", logging.DurationMS(started)}, logging.ErrorAttrs(err)...)...)
			_ = server.Close()
			<-served
			return err
		}
		err := <-served
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		slog.Info("HTTP 服务已停止，所有处理中请求已退出", "component", "http", "event", "http.shutdown.completed", "duration_ms", logging.DurationMS(started))
		return nil
	}
}
func migrate(ctx context.Context, db *pgxpool.Pool) error {
	ctx = logging.WithOperation(ctx, "startup.migration")
	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("开始数据库迁移事务: %w", err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		if err := tx.Rollback(cleanup); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			logging.FromContext(ctx).DebugContext(cleanup, "数据库迁移回滚未成功，连接将由驱动回收", append([]any{"component", "database", "event", "db.migration.rollback_failed"}, logging.ErrorAttrs(err)...)...)
		}
	}()
	started := time.Now()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(884212)`); err != nil {
		return fmt.Errorf("获取数据库迁移锁: %w", err)
	}
	logging.FromContext(ctx).DebugContext(ctx, "已获取数据库迁移锁", "component", "database", "event", "db.migration.locked", "wait_ms", logging.DurationMS(started))
	if _, err = tx.Exec(ctx, migrations.SQL); err != nil {
		return fmt.Errorf("执行数据库表结构迁移: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("提交数据库迁移事务: %w", err)
	}
	return nil
}

func startupHint(stage string) string {
	switch stage {
	case "environment":
		return "检查工作目录 .env 的可读权限、引号及键值格式"
	case "logging_config":
		return "LOG_LEVEL 仅支持 debug/info/warn/error"
	case "config":
		return "检查 DATABASE_URL、PUBLIC_URL、PORT 以及已启用登录方式的必要配置"
	case "http_bind":
		return "检查端口占用、监听权限及 PORT 配置"
	case "database_connect":
		return "检查 PostgreSQL 地址、凭据、TLS、网络及数据库是否存在"
	case "database_migrate":
		return "检查数据库建表权限、迁移锁及已有表结构，修改前备份数据库"
	default:
		return "根据前面的关联错误和阶段排查运行环境"
	}
}
