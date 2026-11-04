package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
)

type mongoConfig struct {
	URI      string `json:"uri"`
	Database string `json:"database"`
}

type runtimeConfig struct {
	AppID       string                     `json:"appId"`
	AppSecret   string                     `json:"appSecret,omitempty"`
	Runtime     string                     `json:"runtime"`
	Environment string                     `json:"environment,omitempty"`
	MongoDB     *mongoConfig               `json:"mongodb,omitempty"`
	Variables   map[string]json.RawMessage `json:"variables,omitempty"`
}

// The release packager replaces this marker in a private copy of main.go.
// Keep the source checkout free of embedded credentials.
var embeddedProductionConfig string

const openAPIAuthBase = "https://openapi.qiandao.com/auth/v1"

var appIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{4,63}$`)

func parseConfig(data []byte) (runtimeConfig, error) {
	var cfg runtimeConfig
	if json.Unmarshal(data, &cfg) != nil {
		return cfg, errors.New("backend configuration is not valid JSON")
	}
	cfg.AppID = strings.TrimSpace(cfg.AppID)
	if !appIDPattern.MatchString(cfg.AppID) || strings.EqualFold(strings.ReplaceAll(cfg.AppID, "-", "_"), "YOUR_APP_ID") || cfg.Runtime != "go" {
		return cfg, errors.New("backend configuration requires a valid appId and runtime go")
	}
	if cfg.Environment != "" && cfg.Environment != "production" {
		return cfg, errors.New("backend configuration only accepts production environment")
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil {
		return cfg, errors.New("backend configuration must be a JSON object")
	}
	for _, key := range []string{"environment", "appSecret"} {
		if raw, exists := fields[key]; exists && strings.TrimSpace(string(raw)) == "null" {
			return cfg, errors.New("backend string configuration fields cannot be null")
		}
	}
	if raw, exists := fields["variables"]; exists && strings.TrimSpace(string(raw)) == "null" {
		return cfg, errors.New("backend variables must be a JSON object")
	}
	if raw, exists := fields["variables"]; exists {
		decoder := json.NewDecoder(strings.NewReader(string(raw)))
		decoder.UseNumber()
		var variables any
		if decoder.Decode(&variables) != nil || validateVariableNumbers(variables) != nil {
			return cfg, errors.New("backend variables require finite numbers and safe integers; encode large IDs as strings")
		}
	}
	if strings.TrimSpace(cfg.AppSecret) == "" {
		cfg.AppSecret = ""
	} else if strings.EqualFold(strings.ReplaceAll(cfg.AppSecret, "-", "_"), "YOUR_APP_SECRET") {
		return cfg, errors.New("backend appSecret still contains an example value")
	}
	if cfg.MongoDB != nil {
		var mongoFields map[string]json.RawMessage
		if json.Unmarshal(fields["mongodb"], &mongoFields) != nil {
			return cfg, errors.New("MongoDB configuration must be an object")
		}
		for _, key := range []string{"uri", "database"} {
			if raw, exists := mongoFields[key]; exists && strings.TrimSpace(string(raw)) == "null" {
				return cfg, errors.New("MongoDB string configuration fields cannot be null")
			}
		}
		uri := strings.TrimSpace(cfg.MongoDB.URI)
		cfg.MongoDB.Database = strings.TrimSpace(cfg.MongoDB.Database)
		if uri == "" && cfg.MongoDB.Database == "" {
			cfg.MongoDB = nil
			return cfg, nil
		}
		if cfg.MongoDB.Database != cfg.AppID || !(strings.HasPrefix(uri, "mongodb://") || strings.HasPrefix(uri, "mongodb+srv://")) || strings.ContainsAny(uri, " \t\r\n") {
			return cfg, errors.New("MongoDB configuration requires a URI and a database equal to appId")
		}
		cfg.MongoDB.URI = normalizeMongoURI(uri, cfg.MongoDB.Database)
	}
	return cfg, nil
}

func validateVariableNumbers(value any) error {
	switch typed := value.(type) {
	case json.Number:
		number, err := strconv.ParseFloat(string(typed), 64)
		if err != nil || math.IsInf(number, 0) || math.IsNaN(number) || (math.Trunc(number) == number && math.Abs(number) > 9007199254740991) {
			return errors.New("variable number is outside the supported JSON range")
		}
	case map[string]any:
		for _, child := range typed {
			if err := validateVariableNumbers(child); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range typed {
			if err := validateVariableNumbers(child); err != nil {
				return err
			}
		}
	}
	return nil
}

func normalizeMongoURI(uri, database string) string {
	queryIndex := strings.IndexByte(uri, '?')
	if queryIndex < 0 {
		return uri
	}
	authorityStart := strings.Index(uri, "://") + 3
	pathIndex := strings.IndexByte(uri[authorityStart:queryIndex], '/')
	authorityEnd := queryIndex
	if pathIndex >= 0 {
		pathIndex += authorityStart
		authorityEnd = pathIndex
	}
	query, _ := url.ParseQuery(uri[queryIndex+1:])
	hasAuthSource := false
	for key := range query {
		if strings.EqualFold(key, "authSource") {
			hasAuthSource = true
		}
	}
	// Changing a database path can change the driver's implicit auth database.
	if strings.Contains(uri[authorityStart:authorityEnd], "@") && !hasAuthSource {
		database = ""
	}
	if pathIndex < 0 {
		return uri[:queryIndex] + "/" + database + uri[queryIndex:]
	}
	if pathIndex == queryIndex-1 && database != "" {
		return uri[:queryIndex] + database + uri[queryIndex:]
	}
	return uri
}

func loadConfig() (runtimeConfig, error) {
	if embeddedProductionConfig != "" {
		return parseConfig([]byte(embeddedProductionConfig))
	}
	configPath := os.Getenv("QDMP_CONFIG")
	if configPath == "" {
		configPath = "../qdmp-config.json"
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		return runtimeConfig{}, errors.New("backend configuration is missing or unreadable")
	}
	return parseConfig(data)
}

// Startup and readiness errors never include driver errors, hostnames or secrets.
func mongoFailure(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return errors.New("MongoDB connection timed out")
	}
	var command mongo.CommandError
	if errors.As(err, &command) {
		switch command.Code {
		case 13:
			return errors.New("MongoDB permissions insufficient")
		case 18:
			return errors.New("MongoDB authentication failed")
		}
	}
	return errors.New("MongoDB connection or configuration failed")
}

func openMongo(ctx context.Context, cfg *mongoConfig) (*mongo.Client, error) {
	if cfg == nil {
		return nil, nil
	}
	client, err := mongo.Connect(options.Client().ApplyURI(cfg.URI).SetServerSelectionTimeout(5 * time.Second).SetConnectTimeout(5 * time.Second))
	if err != nil {
		return nil, mongoFailure(err)
	}
	probe, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := client.Ping(probe, readpref.Primary()); err != nil {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer closeCancel()
		_ = client.Disconnect(closeCtx)
		return nil, mongoFailure(err)
	}
	return client, nil
}

func healthRoutes(ready func(context.Context) error) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte("{\"ok\":true}\n"))
	})
	mux.HandleFunc("GET /readyz", func(response http.ResponseWriter, request *http.Request) {
		ctx, cancel := context.WithTimeout(request.Context(), 2*time.Second)
		defer cancel()
		response.Header().Set("Content-Type", "application/json")
		if err := ready(ctx); err != nil {
			response.WriteHeader(http.StatusServiceUnavailable)
			_, _ = response.Write([]byte("{\"ok\":false}\n"))
			return
		}
		_, _ = response.Write([]byte("{\"ok\":true}\n"))
	})
	// Add business routes here. Persistent state belongs in the configured MongoDB;
	// requests may reach different production replicas.
	return mux
}

type authHTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

// Only the backend reads application credentials. Clients send a login code;
// they cannot select another application's ID or secret.
func applicationRoutes(cfg runtimeConfig, ready func(context.Context) error, client authHTTPClient, authBase string) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /healthz", healthRoutes(ready))
	mux.Handle("GET /readyz", healthRoutes(ready))
	mux.HandleFunc("OPTIONS /auth/v1/token", func(response http.ResponseWriter, _ *http.Request) {
		authCORS(response)
		response.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /auth/v1/token", func(response http.ResponseWriter, request *http.Request) {
		authCORS(response)
		if cfg.AppSecret == "" {
			authError(response, http.StatusServiceUnavailable, "后端应用凭据尚未配置")
			return
		}
		defer request.Body.Close()
		var input struct {
			Code string `json:"code"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(response, request.Body, 32<<10))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF {
			authError(response, http.StatusBadRequest, "认证请求格式无效")
			return
		}
		if strings.TrimSpace(input.Code) == "" || len(input.Code) > 4096 {
			authError(response, http.StatusBadRequest, "认证参数缺失或不适用于此接口")
			return
		}
		body := map[string]string{"appId": cfg.AppID, "appSecret": cfg.AppSecret, "code": input.Code, "grantType": "AUTHORIZATION_CODE"}
		encoded, _ := json.Marshal(body)
		upstream, err := http.NewRequestWithContext(request.Context(), http.MethodPost, authBase+"/token", bytes.NewReader(encoded))
		if err != nil {
			authError(response, http.StatusBadGateway, "认证服务请求失败")
			return
		}
		upstream.Header.Set("Content-Type", "application/json")
		result, err := client.Do(upstream)
		if err != nil {
			authError(response, http.StatusBadGateway, "认证服务暂不可用")
			return
		}
		defer result.Body.Close()
		raw, err := io.ReadAll(io.LimitReader(result.Body, (1<<20)+1))
		var payload any
		jsonDecoder := json.NewDecoder(bytes.NewReader(raw))
		jsonDecoder.UseNumber()
		if err != nil || len(raw) > 1<<20 || jsonDecoder.Decode(&payload) != nil || jsonDecoder.Decode(new(any)) != io.EOF {
			authError(response, http.StatusBadGateway, "认证服务响应无效")
			return
		}
		envelope, _ := payload.(map[string]any)
		if result.StatusCode >= http.StatusOK && result.StatusCode < http.StatusMultipleChoices && authCodeIsZero(envelope["code"]) && !validAuthSession(envelope["data"]) {
			authError(response, http.StatusBadGateway, "认证服务成功响应不完整")
			return
		}
		response.Header().Set("Content-Type", "application/json")
		response.Header().Set("Cache-Control", "no-store")
		response.WriteHeader(result.StatusCode)
		_ = json.NewEncoder(response).Encode(redactAuthResponse(payload, cfg))
	})
	return mux
}

