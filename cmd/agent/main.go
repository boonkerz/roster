// Command agent ist der plattformübergreifende Inventar-Agent. Er läuft als
// Dienst (install/uninstall/start/stop) oder im Vordergrund (run).
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kardianos/service"

	"github.com/thomaspeterson/pc-inventory/internal/agent/collect"
	agentcfg "github.com/thomaspeterson/pc-inventory/internal/agent/config"
	"github.com/thomaspeterson/pc-inventory/internal/agent/policy"
	"github.com/thomaspeterson/pc-inventory/internal/agent/remote"
	"github.com/thomaspeterson/pc-inventory/internal/agent/transport"
	"github.com/thomaspeterson/pc-inventory/internal/agent/update"
	"github.com/thomaspeterson/pc-inventory/internal/shared"
)

// version wird beim Build via -ldflags gesetzt.
var version = "dev"

// errNoEnrollToken signalisiert fehlende Enrollment-Daten beim ersten Start.
var errNoEnrollToken = errors.New("noch nicht enrolled und kein enrollment_token in der konfiguration")

func main() {
	configPath := flag.String("config", defaultConfigPath(), "Pfad zur Agent-Konfiguration (YAML)")
	flag.Parse()

	if flag.Arg(0) == "version" {
		fmt.Println(version)
		return
	}
	// Versteckter Aufnahme-Helfer für die Fernsteuerung: läuft in der Nutzer-Session
	// (vom Dienst per CreateProcessAsUser gestartet) und umgeht die Session-0-Isolation.
	if flag.Arg(0) == "__capture" {
		mon := 1
		if v, err := strconv.Atoi(flag.Arg(1)); err == nil {
			mon = v
		}
		remote.RunCaptureHelper(mon)
		return
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	prg := &program{configPath: *configPath, log: log}
	svcConfig := &service.Config{
		Name:        "pc-inventory-agent",
		DisplayName: "PC-Inventory Agent",
		Description: "Meldet Hardware-, MAC- und IP-Inventar an den PC-Inventory-Server.",
		Arguments:   []string{"-config", *configPath, "run"},
	}
	svc, err := service.New(prg, svcConfig)
	if err != nil {
		log.Error("dienst initialisieren", "err", err)
		os.Exit(1)
	}

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

type program struct {
	configPath string
	log        *slog.Logger
	cancel     context.CancelFunc

	client     *transport.Client // HTTP-Client zum Server (für Fortschritts-Posts)
	agentToken string            // Agent-Token (für authentifizierte Posts)

	mu       sync.Mutex
	updates  *shared.OSUpdateInfo // zuletzt ermittelte OS-Updates (Cache)
	publicIP string               // öffentliche IP (einmal ermittelt)

	policy            *shared.PolicyBundle   // zuletzt empfangene Policy
	pendingChecks     []shared.CheckResult   // beim nächsten Checkin zu meldende Check-Ergebnisse
	pendingTasks      []shared.TaskResult    // beim nächsten Checkin zu meldende Task-Läufe
	commandQueue      []shared.Command       // empfangene Ad-hoc-Befehle, noch auszuführen
	pendingCmdResults []shared.CommandResult // beim nächsten Checkin zu meldende Befehls-Ergebnisse
	taskLastRun       map[string]time.Time   // letzte Ausführung je Task-ID
	checkLastRun      map[string]time.Time   // letzte Auswertung je Check-ID (für Häufigkeit)
	checkinNow        chan struct{}          // Server-Push: sofort einchecken
}

func (p *program) Start(s service.Service) error {
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	go p.run(ctx)
	return nil
}

func (p *program) Stop(s service.Service) error {
	if p.cancel != nil {
		p.cancel()
	}
	return nil
}

func (p *program) run(ctx context.Context) {
	cfg, err := agentcfg.Load(p.configPath)
	if err != nil {
		p.log.Error("konfiguration", "err", err)
		return
	}
	client, err := transport.New(cfg.ServerURL, cfg.CACertPath, cfg.InsecureSkipVerify)
	if err != nil {
		p.log.Error("transport", "err", err)
		return
	}

	state, err := agentcfg.LoadState(cfg.StatePath)
	if err != nil {
		p.log.Error("state laden", "err", err)
		return
	}

	if !state.Enrolled() {
		if state, err = p.enroll(ctx, cfg, client); err != nil {
			p.log.Error("enrollment fehlgeschlagen", "err", err)
			return
		}
	}

	p.client = client
	p.agentToken = state.AgentToken
	if !cfg.DisableUpdateCheck {
		go p.updateChecker(ctx, cfg.UpdateCheckInterval)
	}
	p.checkinNow = make(chan struct{}, 1)
	if !cfg.DisableRemote {
		// Echtzeit-Wake-Schleife (Remote-Terminal + Push-Checkin). Hält keine
		// Dauerverbindung, sondern parkt nur einen leichten Long-Poll.
		go remote.Run(ctx, client, state.AgentToken, p.log, func() {
			select {
			case p.checkinNow <- struct{}{}:
			default: // schon ein Checkin angefordert
			}
		})
	}
	if !cfg.DisablePublicIP {
		go func() {
			if ip := collect.PublicIP(ctx); ip != "" {
				p.mu.Lock()
				p.publicIP = ip
				p.mu.Unlock()
			}
		}()
	}

	p.taskLastRun = map[string]time.Time{}
	p.checkLastRun = map[string]time.Time{}
	autoUpdate := !cfg.DisableAutoUpdate
	interval := cfg.Interval
	for {
		next, latest := p.checkin(ctx, client, state, version)
		if next > 0 {
			interval = next
		}
		// Policy auswerten (Checks + fällige Tasks) -> Ergebnisse für den nächsten Checkin.
		p.runPolicy(ctx)
		if autoUpdate && update.ShouldUpdate(version, latest) {
			p.log.Info("agent-update verfügbar – wird angewendet", "von", version, "auf", latest)
			if err := update.Apply(ctx, client); err != nil {
				p.log.Warn("selbst-update fehlgeschlagen", "err", err)
			} else {
				// Windows: Dienst-Neustart wurde angestoßen -> Schleife beenden.
				p.log.Info("update angewendet, Agent startet neu")
				return
			}
		}
		select {
		case <-ctx.Done():
			p.log.Info("agent beendet")
			return
		case <-p.checkinNow:
			// Vom Server angestoßen (neuer Befehl) -> sofort erneut einchecken.
		case <-time.After(interval):
		}
	}
}

// enroll registriert den Agent einmalig und speichert das erhaltene Token.
func (p *program) enroll(ctx context.Context, cfg agentcfg.Config, client *transport.Client) (agentcfg.State, error) {
	if cfg.EnrollmentToken == "" {
		return agentcfg.State{}, errNoEnrollToken
	}
	inv := collect.Collect(ctx, version)
	resp, err := client.Enroll(ctx, shared.EnrollRequest{
		EnrollmentToken: cfg.EnrollmentToken,
		Hostname:        inv.Hostname,
		OS:              inv.OS,
		OSVersion:       inv.OSVersion,
		AgentVersion:    version,
	})
	if err != nil {
		return agentcfg.State{}, err
	}
	st := agentcfg.State{AgentID: resp.AgentID, AgentToken: resp.AgentToken}
	if err := agentcfg.SaveState(cfg.StatePath, st); err != nil {
		return agentcfg.State{}, err
	}
	p.log.Info("agent enrolled", "agent_id", st.AgentID)
	return st, nil
}

// checkin sammelt das Inventar und meldet es; liefert ein ggf. vom Server vorgeschlagenes
// Intervall sowie die vom Server bereitgestellte Agent-Version.
func (p *program) checkin(ctx context.Context, client *transport.Client, state agentcfg.State, ver string) (time.Duration, string) {
	inv := collect.Collect(ctx, ver)
	p.mu.Lock()
	inv.OSUpdates = p.updates
	inv.PublicIP = p.publicIP
	checks := p.pendingChecks
	tasks := p.pendingTasks
	cmds := p.pendingCmdResults
	p.pendingChecks, p.pendingTasks, p.pendingCmdResults = nil, nil, nil
	p.mu.Unlock()

	resp, err := client.Checkin(ctx, state.AgentToken, shared.CheckinRequest{
		Inventory: inv, CheckResults: checks, TaskResults: tasks, CommandResults: cmds,
		Sample: collect.Sample(ctx),
	})
	if err != nil {
		p.log.Warn("checkin fehlgeschlagen", "err", err)
		// Einmal-Läufe (Tasks/Befehle) nicht verlieren; für den nächsten Versuch zurücklegen.
		p.mu.Lock()
		p.pendingTasks = append(tasks, p.pendingTasks...)
		p.pendingCmdResults = append(cmds, p.pendingCmdResults...)
		p.mu.Unlock()
		return 0, ""
	}
	p.mu.Lock()
	p.policy = resp.Policy
	p.commandQueue = append(p.commandQueue, resp.Commands...)
	p.mu.Unlock()
	p.log.Info("checkin ok", "hostname", inv.Hostname, "interfaces", len(inv.Interfaces))
	var next time.Duration
	if resp.NextCheckinSec > 0 {
		next = time.Duration(resp.NextCheckinSec) * time.Second
	}
	return next, resp.LatestAgentVersion
}

// requestCheckin stößt einen sofortigen Checkin an (nicht blockierend), z.B. um ein
// Befehlsergebnis ohne Wartezeit zu melden.
func (p *program) requestCheckin() {
	if p.checkinNow == nil {
		return
	}
	select {
	case p.checkinNow <- struct{}{}:
	default:
	}
}

// runPolicy führt zuerst empfangene Ad-hoc-Befehle aus und wertet danach – sofern
// eine Policy vorliegt – Checks aus und startet fällige Tasks.
func (p *program) runPolicy(ctx context.Context) {
	// 1) Ad-hoc-Befehle (laufen unabhängig von einer Policy).
	p.mu.Lock()
	queue := p.commandQueue
	p.commandQueue = nil
	bundle := p.policy
	var updCount *int
	if p.updates != nil {
		c := p.updates.Count
		updCount = &c
	}
	p.mu.Unlock()

	for _, cmd := range queue {
		var exit int
		var output string
		switch cmd.Type {
		case "run_script":
			shell, _ := cmd.Payload["shell"].(string)
			script, _ := cmd.Payload["script"].(string)
			var ok bool
			exit, output, ok = policy.RunScript(ctx, shell, script, stringList(cmd.Payload["platforms"]))
			if !ok {
				exit = -1
				output = "Nicht unterstützt auf diesem Betriebssystem (" + runtime.GOOS + ")"
			}
		case "scan_updates":
			u := collect.OSUpdates(ctx)
			p.mu.Lock()
			p.updates = u
			p.mu.Unlock()
			if u != nil {
				output = fmt.Sprintf("%d Updates gefunden", u.Count)
			} else {
				output = "Update-Status unbekannt"
			}
		case "install_updates":
			// Kann lange dauern -> asynchron, um den Checkin-Loop nicht zu blockieren.
			// full=true (Default) -> apt full-upgrade; false -> konservatives upgrade.
			full := true
			if v, ok := cmd.Payload["full"].(bool); ok {
				full = v
			}
			go p.installUpdates(ctx, cmd.ID, stringList(cmd.Payload["packages"]), full)
			p.log.Info("update-installation gestartet", "command", cmd.ID, "full", full)
			continue
		case "install_package":
			ids := map[string]string{}
			for _, k := range []string{"winget", "choco", "apt", "dnf", "brew"} {
				if v, ok := cmd.Payload[k].(string); ok {
					ids[k] = v
				}
			}
			go p.installPackage(ctx, cmd.ID, ids)
			p.log.Info("software-installation gestartet", "command", cmd.ID)
			continue
		case "dir_usage":
			// Verzeichnis-Scan kann lange dauern -> asynchron mit Timeout.
			path, _ := cmd.Payload["path"].(string)
			go p.scanDir(ctx, cmd.ID, path)
			p.log.Info("speicher-scan gestartet", "command", cmd.ID, "path", path)
			continue
		case "list_services":
			output = collect.ListServices(ctx)
		case "list_processes":
			output = collect.ListProcesses(ctx)
		case "service_control":
			name, _ := cmd.Payload["name"].(string)
			action, _ := cmd.Payload["action"].(string)
			exit, output = collect.ControlService(ctx, name, action)
		case "process_kill":
			pid := 0
			if f, ok := cmd.Payload["pid"].(float64); ok {
				pid = int(f)
			}
			exit, output = collect.KillProcess(int32(pid))
		case "wake_lan":
			mac, _ := cmd.Payload["mac"].(string)
			exit, output = collect.SendWOL(mac)
		case "browse_dir":
			path, _ := cmd.Payload["path"].(string)
			output = collect.BrowseDir(path)
		case "metrics":
			output = collect.MetricsJSON(ctx)
		case "av_status":
			output = collect.AVStatusJSON(ctx)
		case "bitlocker_status":
			output = collect.BitLockerJSON(ctx)
		case "smart_status":
			output = collect.SmartJSON(ctx)
		case "event_log":
			logName, _ := cmd.Payload["log"].(string)
			n := 100
			if f, ok := cmd.Payload["count"].(float64); ok {
				n = int(f)
			}
			output = collect.EventLogJSON(ctx, logName, n)
		case "read_file":
			path, _ := cmd.Payload["path"].(string)
			exit, output = p.readFile(ctx, cmd.ID, path)
		case "write_file":
			path, _ := cmd.Payload["path"].(string)
			xfer, _ := cmd.Payload["xfer"].(string)
			exit, output = p.writeFile(ctx, xfer, path)
		case "reboot":
			output = "Neustart wird eingeleitet"
			// Verzögert neu starten, damit dieses Ergebnis noch gemeldet werden kann.
			go func() {
				time.Sleep(12 * time.Second)
				_ = collect.Reboot(context.Background())
			}()
			p.log.Info("neustart angefordert", "command", cmd.ID)
		default:
			continue
		}
		p.mu.Lock()
		p.pendingCmdResults = append(p.pendingCmdResults, shared.CommandResult{
			CommandID: cmd.ID, ExitCode: exit, Output: output, RanAt: time.Now().UTC(),
		})
		p.mu.Unlock()
		p.log.Info("befehl ausgeführt", "command", cmd.ID, "type", cmd.Type, "exit", exit)
		p.requestCheckin() // Ergebnis sofort melden, nicht erst beim nächsten Intervall
	}

	if bundle == nil {
		return
	}

	now := time.Now()

	// 2) Fällige Checks auswerten (Häufigkeit je Check; "" = jeden Checkin). Nicht
	// fällige Checks werden nicht erneut gesendet – der Server behält ihr Ergebnis.
	var dueChecks []shared.CheckSpec
	for _, c := range bundle.Checks {
		p.mu.Lock()
		last := p.checkLastRun[c.ID]
		p.mu.Unlock()
		if freqDue(c.Frequency, last, now, "", "") {
			dueChecks = append(dueChecks, c)
		}
	}
	checkResults := policy.EvalChecks(ctx, dueChecks, updCount)
	p.mu.Lock()
	for _, c := range dueChecks {
		p.checkLastRun[c.ID] = now
	}
	p.mu.Unlock()

	// 3) Fällige Tasks ausführen (Häufigkeit bzw. Legacy-Plan).
	var taskResults []shared.TaskResult
	for _, t := range bundle.Tasks {
		p.mu.Lock()
		last := p.taskLastRun[t.ID]
		p.mu.Unlock()
		if !taskDue(t, last, now) {
			continue
		}
		exit, output, applicable := policy.RunScript(ctx, t.Shell, t.Script, t.Platforms)
		if !applicable {
			continue
		}
		p.mu.Lock()
		p.taskLastRun[t.ID] = now
		p.mu.Unlock()
		taskResults = append(taskResults, shared.TaskResult{
			TaskID: t.ID, ExitCode: exit, Output: output, RanAt: time.Now().UTC(),
		})
		p.log.Info("task ausgeführt", "task", t.Name, "exit", exit)
	}

	p.mu.Lock()
	p.pendingChecks = checkResults // jüngste Bewertung ersetzt ältere
	p.pendingTasks = append(p.pendingTasks, taskResults...)
	p.mu.Unlock()

	// Frische Check-/Task-Ergebnisse sofort melden, nicht erst beim nächsten
	// Intervall – sonst hinkt das Ergebnis (und die Benachrichtigung) der
	// eigentlichen Auswertung einen ganzen Checkin-Zyklus hinterher.
	if len(checkResults) > 0 || len(taskResults) > 0 {
		p.requestCheckin()
	}
}

// installUpdates installiert Updates (lang laufend) und meldet das Ergebnis; danach
// wird der Update-Status neu ermittelt.
// stringList wandelt einen JSON-Payload-Wert ([]any) in []string um.
func stringList(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, x := range arr {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// scanDir führt einen (potenziell langen) Verzeichnis-Scan aus und meldet das
// JSON-Ergebnis als Befehls-Ergebnis. Mit Zeitlimit, damit ein riesiger Pfad
// nicht endlos läuft.
func (p *program) scanDir(ctx context.Context, cmdID, path string) {
	if path == "" {
		p.mu.Lock()
		p.pendingCmdResults = append(p.pendingCmdResults, shared.CommandResult{
			CommandID: cmdID, ExitCode: 1, Output: `{"error":"kein Pfad angegeben"}`, RanAt: time.Now().UTC(),
		})
		p.mu.Unlock()
		p.requestCheckin()
		return
	}
	sctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	var lastJSON string
	// Zwischenstände live an den Server melden (gedrosselt durch DirUsageStream).
	collect.DirUsageStream(sctx, path, func(r collect.DUResult) {
		b, _ := json.Marshal(r)
		lastJSON = string(b)
		if p.client != nil {
			if err := p.client.ReportProgress(sctx, p.agentToken, cmdID, lastJSON); err != nil {
				p.log.Debug("scan-fortschritt melden", "err", err)
			}
		}
	})
	// Endstand als reguläres Befehls-Ergebnis (Status -> done) über den Checkin.
	p.mu.Lock()
	p.pendingCmdResults = append(p.pendingCmdResults, shared.CommandResult{
		CommandID: cmdID, ExitCode: 0, Output: lastJSON, RanAt: time.Now().UTC(),
	})
	p.mu.Unlock()
	p.log.Info("speicher-scan abgeschlossen", "command", cmdID, "path", path)
	p.requestCheckin()
}

// readFile liest eine Datei und lädt sie (unter dem Befehls-ID als Transfer-Key)
// zum Server hoch, wo der Browser sie abholt. Rückgabe ist ein kleiner Statustext.
func (p *program) readFile(ctx context.Context, cmdID, path string) (int, string) {
	if path == "" {
		return 1, "kein Pfad angegeben"
	}
	data, err := collect.ReadFileCapped(path)
	if err != nil {
		return 1, "Lesen fehlgeschlagen: " + err.Error()
	}
	if p.client == nil {
		return 1, "kein Server-Client"
	}
	name := filepath.Base(path)
	if err := p.client.UploadFile(ctx, p.agentToken, cmdID, name, data); err != nil {
		return 1, "Upload zum Server fehlgeschlagen: " + err.Error()
	}
	return 0, fmt.Sprintf("%d Bytes bereit", len(data))
}

// writeFile holt die vom Browser hochgeladenen Bytes und schreibt sie an path.
func (p *program) writeFile(ctx context.Context, xfer, path string) (int, string) {
	if path == "" || xfer == "" {
		return 1, "Pfad oder Transfer fehlt"
	}
	if p.client == nil {
		return 1, "kein Server-Client"
	}
	data, err := p.client.DownloadPayload(ctx, p.agentToken, xfer)
	if err != nil {
		return 1, "Abholen der Daten fehlgeschlagen: " + err.Error()
	}
	if err := collect.WriteFileCapped(path, data); err != nil {
		return 1, "Schreiben fehlgeschlagen: " + err.Error()
	}
	return 0, fmt.Sprintf("%d Bytes geschrieben nach %s", len(data), path)
}

// installPackage installiert ein Paket über den Paketmanager und meldet das Ergebnis.
func (p *program) installPackage(ctx context.Context, cmdID string, ids map[string]string) {
	exit, output := collect.InstallPackage(ctx, ids)
	p.mu.Lock()
	p.pendingCmdResults = append(p.pendingCmdResults, shared.CommandResult{
		CommandID: cmdID, ExitCode: exit, Output: output, RanAt: time.Now().UTC(),
	})
	p.mu.Unlock()
	p.log.Info("software-installation abgeschlossen", "command", cmdID, "exit", exit)
	p.requestCheckin()
}

func (p *program) installUpdates(ctx context.Context, cmdID string, names []string, full bool) {
	exit, output := collect.InstallUpdates(ctx, names, full)
	u := collect.OSUpdates(ctx)
	p.mu.Lock()
	if u != nil {
		p.updates = u
	}
	p.pendingCmdResults = append(p.pendingCmdResults, shared.CommandResult{
		CommandID: cmdID, ExitCode: exit, Output: output, RanAt: time.Now().UTC(),
	})
	p.mu.Unlock()
	p.log.Info("update-installation abgeschlossen", "command", cmdID, "exit", exit)
	p.requestCheckin()
}

// taskDue entscheidet, ob ein Task jetzt fällig ist. Ist eine Häufigkeit gesetzt,
// gilt diese; sonst der Legacy-Plan (Intervall/täglich).
func taskDue(t shared.TaskSpec, last, now time.Time) bool {
	if t.Frequency != "" {
		return freqDue(t.Frequency, last, now, t.DailyTime, t.Weekdays)
	}
	if t.ScheduleType == "daily" {
		if !weekdayAllowed(t.Weekdays, now.Weekday()) {
			return false
		}
		sched, ok := parseHHMM(t.DailyTime)
		if !ok {
			return false
		}
		if now.Hour()*60+now.Minute() < sched {
			return false
		}
		return last.IsZero() || !sameDay(last, now) // heute noch nicht gelaufen
	}
	if t.IntervalMinutes <= 0 {
		return false
	}
	return now.Sub(last) >= time.Duration(t.IntervalMinutes)*time.Minute
}

// freqIntervals bildet die Sub-Tages-Presets auf Dauern ab.
var freqIntervals = map[string]time.Duration{
	"1m": time.Minute, "5m": 5 * time.Minute, "15m": 15 * time.Minute, "30m": 30 * time.Minute,
	"1h": time.Hour, "2h": 2 * time.Hour, "6h": 6 * time.Hour, "12h": 12 * time.Hour,
}

// freqDue entscheidet anhand eines Häufigkeits-Presets, ob jetzt ausgeführt werden soll.
// "" = immer (jeder Checkin). Kalender-Presets laufen einmal je Periode; daily/weekly
// berücksichtigen optional Uhrzeit/Wochentage.
func freqDue(freq string, last, now time.Time, dailyTime, weekdays string) bool {
	if d, ok := freqIntervals[freq]; ok {
		return now.Sub(last) >= d
	}
	switch freq {
	case "daily":
		if !weekdayAllowed(weekdays, now.Weekday()) {
			return false
		}
		if sched, ok := parseHHMM(dailyTime); ok && now.Hour()*60+now.Minute() < sched {
			return false
		}
		return last.IsZero() || !sameDay(last, now)
	case "weekly":
		ly, lw := last.ISOWeek()
		ny, nw := now.ISOWeek()
		return last.IsZero() || ly != ny || lw != nw
	case "monthly":
		return last.IsZero() || last.Year() != now.Year() || last.Month() != now.Month()
	case "yearly":
		return last.IsZero() || last.Year() != now.Year()
	default:
		return true // "" oder unbekannt -> jeden Checkin
	}
}

func weekdayAllowed(weekdays string, wd time.Weekday) bool {
	weekdays = strings.TrimSpace(weekdays)
	if weekdays == "" {
		return true
	}
	for _, p := range strings.Split(weekdays, ",") {
		if n, err := strconv.Atoi(strings.TrimSpace(p)); err == nil && time.Weekday(n) == wd {
			return true
		}
	}
	return false
}

func parseHHMM(s string) (int, bool) {
	parts := strings.SplitN(strings.TrimSpace(s), ":", 2)
	if len(parts) != 2 {
		return 0, false
	}
	h, err1 := strconv.Atoi(parts[0])
	m, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, false
	}
	return h*60 + m, true
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

// updateChecker sucht initial und danach periodisch nach OS-Updates und legt das
// Ergebnis im Cache ab, den jeder Checkin mitschickt.
func (p *program) updateChecker(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 6 * time.Hour
	}
	check := func() {
		u := collect.OSUpdates(ctx)
		p.mu.Lock()
		p.updates = u
		p.mu.Unlock()
		if u != nil {
			p.log.Info("os-update-check", "verfügbar", u.Count)
		}
	}
	check()
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			check()
		}
	}
}

func defaultConfigPath() string {
	if runtime.GOOS == "windows" {
		return `C:\ProgramData\PC-Inventory\agent.yaml`
	}
	return "/etc/pc-inventory/agent.yaml"
}
