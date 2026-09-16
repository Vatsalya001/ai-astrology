// Package config loads and validates all runtime configuration.
//
// Two rules are enforced here and nowhere else:
//
//  1. Nothing outside this package reads os.Getenv. If you need a value,
//     add a field to Config.
//  2. The process refuses to start if anything required is missing or
//     invalid. A named startup failure is always better than a nil
//     dereference three hours into a deploy.
package config

import (
	"fmt"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/go-playground/validator/v10"
)

// Environment names. Declared as constants so comparisons can't drift
// through a typo.
const (
	EnvDevelopment = "development"
	EnvStaging     = "staging"
	EnvProduction  = "production"
)

type Config struct {
	// ─── Core ───────────────────────────────────────────────────
	Env      string `env:"ENV"       envDefault:"development" validate:"required,oneof=development staging production"`
	Port     int    `env:"PORT"      envDefault:"4000"       validate:"required,min=1,max=65535"`
	WebURL   string `env:"WEB_URL"   envDefault:"http://localhost:3000" validate:"required,url"`
	LogLevel string `env:"LOG_LEVEL" envDefault:"info"       validate:"required,oneof=debug info warn error"`

	// ─── Data ───────────────────────────────────────────────────
	DatabaseURL      string `env:"DATABASE_URL,required"      validate:"required,startswith=postgres"`
	DatabaseMaxConns int32  `env:"DATABASE_MAX_CONNS"         envDefault:"20" validate:"min=1,max=200"`
	RedisURL         string `env:"REDIS_URL,required"         validate:"required,startswith=redis"`

	// ─── Object storage ─────────────────────────────────────────
	S3Endpoint       string `env:"S3_ENDPOINT"           envDefault:"http://localhost:9000"`
	S3Bucket         string `env:"S3_BUCKET"             envDefault:"astro-dev"`
	S3AccessKey      string `env:"S3_ACCESS_KEY"         envDefault:""`
	S3SecretKey      string `env:"S3_SECRET_KEY"         envDefault:""`
	S3Region         string `env:"S3_REGION"             envDefault:"us-east-1"`
	S3ForcePathStyle bool   `env:"S3_FORCE_PATH_STYLE"   envDefault:"true"`

	// ─── Internal services ──────────────────────────────────────
	AstroServiceURL string        `env:"ASTRO_SERVICE_URL,required" validate:"required,url"`
	AIServiceURL    string        `env:"AI_SERVICE_URL,required"    validate:"required,url"`
	ServiceTimeout  time.Duration `env:"SERVICE_TIMEOUT"            envDefault:"10s" validate:"required"`

	// ─── Observability (all optional) ───────────────────────────
	// Empty values disable the exporter/reporter. The instrumentation
	// still runs, so these code paths cannot rot between releases, and
	// enabling them in staging is a config change not a code change.
	OTLPEndpoint    string  `env:"OTEL_EXPORTER_OTLP_ENDPOINT" envDefault:""`
	OTLPSampleRatio float64 `env:"OTEL_TRACES_SAMPLER_ARG" envDefault:"1.0" validate:"min=0,max=1"`
	SentryDSN       string  `env:"SENTRY_DSN" envDefault:""`

	// ─── Optional health probes ─────────────────────────────────
	// Object storage and mail are reported by /health when a probe URL
	// is configured, and silently omitted when it is not. Optional
	// because a deployment may front them with something that has no
	// health endpoint, and a health check that cannot be satisfied is
	// worse than one that is absent.
	StorageHealthURL string `env:"STORAGE_HEALTH_URL" envDefault:""`
	MailHealthURL    string `env:"MAIL_HEALTH_URL"    envDefault:""`

	// InternalToken authenticates api-service to the Python services.
	// Minimum length is validated because a short shared secret is
	// worse than an obviously absent one — it looks configured.
	InternalToken string `env:"INTERNAL_TOKEN,required" validate:"required,min=16"`

	// ─── Auth (Phase 1) ──────────────────────────────────────────
	//
	// JWTSecret and IPHashSalt have NO default and NO fallback. A
	// generated default would be identical on every deployment, which is
	// the same as having no secret at all — and the failure is silent,
	// because everything still works.
	JWTSecret     string        `env:"JWT_SECRET,required"   validate:"required,min=32"`
	JWTAccessTTL  time.Duration `env:"JWT_ACCESS_TTL"        envDefault:"15m"  validate:"required"`
	JWTRefreshTTL time.Duration `env:"JWT_REFRESH_TTL"       envDefault:"720h" validate:"required"`

	// IPs are hashed with this salt before storage. Raw IPs are never
	// persisted: an IP address is PII, and a rate-limit table full of
	// them is a breach waiting to be interesting.
	IPHashSalt string `env:"IP_HASH_SALT,required" validate:"required,min=16"`

	AuthChannel string `env:"AUTH_CHANNEL" envDefault:"console" validate:"required,oneof=console smtp sms"`

	SMTPHost string `env:"SMTP_HOST" envDefault:"localhost"`
	SMTPPort int    `env:"SMTP_PORT" envDefault:"1025" validate:"min=1,max=65535"`
	SMTPFrom string `env:"SMTP_FROM" envDefault:"Ayana <noreply@localhost>"`

	OTPLength      int           `env:"OTP_LENGTH"       envDefault:"6"  validate:"required,min=4,max=10"`
	OTPTTL         time.Duration `env:"OTP_TTL"          envDefault:"5m" validate:"required"`
	OTPMaxAttempts int           `env:"OTP_MAX_ATTEMPTS" envDefault:"5"  validate:"required,min=1,max=20"`

	AccountDeleteGrace time.Duration `env:"ACCOUNT_DELETE_GRACE" envDefault:"168h" validate:"required"`

	// ─── Background jobs ────────────────────────────────────────
	// How many tasks one worker replica runs at once. Two, because the
	// only Phase 2 job is a six-hourly call to astro-service: concurrency
	// here buys nothing and a large pool would just hold Redis
	// connections open. Phase 3's PDF renderer is what makes this worth
	// raising.
	WorkerConcurrency int `env:"WORKER_CONCURRENCY" envDefault:"2" validate:"required,min=1,max=64"`

	// ─── Feature flags ──────────────────────────────────────────
	FeatureAIChat        bool `env:"FEATURE_AI_CHAT_ENABLED"          envDefault:"false"`
	FeatureVoice         bool `env:"FEATURE_VOICE_ENABLED"            envDefault:"false"`
	FeatureAstrologers   bool `env:"FEATURE_HUMAN_ASTROLOGER_ENABLED" envDefault:"false"`
	FeatureCompatibility bool `env:"FEATURE_COMPATIBILITY_ENABLED"    envDefault:"false"`
	FeaturePayments      bool `env:"FEATURE_PAYMENTS_ENABLED"         envDefault:"false"`
	FeaturePDF           bool `env:"FEATURE_PDF_ENABLED"              envDefault:"false"`
}

