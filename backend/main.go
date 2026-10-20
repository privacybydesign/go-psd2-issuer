package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"psd2-issuer/credentials"
	"psd2-issuer/enablenow"
	"psd2-issuer/logging"
	rate "psd2-issuer/rate_limiter"
	redis "psd2-issuer/redis"
	"strings"
	"time"
)

type Config struct {
	ServerConfig ServerConfig `json:"server_config"`

	// LogLevel accepts "debug", "info", "warn" and "error".
	LogLevel          slog.Level `json:"log_level"`
	JwtPrivateKeyPath string     `json:"jwt_private_key_path"`
	IrmaServerUrl     string     `json:"irma_server_url"`
	IssuerId          string     `json:"issuer_id"`

	// PublicUrl is the externally reachable base URL of this issuer. The
	// AppScreen redirects back to <public_url>/callback, which must be
	// registered as redirect URI in the enablenow Management Portal.
	PublicUrl string `json:"public_url"`

	// CredentialPrefix is prepended to the credential keys (income, housing,
	// debts, benefits, bankaccount) to form the Yivi credential ids, e.g.
	// "irma-demo.enablenow". Entries in Credentials override single ids.
	CredentialPrefix string            `json:"credential_prefix"`
	Credentials      map[string]string `json:"credentials,omitempty"`
	// CredentialValidityDays is how long issued credentials stay valid.
	CredentialValidityDays int `json:"credential_validity_days,omitempty"`

	// EnablenowBackend is "enablenow" for the real API or "fake" for local
	// development with synthetic data.
	EnablenowBackend string             `json:"enablenow_backend"`
	EnablenowConfig  enablenow.Config   `json:"enablenow_config"`
	Categorization   CategorizationConf `json:"categorization"`

	StorageType         string                    `json:"storage_type"`
	RedisConfig         redis.RedisConfig         `json:"redis_config"`
	RedisSentinelConfig redis.RedisSentinelConfig `json:"redis_sentinel_config"`

	// TrustedProxies lists CIDR ranges of reverse proxies allowed to set the
	// X-Real-IP header.
	TrustedProxies []string `json:"trusted_proxies"`

	// DemoVerifier enables the demo verifier at /<lang>/verifier.
	DemoVerifier DemoVerifierConfig `json:"demo_verifier"`
}

type CategorizationConf struct {
	// Profile is the E1 taxonomy: "retail" (default) or "corporate".
	Profile string `json:"profile,omitempty"`
	// CountryCode is "nl" (default) or "be".
	CountryCode string `json:"country_code,omitempty"`
	// Months is the analysis window in full calendar months (default 12).
	Months int `json:"months,omitempty"`
	// Rounding rounds amounts to a multiple of this many euros (default 10).
	Rounding int `json:"rounding,omitempty"`
	// MinConfidence ignores less confident categorizations (default 0).
	MinConfidence float64 `json:"min_confidence,omitempty"`
	// KeepConsent keeps the consent at enablenow after the credentials are
	// computed. By default it is deleted.
	KeepConsent bool `json:"keep_consent,omitempty"`
}

func main() {
	configPath := flag.String("config", "", "Path for the config.json to use")
	flag.Parse()

	if *configPath == "" {
		slog.Error("please provide a config path using the --config flag")
		os.Exit(1)
	}

	config, err := readConfigFile(*configPath)
	if err != nil {
		slog.Error("failed to read config file", "error", err)
		os.Exit(1)
	}

	logging.InitLogger(config.LogLevel)

	slog.Info("using config", "path", *configPath)
	slog.Info("hosting on", "host", config.ServerConfig.Host, "port", config.ServerConfig.Port)

	state, err := createServerState(&config)
	if err != nil {
		slog.Error("failed to initialize", "error", err)
		os.Exit(1)
	}

	server, err := NewServer(state, config.ServerConfig)
	if err != nil {
		slog.Error("failed to create server", "error", err)
		os.Exit(1)
	}

	err = server.ListenAndServe()
	if err != nil {
		slog.Error("failed to listen and serve", "error", err)
		os.Exit(1)
	}
}

func createServerState(config *Config) (*ServerState, error) {
	if config.PublicUrl == "" {
		return nil, errors.New("public_url must be set")
	}

	validityDays := config.CredentialValidityDays
	if validityDays == 0 {
		validityDays = 90
	}
	credentialIds, err := credentialIds(config)
	if err != nil {
		return nil, err
	}
	jwtCreator, err := NewIrmaJwtCreator(
		config.JwtPrivateKeyPath,
		config.IssuerId,
		credentialIds,
		time.Duration(validityDays)*24*time.Hour,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to instantiate jwt creator: %w", err)
	}

	client, err := createEnablenowClient(config)
	if err != nil {
		return nil, fmt.Errorf("failed to instantiate enablenow client: %w", err)
	}

	sessionStorage, err := createSessionStorage(config)
	if err != nil {
		return nil, fmt.Errorf("failed to instantiate session storage: %w", err)
	}

	startRateLimiter, err := createStartRateLimiter(config)
	if err != nil {
		return nil, fmt.Errorf("failed to instantiate rate limiter: %w", err)
	}

	trustedProxies, err := parseTrustedProxies(config.TrustedProxies)
	if err != nil {
		return nil, fmt.Errorf("failed to parse trusted_proxies: %w", err)
	}

	var verifier *Verifier
	if config.DemoVerifier.Enabled {
		irmaServerUrl := config.DemoVerifier.IrmaServerUrl
		if irmaServerUrl == "" {
			irmaServerUrl = config.IrmaServerUrl
		}
		verifier, err = NewVerifier(irmaServerUrl, jwtCreator, credentialIds, config.DemoVerifier.DateOfBirthAttribute, config.DemoVerifier.PostcodeAttribute)
		if err != nil {
			return nil, fmt.Errorf("failed to instantiate demo verifier: %w", err)
		}
		slog.Info("demo verifier enabled", "irma_server_url", irmaServerUrl)
	}

	return &ServerState{
		irmaServerURL:    config.IrmaServerUrl,
		publicURL:        config.PublicUrl,
		secureCookies:    strings.HasPrefix(config.PublicUrl, "https://"),
		enablenow:        client,
		flow:             NewFlow(flowConfig(config), client),
		sessionStorage:   sessionStorage,
		jwtCreator:       jwtCreator,
		startRateLimiter: startRateLimiter,
		trustedProxies:   trustedProxies,
		verifier:         verifier,
	}, nil
}

