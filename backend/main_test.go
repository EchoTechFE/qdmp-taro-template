package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/iotest"
	"time"
)

type authClientFunc func(*http.Request) (*http.Response, error)

func (client authClientFunc) Do(request *http.Request) (*http.Response, error) {
	return client(request)
}

func authTestConfig() runtimeConfig {
	return runtimeConfig{AppID: "server-app", AppSecret: "server-private-app-secret", Runtime: "go"}
}

func TestApplicationRoutesForwardOnlyServerCredentialsAndLoginCode(t *testing.T) {
	cfg := authTestConfig()
	observed := make(chan map[string]string, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/auth/v1/token" || request.Header.Get("Content-Type") != "application/json" {
			t.Error("proxy sent the wrong upstream operation or media type")
		}
		var body map[string]string
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Error("upstream request was not valid JSON")
		}
		observed <- body
		response.Header().Set("Content-Type", "application/json")
		response.Header().Set("Set-Cookie", "upstream-private-cookie=must-not-forward")
		_, _ = io.WriteString(response, `{"code":0,"data":{"accessToken":"new-user-access-token","refreshToken":"new-user-refresh-token","openId":"user-open-id","expiresAt":1893456000,"userId":9223372036854775807}}`)
	}))
	defer upstream.Close()
	handler := applicationRoutes(cfg, func(context.Context) error {
		t.Error("login proxy must not use database readiness as its authentication implementation")
		return nil
	}, upstream.Client(), upstream.URL+"/auth/v1")
	request := httptest.NewRequest(http.MethodPost, "/auth/v1/token", strings.NewReader(`{"code":"native-login-code"}`))
	request.Header.Set("AppSecret", "client-controlled-header")
	result := httptest.NewRecorder()
	handler.ServeHTTP(result, request)
	if result.Code != http.StatusOK || !strings.Contains(result.Body.String(), "new-user-access-token") || !strings.Contains(result.Body.String(), "9223372036854775807") {
		t.Fatalf("valid token response was not retained: %d %s", result.Code, result.Body.String())
	}
	want := map[string]string{"appId": cfg.AppID, "appSecret": cfg.AppSecret, "code": "native-login-code", "grantType": "AUTHORIZATION_CODE"}
	if got := <-observed; !reflect.DeepEqual(got, want) {
		t.Fatalf("upstream authentication fields differ: got %v", got)
	}
	if result.Header().Get("Set-Cookie") != "" || result.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("token proxy must not forward upstream cookies or permit response caching")
	}
}

func TestApplicationRoutesKeepCompleteTokenSessionsAndOriginalJSONTypes(t *testing.T) {
	for _, body := range []string{
		`{"code":"0","data":{"accessToken":"user-access-token","refreshToken":"user-refresh-token","openId":"user-open-id","expiresAt":"1893456000","userId":9223372036854775807}}`,
		`{"code":0.0,"data":{"accessToken":"user-access-token","refreshToken":"user-refresh-token","openId":"user-open-id","expiresAt":1893456000.0,"userId":9223372036854775807}}`,
	} {
		calls := 0
		client := authClientFunc(func(request *http.Request) (*http.Response, error) {
			calls++
			if request.URL.Path != "/auth/v1/token" {
				t.Error("login must not query another identity endpoint")
			}
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
		})
		result := httptest.NewRecorder()
		applicationRoutes(authTestConfig(), func(context.Context) error { return nil }, client, "https://upstream.invalid/auth/v1").ServeHTTP(result,
			httptest.NewRequest(http.MethodPost, "/auth/v1/token", strings.NewReader(`{"code":"native-code"}`)))
		var want, got any
		for _, item := range []struct {
			body   string
			target *any
		}{{body, &want}, {result.Body.String(), &got}} {
			decoder := json.NewDecoder(strings.NewReader(item.body))
			decoder.UseNumber()
			if err := decoder.Decode(item.target); err != nil {
				t.Fatal(err)
			}
		}
		if result.Code != http.StatusOK || !reflect.DeepEqual(got, want) || calls != 1 {
			t.Fatal("complete token session or original numeric values changed, or login code was replayed")
		}
	}
}

