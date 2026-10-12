package main

import (
	"errors"
	"net"
	"net/url"
	"os"

	"github.com/joho/godotenv"
)

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
	if value := os.Getenv("DATABASE_URL"); value != "" {
		return value
	}
	connection := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword("nailist", env("POSTGRES_PASSWORD", "nailist")),
		Host:     net.JoinHostPort(env("POSTGRES_HOST", "127.0.0.1"), env("POSTGRES_PORT", "5432")),
		Path:     "/nailist",
		RawQuery: "sslmode=disable",
	}
	return connection.String()
}

func listenAddr() string {
	return env("LISTEN_ADDR", ":"+env("APP_PORT", "8080"))
}
