// Package policy wertet Policy-Checks aus und führt Skript-Tasks aus.
package policy

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/mem"

	"github.com/thomaspeterson/pc-inventory/internal/shared"
)

const maxOutput = 4000 // Ausgabe je Check/Task kappen

// EvalChecks wertet alle Checks aus und liefert die Ergebnisse.
// updatesCount stammt aus dem Agent-Cache (nil = unbekannt).
func EvalChecks(ctx context.Context, checks []shared.CheckSpec, updatesCount *int) []shared.CheckResult {
	out := make([]shared.CheckResult, 0, len(checks))
	for _, c := range checks {
		out = append(out, evalOne(ctx, c, updatesCount))
	}
	return out
}

func evalOne(ctx context.Context, c shared.CheckSpec, updatesCount *int) shared.CheckResult {
	switch c.Type {
	case "disk":
		return diskCheck(ctx, c)
	case "memory":
		return memCheck(ctx, c)
	case "cpu":
		return cpuCheck(ctx, c)
	case "updates":
		return updatesCheck(c, updatesCount)
	case "script":
		return scriptCheck(ctx, c)
	case "ping":
		return pingCheck(ctx, c)
	case "tcp":
		return tcpCheck(ctx, c)
	case "http":
		return httpCheck(ctx, c)
	case "ports":
		return portsCheck(ctx, c)
	default:
		return shared.CheckResult{CheckID: c.ID, Status: "unknown", Output: "unbekannter Checktyp: " + c.Type}
	}
}

func threshold(c shared.CheckSpec, def float64) float64 {
	if v, ok := c.Config["threshold"]; ok {
		if f, ok := v.(float64); ok {
			return f
		}
	}
	return def
}

func diskCheck(ctx context.Context, c shared.CheckSpec) shared.CheckResult {
	minFree := threshold(c, 15)
	parts, err := disk.PartitionsWithContext(ctx, false)
	if err != nil {
		return shared.CheckResult{CheckID: c.ID, Status: "unknown", Output: err.Error()}
	}
	worst, where := 100.0, ""
	for _, p := range parts {
		u, err := disk.UsageWithContext(ctx, p.Mountpoint)
		if err != nil || u.Total == 0 {
			continue
		}
		free := 100 - u.UsedPercent
		if free < worst {
			worst, where = free, p.Mountpoint
		}
	}
	status := "passing"
	if worst < minFree {
		status = "failing"
	}
	return shared.CheckResult{
		CheckID: c.ID, Status: status, Value: worst,
		Output: fmt.Sprintf("geringster freier Platz: %.1f%% (%s), Schwelle %.0f%%", worst, where, minFree),
	}
}

func memCheck(ctx context.Context, c shared.CheckSpec) shared.CheckResult {
	maxUsed := threshold(c, 90)
	vm, err := mem.VirtualMemoryWithContext(ctx)
	if err != nil {
		return shared.CheckResult{CheckID: c.ID, Status: "unknown", Output: err.Error()}
	}
	status := "passing"
	if vm.UsedPercent > maxUsed {
		status = "failing"
	}
	return shared.CheckResult{
		CheckID: c.ID, Status: status, Value: vm.UsedPercent,
		Output: fmt.Sprintf("RAM-Auslastung %.1f%%, Schwelle %.0f%%", vm.UsedPercent, maxUsed),
	}
}

func cpuCheck(ctx context.Context, c shared.CheckSpec) shared.CheckResult {
	maxLoad := threshold(c, 90)
	pcts, err := cpu.PercentWithContext(ctx, 500*time.Millisecond, false)
	if err != nil || len(pcts) == 0 {
		return shared.CheckResult{CheckID: c.ID, Status: "unknown", Output: "CPU nicht lesbar"}
	}
	status := "passing"
	if pcts[0] > maxLoad {
		status = "failing"
	}
	return shared.CheckResult{
		CheckID: c.ID, Status: status, Value: pcts[0],
		Output: fmt.Sprintf("CPU-Last %.1f%%, Schwelle %.0f%%", pcts[0], maxLoad),
	}
}

func updatesCheck(c shared.CheckSpec, count *int) shared.CheckResult {
	if count == nil {
		return shared.CheckResult{CheckID: c.ID, Status: "unknown", Output: "Update-Status noch unbekannt"}
	}
	maxN := threshold(c, 0)
	status := "passing"
	if float64(*count) > maxN {
		status = "failing"
	}
	return shared.CheckResult{
		CheckID: c.ID, Status: status, Value: float64(*count),
		Output: fmt.Sprintf("%d ausstehende Updates, Schwelle %.0f", *count, maxN),
	}
}

