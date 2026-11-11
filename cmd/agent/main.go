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
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	// Zeitzonen-Datenbank einbetten: der Agent plant in der zentral eingestellten Zone,
	// und time.LoadLocation findet unter Windows (keine Registry-Auswertung in Go) und in
	// schlanken Containern sonst nichts. Kostet ~450 KB, hält das Binary selbst-enthalten.
	_ "time/tzdata"

	"github.com/kardianos/service"

	"github.com/boonkerz/roster/internal/agent/authlog"
	"github.com/boonkerz/roster/internal/agent/collect"
	"github.com/boonkerz/roster/internal/agent/compliance"
	agentcfg "github.com/boonkerz/roster/internal/agent/config"
	"github.com/boonkerz/roster/internal/agent/fim"
	"github.com/boonkerz/roster/internal/agent/logmon"
	"github.com/boonkerz/roster/internal/agent/flow"
	"github.com/boonkerz/roster/internal/agent/persist"
	"github.com/boonkerz/roster/internal/agent/policy"
	"github.com/boonkerz/roster/internal/agent/remote"
	"github.com/boonkerz/roster/internal/agent/response"
	"github.com/boonkerz/roster/internal/agent/rootcheck"
	"github.com/boonkerz/roster/internal/agent/statemon"
	"github.com/boonkerz/roster/internal/agent/transport"
	"github.com/boonkerz/roster/internal/agent/update"
	"github.com/boonkerz/roster/internal/netscan"
	"github.com/boonkerz/roster/internal/shared"
	"github.com/boonkerz/roster/internal/snmp"
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

	log := slog.New(slog.NewTextHandler(logWriter(*configPath), &slog.HandlerOptions{Level: slog.LevelInfo}))

	prg := &program{configPath: *configPath, log: log}
	svcConfig := &service.Config{
		Name:        "roster-agent",
		DisplayName: "Roster Agent",
		Description: "Meldet Hardware-, MAC- und IP-Inventar an den Roster-Server.",
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
	// patchHistory wartet auf den nächsten Checkin (vom Update-Check bzw. nach einer
	// Installation neu gelesen); nach erfolgreichem Versand leer.
	patchHistory []shared.PatchEvent

	// Datei-Integrität: Zustandsverzeichnis (Baseline-Datei), wartende Ereignisse,
	// Anstoß für sofortigen Lauf und Reset-Wunsch (Baseline verwerfen).
	stateDir  string
	fimEvents []shared.FIMEvent
	// imageScans sind fertige Image-Paketlisten für den nächsten Checkin; imageScanned merkt
	// sich je Image-ID den letzten Scan (täglich), imageScanNow stößt einen Lauf an.
	imageScans   []shared.ImageScan
	imageScanned map[string]time.Time
	imageScanNow chan struct{}
	fimNow       chan struct{}
	fimReset     bool
	// Rootkit-Prüfung: Anstoß für sofortigen Lauf (Befehl rootcheck_now).
	rootcheckNow chan struct{}
	// Anmeldungs-Überwachung: wartende Ereignisse (Cursor hält das authlog-Paket).
	authEvents []shared.AuthEvent
	// Lesestatus der Log-Quellen (kumuliert seit Agent-Start), geht mit jedem Checkin mit.
	logStatus map[string]shared.LogSourceStatus
	// Compliance: letzter Bericht (für den Check-Typ) und ob er noch zu senden ist.
	compliance        *shared.ComplianceReport
	compliancePending bool
	complianceNow     chan struct{}

	policy            *shared.PolicyBundle   // zuletzt empfangene Policy
	pendingChecks     []shared.CheckResult   // beim nächsten Checkin zu meldende Check-Ergebnisse
	pendingTasks      []shared.TaskResult    // beim nächsten Checkin zu meldende Task-Läufe
	commandQueue      []shared.Command       // empfangene Ad-hoc-Befehle, noch auszuführen
	pendingCmdResults []shared.CommandResult // beim nächsten Checkin zu meldende Befehls-Ergebnisse
	backupRunning     bool                   // ein vzdump-Lauf ist aktiv (kein zweiter parallel)
	taskLastRun       map[string]time.Time   // letzte Ausführung je Task-ID
	checkLastRun      map[string]time.Time   // letzte Auswertung je Check-ID (für Häufigkeit)
	checkLastStatus   map[string]string      // zuletzt gemeldeter Status je Check-ID
	lastQuickCheckin  time.Time              // letzter Sofort-Checkin wegen Check-/Task-Ergebnissen
	checkinNow        chan struct{}          // Server-Push: sofort einchecken
	tzName            string                 // zuletzt vom Server gemeldete Zeitzone
	tzLoc             *time.Location         // dazu aufgelöste Zone (nil = lokale Zeit)
}