func TestApplicationRoutesRejectIncompleteSuccessfulSessionsWithoutReplayingLoginCode(t *testing.T) {
	cfg := authTestConfig()
	for _, item := range []struct {
		name, field string
		value       any
		omit        bool
	}{
		{"missing-open-id", "openId", nil, true},
		{"empty-open-id", "openId", "", false},
		{"whitespace-open-id", "openId", " ", false},
		{"non-string-open-id", "openId", json.Number("123"), false},
		{"missing-access-token", "accessToken", nil, true},
		{"empty-access-token", "accessToken", "", false},
		{"missing-refresh-token", "refreshToken", nil, true},
		{"empty-refresh-token", "refreshToken", " ", false},
		{"missing-expiry", "expiresAt", nil, true},
		{"zero-expiry", "expiresAt", json.Number("0"), false},
		{"negative-expiry", "expiresAt", json.Number("-1"), false},
		{"fractional-expiry", "expiresAt", json.Number("1.5"), false},
		{"unsafe-expiry", "expiresAt", json.Number("9007199254740992"), false},
		{"overflow-expiry", "expiresAt", json.Number("1e400"), false},
		{"invalid-expiry-string", "expiresAt", "not-a-timestamp", false},
		{"boolean-expiry", "expiresAt", true, false},
		{"null-expiry", "expiresAt", nil, false},
	} {
		t.Run(item.name, func(t *testing.T) {
			data := map[string]any{"accessToken": "user-access-token", "refreshToken": "user-refresh-token", "openId": "user-open-id", "expiresAt": json.Number("1893456000")}
			if item.omit {
				delete(data, item.field)
			} else {
				data[item.field] = item.value
			}
			body, err := json.Marshal(map[string]any{"code": 0, "data": data, "appSecret": cfg.AppSecret, "message": "upstream detail " + cfg.AppSecret})
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			client := authClientFunc(func(request *http.Request) (*http.Response, error) {
				calls++
				if request.URL.Path != "/auth/v1/token" {
					t.Error("incomplete login must not query another identity endpoint")
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(body))}, nil
			})
			result := httptest.NewRecorder()
			applicationRoutes(cfg, func(context.Context) error { return nil }, client, "https://upstream.invalid/auth/v1").ServeHTTP(result,
				httptest.NewRequest(http.MethodPost, "/auth/v1/token", strings.NewReader(`{"code":"native-code"}`)))
			if result.Code != http.StatusBadGateway || calls != 1 || strings.Contains(result.Body.String(), cfg.AppSecret) || strings.Contains(result.Body.String(), "user-access-token") {
				t.Fatal("incomplete success must return a sanitized 502 without a second upstream call")
			}
			if result.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("incomplete token response must not be cached")
			}
		})
	}
}

