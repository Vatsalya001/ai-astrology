package config

import (
	"strings"
	"testing"
	"time"
)

// validEnv is the minimum set of variables required to start.
func validEnv() map[string]string {
	return map[string]string{
		"DATABASE_URL":      "postgresql://astro:astro@localhost:5433/astro_dev",
		"REDIS_URL":         "redis://localhost:6381",
		"ASTRO_SERVICE_URL": "http://localhost:8100",
		"AI_SERVICE_URL":    "http://localhost:8200",
		"INTERNAL_TOKEN":    "a-sufficiently-long-token",
		// Phase 1. Neither has a default: a shared default secret is the
		// same as no secret, and the failure would be silent.
		"JWT_SECRET":   "a-32-byte-minimum-jwt-signing-secret",
		"IP_HASH_SALT": "a-16-byte-min-salt",
	}
}

func setEnv(t *testing.T, env map[string]string) {
	t.Helper()
	for k, v := range env {
		t.Setenv(k, v)
	}
}

func TestLoadSucceedsWithValidEnvironment(t *testing.T) {
	setEnv(t, validEnv())

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned an unexpected error: %v", err)
	}

	if cfg.Port != 4000 {
		t.Errorf("Port = %d, want default 4000", cfg.Port)
	}
	if cfg.Env != EnvDevelopment {
		t.Errorf("Env = %q, want %q", cfg.Env, EnvDevelopment)
	}
	if cfg.ServiceTimeout != 10*time.Second {
		t.Errorf("ServiceTimeout = %v, want 10s", cfg.ServiceTimeout)
	}
	// Every feature flag must default to off. Phase 0 ships no features,
	// and a flag that defaults on is a feature shipped by accident.
	if cfg.FeatureAIChat || cfg.FeaturePayments || cfg.FeatureVoice ||
		cfg.FeatureAstrologers || cfg.FeatureCompatibility || cfg.FeaturePDF {
		t.Error("a feature flag defaulted to enabled; all must default to false")
	}
}

// TestLoadFailsOnMissingRequired is the important one: a missing variable
// must be a named startup failure, not a nil dereference in a handler
// three hours into a deploy.
func TestLoadFailsOnMissingRequired(t *testing.T) {
	required := []string{
		"DATABASE_URL",
		"REDIS_URL",
		"ASTRO_SERVICE_URL",
		"AI_SERVICE_URL",
		"INTERNAL_TOKEN",
	}

	for _, missing := range required {
		t.Run("missing_"+missing, func(t *testing.T) {
			env := validEnv()
			delete(env, missing)
			setEnv(t, env)
			// t.Setenv cannot unset, so explicitly blank it.
			t.Setenv(missing, "")

			_, err := Load()
			if err == nil {
				t.Fatalf("Load() succeeded with %s unset; it must fail", missing)
			}
			if !strings.Contains(err.Error(), missing) &&
				!strings.Contains(strings.ToLower(err.Error()), "validate") {
				t.Errorf("error does not identify the problem: %v", err)
			}
		})
	}
}

func TestLoadRejectsInvalidValues(t *testing.T) {
	cases := []struct {
		name  string
		key   string
		value string
	}{
		{"unknown environment", "ENV", "staging-two"},
		{"port out of range", "PORT", "70000"},
		{"non-postgres database url", "DATABASE_URL", "mysql://localhost/db"},
		{"non-redis cache url", "REDIS_URL", "memcached://localhost"},
		{"short internal token", "INTERNAL_TOKEN", "tooshort"},
		{"malformed service url", "ASTRO_SERVICE_URL", "not-a-url"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setEnv(t, validEnv())
			t.Setenv(tc.key, tc.value)

			if _, err := Load(); err == nil {
				t.Fatalf("Load() accepted %s=%q; it must be rejected", tc.key, tc.value)
			}
		})
	}
}

// TestProductionRefusesDevelopmentToken covers the fail-loud pattern:
// a configuration that is fine locally but dangerous in production must
// be a startup crash, not a policy document.
func TestProductionRefusesDevelopmentToken(t *testing.T) {
	setEnv(t, validEnv())
	t.Setenv("ENV", EnvProduction)
	t.Setenv("INTERNAL_TOKEN", "dev-internal-token-change-me")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() accepted the development token in production")
	}
	if !strings.Contains(err.Error(), "INTERNAL_TOKEN") {
		t.Errorf("error should name INTERNAL_TOKEN, got: %v", err)
	}
}

