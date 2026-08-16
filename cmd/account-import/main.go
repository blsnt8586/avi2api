package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/leonardo2api/leonardo2api/internal/accounts"
	"github.com/leonardo2api/leonardo2api/internal/config"
	"github.com/leonardo2api/leonardo2api/internal/cryptox"
	"github.com/leonardo2api/leonardo2api/internal/store"
)

type importRequest struct {
	Name               string `json:"name"`
	Email              string `json:"email"`
	Password           string `json:"password"`
	ProxyURL           string `json:"proxy_url"`
	BrowserWorkerGroup string `json:"browser_worker_group"`
	ImageConcurrency   int    `json:"image_concurrency"`
	QueueCapacity      int    `json:"queue_capacity"`
	RoutingRole        string `json:"routing_role"`
	ProtectedTokens    int64  `json:"protected_tokens"`
	VideoReservedSlots int    `json:"video_reserved_slots"`
	SessionTokenPath   string `json:"session_token_path"`
	CookieJSONPath     string `json:"cookie_json_path"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	var req importRequest
	decoder := json.NewDecoder(io.LimitReader(os.Stdin, 64<<10))
	if err := decoder.Decode(&req); err != nil {
		return fmt.Errorf("decode import request: %w", err)
	}
	if strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.Email) == "" || req.Password == "" {
		return errors.New("name, email and password are required")
	}
	if req.ProxyURL == "" {
		return errors.New("proxy_url is required")
	}
	if req.BrowserWorkerGroup == "" {
		req.BrowserWorkerGroup = "default"
	}
	if req.ImageConcurrency == 0 {
		req.ImageConcurrency = 5
	}
	if req.QueueCapacity == 0 {
		req.QueueCapacity = 40
	}
	if req.RoutingRole == "" {
		req.RoutingRole = "general"
	}
	if req.SessionTokenPath == "" || req.CookieJSONPath == "" {
		return errors.New("session_token_path and cookie_json_path are required")
	}

	sessionData, err := os.ReadFile(req.SessionTokenPath)
	if err != nil {
		return fmt.Errorf("read browser session: %w", err)
	}
	var session accounts.BrowserSession
	if err := json.Unmarshal(sessionData, &session); err != nil {
		return fmt.Errorf("decode browser session: %w", err)
	}
	cookie, err := os.ReadFile(req.CookieJSONPath)
	if err != nil {
		return fmt.Errorf("read cookie json: %w", err)
	}
	session.CookieJSON = cookie
	normalizedCookieJSON, err := accounts.NormalizeBrowserCookieJSON(session.CookieJSON)
	if err != nil {
		return err
	}
	session.CookieHeader, err = accounts.CookieHeaderFromJSON(normalizedCookieJSON)
	if err != nil {
		return err
	}
	session.CookieJSON = json.RawMessage(normalizedCookieJSON)
	if session.AccessToken == "" || session.AccessTokenExpiry <= time.Now().Unix() || session.CookieHeader == "" {
		return errors.New("browser session is incomplete or expired")
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	st, err := store.NewWithMaxConns(ctx, cfg.DatabaseURL, int32(cfg.DatabaseMaxConns))
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	defer st.Close()
	rdb := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr, Password: cfg.RedisPassword, DB: cfg.RedisDB})
	defer rdb.Close()
	if err := rdb.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("connect redis: %w", err)
	}
	cipher, err := cryptox.New(cfg.MasterKey)
	if err != nil {
		return fmt.Errorf("initialize cipher: %w", err)
	}
	service := &accounts.Service{Store: st, Redis: rdb, Cipher: cipher, Config: cfg}
	account, err := service.Create(ctx, strings.TrimSpace(req.Name), strings.TrimSpace(req.Email), req.Password, session.CookieHeader, req.ProxyURL, req.BrowserWorkerGroup, req.ImageConcurrency, req.QueueCapacity, req.RoutingRole, req.ProtectedTokens, req.VideoReservedSlots, &session)
	if err != nil {
		return fmt.Errorf("create account: %w", err)
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{
		"id":                      account.ID,
		"name":                    account.Name,
		"email":                   account.Email,
		"status":                  account.Status,
		"available_tokens":        account.AvailableTokens,
		"access_token_expires_at": account.AccessTokenExpiresAt,
	})
}