func TestApplicationRoutesRetainHTTPAndBusinessFailuresEvenWithIncompleteSessionData(t *testing.T) {
	cfg := authTestConfig()
	for _, item := range []struct {
		name   string
		status int
		code   any
	}{
		{"http-401-with-zero-code", http.StatusUnauthorized, 0},
		{"http-500-with-zero-code", http.StatusInternalServerError, "0"},
		{"business-error", http.StatusOK, 10002},
	} {
		t.Run(item.name, func(t *testing.T) {
			calls := 0
			body, err := json.Marshal(map[string]any{"code": item.code, "data": map[string]any{}, "message": "upstream failure " + cfg.AppSecret, "appSecret": cfg.AppSecret})
			if err != nil {
				t.Fatal(err)
			}
			client := authClientFunc(func(request *http.Request) (*http.Response, error) {
				calls++
				if request.URL.Path != "/auth/v1/token" {
					t.Error("failed login must not query another identity endpoint")
				}
				return &http.Response{StatusCode: item.status, Body: io.NopCloser(bytes.NewReader(body))}, nil
			})
			result := httptest.NewRecorder()
			applicationRoutes(cfg, func(context.Context) error { return nil }, client, "https://upstream.invalid/auth/v1").ServeHTTP(result,
				httptest.NewRequest(http.MethodPost, "/auth/v1/token", strings.NewReader(`{"code":"native-code"}`)))
			var got map[string]any
			if err := json.Unmarshal(result.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			wantCode, _ := json.Marshal(item.code)
			gotCode, _ := json.Marshal(got["code"])
			if result.Code != item.status || string(gotCode) != string(wantCode) || calls != 1 || strings.Contains(result.Body.String(), cfg.AppSecret) {
				t.Fatal("failure evidence changed, leaked credentials, or replayed the login code")
			}
		})
	}
}

func TestApplicationRoutesRejectClientCredentialsAndInvalidInputsBeforeUpstream(t *testing.T) {
	client := authClientFunc(func(*http.Request) (*http.Response, error) {
		t.Error("invalid client input reached the upstream service")
		return nil, errors.New("must not execute")
	})
	handler := applicationRoutes(authTestConfig(), func(context.Context) error { return nil }, client, "https://upstream.invalid/auth/v1")
	for _, body := range []string{
		`{"code":"native-code","appId":"another-app"}`,
		`{"code":"native-code","appSecret":"client-secret"}`,
		`{"code":"native-code","grantType":"CLIENT_CREDENTIALS"}`,
		`{"refreshToken":"wrong-operation"}`,
		`{"code":"native-code","refreshToken":"mixed-operation"}`,
		`{"code":"   "}`,
		`{}`,
		`{"code":"native-code"}{}`,
		`null`,
		`{"code":`,
		`{"code":"` + strings.Repeat("c", 4097) + `"}`,
		`{"code":"` + strings.Repeat("c", 33<<10) + `"}`,
	} {
		result := httptest.NewRecorder()
		handler.ServeHTTP(result, httptest.NewRequest(http.MethodPost, "/auth/v1/token", strings.NewReader(body)))
		if result.Code != http.StatusBadRequest {
			t.Errorf("invalid token request was accepted with status %d", result.Code)
		}
		if result.Header().Get("Access-Control-Allow-Origin") != "*" || result.Header().Get("Cache-Control") != "no-store" {
			t.Error("authentication errors must retain CORS and no-store headers")
		}
	}
}

func TestApplicationRoutesMissingSecretFailsClosedWhilePreflightRemainsUsable(t *testing.T) {
	client := authClientFunc(func(*http.Request) (*http.Response, error) {
		t.Error("missing credentials or CORS preflight reached the upstream service")
		return nil, errors.New("must not execute")
	})
	handler := applicationRoutes(runtimeConfig{AppID: "server-app", Runtime: "go"}, func(context.Context) error { return nil }, client, "https://upstream.invalid/auth/v1")
	preflight := httptest.NewRequest(http.MethodOptions, "/auth/v1/token", nil)
	preflight.Header.Set("Origin", "https://miniapp.example")
	preflight.Header.Set("Access-Control-Request-Method", "POST")
	preflight.Header.Set("Access-Control-Request-Headers", "content-type,access-token,x-echo-qdmp-version")
	result := httptest.NewRecorder()
	handler.ServeHTTP(result, preflight)
	if result.Code != http.StatusNoContent || result.Body.Len() != 0 || result.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatal("CORS preflight did not allow the frontend's token request")
	}
	for _, method := range []string{"POST", "OPTIONS"} {
		if !strings.Contains(result.Header().Get("Access-Control-Allow-Methods"), method) {
			t.Errorf("CORS preflight omitted %s", method)
		}
	}
	for _, header := range []string{"content-type", "authorization", "access-token", "x-echo-qdmp-version"} {
		if !strings.Contains(strings.ToLower(result.Header().Get("Access-Control-Allow-Headers")), header) {
			t.Errorf("CORS preflight omitted frontend header %s", header)
		}
	}
	if result.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Fatal("wildcard CORS must not enable credential cookies")
	}
	result = httptest.NewRecorder()
	handler.ServeHTTP(result, httptest.NewRequest(http.MethodPost, "/auth/v1/token", strings.NewReader(`{"code":"native-code"}`)))
	if result.Code != http.StatusServiceUnavailable || strings.Contains(result.Body.String(), "native-code") {
		t.Fatal("missing backend credentials must report 503 without forwarding client data")
	}
}