// scheduleLocation liefert die Zone, in der Zeitpläne gerechnet werden. Der Name kommt im
// Policy-Bundle vom Server; leer oder unbekannt heißt lokale Zeit wie früher. Ergebnis
// wird gecacht, LoadLocation liest sonst bei jedem Checkin die Zonendatei.
func (p *program) scheduleLocation(name string) *time.Location {
	name = strings.TrimSpace(name)
	p.mu.Lock()
	defer p.mu.Unlock()
	if name == "" {
		p.tzName, p.tzLoc = "", nil
		return time.Local
	}
	if p.tzName == name && p.tzLoc != nil {
		return p.tzLoc
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		p.log.Warn("zeitzone nicht auflösbar, nutze lokale Zeit", "zone", name, "err", err)
		p.tzName, p.tzLoc = "", nil
		return time.Local
	}
	p.tzName, p.tzLoc = name, loc
	return loc
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
	p.stateDir = filepath.Dir(cfg.StatePath)
	response.Init(p.stateDir, cfg.ServerURL)
	go p.responseExpireLoop(ctx)
	p.fimNow = make(chan struct{}, 1)
	go p.fimLoop(ctx)
	go p.authLoop(ctx)
	go p.stateLoop(ctx)
	p.rootcheckNow = make(chan struct{}, 1)
	go p.rootcheckLoop(ctx)
	p.imageScanNow = make(chan struct{}, 1)
	go p.imageScanLoop(ctx)
	// OPNsense/FreeBSD: Installation upgrade-fest machen (Plugin-Registrierung, rc.conf.d).
	go persist.Ensure(ctx, p.log, version)
	p.complianceNow = make(chan struct{}, 1)
	policy.ComplianceReport = func() *shared.ComplianceReport {
		p.mu.Lock()
		defer p.mu.Unlock()
		return p.compliance
	}
	go p.complianceLoop(ctx)
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
	p.checkLastStatus = map[string]string{}
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
	history := p.patchHistory
	// FIM-Ereignisse in Portionen (max. 200 je Checkin), Rest bleibt in der Warteschlange.
	fimEv := p.fimEvents
	if len(fimEv) > 200 {
		fimEv, p.fimEvents = fimEv[:200], fimEv[200:]
	} else {
		p.fimEvents = nil
	}
	imgScans := p.imageScans
	p.imageScans = nil
	authEv := p.authEvents
	if len(authEv) > 500 {
		authEv, p.authEvents = authEv[:500], authEv[500:]
	} else {
		p.authEvents = nil
	}
	var logStatus []shared.LogSourceStatus
	for _, st := range p.logStatus {
		logStatus = append(logStatus, st)
	}
	sort.Slice(logStatus, func(i, j int) bool { return logStatus[i].ID < logStatus[j].ID })
	var compliance *shared.ComplianceReport
	if p.compliancePending {
		compliance = p.compliance
		p.compliancePending = false
	}
	p.pendingChecks, p.pendingTasks, p.pendingCmdResults, p.patchHistory = nil, nil, nil, nil
	p.mu.Unlock()

	sample := collect.Sample(ctx)
	if sample != nil {
		sample.Temp = collect.HeadlineTemperature(inv.Temperatures) // für den Temperaturverlauf
	}
	resp, err := client.Checkin(ctx, state.AgentToken, shared.CheckinRequest{
		Inventory: inv, CheckResults: checks, TaskResults: tasks, CommandResults: cmds,
		Sample: sample, PatchHistory: history, FIMEvents: fimEv, AuthEvents: authEv, Compliance: compliance,
		LogSources: logStatus, ImageScans: imgScans,
	})
	if err != nil {
		p.log.Warn("checkin fehlgeschlagen", "err", err)
		// Einmal-Läufe (Tasks/Befehle) nicht verlieren; für den nächsten Versuch zurücklegen.
		p.mu.Lock()
		p.pendingTasks = append(tasks, p.pendingTasks...)
		p.pendingCmdResults = append(cmds, p.pendingCmdResults...)
		if p.patchHistory == nil {
			p.patchHistory = history
		}
		p.fimEvents = append(fimEv, p.fimEvents...)
		p.authEvents = append(authEv, p.authEvents...)
		p.imageScans = append(imgScans, p.imageScans...)
		if compliance != nil {
			p.compliancePending = true
		}
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
// bulkCommand sind Massenbefehle (Active Response / Flottensperre), deren Ergebnis nicht
// sofort gemeldet werden muss: Bei vielen Sperren je Minute checkte sonst jedes Gerät
// ständig ein (Sechsfaches der regulären Last auf dem Server).
var bulkCommand = map[string]bool{"block_ip": true, "unblock_ip": true}

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

	// Per Rerun angeforderte Checks/Tasks: werden unten zusätzlich zu den fälligen
	// ausgeführt – unabhängig vom Zeitplan („sofort neu starten").
	var forceChecks, forceTasks []string

	var reportNow bool
	for _, cmd := range queue {
		var exit int
		var output string
		// Synchrone Befehle bekommen ein Zeitlimit, damit ein hängendes Werkzeug (z. B. die
		// Windows-Update-Suche auf alten Servern, ein blockierter Dienst-Aufruf) nicht die
		// ganze Checkin-Schleife anhält – der Agent gälte sonst als offline.
		cmdCtx, cancelCmd := context.WithTimeout(ctx, cmdTimeout)
		defer cancelCmd()
		switch cmd.Type {
		case "run_check":
			cid, _ := cmd.Payload["check_id"].(string)
			found := false
			if bundle != nil {
				for _, c := range bundle.Checks {
					if c.ID == cid {
						found = true
						break
					}
				}
			}
			if !found {
				exit, output = 1, "Check nicht (mehr) zugewiesen"
				break
			}
			forceChecks = append(forceChecks, cid)
			output = "Check wird neu ausgewertet"
		case "run_task":
			tid, _ := cmd.Payload["task_id"].(string)
			found := false
			if bundle != nil {
				for _, t := range bundle.Tasks {
					if t.ID == tid {
						found = true
						break
					}
				}
			}
			if !found {
				exit, output = 1, "Task nicht (mehr) zugewiesen"
				break
			}
			forceTasks = append(forceTasks, tid)
			output = "Task wird neu gestartet"
		case "run_script":
			shell, _ := cmd.Payload["shell"].(string)
			script, _ := cmd.Payload["script"].(string)
			var ok bool
			exit, output, ok = policy.RunScriptEnv(cmdCtx, shell, script, stringList(cmd.Payload["platforms"]), stringMap(cmd.Payload["env"]))
			if !ok {
				exit = -1
				output = "Nicht unterstützt auf diesem Betriebssystem (" + runtime.GOOS + ")"
			}
		case "run_flow":
			// Abläufe (Flow-Designer) können lange dauern (pull/up) -> asynchron.
			go p.runFlow(ctx, cmd.ID, cmd.Payload)
			continue
		case "scan_updates":
			// Die Update-Suche kann auf alten Windows-Servern Stunden dauern -> asynchron.
			go p.scanUpdates(ctx, cmd.ID)
			continue
		case "install_updates":
			// Kann lange dauern -> asynchron, um den Checkin-Loop nicht zu blockieren.
			// full=true (Default) -> apt full-upgrade; false -> konservatives upgrade.
			full := true
			if v, ok := cmd.Payload["full"].(bool); ok {
				full = v
			}
			phased, _ := cmd.Payload["phased"].(bool)
			go p.installUpdates(ctx, cmd.ID, stringList(cmd.Payload["packages"]), full, phased)
			p.log.Info("update-installation gestartet", "command", cmd.ID, "full", full, "phased", phased)
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
		case "network_scan":
			cidr, _ := cmd.Payload["cidr"].(string)
			go p.networkScan(ctx, cmd.ID, cidr)
			p.log.Info("netzwerk-scan gestartet", "command", cmd.ID, "cidr", cidr)
			continue
		case "snmp_query":
			ip, _ := cmd.Payload["ip"].(string)
			community, _ := cmd.Payload["community"].(string)
			go p.snmpQuery(ctx, cmd.ID, ip, community)
			continue
		case "dir_usage":
			// Verzeichnis-Scan kann lange dauern -> asynchron mit Timeout.
			path, _ := cmd.Payload["path"].(string)
			go p.scanDir(ctx, cmd.ID, path)
			p.log.Info("speicher-scan gestartet", "command", cmd.ID, "path", path)
			continue
		case "list_services":
			output = collect.ListServices(cmdCtx)
		case "list_processes":
			output = collect.ListProcesses(cmdCtx)
		case "list_resolutions":
			output = collect.ListResolutions()
		case "set_resolution":
			w, h := 0, 0
			if f, ok := cmd.Payload["width"].(float64); ok {
				w = int(f)
			}
			if f, ok := cmd.Payload["height"].(float64); ok {
				h = int(f)
			}
			exit, output = collect.SetResolution(w, h)
		case "service_control":
			name, _ := cmd.Payload["name"].(string)
			action, _ := cmd.Payload["action"].(string)
			exit, output = collect.ControlService(cmdCtx, name, action)
		case "docker_start", "docker_stop", "docker_restart":
			id, _ := cmd.Payload["container_id"].(string)
			exit, output = collect.DockerControl(cmdCtx, id, strings.TrimPrefix(cmd.Type, "docker_"))
		case "proxmox_backup":
			// Kann Stunden dauern -> asynchron; Zwischenstände meldet der Lauf selbst.
			var spec collect.BackupSpec
			if raw, err := json.Marshal(cmd.Payload); err == nil {
				_ = json.Unmarshal(raw, &spec)
			}
			go p.proxmoxBackup(ctx, cmd.ID, spec)
			continue
		case "proxmox_prune":
			// Löschen großer Archive auf NFS kann dauern -> asynchron wie das Backup.
			var spec collect.PruneSpec
			if raw, err := json.Marshal(cmd.Payload); err == nil {
				_ = json.Unmarshal(raw, &spec)
			}
			go p.proxmoxPrune(ctx, cmd.ID, spec)
			continue
		case "proxmox_start", "proxmox_stop", "proxmox_shutdown", "proxmox_reboot":
			// Herunterfahren kann Minuten dauern -> asynchron, Ergebnis + frisches
			// Inventar (neuer Gast-Status) kommen mit dem nächsten Checkin.
			node, _ := cmd.Payload["node"].(string)
			gtype, _ := cmd.Payload["type"].(string)
			vmid := 0
			if f, ok := cmd.Payload["vmid"].(float64); ok {
				vmid = int(f)
			}
			go p.proxmoxControl(ctx, cmd.ID, node, gtype, vmid, strings.TrimPrefix(cmd.Type, "proxmox_"))
			continue
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
			output = collect.MetricsJSON(cmdCtx)
		case "av_status":
			output = collect.AVStatusJSON(cmdCtx)
		case "bitlocker_status":
			output = collect.BitLockerJSON(cmdCtx)
		case "smart_status":
			output = collect.SmartJSON(cmdCtx)
		case "event_log":
			logName, _ := cmd.Payload["log"].(string)
			n := 100
			if f, ok := cmd.Payload["count"].(float64); ok {
				n = int(f)
			}
			output = collect.EventLogJSON(cmdCtx, logName, n)
		case "read_file", "write_file":
			// Große Dateien brauchen Zeit – im Hintergrund, sonst stünde die Checkin-Schleife.
			go p.fileTransfer(ctx, cmd)
			continue
		case "make_dir":
			path, _ := cmd.Payload["path"].(string)
			if err := collect.MakeDir(path); err != nil {
				exit, output = 1, err.Error()
			} else {
				output = "Ordner angelegt"
			}
		case "delete_path":
			path, _ := cmd.Payload["path"].(string)
			if err := collect.DeletePath(path); err != nil {
				exit, output = 1, err.Error()
			} else {
				output = "Gelöscht"
			}
		case "block_ip":
			ip, _ := cmd.Payload["ip"].(string)
			minutes := 0
			if f, ok := cmd.Payload["minutes"].(float64); ok {
				minutes = int(f)
			}
			exit, output = response.BlockIP(cmdCtx, ip, minutes)
		case "unblock_ip":
			ip, _ := cmd.Payload["ip"].(string)
			exit, output = response.UnblockIP(cmdCtx, ip)
		case "lock_user":
			user, _ := cmd.Payload["user"].(string)
			exit, output = response.LockUser(cmdCtx, user)
		case "unlock_user":
			user, _ := cmd.Payload["user"].(string)
			exit, output = response.UnlockUser(cmdCtx, user)
		case "list_blocks":
			exit, output = 0, response.ListJSON()
		case "test_log_source":
			// Probelauf einer Log-Quelle (aus dem Richtlinien-Editor): letzte Zeilen lesen
			// und mit dem Format parsen, Ergebnis als JSON.
			var src shared.LogSource
			if b, err := json.Marshal(cmd.Payload["source"]); err == nil {
				_ = json.Unmarshal(b, &src)
			}
			n := 300
			if f, ok := cmd.Payload["lines"].(float64); ok {
				n = int(f)
			}
			res := logmon.Test(cmdCtx, src, n)
			if res.Error != "" {
				exit = 1
			}
			b, _ := json.Marshal(res)
			output = string(b)
		case "compliance_scan":
			select {
			case p.complianceNow <- struct{}{}:
			default:
			}
			exit, output = 0, "Compliance-Prüfung angestoßen"
		case "rootcheck_now":
			select {
			case p.rootcheckNow <- struct{}{}:
			default:
			}
			exit, output = 0, "Rootkit-Prüfung angestoßen"
		case "scan_images":
			p.mu.Lock()
			p.imageScanned = map[string]time.Time{}
			p.mu.Unlock()
			select {
			case p.imageScanNow <- struct{}{}:
			default:
			}
			exit, output = 0, "Image-Scan angestoßen"
		case "fim_rescan", "fim_reset":
			p.mu.Lock()
			if cmd.Type == "fim_reset" {
				p.fimReset = true
			}
			p.mu.Unlock()
			select {
			case p.fimNow <- struct{}{}:
			default:
			}
			exit, output = 0, "Datei-Integritätsprüfung angestoßen"
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
		if cmdCtx.Err() == context.DeadlineExceeded {
			exit, output = 124, strings.TrimSpace(output+"\n[abgebrochen: Zeitlimit "+cmdTimeout.String()+" überschritten]")
		}
		p.mu.Lock()
		p.pendingCmdResults = append(p.pendingCmdResults, shared.CommandResult{
			CommandID: cmd.ID, ExitCode: exit, Output: output, RanAt: time.Now().UTC(),
		})
		p.mu.Unlock()
		p.log.Info("befehl ausgeführt", "command", cmd.ID, "type", cmd.Type, "exit", exit)
		// Ergebnis melden: interaktive Befehle sofort; Sperren (Massenbefehle) erst mit dem
		// nächsten regulären Checkin – jeder zusätzliche Checkin je Sperre vervielfachte
		// die Serverlast bei vielen Vorfällen.
		if !bulkCommand[cmd.Type] {
			reportNow = true
		}
	}
	if reportNow {
		p.requestCheckin()
	}

	if bundle == nil {
		return
	}

	// Zeitpläne in der zentral eingestellten Zone rechnen – sonst bedeutet "täglich 02:00"
	// auf jedem Gerät der Flotte einen anderen Moment.
	now := time.Now().In(p.scheduleLocation(bundle.Timezone))

	// 2) Fällige Checks auswerten (Häufigkeit je Check; "" = jeden Checkin). Nicht
	// fällige Checks werden nicht erneut gesendet – der Server behält ihr Ergebnis.
	var dueChecks []shared.CheckSpec
	for _, c := range bundle.Checks {
		p.mu.Lock()
		last := p.checkLastRun[c.ID]
		p.mu.Unlock()
		if freqDue(c.Frequency, last.In(now.Location()), now, "", "") || contains(forceChecks, c.ID) {
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
		if !taskDue(t, last.In(now.Location()), now) && !contains(forceTasks, t.ID) {
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

	// Hat sich ein Check-Status geändert? Nur dann lohnt ein Sofort-Checkin –
	// Checks mit Häufigkeit „jeder Checkin“ sind sonst immer „frisch“ und würden
	// eine Endlosschleife aus Sofort-Checkins auslösen (Checkin-Sturm).
	changed := false
	p.mu.Lock()
	for _, r := range checkResults {
		if p.checkLastStatus[r.CheckID] != r.Status {
			changed = true
		}
		p.checkLastStatus[r.CheckID] = r.Status
	}
	p.pendingChecks = checkResults // jüngste Bewertung ersetzt ältere
	p.pendingTasks = append(p.pendingTasks, taskResults...)
	quickOK := time.Since(p.lastQuickCheckin) >= quickCheckinMinGap
	if (changed || len(taskResults) > 0) && quickOK {
		p.lastQuickCheckin = time.Now()
	}
	p.mu.Unlock()

	// Geänderte Check-Status und Task-Läufe sofort melden, nicht erst beim nächsten
	// Intervall – sonst hinkt das Ergebnis (und die Benachrichtigung) der
	// eigentlichen Auswertung einen ganzen Checkin-Zyklus hinterher. Höchstens
	// einmal je quickCheckinMinGap, damit ein flatternder Check keinen Sturm auslöst.
	if (changed || len(taskResults) > 0) && quickOK {
		p.requestCheckin()
	}
}

// quickCheckinMinGap ist der Mindestabstand zwischen zwei Sofort-Checkins, die durch
// Check-/Task-Ergebnisse ausgelöst werden.
const quickCheckinMinGap = 30 * time.Second

// installUpdates installiert Updates (lang laufend) und meldet das Ergebnis; danach
// wird der Update-Status neu ermittelt.
// stringList wandelt einen JSON-Payload-Wert ([]any) in []string um.
// stringMap liest eine JSON-Map mit Textwerten (z. B. env eines run_script-Befehls).
func stringMap(v any) map[string]string {
	raw, ok := v.(map[string]any)
	if !ok || len(raw) == 0 {
		return nil
	}
	out := make(map[string]string, len(raw))
	for k, val := range raw {
		if s, ok := val.(string); ok {
			out[k] = s
		}
	}
	return out
}

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

// fileTransfer führt read_file/write_file im Hintergrund aus, meldet unterwegs den
// Fortschritt und liefert das Ergebnis über den nächsten Checkin.
func (p *program) fileTransfer(ctx context.Context, cmd shared.Command) {
	tctx, cancel := context.WithTimeout(ctx, 6*time.Hour)
	defer cancel()
	path, _ := cmd.Payload["path"].(string)
	var exit int
	var output string
	if cmd.Type == "read_file" {
		exit, output = p.readFile(tctx, cmd.ID, path)
	} else {
		xfer, _ := cmd.Payload["xfer"].(string)
		exit, output = p.writeFile(tctx, cmd.ID, xfer, path)
	}
	p.mu.Lock()
	p.pendingCmdResults = append(p.pendingCmdResults, shared.CommandResult{
		CommandID: cmd.ID, ExitCode: exit, Output: output, RanAt: time.Now().UTC(),
	})
	p.mu.Unlock()
	p.log.Info("datei-übertragung beendet", "command", cmd.ID, "typ", cmd.Type, "exit", exit, "ergebnis", output)
	p.requestCheckin()
}

// transferProgress meldet höchstens alle 2 s den Stand („120 / 1024 MB“).
func (p *program) transferProgress(ctx context.Context, cmdID, verb string, total int64) func(int64) {
	var last time.Time
	return func(n int64) {
		if p.client == nil || time.Since(last) < 2*time.Second {
			return
		}
		last = time.Now()
		msg := fmt.Sprintf("%s: %s", verb, collect.FormatTransfer(n, total))
		_ = p.client.ReportProgress(ctx, p.agentToken, cmdID, msg)
	}
}

// readFile lädt eine Datei (unter der Befehls-ID als Transfer-Key) gestreamt zum Server
// hoch, wo der Browser sie abholt. Rückgabe ist ein kleiner Statustext.
func (p *program) readFile(ctx context.Context, cmdID, path string) (int, string) {
	if path == "" {
		return 1, "kein Pfad angegeben"
	}
	f, size, err := collect.OpenForTransfer(path)
	if err != nil {
		return 1, "Lesen fehlgeschlagen: " + err.Error()
	}
	defer f.Close()
	if p.client == nil {
		return 1, "kein Server-Client"
	}
	r := collect.CountingReader(f, p.transferProgress(ctx, cmdID, "Gerät → Server", size))
	if err := p.client.UploadFile(ctx, p.agentToken, cmdID, filepath.Base(path), r, size); err != nil {
		return 1, "Upload zum Server fehlgeschlagen: " + err.Error()
	}
	return 0, fmt.Sprintf("%d Bytes bereit", size)
}

// writeFile holt die vom Browser hochgeladenen Bytes gestreamt ab und schreibt sie an
// path (erst in eine Zwischendatei daneben, dann umbenennen – nie halbe Dateien).
func (p *program) writeFile(ctx context.Context, cmdID, xfer, path string) (int, string) {
	if path == "" || xfer == "" {
		return 1, "Pfad oder Transfer fehlt"
	}
	if p.client == nil {
		return 1, "kein Server-Client"
	}
	body, size, err := p.client.DownloadPayload(ctx, p.agentToken, xfer)
	if err != nil {
		return 1, "Abholen der Daten fehlgeschlagen: " + err.Error()
	}
	defer body.Close()
	r := collect.CountingReader(body, p.transferProgress(ctx, cmdID, "Server → Gerät", size))
	n, err := collect.WriteFromReader(path, r, size)
	if err != nil {
		return 1, "Schreiben fehlgeschlagen: " + err.Error()
	}
	return 0, fmt.Sprintf("%d Bytes geschrieben nach %s", n, path)
}

// networkScan tastet den CIDR-Bereich ab und meldet die Funde als JSON (Output).
func (p *program) networkScan(ctx context.Context, cmdID, cidr string) {
	exit, output := 0, ""
	hosts, err := netscan.Scan(ctx, cidr)
	if err != nil {
		exit, output = 1, "Scan fehlgeschlagen: "+err.Error()
	} else {
		b, _ := json.Marshal(hosts)
		output = string(b)
	}
	p.mu.Lock()
	p.pendingCmdResults = append(p.pendingCmdResults, shared.CommandResult{
		CommandID: cmdID, ExitCode: exit, Output: output, RanAt: time.Now().UTC(),
	})
	p.mu.Unlock()
	p.log.Info("netzwerk-scan abgeschlossen", "command", cmdID, "hosts", len(hosts))
	p.requestCheckin()
}

// snmpQuery liest einen Drucker per SNMP aus und meldet die Daten als JSON.
func (p *program) snmpQuery(ctx context.Context, cmdID, ip, community string) {
	exit, output := 0, ""
	info, err := snmp.Query(ctx, ip, community)
	if err != nil {
		exit, output = 1, "SNMP fehlgeschlagen: "+err.Error()
	} else {
		b, _ := json.Marshal(info)
		output = string(b)
	}
	p.mu.Lock()
	p.pendingCmdResults = append(p.pendingCmdResults, shared.CommandResult{
		CommandID: cmdID, ExitCode: exit, Output: output, RanAt: time.Now().UTC(),
	})
	p.mu.Unlock()
	p.requestCheckin()
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

// proxmoxBackup führt einen vzdump-Lauf aus und meldet Zwischenstände. Ein zweiter
// Lauf wird abgewiesen: parallele vzdump-Läufe behindern sich gegenseitig, und der
// Server erwartet je Lauf genau ein Ergebnis.
func (p *program) proxmoxBackup(ctx context.Context, cmdID string, spec collect.BackupSpec) {
	p.mu.Lock()
	busy := p.backupRunning
	p.backupRunning = true
	p.mu.Unlock()
	if busy {
		p.finishCommand(cmdID, 1, "auf diesem Host läuft bereits ein Backup")
		return
	}
	defer func() {
		p.mu.Lock()
		p.backupRunning = false
		p.mu.Unlock()
	}()

	p.log.Info("backup gestartet", "command", cmdID, "gäste", len(spec.Guests))
	exit, output := collect.ProxmoxBackup(ctx, spec, func(note string) {
		pctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		_ = p.client.ReportProgress(pctx, p.agentToken, cmdID, note)
	})
	p.log.Info("backup beendet", "command", cmdID, "exit", exit)
	p.finishCommand(cmdID, exit, output)
}

// proxmoxPrune räumt alte Backups auf. Nicht parallel zu einem Backup-Lauf: vzdump
// könnte sonst gerade das Archiv schreiben, das hier bewertet wird.
func (p *program) proxmoxPrune(ctx context.Context, cmdID string, spec collect.PruneSpec) {
	p.mu.Lock()
	busy := p.backupRunning
	p.backupRunning = true
	p.mu.Unlock()
	if busy {
		p.finishCommand(cmdID, 1, "auf diesem Host läuft gerade ein Backup – später erneut versuchen")
		return
	}
	defer func() {
		p.mu.Lock()
		p.backupRunning = false
		p.mu.Unlock()
	}()
	p.log.Info("backup-aufräumen gestartet", "command", cmdID, "gäste", len(spec.Guests), "trockenlauf", spec.DryRun)
	exit, output := collect.ProxmoxPrune(ctx, spec, func(note string) {
		pctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		_ = p.client.ReportProgress(pctx, p.agentToken, cmdID, note)
	})
	p.finishCommand(cmdID, exit, output)
}

// finishCommand legt ein Befehlsergebnis ab und stößt den Checkin an.
func (p *program) finishCommand(cmdID string, exit int, output string) {
	p.mu.Lock()
	p.pendingCmdResults = append(p.pendingCmdResults, shared.CommandResult{
		CommandID: cmdID, ExitCode: exit, Output: output, RanAt: time.Now().UTC(),
	})
	p.mu.Unlock()
	p.requestCheckin()
}

func (p *program) proxmoxControl(ctx context.Context, cmdID, node, gtype string, vmid int, action string) {
	exit, output := collect.ProxmoxControl(ctx, node, gtype, vmid, action)
	p.mu.Lock()
	p.pendingCmdResults = append(p.pendingCmdResults, shared.CommandResult{
		CommandID: cmdID, ExitCode: exit, Output: output, RanAt: time.Now().UTC(),
	})
	p.mu.Unlock()
	p.log.Info("proxmox-aktion abgeschlossen", "command", cmdID, "action", action, "vmid", vmid, "exit", exit)
	p.requestCheckin()
}

// cmdTimeout begrenzt synchrone Ad-hoc-Befehle (siehe runPolicy).
const cmdTimeout = 10 * time.Minute

// scanUpdates führt die Update-Suche im Hintergrund aus (Obergrenze 30 min) und meldet
// das Ergebnis mit dem nächsten Checkin.
func (p *program) scanUpdates(ctx context.Context, cmdID string) {
	uctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	u := collect.OSUpdates(uctx)
	exit, output := 0, "Update-Status unbekannt"
	if u != nil {
		output = fmt.Sprintf("%d Updates gefunden", u.Count)
	} else if uctx.Err() == context.DeadlineExceeded {
		exit, output = 124, "Update-Suche nach 30 min abgebrochen"
	}
	p.mu.Lock()
	if u != nil {
		p.updates = u
	}
	p.pendingCmdResults = append(p.pendingCmdResults, shared.CommandResult{
		CommandID: cmdID, ExitCode: exit, Output: output, RanAt: time.Now().UTC(),
	})
	p.mu.Unlock()
	p.log.Info("update-suche abgeschlossen", "command", cmdID, "exit", exit)
	p.requestCheckin()
}

func (p *program) installUpdates(ctx context.Context, cmdID string, names []string, full, phased bool) {
	exit, output := collect.InstallUpdates(ctx, names, full, phased)
	u := collect.OSUpdates(ctx)
	hist := collect.PatchHistory(ctx) // frisch installierte Pakete sofort in den Verlauf
	p.mu.Lock()
	if u != nil {
		p.updates = u
	}
	if len(hist) > 0 {
		p.patchHistory = hist
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
func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

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
		// Verlauf zuerst (nur Dateien, schnell); der Paketmanager-Scan kann hängen
		// (z. B. checkupdates ohne Netz) und bekommt deshalb eine Obergrenze.
		hist := collect.PatchHistory(ctx)
		uctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
		u := collect.OSUpdates(uctx)
		cancel()
		p.mu.Lock()
		p.updates = u
		if len(hist) > 0 {
			p.patchHistory = hist
		}
		p.mu.Unlock()
		if u != nil {
			p.log.Info("os-update-check", "verfügbar", u.Count, "sicherheit", u.SecurityCount, "verlauf", len(hist))
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
	newPath := "/etc/roster/agent.yaml"
	legacy := "/etc/pc-inventory/agent.yaml"
	if runtime.GOOS == "windows" {
		newPath = `C:\ProgramData\Roster\agent.yaml`
		legacy = `C:\ProgramData\PC-Inventory\agent.yaml`
	}
	// Rückwärtskompatibilität: existiert nur die alte Konfiguration (Installation vor
	// dem Rebranding, ohne gepinnten -config-Pfad im Dienst), diese weiter verwenden.
	if _, err := os.Stat(newPath); err != nil {
		if _, err2 := os.Stat(legacy); err2 == nil {
			return legacy
		}
	}
	return newPath
}

// fimLoop führt die Datei-Integritätsprüfung nach dem Intervall der wirksamen Richtlinie
// aus (Standard 60 min), auf Anstoß sofort; ohne Regeln passiert nichts. Die Baseline
// rootcheckLoop führt täglich die Rootkit-Prüfung aus (Richtlinienschalter), auf Befehl sofort.
func (p *program) rootcheckLoop(ctx context.Context) {
	var last time.Time
	run := func(force bool) {
		p.mu.Lock()
		on := p.policy != nil && p.policy.Security != nil && p.policy.Security.Rootcheck
		p.mu.Unlock()
		if !on || (!force && time.Since(last) < 24*time.Hour) {
			return
		}
		sctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
		findings := rootcheck.Run(sctx)
		cancel()
		last = time.Now()
		p.log.Info("rootcheck", "befunde", len(findings))
		if len(findings) > 0 {
			p.mu.Lock()
			p.authEvents = append(p.authEvents, rootcheck.Events(findings, last.UTC())...)
			p.mu.Unlock()
			p.requestCheckin()
		}
	}
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-p.rootcheckNow:
			run(true)
		case <-t.C:
			run(false)
		}
	}
}

// stateLoop überwacht den Systemzustand (statemon): stündlicher Abgleich mit der Baseline
// (state-baseline.json), Änderungen als Ereignisse „state_change“ an den Server.
func (p *program) stateLoop(ctx context.Context) {
	path := statemon.Path(p.stateDir)
	var last time.Time
	run := func() {
		p.mu.Lock()
		on := p.policy != nil && p.policy.Security != nil && p.policy.Security.StateMonitor
		p.mu.Unlock()
		if !on || time.Since(last) < time.Hour {
			return
		}
		sctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		cur := statemon.Take(sctx)
		cancel()
		last = time.Now()
		prev := statemon.Load(path)
		if err := statemon.Save(path, &statemon.Baseline{Taken: last, Items: cur}); err != nil {
			p.log.Warn("statemon: baseline speichern fehlgeschlagen", "err", err)
		}
		if prev == nil {
			p.log.Info("statemon: baseline aufgenommen", "kategorien", len(cur))
			return
		}
		events := statemon.Diff(prev.Items, cur, last.UTC())
		if len(events) > 0 {
			p.log.Info("statemon: änderungen", "ereignisse", len(events))
			p.mu.Lock()
			p.authEvents = append(p.authEvents, events...)
			p.mu.Unlock()
			p.requestCheckin()
		}
	}
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			run()
		}
	}
}

// liegt als fim-baseline.json im Zustandsverzeichnis des Agents.
func (p *program) fimLoop(ctx context.Context) {
	baselinePath := filepath.Join(p.stateDir, "fim-baseline.json")
	var last time.Time
	whoWarned := false
	run := func(force bool) {
		p.mu.Lock()
		var spec *shared.FIMSpec
		if p.policy != nil && p.policy.Security != nil {
			spec = p.policy.Security.FIM
		}
		reset := p.fimReset
		p.fimReset = false
		rootcheckOn := p.policy != nil && p.policy.Security != nil && p.policy.Security.Rootcheck
		p.mu.Unlock()
		if spec == nil || len(spec.Rules) == 0 {
			return
		}
		interval := time.Duration(spec.IntervalMin) * time.Minute
		if interval < 5*time.Minute {
			interval = 60 * time.Minute
		}
		if !force && time.Since(last) < interval {
			return
		}
		prev := fim.Load(baselinePath)
		if reset {
			prev = &fim.Baseline{}
		}
		sctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
		if spec.WhoData {
			if err := fim.EnsureAuditRules(sctx, spec.Rules); err != nil && !whoWarned {
				p.log.Warn("fim: who-data nicht verfügbar", "err", err)
				whoWarned = true
			}
		}
		next, events, st := fim.Scan(sctx, spec.Rules, prev, time.Now().UTC())
		if spec.WhoData && len(events) > 0 {
			if n := fim.ApplyWho(events, fim.CollectWho(sctx, p.stateDir)); n > 0 {
				p.log.Info("fim: verursacher ermittelt", "ereignisse", n)
			}
		}
		if rootcheckOn && len(events) > 0 {
			// Neue/geänderte Dateien mit dem vorhandenen Virenscanner prüfen (ClamAV/Defender).
			var files []string
			for _, e := range events {
				if (e.Change == "created" || e.Change == "modified") && !shared.IsRegistryPath(e.Path) && len(files) < 200 {
					files = append(files, e.Path)
				}
			}
			if hits, scanner := rootcheck.ScanFiles(sctx, files); len(hits) > 0 {
				p.log.Warn("malware-fund", "scanner", scanner, "treffer", len(hits))
				p.mu.Lock()
				p.authEvents = append(p.authEvents, rootcheck.MalwareEvents(hits, scanner, time.Now().UTC())...)
				p.mu.Unlock()
			}
		}
		cancel()
		last = time.Now()
		if err := fim.Save(baselinePath, next); err != nil {
			p.log.Warn("fim: baseline speichern fehlgeschlagen", "err", err)
		}
		p.log.Info("fim-scan", "dateien", st.Files, "gehasht", st.Hashed, "ereignisse", len(events), "limit", st.Limit)
		if len(events) > 0 {
			p.mu.Lock()
			p.fimEvents = append(p.fimEvents, events...)
			p.mu.Unlock()
			p.requestCheckin()
		}
	}
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-p.fimNow:
			run(true)
		case <-t.C:
			run(false)
		}
	}
}

// authLoop liest jede Minute neue Anmelde-/Kontoereignisse, solange die wirksame
// Richtlinie die Anmeldungs-Überwachung einschaltet; neue Ereignisse stoßen einen
// Checkin an (höchstens einer pro Minute), damit Brute-Force schnell beim Server ankommt.
func (p *program) authLoop(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		p.mu.Lock()
		on := p.policy != nil && p.policy.Security != nil && p.policy.Security.AuthMonitor
		var sources []shared.LogSource
		if p.policy != nil && p.policy.Security != nil {
			sources = p.policy.Security.LogSources
		}
		p.mu.Unlock()
		if !on && len(sources) == 0 {
			continue
		}
		cctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		var events []shared.AuthEvent
		if on {
			events = authlog.Collect(cctx, p.stateDir)
		}
		// Zusätzliche Protokolle (nginx, SFTP-/FTP-Container, eigene Muster) der Richtlinie.
		more, status := logmon.Collect(cctx, p.stateDir, sources)
		events = append(events, more...)
		cancel()
		p.mu.Lock()
		// Status je Quelle fortschreiben; Quellen, die nicht mehr konfiguriert sind, fallen weg.
		next := make(map[string]shared.LogSourceStatus, len(status))
		for _, st := range status {
			prev := p.logStatus[st.ID]
			st.Events += prev.Events
			if st.Events > prev.Events {
				st.LastEvent = time.Now()
			} else {
				st.LastEvent = prev.LastEvent
			}
			next[st.ID] = st
		}
		p.logStatus = next
		if len(events) == 0 {
			p.mu.Unlock()
			continue
		}
		p.authEvents = append(p.authEvents, events...)
		p.mu.Unlock()
		p.log.Info("auth-ereignisse gelesen", "n", len(events))
		p.requestCheckin()
	}
}

// complianceLoop prüft die Baseline-Regeln nach dem Intervall der wirksamen Richtlinie
// (Standard 24 h), auf Anstoß sofort; der Bericht geht beim nächsten Checkin mit und
// speist den Check-Typ „compliance“.
func (p *program) complianceLoop(ctx context.Context) {
	var last time.Time
	run := func(force bool) {
		p.mu.Lock()
		var spec *shared.ComplianceSpec
		if p.policy != nil && p.policy.Security != nil {
			spec = p.policy.Security.Compliance
		}
		p.mu.Unlock()
		if spec == nil || !spec.Enabled {
			return
		}
		interval := time.Duration(spec.IntervalH) * time.Hour
		if interval < time.Hour {
			interval = 24 * time.Hour
		}
		if !force && time.Since(last) < interval {
			return
		}
		cctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
		rep := compliance.Run(cctx, spec, compliance.NewEnv(cctx))
		cancel()
		last = time.Now()
		score, pass, fail := compliance.Score(rep.Results)
		p.log.Info("compliance-prüfung", "profil", rep.Profile, "score", score, "bestanden", pass, "fehlgeschlagen", fail)
		p.mu.Lock()
		p.compliance = &rep
		p.compliancePending = true
		p.mu.Unlock()
		p.requestCheckin()
	}
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-p.complianceNow:
			run(true)
		case <-t.C:
			run(false)
		}
	}
}

