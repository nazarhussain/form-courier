package env

import (
	"log"
	"os"
	"strconv"
	"strings"
)

func MustEnv(k string) string {
	v := os.Getenv(k)
	if v == "" {
		log.Fatalf("missing env %s", k)
	}
	return v
}

func MustEnvInt(k string) int {
	v := os.Getenv(k)
	if v == "" {
		log.Fatalf("missing env %s", k)
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		log.Fatalf("env %s must be int", k)
	}
	return n
}

func Env(k, d string) string {
	v := os.Getenv(k)
	if v == "" {
		return d
	}
	return v
}

func EnvInt(k string, d int) int {
	v := os.Getenv(k)
	if v == "" {
		return d
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		log.Fatalf("env %s must be int", k)
	}
	return n
}

func EnvBool(k string, d bool) bool {
	v := os.Getenv(k)
	if v == "" {
		return d
	}
	switch strings.ToLower(v) {
	case "1", "t", "true", "y", "yes":
		return true
	case "0", "f", "false", "n", "no":
		return false
	default:
		log.Fatalf("env %s must be boolean", k)
		return false
	}
}

func ToEnvKey(s string) string {
	// Uppercase and replace non-alnum with underscore
	var b strings.Builder
	for _, r := range strings.ToUpper(s) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	return b.String()
}