func scriptCheck(ctx context.Context, c shared.CheckSpec) shared.CheckResult {
	exit, output, ok := RunScript(ctx, c.Shell, c.Script, c.Platforms)
	if !ok {
		return shared.CheckResult{CheckID: c.ID, Status: "unknown", Output: "Nicht unterstützt auf diesem Betriebssystem"}
	}
	value, hasValue := lastNumber(output)

	// Harte Fehler des Skripts (Exit-Code) gehen immer vor.
	status := "passing"
	if exit != 0 {
		status = "failing"
	} else if op, _ := c.Config["operator"].(string); op != "" && hasValue {
		// Optionaler Ausgabe-Vergleich: zuerst kritisch, dann Warnung.
		if crit, ok := numConfig(c, "crit"); ok && compare(value, op, crit) {
			status = "failing"
		} else if warn, ok := numConfig(c, "warn"); ok && compare(value, op, warn) {
			status = "warning"
		}
	}

	v := float64(exit)
	if hasValue {
		v = value
	}
	return shared.CheckResult{CheckID: c.ID, Status: status, Value: v, Output: trunc(output)}
}

// numConfig liest einen numerischen Konfigwert (kommt als float64 aus JSON).
func numConfig(c shared.CheckSpec, key string) (float64, bool) {
	switch v := c.Config[key].(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	}
	return 0, false
}

// compare wertet "value <op> threshold" aus.
func compare(value float64, op string, threshold float64) bool {
	switch op {
	case ">":
		return value > threshold
	case ">=":
		return value >= threshold
	case "<":
		return value < threshold
	case "<=":
		return value <= threshold
	case "==":
		return value == threshold
	case "!=":
		return value != threshold
	}
	return false
}

// lastNumber extrahiert die letzte als Zahl interpretierbare Token der Ausgabe
// (z.B. "Anzahl 7" -> 7). Erlaubt so Vergleiche auf die Skript-Ausgabe.
func lastNumber(output string) (float64, bool) {
	fields := strings.Fields(output)
	for i := len(fields) - 1; i >= 0; i-- {
		if f, err := strconv.ParseFloat(strings.TrimRight(fields[i], "%"), 64); err == nil {
			return f, true
		}
	}
	return 0, false
}

// PlatformAllowed prüft, ob das aktuelle OS in der Plattform-Liste enthalten ist
// (leere Liste = keine Einschränkung). Werte: windows|linux|darwin.
func PlatformAllowed(platforms []string) bool {
	if len(platforms) == 0 {
		return true
	}
	for _, p := range platforms {
		if p == runtime.GOOS {
			return true
		}
	}
	return false
}

// RunScript führt ein Skript aus und liefert Exit-Code, Ausgabe und ob es auf diesem OS
// anwendbar war (false = übersprungen, z.B. falsche Shell oder Plattform).
func RunScript(ctx context.Context, shell, content string, platforms []string) (exitCode int, output string, applicable bool) {
	if !PlatformAllowed(platforms) {
		return 0, "", false
	}
	var cmd *exec.Cmd
	switch shell {
	case "powershell":
		if runtime.GOOS != "windows" {
			return 0, "", false
		}
		cmd = exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", content)
	case "shell":
		if runtime.GOOS == "windows" {
			return 0, "", false
		}
		// Beginnt das Skript mit einem Shebang (#!/usr/bin/env bash, python …), in eine
		// temporäre Datei schreiben und direkt ausführen, damit der angegebene
		// Interpreter genutzt wird. Sonst wie bisher über sh -c (POSIX-Kompatibilität).
		if strings.HasPrefix(strings.TrimSpace(content), "#!") {
			if c, cleanup, err := shebangCommand(ctx, content); err == nil {
				defer cleanup()
				cmd = c
			}
		}
		if cmd == nil {
			cmd = exec.CommandContext(ctx, "sh", "-c", content)
		}
	default:
		return 0, "", false
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode(), trunc(string(out)), true
		}
		return -1, trunc(string(out) + err.Error()), true
	}
	return 0, trunc(string(out)), true
}

// shebangCommand schreibt das Skript in eine ausführbare temporäre Datei und liefert
// ein Kommando, das sie direkt startet (Shebang wird also respektiert).
func shebangCommand(ctx context.Context, content string) (*exec.Cmd, func(), error) {
	f, err := os.CreateTemp("", "pcinv-script-*")
	if err != nil {
		return nil, func() {}, err
	}
	name := f.Name()
	cleanup := func() { _ = os.Remove(name) }
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		cleanup()
		return nil, func() {}, err
	}
	if err := f.Close(); err != nil {
		cleanup()
		return nil, func() {}, err
	}
	if err := os.Chmod(name, 0700); err != nil {
		cleanup()
		return nil, func() {}, err
	}
	return exec.CommandContext(ctx, name), cleanup, nil
}

func trunc(s string) string {
	if len(s) > maxOutput {
		return s[:maxOutput] + "…"
	}
	return s
}