func authCodeIsZero(value any) bool {
	switch code := value.(type) {
	case string:
		return code == "0"
	case json.Number:
		number, err := code.Float64()
		return err == nil && number == 0
	}
	return false
}

func validAuthSession(value any) bool {
	data, ok := value.(map[string]any)
	if !ok {
		return false
	}
	for _, key := range []string{"accessToken", "refreshToken", "openId"} {
		text, ok := data[key].(string)
		if !ok || strings.TrimSpace(text) == "" {
			return false
		}
	}
	var expiry string
	switch value := data["expiresAt"].(type) {
	case string:
		expiry = strings.TrimSpace(value)
	case json.Number:
		expiry = string(value)
	default:
		return false
	}
	seconds, err := strconv.ParseFloat(expiry, 64)
	return err == nil && !math.IsNaN(seconds) && !math.IsInf(seconds, 0) && seconds > 0 && seconds <= 9007199254740991 && math.Trunc(seconds) == seconds
}

func authCORS(response http.ResponseWriter) {
	response.Header().Set("Access-Control-Allow-Origin", "*")
	response.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	response.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, access-token, x-echo-qdmp-version")
	response.Header().Set("Cache-Control", "no-store")
}

func authError(response http.ResponseWriter, status int, message string) {
	response.Header().Set("Content-Type", "application/json")
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(map[string]any{"code": status, "message": message})
}

