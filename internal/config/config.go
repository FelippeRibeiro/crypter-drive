package config

import (
	"encoding/base64"
	"errors"
	"os"
	"strconv"
)

type Config struct {
	HTTPPort            string
	DatabaseURL         string
	JWTSecret           string
	MasterKey           []byte
	GoogleCredentials   string
	GoogleTokenFile     string
	GoogleDriveRootName string
}

func Load() (Config, error) {
	cfg := Config{
		HTTPPort:            getEnv("HTTP_PORT", "8080"),
		DatabaseURL:         os.Getenv("DATABASE_URL"),
		JWTSecret:           os.Getenv("JWT_SECRET"),
		GoogleCredentials:   getEnv("GOOGLE_CREDENTIALS_FILE", "credentials.json"),
		GoogleTokenFile:     getEnv("GOOGLE_TOKEN_FILE", "token.json"),
		GoogleDriveRootName: getEnv("GOOGLE_DRIVE_ROOT_FOLDER", "crypter"),
	}

	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}
	if cfg.JWTSecret == "" {
		return Config{}, errors.New("JWT_SECRET is required")
	}

	masterKeyB64 := os.Getenv("MASTER_KEY_BASE64")
	if masterKeyB64 == "" {
		return Config{}, errors.New("MASTER_KEY_BASE64 is required")
	}

	masterKey, err := base64.StdEncoding.DecodeString(masterKeyB64)
	if err != nil {
		return Config{}, errors.New("MASTER_KEY_BASE64 is not valid base64")
	}
	if len(masterKey) != 32 {
		return Config{}, errors.New("MASTER_KEY_BASE64 must decode to 32 bytes (AES-256)")
	}
	cfg.MasterKey = masterKey

	return cfg, nil
}

func getEnv(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func GetIntEnv(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	num, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return num
}