// responseExpireLoop hebt abgelaufene IP-Sperren auf (iptables/Windows/pf; nft läuft selbst ab).
func (p *program) responseExpireLoop(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			response.Expire(ctx)
		}
	}
}

// imageScanLoop liest täglich je Image die Paketliste aus einem laufenden Container (für den
// OSV-Abgleich auf dem Server). Erster Lauf kurz nach dem Start, danach halbstündliche
// Prüfung auf neue/abgelaufene Images; scan_images erzwingt einen sofortigen Lauf.
func (p *program) imageScanLoop(ctx context.Context) {
	if !collect.DockerAvailable() {
		return
	}
	run := func() {
		containers := collect.DockerContainers(ctx)
		p.mu.Lock()
		if p.imageScanned == nil {
			p.imageScanned = map[string]time.Time{}
		}
		scanned := p.imageScanned
		p.mu.Unlock()
		skip := func(id string) bool {
			p.mu.Lock()
			defer p.mu.Unlock()
			return time.Since(scanned[id]) < 24*time.Hour
		}
		scans := collect.ScanImages(ctx, containers, skip)
		if len(scans) == 0 {
			return
		}
		p.mu.Lock()
		for _, sc := range scans {
			scanned[sc.ImageID] = time.Now()
		}
		p.imageScans = append(p.imageScans, scans...)
		if len(p.imageScans) > 50 {
			p.imageScans = p.imageScans[len(p.imageScans)-50:]
		}
		p.mu.Unlock()
		p.log.Info("container-images gescannt", "images", len(scans))
		p.requestCheckin()
	}
	t := time.NewTicker(30 * time.Minute)
	defer t.Stop()
	first := time.After(90 * time.Second)
	for {
		select {
		case <-ctx.Done():
			return
		case <-first:
			run()
		case <-t.C:
			run()
		case <-p.imageScanNow:
			run()
		}
	}
}

