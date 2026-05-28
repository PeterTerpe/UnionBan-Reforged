package minecraft

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PeterTerpe/MeshBan/internal/config"
	"github.com/PeterTerpe/MeshBan/internal/database"
	"github.com/PeterTerpe/MeshBan/internal/secrets"
)

type UUIDResolver struct {
	config        config.MinecraftUUIDResolverConfig
	secretManager *secrets.Manager
	logger        *slog.Logger
}

type UUIDResolveResult struct {
	UUID   string
	Source string
}

func NewUUIDResolver(cfg config.MinecraftUUIDResolverConfig, secretManager *secrets.Manager, logger *slog.Logger) *UUIDResolver {
	return &UUIDResolver{
		config:        cfg,
		secretManager: secretManager,
		logger:        logger,
	}
}

func (r *UUIDResolver) Resolve(ctx context.Context, name string) (string, error) {
	result, err := r.ResolveWithSource(ctx, name)
	if err != nil {
		return "", err
	}

	return result.UUID, nil
}

func (r *UUIDResolver) ResolveWithSource(ctx context.Context, name string) (UUIDResolveResult, error) {
	if !r.config.Enabled {
		return UUIDResolveResult{}, errors.New("UUID resolver is disabled")
	}

	client, err := r.httpClient()
	if err != nil {
		return UUIDResolveResult{}, err
	}

	endpoint := strings.ReplaceAll(r.config.Endpoint, "{name}", url.PathEscape(name))
	timeout := time.Duration(r.config.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	attempts := r.config.RetryCount + 1
	if attempts < 1 {
		attempts = 1
	}

	var lastErr error

	for i := 0; i < attempts; i++ {
		requestCtx, cancel := context.WithTimeout(ctx, timeout)
		request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, endpoint, nil)
		if err != nil {
			cancel()
			return UUIDResolveResult{}, err
		}

		response, err := client.Do(request)
		if err != nil {
			cancel()
			lastErr = err
			continue
		}

		if response.StatusCode < 200 || response.StatusCode >= 300 {
			_ = response.Body.Close()
			cancel()
			lastErr = fmt.Errorf("UUID resolver returned status %d", response.StatusCode)
			continue
		}

		var payload map[string]any
		decodeErr := json.NewDecoder(response.Body).Decode(&payload)
		closeErr := response.Body.Close()
		cancel()

		if decodeErr != nil {
			lastErr = decodeErr
			continue
		}

		if closeErr != nil {
			lastErr = closeErr
			continue
		}

		field := strings.TrimSpace(r.config.ResponseUUIDField)
		if field == "" {
			field = "id"
		}

		value, ok := payload[field].(string)
		if !ok || strings.TrimSpace(value) == "" {
			lastErr = fmt.Errorf("UUID resolver response field %q is missing", field)
			continue
		}

		return UUIDResolveResult{
			UUID:   database.NormalizePlayerUUID(value),
			Source: endpoint,
		}, nil
	}

	if lastErr == nil {
		lastErr = errors.New("UUID resolver failed")
	}

	return UUIDResolveResult{}, lastErr
}

func (r *UUIDResolver) httpClient() (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()

	switch strings.ToLower(strings.TrimSpace(r.config.ProxyType)) {
	case "", "none":
		transport.Proxy = nil
	case "environment":
		transport.Proxy = http.ProxyFromEnvironment
	case "http", "https":
		proxyURL := strings.TrimSpace(r.secretOrLiteral(firstNonEmpty(r.config.ProxyURLEnv, r.config.ProxyURL)))
		if proxyURL == "" {
			return nil, errors.New("proxy URL is required")
		}

		parsedProxyURL, err := url.Parse(proxyURL)
		if err != nil {
			return nil, err
		}

		if r.config.ProxyAuth {
			username := r.secretValue(r.config.ProxyUsernameEnv)
			password := r.secretValue(r.config.ProxyPassEnv)
			parsedProxyURL.User = url.UserPassword(username, password)
		}

		transport.Proxy = http.ProxyURL(parsedProxyURL)
	case "socks5":
		if r.logger != nil {
			r.logger.Warn("SOCKS5 proxy is not supported by the built-in UUID resolver")
		}

		return nil, errors.New("SOCKS5 proxy is not supported")
	default:
		return nil, fmt.Errorf("unsupported proxy type %q", r.config.ProxyType)
	}

	return &http.Client{Transport: transport}, nil
}

func (r *UUIDResolver) secretValue(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}

	if r.secretManager != nil {
		return strings.TrimSpace(r.secretManager.Get(key))
	}

	return strings.TrimSpace(key)
}

func (r *UUIDResolver) secretOrLiteral(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}

	if r.secretManager == nil {
		return value
	}

	if secretValue := strings.TrimSpace(r.secretManager.Get(value)); secretValue != "" {
		return secretValue
	}

	return value
}
