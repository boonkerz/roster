// Command server ist der zentrale Inventar-Server. Er läuft im Vordergrund
// (Subkommando run / kein Argument) oder als Dienst (install/uninstall/start/stop).
package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/kardianos/service"

	"github.com/boonkerz/roster/internal/server/api"
	"github.com/boonkerz/roster/internal/server/auth"
	"github.com/boonkerz/roster/internal/server/config"
	"github.com/boonkerz/roster/internal/server/cve"
	"github.com/boonkerz/roster/internal/server/model"
	"github.com/boonkerz/roster/internal/server/selfsign"
	"github.com/boonkerz/roster/internal/server/store"
	"github.com/boonkerz/roster/web"
)

// version wird beim Build via -ldflags gesetzt und entspricht der Version der
// eingebetteten Agent-Binaries (für das Agent-Auto-Update).
var version = "dev"

func main() {
	configPath := flag.String("config", "", "Pfad zur YAML-Konfigurationsdatei")
	flag.Parse()

	if flag.Arg(0) == "version" {
		fmt.Println(version)
		return
	}

	// Admin-CLI (ohne laufenden Dienst nutzbar): Konto-Wiederherstellung.
	switch flag.Arg(0) {
	case "list-users", "reset-password", "disable-2fa":
		if err := runAdminCLI(*configPath, flag.Args()); err != nil {
			fmt.Fprintln(os.Stderr, "Fehler:", err)
			os.Exit(1)
		}
		return
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	prg := &program{configPath: *configPath, log: log}
	svcConfig := &service.Config{
		Name:        "roster-server",
		DisplayName: "Roster Server",
		Description: "Zentraler Server für das PC-/Server-Inventar.",
		Arguments:   serviceArgs(*configPath),
	}
	svc, err := service.New(prg, svcConfig)
	if err != nil {
		log.Error("dienst initialisieren", "err", err)
		os.Exit(1)
	}

	// Dienst-Steuerung: server install|uninstall|start|stop|restart
	if action := flag.Arg(0); action != "" && action != "run" {
		if err := service.Control(svc, action); err != nil {
			log.Error("dienst-aktion fehlgeschlagen", "action", action, "err", err)
			os.Exit(1)
		}
		log.Info("dienst-aktion ausgeführt", "action", action)
		return
	}

	if err := svc.Run(); err != nil {
		log.Error("dienst-lauf", "err", err)
		os.Exit(1)
	}
}

// program implementiert service.Interface (Start nicht-blockierend, Stop fährt herunter).
type program struct {
	configPath string
	log        *slog.Logger
	srv        *http.Server
	st         *store.Store
}

func (p *program) Start(s service.Service) error {
	cfg, err := config.Load(p.configPath)
	if err != nil {
		return err
	}
	st, err := store.Open(cfg.DatabaseURL)
	if err != nil {
		return err
	}
	p.st = st

	if err := seedAdmin(context.Background(), st, cfg, p.log); err != nil {
		return err
	}
	if cfg.SeedEnrollToken != "" {
		if err := st.EnsureEnrollmentToken(context.Background(), "seed (env)", auth.HashToken(cfg.SeedEnrollToken), "system"); err != nil {
			return err
		}
		p.log.Warn("festes Seed-Enrollment-Token aktiv (nur für Test-/Automatisierung gedacht)")
	}

	// Für Entwicklung/Docker: fehlt das Zertifikat, ein selbstsigniertes erzeugen.
	if cfg.TLSSelfSigned && cfg.TLSCert != "" && cfg.TLSKey != "" {
		hosts := strings.Split(cfg.TLSHosts, ",")
		if err := selfsign.EnsureCert(cfg.TLSCert, cfg.TLSKey, hosts); err != nil {
			return fmt.Errorf("selbstsigniertes zertifikat: %w", err)
		}
		p.log.Info("selbstsigniertes TLS-Zertifikat bereit", "cert", cfg.TLSCert, "hosts", cfg.TLSHosts)
	}

	srv := api.New(st, cfg, p.log, web.FS(), version)
	p.srv = &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv,
		ReadHeaderTimeout: 10 * time.Second,
	}

	if cfg.ResultRetention > 0 {
		go p.pruneLoop(cfg.ResultRetention)
	}
	go srv.RunReportLoop(context.Background())
	go p.cveScanLoop()

	go p.serve(cfg)
	return nil
}