func flowConfig(config *Config) FlowConfig {
	c := config.Categorization
	fc := FlowConfig{
		Engine:             "e1",
		Profile:            c.Profile,
		CountryCode:        c.CountryCode,
		Months:             c.Months,
		Rounding:           c.Rounding,
		MinConfidence:      c.MinConfidence,
		DeleteConsent:      !c.KeepConsent,
		PollInterval:       2 * time.Second,
		StepTimeout:        10 * time.Minute,
		MaxTransientErrors: 5,
	}
	if fc.Profile == "" {
		fc.Profile = "retail"
	}
	if fc.CountryCode == "" {
		fc.CountryCode = "nl"
	}
	if fc.Months == 0 {
		fc.Months = 12
	}
	if fc.Rounding == 0 {
		fc.Rounding = 10
	}
	return fc
}

// credentialIds maps every credential key to its full Yivi credential id.
func credentialIds(config *Config) (map[string]string, error) {
	ids := map[string]string{}
	for _, key := range credentials.AllKeys {
		if id, ok := config.Credentials[key]; ok {
			ids[key] = id
		} else if config.CredentialPrefix != "" {
			ids[key] = config.CredentialPrefix + "." + key
		} else {
			return nil, fmt.Errorf("no credential id for %q: set credential_prefix or credentials.%s", key, key)
		}
		if strings.Count(ids[key], ".") != 2 {
			return nil, fmt.Errorf("credential id %q for %q is not of the form scheme.issuer.credential", ids[key], key)
		}
	}
	return ids, nil
}

func createEnablenowClient(config *Config) (enablenow.Client, error) {
	switch config.EnablenowBackend {
	case "enablenow":
		slog.Info("using enablenow backend")
		return enablenow.NewHttpClient(config.EnablenowConfig)
	case "fake":
		slog.Warn("using FAKE enablenow backend with synthetic data, never use this in production")
		return enablenow.NewFakeClient(), nil
	}
	return nil, fmt.Errorf("invalid enablenow_backend: %q", config.EnablenowBackend)
}

func createSessionStorage(config *Config) (SessionStorage, error) {
	switch config.StorageType {
	case "redis":
		slog.Info("Using redis session storage")
		client, err := redis.NewRedisClient(&config.RedisConfig)
		if err != nil {
			return nil, err
		}
		return NewRedisSessionStorage(client, config.RedisConfig.Namespace), nil
	case "redis_sentinel":
		slog.Info("Using redis sentinel session storage")
		client, err := redis.NewRedisSentinelClient(&config.RedisSentinelConfig)
		if err != nil {
			return nil, err
		}
		return NewRedisSessionStorage(client, config.RedisSentinelConfig.Namespace), nil
	case "memory":
		slog.Info("Using in memory session storage")
		return NewInMemorySessionStorage(), nil
	}
	return nil, fmt.Errorf("%v is not a valid storage type", config.StorageType)
}

func createStartRateLimiter(config *Config) (rate.RateLimiter, error) {
	policy := rate.RateLimitingPolicy{
		Limit:  20,
		Window: 30 * time.Minute,
	}

	switch config.StorageType {
	case "redis":
		client, err := redis.NewRedisClient(&config.RedisConfig)
		if err != nil {
			return nil, err
		}
		return rate.NewRedisRateLimiter(client, config.RedisConfig.Namespace+":start", policy), nil
	case "redis_sentinel":
		client, err := redis.NewRedisSentinelClient(&config.RedisSentinelConfig)
		if err != nil {
			return nil, err
		}
		return rate.NewRedisRateLimiter(client, config.RedisSentinelConfig.Namespace+":start", policy), nil
	case "memory":
		return rate.NewInMemoryRateLimiter(rate.NewSystemClock(), policy), nil
	}
	return nil, errors.New("no valid storage type was set")
}

// parseTrustedProxies parses a list of CIDR strings into IP networks. A bare
// IP address (without a mask) is accepted and treated as a single-host range.
func parseTrustedProxies(cidrs []string) ([]*net.IPNet, error) {
	nets := make([]*net.IPNet, 0, len(cidrs))
	for _, raw := range cidrs {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			continue
		}
		if !strings.Contains(entry, "/") {
			if ip := net.ParseIP(entry); ip != nil {
				if ip.To4() != nil {
					entry += "/32"
				} else {
					entry += "/128"
				}
			}
		}
		_, network, err := net.ParseCIDR(entry)
		if err != nil {
			return nil, fmt.Errorf("invalid trusted proxy CIDR %q: %w", raw, err)
		}
		nets = append(nets, network)
	}
	return nets, nil
}

func readConfigFile(path string) (Config, error) {
	configBytes, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}

	var config Config
	if err := json.Unmarshal(configBytes, &config); err != nil {
		return Config{}, err
	}

	return config, nil
}
