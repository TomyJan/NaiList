package main

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

func parseAdminUserIDs(value string) (map[int64]bool, error) {
	ids := map[int64]bool{}
	if strings.TrimSpace(value) == "" {
		return ids, nil
	}
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part == "" || strings.IndexFunc(part, func(r rune) bool { return r < '0' || r > '9' }) != -1 {
			return nil, errors.New("ADMIN_USER_IDS 必须为逗号分隔的正整数账号 ID")
		}
		id, err := strconv.ParseInt(part, 10, 64)
		if err != nil || id < 1 {
			return nil, errors.New("ADMIN_USER_IDS 必须为逗号分隔的正整数账号 ID")
		}
		ids[id] = true
	}
	return ids, nil
}

// Preserve classification and IO causes without printing dotenv parser errors,
// which may include the complete source line and credentials.
type environmentLoadError struct{ cause error }

func (e *environmentLoadError) Error() string {
	var path *os.PathError
	if errors.As(e.cause, &path) {
		return fmt.Sprintf("读取 .env 失败: operation=%s: %v", path.Op, path.Err)
	}
	return "解析 .env 失败，请检查键值格式和引号"
}
func (e *environmentLoadError) Unwrap() error { return e.cause }

func loadEnvironment() error {
	err := godotenv.Load(".env")
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func databaseURL() string {
	return os.Getenv("DATABASE_URL")
}

func listenAddr() string {
	return ":" + env("PORT", "8080")
}
