// Package config lädt die Serverkonfiguration aus Defaults, einer optionalen
// YAML-Datei und Umgebungsvariablen (ENV gewinnt vor Datei vor Default).
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config bündelt alle Laufzeiteinstellungen des Servers.
type Config struct {
	// Addr ist die Listen-Adresse, z.B. ":8443".
	Addr string `yaml:"addr"`
	// DatabaseURL: "sqlite:///pfad/inventory.db" oder "postgres://user:pw@host/db".
	DatabaseURL string `yaml:"database_url"`
	// TLSCert/TLSKey: Pfade zum Serverzertifikat. Leer => HTTP (nur für lokale Tests).
	TLSCert string `yaml:"tls_cert"`
	TLSKey  string `yaml:"tls_key"`
	// TLSSelfSigned: fehlt das Zertifikat unter TLSCert/TLSKey, erzeugt der Server beim
	// Start ein selbstsigniertes (für Entwicklung/Docker – der Agent pinnt es als CA).
	TLSSelfSigned bool `yaml:"tls_self_signed"`
	// TLSHosts sind die SANs (Hostnamen/IPs) des selbstsignierten Zertifikats.
	TLSHosts string `yaml:"tls_hosts"`
	// CheckinInterval ist das empfohlene Agent-Polling-Intervall.
	CheckinInterval time.Duration `yaml:"checkin_interval"`
	// OfflineAfter: ab dieser Stille gilt ein Gerät als offline (Default 2.5x Intervall).
	OfflineAfter time.Duration `yaml:"offline_after"`
	// Seed-Admin beim ersten Start (nur wenn noch kein Benutzer existiert).
	SeedAdminUser     string `yaml:"seed_admin_user"`
	SeedAdminPassword string `yaml:"seed_admin_password"`
	// SeedEnrollToken legt beim Start ein festes Enrollment-Token an (für Test-/
	// Automatisierungsumgebungen, z.B. den Docker-Stack). Leer = deaktiviert.
	SeedEnrollToken string `yaml:"seed_enroll_token"`
	// SecureCookie: Cookie nur über HTTPS senden (in Produktion true).
	SecureCookie bool `yaml:"secure_cookie"`
	// Require2FA: alle Benutzer müssen Zwei-Faktor (TOTP) einrichten (Default true).
	Require2FA bool `yaml:"require_2fa"`
	// BehindProxy: der Server läuft hinter einem TLS-terminierenden Reverse-Proxy.
	// Dann bleibt SecureCookie aktiv, obwohl der Server selbst nur HTTP spricht.
	BehindProxy bool `yaml:"behind_proxy"`
	// ExternalURL: von außen sichtbare Basis-URL (z.B. https://inventory.example.com).
	// Wird für Install-Links/One-Liner genutzt; falls leer, aus der Anfrage abgeleitet.
	ExternalURL string `yaml:"external_url"`
	// ResultRetention: Aufbewahrungsdauer der Task-/Befehls-Historie; ältere Läufe
	// werden periodisch gelöscht (0 = nie löschen). Default 30 Tage.
	ResultRetention time.Duration `yaml:"result_retention"`
	// CheckHistoryRetention: Aufbewahrungsdauer des Check-Verlaufs (ein Punkt je
	// Check und Checkin, für Verlaufsdiagramme). Default 10 Tage, 0 = nie löschen.
	CheckHistoryRetention time.Duration `yaml:"check_history_retention"`
}

// Default liefert eine sinnvolle Basiskonfiguration.
func Default() Config {
	return Config{
		Addr:                  ":8443",
		DatabaseURL:           "sqlite://./inventory.db",
		CheckinInterval:       5 * time.Minute,
		OfflineAfter:          0, // 0 => abgeleitet aus CheckinInterval
		SeedAdminUser:         "admin",
		SeedAdminPassword:     "",
		SecureCookie:          true,
		Require2FA:            true,
		ResultRetention:       30 * 24 * time.Hour,
		CheckHistoryRetention: 10 * 24 * time.Hour,
	}
}