func TestApplicationRoutesDoNotExposeRefreshProxy(t *testing.T) {
	client := authClientFunc(func(*http.Request) (*http.Response, error) {
		t.Error("removed refresh route reached the upstream service")
		return nil, errors.New("must not execute")
	})
	handler := applicationRoutes(authTestConfig(), func(context.Context) error { return nil }, client, "https://upstream.invalid/auth/v1")
	for _, method := range []string{http.MethodPost, http.MethodOptions} {
		result := httptest.NewRecorder()
		handler.ServeHTTP(result, httptest.NewRequest(method, "/auth/v1/refresh", strings.NewReader(`{"refreshToken":"user-refresh-token"}`)))
		if result.Code != http.StatusNotFound {
			t.Errorf("removed refresh route returned status %d for %s", result.Code, method)
		}
	}
}

func TestApplicationRoutesInvalidUpstreamResponsesNeverExposeSecretData(t *testing.T) {
	cfg := authTestConfig()
	for _, body := range []string{
		"<html>" + cfg.AppSecret + "</html>",
		`{"message":"` + cfg.AppSecret + `"} {"extra":true}`,
		`{"message":"` + cfg.AppSecret + strings.Repeat("x", 1<<20) + `"}`,
	} {
		upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(response, body)
		}))
		handler := applicationRoutes(cfg, func(context.Context) error { return nil }, upstream.Client(), upstream.URL)
		result := httptest.NewRecorder()
		handler.ServeHTTP(result, httptest.NewRequest(http.MethodPost, "/auth/v1/token", strings.NewReader(`{"code":"native-code"}`)))
		upstream.Close()
		if result.Code != http.StatusBadGateway || strings.Contains(result.Body.String(), cfg.AppSecret) {
			t.Fatal("invalid upstream response must fail with a sanitized 502")
		}
	}
	for _, client := range []authClientFunc{
		func(*http.Request) (*http.Response, error) {
			return nil, errors.New("transport failed with " + cfg.AppSecret)
		},
		func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(iotest.ErrReader(errors.New("read failed with " + cfg.AppSecret)))}, nil
		},
	} {
		result := httptest.NewRecorder()
		applicationRoutes(cfg, func(context.Context) error { return nil }, client, "https://upstream.invalid/auth/v1").ServeHTTP(result,
			httptest.NewRequest(http.MethodPost, "/auth/v1/token", strings.NewReader(`{"code":"native-code"}`)))
		if result.Code != http.StatusBadGateway || strings.Contains(result.Body.String(), cfg.AppSecret) {
			t.Fatal("upstream transport/read failure disclosed internal credentials")
		}
	}
}

func TestApplicationRoutesSanitizeNestedSecretsWhileRetainingBusinessFailureAndUserTokens(t *testing.T) {
	cfg := authTestConfig()
	upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(response).Encode(map[string]any{
			"code": 10002, "appSecret": "another-upstream-secret", "message": "upstream rejected " + cfg.AppSecret,
			"diagnostic-" + cfg.AppSecret: "upstream used a credential as an object key",
			"data":                        map[string]any{"accessToken": "user-access-token", "nested": []any{map[string]any{"CLIENTSECRET": "nested-upstream-secret", "detail": cfg.AppSecret}}},
		})
	}))
	defer upstream.Close()
	result := httptest.NewRecorder()
	applicationRoutes(cfg, func(context.Context) error { return nil }, upstream.Client(), upstream.URL).ServeHTTP(result,
		httptest.NewRequest(http.MethodPost, "/auth/v1/token", strings.NewReader(`{"code":"native-code"}`)))
	if result.Code != http.StatusUnauthorized || !strings.Contains(result.Body.String(), `"code":10002`) || !strings.Contains(result.Body.String(), "user-access-token") {
		t.Fatal("proxy changed the upstream business failure or removed user token fields")
	}
	for _, secret := range []string{cfg.AppSecret, "another-upstream-secret", "nested-upstream-secret", "appSecret", "CLIENTSECRET"} {
		if strings.Contains(result.Body.String(), secret) {
			t.Fatal("nested authentication response disclosed an application secret")
		}
	}
}