// runFlow führt einen Ablauf aus dem Flow-Designer aus und meldet das Ergebnis (JSON) als
// Befehlsausgabe. Exit 0 = ok/Vorschau/nicht betroffen, 1 = fehlgeschlagen, 2 = zurückgerollt.
func (p *program) runFlow(ctx context.Context, cmdID string, payload map[string]any) {
	var g shared.FlowGraph
	if b, err := json.Marshal(payload["graph"]); err == nil {
		_ = json.Unmarshal(b, &g)
	}
	params := map[string]string{}
	if m, ok := payload["params"].(map[string]any); ok {
		for k, v := range m {
			if s, ok := v.(string); ok {
				params[k] = s
			}
		}
	}
	dry, _ := payload["dry_run"].(bool)
	runID, _ := payload["run_id"].(string)
	if runID == "" {
		runID = cmdID
	}
	p.log.Info("ablauf gestartet", "command", cmdID, "vorschau", dry, "bausteine", len(g.Nodes))
	res := flow.Run(ctx, g, flow.Options{RunID: runID, DryRun: dry, Params: params,
		RunScript: func(c context.Context, shell, script string) (int, string) {
			code, out, ok := policy.RunScriptEnv(c, shell, script, nil, nil)
			if !ok {
				return -1, "Nicht unterstützt auf diesem Betriebssystem (" + runtime.GOOS + ")"
			}
			return code, out
		}})
	exit := 0
	switch res.Status {
	case "fail":
		exit = 1
	case "rolled_back":
		exit = 2
	}
	out, _ := json.Marshal(res)
	p.mu.Lock()
	p.pendingCmdResults = append(p.pendingCmdResults, shared.CommandResult{CommandID: cmdID, ExitCode: exit, Output: string(out), RanAt: time.Now().UTC()})
	p.mu.Unlock()
	p.log.Info("ablauf beendet", "command", cmdID, "status", res.Status)
	p.requestCheckin()
}