func TestProductionAcceptsRealToken(t *testing.T) {
	setEnv(t, validEnv())
	t.Setenv("ENV", EnvProduction)
	// Deliberately not a random-looking hex string: a value that *looks*
	// like a credential trips secret scanners, and suppressing that with
	// an allowlist entry would blunt the scanner for real secrets too.
	// The only property under test is that it passes the length check.
	t.Setenv("INTERNAL_TOKEN", "example-not-a-real-token")
	// Phase 1 added a second production invariant: console is the
	// development default and is refused in production, so a config that
	// only fixes the token is no longer complete.
	t.Setenv("AUTH_CHANNEL", "smtp")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() rejected a valid production config: %v", err)
	}
	if !cfg.IsProduction() {
		t.Error("IsProduction() = false for ENV=production")
	}
}

func TestAddr(t *testing.T) {
	cfg := &Config{Port: 8080}
	if got := cfg.Addr(); got != ":8080" {
		t.Errorf("Addr() = %q, want \":8080\"", got)
	}
}

// ─── Phase 1: auth configuration ─────────────────────────────────────

// ConsoleChannel writes the OTP into the log. In production that is a
// log full of live credentials — readable by anyone with log access and
// retained for as long as logs are retained. The process must refuse to
// start rather than trust a runbook.
func TestProductionRefusesConsoleAuthChannel(t *testing.T) {
	env := validEnv()
	env["ENV"] = EnvProduction
	env["INTERNAL_TOKEN"] = "a-real-production-token"
	env["AUTH_CHANNEL"] = "console"
	setEnv(t, env)

	_, err := Load()
	if err == nil {
		t.Fatal("Load() accepted AUTH_CHANNEL=console in production — " +
			"OTP codes would be printed to production logs")
	}
	if !strings.Contains(err.Error(), "AUTH_CHANNEL") {
		t.Errorf("error does not name the offending variable: %v", err)
	}
}

func TestProductionAcceptsSMTPAuthChannel(t *testing.T) {
	env := validEnv()
	env["ENV"] = EnvProduction
	env["INTERNAL_TOKEN"] = "a-real-production-token"
	env["AUTH_CHANNEL"] = "smtp"
	setEnv(t, env)

	if _, err := Load(); err != nil {
		t.Fatalf("a production-safe channel was rejected: %v", err)
	}
}

// Development is where console belongs, and it must stay the default —
// otherwise every developer has to configure a mail server before they
// can log in once.
func TestDevelopmentDefaultsToConsole(t *testing.T) {
	setEnv(t, validEnv())

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}
	if cfg.AuthChannel != "console" {
		t.Errorf("AuthChannel = %q, want console by default in development", cfg.AuthChannel)
	}
}

// A short or missing signing secret must be a named startup failure, not
// a warning. HS256 with a weak key is brute-forceable offline, and every
// token ever issued stays forgeable afterwards.
func TestJWTSecretIsRequiredAndLongEnough(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		env := validEnv()
		delete(env, "JWT_SECRET")
		setEnv(t, env)
		// t.Setenv cannot unset, so blank it explicitly.
		t.Setenv("JWT_SECRET", "")

		if _, err := Load(); err == nil {
			t.Fatal("Load() succeeded with no JWT_SECRET")
		}
	})

	t.Run("too short", func(t *testing.T) {
		env := validEnv()
		env["JWT_SECRET"] = "short"
		setEnv(t, env)

		err := loadExpectingFailure(t)
		if !strings.Contains(strings.ToLower(err.Error()), "jwtsecret") {
			t.Errorf("error does not name JWTSecret: %v", err)
		}
	})
}

func TestIPHashSaltIsRequired(t *testing.T) {
	env := validEnv()
	env["IP_HASH_SALT"] = ""
	setEnv(t, env)

	if _, err := Load(); err == nil {
		t.Fatal("Load() succeeded with no IP_HASH_SALT — raw IPs would be persisted")
	}
}

func loadExpectingFailure(t *testing.T) error {
	t.Helper()
	_, err := Load()
	if err == nil {
		t.Fatal("Load() succeeded where it should have failed")
	}
	return err
}