func TestProductionConfig(t *testing.T) {
	for _, source := range []string{
		`{"appId":"test-app","runtime":"go"}`,
		`{"appId":"test-app","runtime":"go","appSecret":"","loader":"EMP","variables":{},"mongodb":{"uri":"","database":""}}`,
		`{"appId":"test-app","runtime":"go","mongodb":null}`,
		`{"appId":"test-app","runtime":"go","mongodb":{"uri":"mongodb://localhost:27017","database":"test-app"}}`,
	} {
		if _, err := parseConfig([]byte(source)); err != nil {
			t.Fatalf("valid production configuration rejected: %v", err)
		}
	}
	for _, source := range []string{
		`{"appId":"bad/path","runtime":"go"}`,
		`{"appId":"YOUR_APP_ID","runtime":"go"}`,
		`{"appId":"test-app","runtime":"nodejs22"}`,
		`{"appId":"test-app","runtime":"go","environment":"other"}`,
		`{"appId":"test-app","runtime":"go","variables":null}`,
		`{"appId":"test-app","runtime":"go","variables":[]}`,
		`{"appId":"test-app","runtime":"go","variables":{"limit":1e400}}`,
		`{"appId":"test-app","runtime":"go","variables":{"id":9007199254740993}}`,
		`{"appId":"test-app","runtime":"go","appSecret":null}`,
		`{"appId":"test-app","runtime":"go","environment":null}`,
		`{"appId":"test-app","runtime":"go","mongodb":{"uri":null,"database":null}}`,
		`{"appId":"test-app","runtime":"go","appSecret":"YOUR_APP_SECRET"}`,
		`{"appId":"test-app","runtime":"go","mongodb":{"uri":"","database":"test-app"}}`,
		`{"appId":"test-app","runtime":"go","mongodb":{"uri":"mongodb://host:27017","database":""}}`,
		`{"appId":"test-app","runtime":"go","mongodb":{"uri":"mongodb://user:private-password@private-host","database":"other-app"}}`,
		`{"appId":"test-app","runtime":"go","mongodb":{"uri":"https://user:private-password@private-host","database":"test-app"}}`,
		`{"appSecret":"private-password",`,
	} {
		_, err := parseConfig([]byte(source))
		if err == nil {
			t.Fatalf("invalid configuration accepted: %s", source)
		}
		if strings.Contains(err.Error(), "private-password") || strings.Contains(err.Error(), "private-host") {
			t.Fatal("configuration error disclosed secret configuration")
		}
	}
}

func TestProjectVariablesAndEmptyMongo(t *testing.T) {
	cfg, err := parseConfig([]byte(`{"appId":"test-app","runtime":"go","appSecret":"","loader":"EMP","variables":{"feature":{"enabled":true},"maxItems":25,"ratio":1.5,"recordId":"9223372036854775807"},"mongodb":{"uri":"","database":""}}`))
	if err != nil || cfg.MongoDB != nil {
		t.Fatalf("empty optional MongoDB must leave the service stateless: %v", err)
	}
	if string(cfg.Variables["maxItems"]) != "25" || string(cfg.Variables["recordId"]) != `"9223372036854775807"` || string(cfg.Variables["feature"]) != `{"enabled":true}` {
		t.Fatal("project variables must retain their JSON values")
	}
}