// cveScanLoop gleicht täglich (und ~2 Min. nach dem Start) die installierte Software
// aller Geräte gegen OSV.dev ab – schonend, sequenziell mit Pause je Gerät.
func (p *program) cveScanLoop() {
	ctx := context.Background()
	client := &http.Client{Timeout: 20 * time.Second}
	scan := func() {
		devs, err := p.st.ListDevices(ctx, nil)
		if err != nil {
			p.log.Warn("cve-scan: geräte laden fehlgeschlagen", "err", err)
			return
		}
		scanned, found := 0, 0
		for _, d := range devs {
			if d.Revoked {
				continue
			}
			sw, err := p.st.DeviceSoftware(ctx, d.ID)
			if err != nil || len(sw) == 0 {
				continue
			}
			in := make([]cve.SW, 0, len(sw))
			for _, s := range sw {
				in = append(in, cve.SW{Name: s.Name, Version: s.Version})
			}
			vulns, err := cve.Scan(ctx, client, in, cve.Ecosystem(d.OS, d.OSVersion))
			if err != nil {
				continue
			}
			mv := make([]model.Vulnerability, 0, len(vulns))
			for _, v := range vulns {
				mv = append(mv, model.Vulnerability{
					Package: v.Package, Version: v.Version, VulnID: v.ID,
					Severity: v.Severity, Summary: v.Summary, Fixed: v.Fixed, URL: v.URL,
				})
			}
			if err := p.st.ReplaceVulnerabilities(ctx, d.ID, mv); err == nil {
				scanned++
				found += len(mv)
			}
			time.Sleep(3 * time.Second) // schonend zu OSV.dev
		}
		p.log.Info("cve-hintergrund-scan fertig", "geräte", scanned, "treffer", found)
	}
	time.Sleep(2 * time.Minute) // nicht direkt beim Start scannen
	scan()
	t := time.NewTicker(24 * time.Hour)
	defer t.Stop()
	for range t.C {
		scan()
	}
}

// pruneLoop löscht periodisch die Task-/Befehls-Historie, die älter als die
// Aufbewahrungsdauer ist (einmal beim Start, danach alle 6 Stunden).
func (p *program) pruneLoop(retention time.Duration) {
	prune := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		// Audit-Log doppelt so lange aufbewahren (sicherheitsrelevant).
		tasks, cmds, err := p.st.PruneHistory(ctx, time.Now().Add(-retention), time.Now().Add(-2*retention))
		if err != nil {
			p.log.Warn("historie aufräumen fehlgeschlagen", "err", err)
			return
		}
		if tasks > 0 || cmds > 0 {
			p.log.Info("historie aufgeräumt", "task_läufe", tasks, "befehle", cmds, "älter_als", retention)
		}
		// Auslastungs-Historie 90 Tage aufbewahren (unabhängig von der Task-Retention).
		_ = p.st.PruneMetrics(ctx, 90*24*time.Hour)
	}
	prune()
	t := time.NewTicker(6 * time.Hour)
	defer t.Stop()
	for range t.C {
		prune()
	}
}

func (p *program) serve(cfg config.Config) {
	useTLS := cfg.TLSCert != "" && cfg.TLSKey != ""
	p.log.Info("server startet", "addr", cfg.Addr, "tls", useTLS, "db", cfg.DatabaseURL)
	var err error
	if useTLS {
		err = p.srv.ListenAndServeTLS(cfg.TLSCert, cfg.TLSKey)
	} else {
		if cfg.BehindProxy {
			p.log.Info("HTTP-Modus – TLS wird vom vorgelagerten Reverse-Proxy terminiert")
		} else {
			p.log.Warn("TLS deaktiviert – nur für lokale Tests verwenden")
		}
		err = p.srv.ListenAndServe()
	}
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		p.log.Error("server beendet", "err", err)
	}
}

func (p *program) Stop(s service.Service) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if p.srv != nil {
		_ = p.srv.Shutdown(ctx)
	}
	if p.st != nil {
		_ = p.st.Close()
	}
	return nil
}

