// Package mitre ist der gemeinsame Katalog der Vorfallsarten der Sicherheitsüberwachung:
// je Art der Standard-Schweregrad und die MITRE-ATT&CK-Techniken. Server (Vorfälle,
// Richtlinien-Validierung, Benachrichtigungen) und Oberfläche (über /security/kinds)
// leiten ihre Listen hieraus ab – bisher war die Liste an sieben Stellen gepflegt.
package mitre

// Technique ist eine ATT&CK-Technik (ID wie "T1110" oder "T1059.001").
type Technique struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Tactic string `json:"tactic"`
}

// Kind beschreibt eine Vorfallsart.
type Kind struct {
	Kind            string      `json:"kind"`
	DefaultSeverity string      `json:"default_severity"` // info | warning | critical
	Techniques      []Technique `json:"techniques"`
}

var (
	tBruteForce   = Technique{"T1110", "Brute Force", "Credential Access"}
	tValidAccount = Technique{"T1078", "Valid Accounts", "Initial Access"}
	tExploitApp   = Technique{"T1190", "Exploit Public-Facing Application", "Initial Access"}
	tActiveScan   = Technique{"T1595", "Active Scanning", "Reconnaissance"}
	tServiceDisc  = Technique{"T1046", "Network Service Discovery", "Discovery"}
	tCreateAcct   = Technique{"T1136", "Create Account", "Persistence"}
	tAcctManip    = Technique{"T1098", "Account Manipulation", "Persistence"}
	tDataManip    = Technique{"T1565.001", "Stored Data Manipulation", "Impact"}
	tSysProcess   = Technique{"T1543", "Create or Modify System Process", "Persistence"}
	tWinService   = Technique{"T1543.003", "Windows Service", "Persistence"}
	tSchedTask    = Technique{"T1053.005", "Scheduled Task", "Persistence"}
	tSchedJob     = Technique{"T1053", "Scheduled Task/Job", "Persistence"}
	tClearLogs    = Technique{"T1070.001", "Clear Windows Event Logs", "Defense Evasion"}
	tPowerShell   = Technique{"T1059.001", "PowerShell", "Execution"}
	tCmdShell     = Technique{"T1059.003", "Windows Command Shell", "Execution"}
	tRootkit      = Technique{"T1014", "Rootkit", "Defense Evasion"}
	tToolTransfer = Technique{"T1105", "Ingress Tool Transfer", "Command and Control"}
	tContainerCmd = Technique{"T1609", "Container Administration Command", "Execution"}
	tDataDestroy  = Technique{"T1485", "Data Destruction", "Impact"}
	tDataEncrypt  = Technique{"T1486", "Data Encrypted for Impact", "Impact"}
	tRemoteSvc    = Technique{"T1021", "Remote Services", "Lateral Movement"}
)

// Kinds listet alle Vorfallsarten in Anzeigereihenfolge.
var Kinds = []Kind{
	{"brute_force", "warning", []Technique{tBruteForce}},
	{"brute_force_success", "critical", []Technique{tBruteForce, tValidAccount}},
	{"scan", "warning", []Technique{tExploitApp, tActiveScan}},
	{"portscan", "warning", []Technique{tServiceDisc}},
	{"threat_listed", "warning", []Technique{tActiveScan}},
	{"new_source_ip", "warning", []Technique{tValidAccount}},
	{"account_change", "warning", []Technique{tCreateAcct, tAcctManip}},
	{"lockout", "warning", []Technique{tBruteForce}},
	{"fim_change", "warning", []Technique{tDataManip, tSysProcess}},
	{"persistence", "warning", []Technique{tWinService, tSchedTask}},
	{"audit_cleared", "critical", []Technique{tClearLogs}},
	{"suspicious_process", "warning", []Technique{tPowerShell, tCmdShell}},
	{"state_change", "warning", []Technique{tSysProcess, tSchedJob, tCreateAcct}},
	{"rootkit", "critical", []Technique{tRootkit}},
	{"malware", "critical", []Technique{tToolTransfer}},
	{"container_exec", "warning", []Technique{tContainerCmd, tRemoteSvc}},
	{"db_change", "critical", []Technique{tDataDestroy, tDataEncrypt}},
}

// Severities sind die gültigen Schweregrade.
var Severities = []string{"info", "warning", "critical"}

// ByKind liefert die Techniken einer Art (nil für unbekannte Arten).
func ByKind(kind string) []Technique {
	for _, k := range Kinds {
		if k.Kind == kind {
			return k.Techniques
		}
	}
	return nil
}

// DefaultSeverity liefert den Standard-Schweregrad einer Art ("" für unbekannte).
func DefaultSeverity(kind string) string {
	for _, k := range Kinds {
		if k.Kind == kind {
			return k.DefaultSeverity
		}
	}
	return ""
}

// KindsForTechnique liefert alle Arten, die eine Technik (oder deren Untertechniken,
// z. B. "T1059" trifft "T1059.001") tragen.
func KindsForTechnique(id string) []string {
	var out []string
	for _, k := range Kinds {
		for _, t := range k.Techniques {
			if t.ID == id || len(t.ID) > len(id) && t.ID[:len(id)] == id && t.ID[len(id)] == '.' {
				out = append(out, k.Kind)
				break
			}
		}
	}
	return out
}

// AllKinds liefert die Artnamen in Katalogreihenfolge.
func AllKinds() []string {
	out := make([]string, 0, len(Kinds))
	for _, k := range Kinds {
		out = append(out, k.Kind)
	}
	return out
}

// IsKind meldet, ob kind im Katalog steht.
func IsKind(kind string) bool { return DefaultSeverity(kind) != "" }

// ValidSeverity meldet, ob s ein gültiger Schweregrad ist.
func ValidSeverity(s string) bool {
	for _, v := range Severities {
		if v == s {
			return true
		}
	}
	return false
}