func TestConfigSources(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "qdmp-config.json")
	if err := os.WriteFile(configPath, []byte(`{"appId":"file-app","runtime":"go"}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("QDMP_CONFIG", configPath)
	previous := embeddedProductionConfig
	t.Cleanup(func() { embeddedProductionConfig = previous })
	embeddedProductionConfig = ""
	cfg, err := loadConfig()
	if err != nil || cfg.AppID != "file-app" {
		t.Fatalf("explicit configuration path was not read: %v", err)
	}
	embeddedProductionConfig = `{"appId":"embedded-app","runtime":"go"}`
	cfg, err = loadConfig()
	if err != nil || cfg.AppID != "embedded-app" {
		t.Fatalf("embedded configuration must take precedence: %v", err)
	}
	embeddedProductionConfig = "invalid"
	if _, err := loadConfig(); err == nil {
		t.Fatal("invalid embedded configuration must not fall back to a local file")
	}
	embeddedProductionConfig = ""
	t.Setenv("QDMP_CONFIG", "")
	backend := filepath.Join(root, "backend")
	if err := os.Mkdir(backend, 0700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(backend)
	if cfg, err := loadConfig(); err != nil || cfg.AppID != "file-app" {
		t.Fatalf("default parent configuration path was not read: %v", err)
	}
}

func TestMongoURINormalizationPreservesAuthenticationDatabase(t *testing.T) {
	for _, item := range []struct{ input, expected string }{
		{"mongodb://host:27017?retryWrites=true", "mongodb://host:27017/test-app?retryWrites=true"},
		{"mongodb://user:secret@host:27017?retryWrites=true", "mongodb://user:secret@host:27017/?retryWrites=true"},
		{"mongodb://user:secret@host:27017?authSource=admin", "mongodb://user:secret@host:27017/test-app?authSource=admin"},
		{"mongodb://user:secret@host:27017/admin?retryWrites=true", "mongodb://user:secret@host:27017/admin?retryWrites=true"},
	} {
		if got := normalizeMongoURI(item.input, "test-app"); got != item.expected {
			t.Fatal("MongoDB URI normalization changed authentication semantics")
		}
	}
}

func TestReadinessDoesNotExposeDependencies(t *testing.T) {
	probed := false
	handler := healthRoutes(func(ctx context.Context) error {
		probed = true
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 2*time.Second {
			t.Error("readiness probe must have a two-second deadline")
		}
		return errors.New("mongodb://user:private-password@private-host")
	})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", "/healthz", nil))
	if response.Code != 200 || probed {
		t.Fatal("liveness must not query MongoDB")
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", "/readyz", nil))
	if response.Code != 503 || response.Body.String() != "{\"ok\":false}\n" || !probed {
		t.Fatalf("readiness must fail without exposing driver details: %d %q", response.Code, response.Body.String())
	}
	response = httptest.NewRecorder()
	healthRoutes(func(context.Context) error { return nil }).ServeHTTP(response, httptest.NewRequest("GET", "/readyz", nil))
	if response.Code != 200 || response.Body.String() != "{\"ok\":true}\n" {
		t.Fatal("ready service must report success")
	}
}

func TestOptionalMongoAndSafeStartupErrors(t *testing.T) {
	if client, err := openMongo(context.Background(), nil); client != nil || err != nil {
		t.Fatal("services without persistence must not connect to MongoDB")
	}
	_, err := openMongo(context.Background(), &mongoConfig{URI: "mongodb://private-password@invalid host", Database: "test-app"})
	if err == nil || strings.Contains(err.Error(), "private-password") || strings.Contains(err.Error(), "invalid host") {
		t.Fatal("driver parsing failures must be sanitized")
	}
}

func TestGracefulShutdownDrainsActiveRequest(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	started, release := make(chan struct{}), make(chan struct{})
	server := newServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		_, _ = response.Write([]byte("finished"))
	}))
	if server.Addr != ":8080" || server.ReadHeaderTimeout == 0 || server.ReadTimeout == 0 || server.IdleTimeout == 0 {
		t.Fatal("production listener and HTTP deadlines must be configured")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- serve(ctx, server, listener) }()
	responseDone := make(chan error, 1)
	go func() {
		client := &http.Client{Timeout: 3 * time.Second}
		response, err := client.Get("http://" + listener.Addr().String())
		if err != nil {
			responseDone <- err
			return
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		if err == nil && string(body) != "finished" {
			err = errors.New("active response was truncated")
		}
		responseDone <- err
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("request did not reach the actual listener")
	}
	cancel()
	select {
	case <-done:
		t.Fatal("shutdown discarded an active request")
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	select {
	case err := <-responseDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("active request did not complete")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("server did not stop after draining")
	}
}
