package main

import (
	"errors"
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
	return os.Getenv("DATABASE_URL")
}

func listenAddr() string {
	return ":" + env("PORT", "8080")
}
