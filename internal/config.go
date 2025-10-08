package form_mailer

import (
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strings"

	"github.com/nazarhussain/form-courier/env"
)

/*
ENV-ONLY CONFIG (documented in README):
  Required global SMTP fallback:
    SMTP_HOST, SMTP_PORT, SMTP_USER, SMTP_PASS, SMTP_SSL (true/false)
  Optional global:
    LISTEN_ADDR (default ":3000")
    FROM_ADDR
    SUBJECT_PREFIX (default "[Contact]")
    RATE_LIMIT_BURST (default 3)
    RATE_LIMIT_REFILL_MINUTES (default 1)
    ALLOW_JSON (default "true")
    ALLOW_FORM (default "true")
    MAX_BODY_KB (default 1024)  // 1MB

  Multi-site:
    SITES="picadortech,instant-umzug"

    For each site, using the SITE key uppercased (non alnum -> _):
      <SITE>_TO (required)
      <SITE>_ALLOWED_ORIGINS="https://a.com,https://b.com"
      <SITE>_SUBJECT_PREFIX
      <SITE>_SECRET                // optional HMAC secret; if set, require X-Signature
      <SITE>_SMTP_HOST (optional per-site override)
      <SITE>_SMTP_PORT
      <SITE>_SMTP_USER
      <SITE>_SMTP_PASS
      <SITE>_SMTP_SSL ("true"/"false")
*/

type SiteCfg struct {
	Key            string
	To             string
	AllowedOrigins []string
	SubjectPrefix  string
	Secret         string
	SMTP           *SmtpCfg
	FromAddr       string
}

type SmtpCfg struct {
	Host string
	Port int
	User string
	Pass string
	SSL  bool
}

type Config struct {
	RateBurst         int
	RateRefillMinutes int
	AllowJSON         bool
	AllowForm         bool
	MaxBodyKB         int
	ListenAddr        string
	Sites             map[string]*SiteCfg
}

var (
	emailRegex = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

	conf *Config
)

func GetConfig() *Config {
	if conf == nil {
		globalSMTP := loadGlobalSMTP()
		globalSubjectPrefix := env.Env("SUBJECT_PREFIX", "[Contact]")
		conf = &Config{
			RateBurst:         env.EnvInt("RATE_LIMIT_BURST", 3),
			RateRefillMinutes: env.EnvInt("RATE_LIMIT_REFILL_MINUTES", 1),
			AllowJSON:         env.EnvBool("ALLOW_JSON", true),
			AllowForm:         env.EnvBool("ALLOW_FORM", true),
			MaxBodyKB:         env.EnvInt("MAX_BODY_KB", 1024),
			ListenAddr:        env.Env("LISTEN_ADDR", ":3000"),
			Sites:             loadSitesFromEnv(globalSMTP, globalSubjectPrefix),
		}
	}
	return conf
}

func loadGlobalSMTP() SmtpCfg {
	return SmtpCfg{
		Host: env.MustEnv("SMTP_HOST"),
		Port: env.MustEnvInt("SMTP_PORT"),
		User: env.MustEnv("SMTP_USER"),
		Pass: env.MustEnv("SMTP_PASS"),
		SSL:  env.EnvBool("SMTP_SSL", false),
	}
}

func loadSitesFromEnv(globalSMTP SmtpCfg, globalSubjectPrefix string) map[string]*SiteCfg {
	siteByKey := map[string]*SiteCfg{}

	raw := os.Getenv("SITES")
	if strings.TrimSpace(raw) == "" {
		fatalf("SITES is required (comma-separated list of site keys, e.g. SITES=my-site,product-alpha)")
	}
	keys := splitString(raw)
	for _, key := range keys {
		if key == "" {
			continue
		}
		uc := env.ToEnvKey(key) // e.g., picadortech -> PICADORTECH
		to := os.Getenv(uc + "_TO")
		if strings.TrimSpace(to) == "" {
			fatalf("missing %s_TO for site %q", uc, key)
		}
		allowed := splitString(os.Getenv(uc + "_ALLOWED_ORIGINS"))
		prefix := env.Env(uc+"_SUBJECT_PREFIX", globalSubjectPrefix)
		secret := os.Getenv(uc + "_SECRET")

		siteSMTP := &SmtpCfg{
			Host: globalSMTP.Host,
			Port: globalSMTP.Port,
			User: globalSMTP.User,
			Pass: globalSMTP.Pass,
			SSL:  globalSMTP.SSL,
		}
		if v := os.Getenv(uc + "_SMTP_HOST"); v != "" {
			siteSMTP = &SmtpCfg{
				Host: v,
				Port: env.EnvInt(uc+"_SMTP_PORT", globalSMTP.Port),
				User: env.Env(uc+"_SMTP_USER", globalSMTP.User),
				Pass: env.Env(uc+"_SMTP_PASS", globalSMTP.Pass),
				SSL:  env.EnvBool(uc+"_SMTP_SSL", globalSMTP.SSL),
			}
		}

		fromAddr := env.Env("FROM_ADDR", globalSMTP.User)
		if v := os.Getenv(uc + "_FROM_ADDR"); strings.TrimSpace(v) != "" {
			fromAddr = v
		}

		siteByKey[key] = &SiteCfg{
			Key:            key,
			To:             to,
			AllowedOrigins: allowed,
			SubjectPrefix:  prefix,
			FromAddr:       fromAddr,
			Secret:         secret,
			SMTP:           siteSMTP,
		}
	}

	return siteByKey
}

func fatalf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	slog.Default().Error(msg)
	os.Exit(1)
}

func splitString(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func LogConfig(logger *slog.Logger, cfg *Config) {
	if cfg == nil {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}
	logger.Info("configuration loaded",
		"listen_addr", cfg.ListenAddr,
		"allow_json", cfg.AllowJSON,
		"allow_form", cfg.AllowForm,
		"rate_burst", cfg.RateBurst,
		"rate_refill_minutes", cfg.RateRefillMinutes,
		"max_body_kb", cfg.MaxBodyKB,
		"sites", len(cfg.Sites),
	)
	for _, site := range cfg.Sites {
		if site == nil || site.SMTP == nil {
			continue
		}
		logger.Info("site configuration",
			"site", site.Key,
			"to", site.To,
			"allowed_origins", site.AllowedOrigins,
			"subject_prefix", site.SubjectPrefix,
			"from_addr", site.FromAddr,
			"smtp_host", site.SMTP.Host,
			"smtp_user", site.SMTP.User,
			"smtp_port", site.SMTP.Port,
			"smtp_ssl", site.SMTP.SSL,
			"has_secret", site.Secret != "",
		)
	}
}