func (c *Config) IsProduction() bool  { return c.Env == EnvProduction }
func (c *Config) IsDevelopment() bool { return c.Env == EnvDevelopment }

// Addr is the listen address for the HTTP server.
func (c *Config) Addr() string { return fmt.Sprintf(":%d", c.Port) }

// Load parses the environment into a Config and validates it.
//
// Both failure modes return an error naming the offending field, so a
// misconfigured deploy says which variable is wrong rather than dying
// somewhere deep in a request handler.
func Load() (*Config, error) {
	var cfg Config

	if err := env.Parse(&cfg); err != nil {
		return nil, fmt.Errorf("parse environment: %w", err)
	}

	if err := validator.New().Struct(&cfg); err != nil {
		return nil, fmt.Errorf("validate configuration: %w", err)
	}

	if err := cfg.checkProductionInvariants(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// checkProductionInvariants refuses configurations that are fine locally
// but dangerous in production.
//
// This is the same fail-loud pattern the specs apply to the LLM provider
// (Phase 4) and the OTP channel (Phase 1): make the unsafe combination a
// startup crash rather than a policy document nobody reads.
func (c *Config) checkProductionInvariants() error {
	if !c.IsProduction() {
		return nil
	}

	if c.InternalToken == "dev-internal-token-change-me" {
		return fmt.Errorf(
			"refusing to start: INTERNAL_TOKEN is still the development default in production")
	}

	// ConsoleChannel writes the OTP to the log. In production that is a
	// log full of live credentials, readable by anyone with log access
	// and retained for as long as logs are retained.
	if c.AuthChannel == "console" {
		return fmt.Errorf(
			"refusing to start: AUTH_CHANNEL=console in production would print OTP codes to logs")
	}

	return nil
}