// seedAdmin legt beim ersten Start einen Admin an, falls noch kein Benutzer existiert.
// runAdminCLI führt Konto-Wiederherstellungs-Befehle direkt gegen die Datenbank aus
// (list-users, reset-password, disable-2fa) – z.B. bei vergessenem Passwort oder
// verlorenem 2FA-Gerät. Läuft der Dienst, sollte er dafür kurz gestoppt werden,
// damit die SQLite-Datei nicht gesperrt ist.
func runAdminCLI(configPath string, args []string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	st, err := store.Open(cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer st.Close()
	ctx := context.Background()

	switch args[0] {
	case "list-users":
		users, err := st.ListUsers(ctx)
		if err != nil {
			return err
		}
		fmt.Printf("%-24s %-8s %-8s %s\n", "BENUTZER", "ROLLE", "2FA", "QUELLE")
		for _, u := range users {
			tfa := "aus"
			if u.TOTPEnabled {
				tfa = "an"
			}
			fmt.Printf("%-24s %-8s %-8s %s\n", u.Username, u.Role, tfa, u.AuthSource)
		}
		return nil

	case "reset-password":
		if len(args) < 2 {
			return fmt.Errorf("Aufruf: server reset-password <benutzer> [neues-passwort]")
		}
		u, err := st.GetUserByUsername(ctx, args[1])
		if err != nil {
			return fmt.Errorf("Benutzer %q nicht gefunden", args[1])
		}
		password := ""
		generated := false
		if len(args) >= 3 {
			password = args[2]
		} else {
			b := make([]byte, 12)
			_, _ = rand.Read(b)
			password = base64.RawURLEncoding.EncodeToString(b)
			generated = true
		}
		hash, err := auth.HashPassword(password)
		if err != nil {
			return err
		}
		if err := st.SetUserPassword(ctx, u.ID, hash); err != nil {
			return err
		}
		if generated {
			fmt.Printf("\n=== Passwort zurückgesetzt ===\n  Benutzer:  %s\n  Passwort:  %s\n==============================\n\n", u.Username, password)
		} else {
			fmt.Printf("Passwort für %q gesetzt.\n", u.Username)
		}
		return nil

	case "disable-2fa":
		if len(args) < 2 {
			return fmt.Errorf("Aufruf: server disable-2fa <benutzer>")
		}
		u, err := st.GetUserByUsername(ctx, args[1])
		if err != nil {
			return fmt.Errorf("Benutzer %q nicht gefunden", args[1])
		}
		if err := st.ClearUserTOTP(ctx, u.ID); err != nil {
			return err
		}
		fmt.Printf("2FA für %q deaktiviert (Secret + Backup-Codes entfernt).\n", u.Username)
		if cfg.Require2FA {
			fmt.Println("Hinweis: 2FA ist Pflicht – beim nächsten Login wird die Einrichtung erneut erzwungen.")
		}
		return nil
	}
	return fmt.Errorf("unbekannter Befehl: %s", args[0])
}

func seedAdmin(ctx context.Context, st *store.Store, cfg config.Config, log *slog.Logger) error {
	n, err := st.CountUsers(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	password := cfg.SeedAdminPassword
	generated := false
	if password == "" {
		b := make([]byte, 12)
		_, _ = rand.Read(b)
		password = base64.RawURLEncoding.EncodeToString(b)
		generated = true
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	u := &model.User{
		ID:           store.NewID(),
		Username:     cfg.SeedAdminUser,
		PasswordHash: hash,
		Role:         model.RoleAdmin,
		AuthSource:   model.AuthLocal,
	}
	if err := st.CreateUser(ctx, u); err != nil {
		return err
	}
	if generated {
		log.Warn("Seed-Admin erstellt – Passwort jetzt notieren!",
			"username", cfg.SeedAdminUser, "password", password)
		fmt.Fprintf(os.Stderr, "\n=== Seed-Admin angelegt ===\n  Benutzer:  %s\n  Passwort:  %s\n===========================\n\n",
			cfg.SeedAdminUser, password)
	} else {
		log.Info("Seed-Admin erstellt", "username", cfg.SeedAdminUser)
	}
	return nil
}

// serviceArgs sorgt dafür, dass der installierte Dienst mit derselben Konfig startet.
func serviceArgs(configPath string) []string {
	if configPath == "" {
		return []string{"run"}
	}
	return []string{"-config", configPath, "run"}
}