// Load liest die Konfiguration. configPath darf leer sein.
func Load(configPath string) (Config, error) {
	cfg := Default()

	if configPath != "" {
		data, err := os.ReadFile(configPath)
		if err != nil {
			return cfg, fmt.Errorf("konfig lesen: %w", err)
		}
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return cfg, fmt.Errorf("konfig parsen: %w", err)
		}
	}

	applyEnv(&cfg)

	if cfg.OfflineAfter == 0 {
		cfg.OfflineAfter = cfg.CheckinInterval*2 + cfg.CheckinInterval/2
	}
	if cfg.TLSCert == "" && !cfg.BehindProxy && cfg.SecureCookie {
		// Ohne TLS (und ohne TLS-terminierenden Proxy) kann der Browser kein
		// Secure-Cookie senden – sonst käme man nicht mehr rein.
		cfg.SecureCookie = false
	}
	return cfg, nil
}

// migrateLegacyEnv übernimmt gesetzte PCINV_*-Variablen (alter Name „PC-Inventory")
// in die neuen ROSTER_*-Namen, sofern die neue Variante nicht gesetzt ist. So läuft
// eine bestehende Installation nach dem Rebranding ohne Anpassung weiter.
func migrateLegacyEnv() {
	for _, e := range os.Environ() {
		i := strings.IndexByte(e, '=')
		if i < 0 || !strings.HasPrefix(e[:i], "PCINV_") {
			continue
		}
		newKey := "ROSTER_" + strings.TrimPrefix(e[:i], "PCINV_")
		if os.Getenv(newKey) == "" {
			_ = os.Setenv(newKey, e[i+1:])
		}
	}
}

func applyEnv(cfg *Config) {
	migrateLegacyEnv() // Rückwärtskompatibilität: alte PCINV_*-Variablen weiter akzeptieren
	if v := os.Getenv("ROSTER_ADDR"); v != "" {
		cfg.Addr = v
	}
	if v := os.Getenv("ROSTER_DB"); v != "" {
		cfg.DatabaseURL = v
	}
	if v := os.Getenv("ROSTER_TLS_CERT"); v != "" {
		cfg.TLSCert = v
	}
	if v := os.Getenv("ROSTER_TLS_KEY"); v != "" {
		cfg.TLSKey = v
	}
	if v := os.Getenv("ROSTER_TLS_SELFSIGNED"); v != "" {
		cfg.TLSSelfSigned = v == "true" || v == "1"
	}
	if v := os.Getenv("ROSTER_TLS_HOSTS"); v != "" {
		cfg.TLSHosts = v
	}
	if v := os.Getenv("ROSTER_SEED_ADMIN_USER"); v != "" {
		cfg.SeedAdminUser = v
	}
	if v := os.Getenv("ROSTER_SEED_ADMIN_PASSWORD"); v != "" {
		cfg.SeedAdminPassword = v
	}
	if v := os.Getenv("ROSTER_SEED_ENROLL_TOKEN"); v != "" {
		cfg.SeedEnrollToken = v
	}
	if v := os.Getenv("ROSTER_CHECKIN_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.CheckinInterval = d
		}
	}
	if v := os.Getenv("ROSTER_REQUIRE_2FA"); v != "" {
		cfg.Require2FA = v == "true" || v == "1"
	}
	if v := os.Getenv("ROSTER_BEHIND_PROXY"); v != "" {
		cfg.BehindProxy = v == "true" || v == "1"
	}
	if v := os.Getenv("ROSTER_EXTERNAL_URL"); v != "" {
		cfg.ExternalURL = v
	}
	if v := os.Getenv("ROSTER_SECURE_COOKIE"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.SecureCookie = b
		}
	}
	if v := os.Getenv("ROSTER_RESULT_RETENTION_DAYS"); v != "" {
		if d, err := strconv.Atoi(v); err == nil && d >= 0 {
			cfg.ResultRetention = time.Duration(d) * 24 * time.Hour
		}
	}
	if v := os.Getenv("ROSTER_CHECK_HISTORY_DAYS"); v != "" {
		if d, err := strconv.Atoi(v); err == nil && d >= 0 {
			cfg.CheckHistoryRetention = time.Duration(d) * 24 * time.Hour
		}
	}
}