func redactAuthResponse(value any, cfg runtimeConfig) any {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if strings.EqualFold(key, "appSecret") || strings.EqualFold(key, "clientSecret") || (cfg.AppSecret != "" && strings.Contains(key, cfg.AppSecret)) {
				delete(typed, key)
			} else {
				typed[key] = redactAuthResponse(child, cfg)
			}
		}
	case []any:
		for index, child := range typed {
			typed[index] = redactAuthResponse(child, cfg)
		}
	case string:
		if cfg.AppSecret != "" {
			typed = strings.ReplaceAll(typed, cfg.AppSecret, "[REDACTED]")
		}
		return typed
	}
	return value
}

func newServer(handler http.Handler) *http.Server {
	return &http.Server{
		Addr: ":8080", Handler: handler,
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second,
		WriteTimeout: 30 * time.Second, IdleTimeout: 90 * time.Second,
	}
}

func serve(ctx context.Context, server *http.Server, listener net.Listener) error {
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return errors.New("HTTP server stopped unexpectedly")
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
		}
		<-done
		return nil
	}
}

func run(ctx context.Context) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	client, err := openMongo(ctx, cfg.MongoDB)
	if err != nil {
		return err
	}
	if client != nil {
		defer func() {
			closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = client.Disconnect(closeCtx)
		}()
	}
	ready := func(ctx context.Context) error {
		if client != nil {
			return client.Ping(ctx, readpref.Primary())
		}
		return nil
	}
	clientHTTP := &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	server := newServer(applicationRoutes(cfg, ready, clientHTTP, openAPIAuthBase))
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return errors.New("HTTP listener unavailable")
	}
	defer listener.Close()
	return serve(ctx, server, listener)
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		log.Fatal(err)
	}
}
