package main

import (
	"net/url"
	"os"
	"testing"
)

func clearConfigEnv(t *testing.T, keys ...string) {
	t.Helper()
	for _, key := range keys {
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLoadEnvironment(t *testing.T) {
	t.Chdir(t.TempDir())
	clearConfigEnv(t, "PUBLIC_URL", "POSTGRES_PASSWORD", "APP_PORT", "QQ_APP_KEY", "OIDC_NAME", "QQ_APP_ID")
	t.Setenv("PUBLIC_URL", "https://environment.example")
	t.Setenv("QQ_APP_ID", "")
	content := "# common configuration\r\nexport PUBLIC_URL=https://file.example\r\nPOSTGRES_PASSWORD='p@ss#word$'\r\nAPP_PORT=8092\r\nQQ_APP_KEY='literal#secret$'\r\nOIDC_NAME=\"校园 账号\"\r\nQQ_APP_ID=from-file\r\n"
	if err := os.WriteFile(".env", []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	if err := loadEnvironment(); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{
		"PUBLIC_URL": "https://environment.example", "POSTGRES_PASSWORD": "p@ss#word$",
		"APP_PORT": "8092", "QQ_APP_KEY": "literal#secret$", "OIDC_NAME": "校园 账号", "QQ_APP_ID": "",
	} {
		if got := os.Getenv(key); got != want {
			t.Errorf("unexpected value for %s", key)
		}
	}
}

func TestLoadEnvironmentMissingAndInvalid(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := loadEnvironment(); err != nil {
		t.Fatalf("missing .env must allow environment-only startup: %v", err)
	}
	if err := os.WriteFile(".env", []byte("PUBLIC_URL=\"unterminated"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := loadEnvironment(); err == nil {
		t.Fatal("invalid .env must reject startup")
	}
}

func TestSharedDatabaseAndPortConfiguration(t *testing.T) {
	clearConfigEnv(t, "DATABASE_URL", "POSTGRES_PASSWORD", "POSTGRES_HOST", "POSTGRES_PORT", "APP_PORT", "LISTEN_ADDR")
	t.Setenv("POSTGRES_PASSWORD", "p@ss:/#%🍐")
	t.Setenv("APP_PORT", "8093")
	connection, err := url.Parse(databaseURL())
	if err != nil {
		t.Fatal(err)
	}
	password, _ := connection.User.Password()
	if connection.Host != "127.0.0.1:5432" || connection.User.Username() != "nailist" || connection.Path != "/nailist" || password != os.Getenv("POSTGRES_PASSWORD") {
		t.Fatal("database fallback must use the common password without URL corruption")
	}
	if listenAddr() != ":8093" {
		t.Fatal("APP_PORT must configure the standalone server")
	}
	t.Setenv("POSTGRES_HOST", "db")
	t.Setenv("POSTGRES_PORT", "55432")
	connection, err = url.Parse(databaseURL())
	if err != nil || connection.Host != "db:55432" {
		t.Fatal("database host/port override failed")
	}
	t.Setenv("DATABASE_URL", "postgres://custom:password@example.com:5433/custom")
	t.Setenv("LISTEN_ADDR", "127.0.0.1:9000")
	if databaseURL() != os.Getenv("DATABASE_URL") || listenAddr() != "127.0.0.1:9000" {
		t.Fatal("explicit connection/listen settings must take priority")
	}
}
