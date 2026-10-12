package main

import (
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
	clearConfigEnv(t, "PUBLIC_URL", "DATABASE_URL", "PORT", "QQ_APP_KEY", "OIDC_NAME", "QQ_APP_ID")
	t.Setenv("PUBLIC_URL", "https://environment.example")
	t.Setenv("QQ_APP_ID", "")
	content := "# common configuration\r\nexport PUBLIC_URL=https://file.example\r\nDATABASE_URL='postgres://nailist:password@database.example/nailist'\r\nPORT=8092\r\nQQ_APP_KEY='literal#secret$'\r\nOIDC_NAME=\"校园 账号\"\r\nQQ_APP_ID=from-file\r\n"
	if err := os.WriteFile(".env", []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	if err := loadEnvironment(); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{
		"PUBLIC_URL": "https://environment.example", "DATABASE_URL": "postgres://nailist:password@database.example/nailist",
		"PORT": "8092", "QQ_APP_KEY": "literal#secret$", "OIDC_NAME": "校园 账号", "QQ_APP_ID": "",
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

func TestDatabaseRequiresExplicitConnection(t *testing.T) {
	clearConfigEnv(t, "DATABASE_URL")
	if databaseURL() != "" {
		t.Fatal("DATABASE_URL must not fall back to an implicit database")
	}
	connection := "postgres://custom:password@example.com:5433/custom?sslmode=require"
	t.Setenv("DATABASE_URL", connection)
	if databaseURL() != connection {
		t.Fatal("DATABASE_URL must be used unchanged")
	}
}

func TestSinglePortConfiguration(t *testing.T) {
	clearConfigEnv(t, "PORT")
	if listenAddr() != ":8080" {
		t.Fatal("default port must be 8080")
	}
	t.Setenv("PORT", "8093")
	if listenAddr() != ":8093" {
		t.Fatal("PORT must configure the server")
	}
}
