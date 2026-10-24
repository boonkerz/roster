// Nimmt die Dokumentations-Screenshots gegen eine lokale Demo-Instanz auf – in Englisch
// und Deutsch (Dateien name.png bzw. name.de.png; mkdocs-static-i18n wählt das passende
// Bild je Sprache). Aufruf aus web/ (dort liegt Playwright):
//   node ../scripts/capture-docs.mjs <base-url> seed <ids.json>      Demo-Daten anlegen
//   node ../scripts/capture-docs.mjs <base-url> <out-dir> <en|de>     Screenshots
// Die UI-Beschriftungen stehen hier auf Deutsch (so wie in web/src); für Englisch werden
// sie über web/src/i18n_en.ts übersetzt – Selektoren bleiben damit in beiden Sprachen gültig.
import { createRequire } from "module";
import { readFileSync, writeFileSync } from "fs";
const require = createRequire(process.cwd() + "/");
const { chromium } = require("playwright");
const BASE = process.argv[2] || "http://127.0.0.1:18080";
const MODE = process.argv[3] || "docs/screenshots";
const ARG = process.argv[4] || "en";

// Deutsche UI-Strings → Englisch (aus der i18n-Tabelle der Web-UI).
const EN = {};
for (const m of readFileSync(new URL("../web/src/i18n_en.ts", import.meta.url), "utf8").matchAll(/^\s*"((?:[^"\\]|\\.)*)":\s*"((?:[^"\\]|\\.)*)",?\s*$/gm)) {
  EN[m[1].replace(/\\"/g, '"')] = m[2].replace(/\\"/g, '"');
}
const LANG = MODE === "seed" ? "en" : ARG;
const L = (de) => (LANG === "de" ? de : (EN[de] ?? de));
const SUFFIX = LANG === "de" ? ".de" : "";

const b = await chromium.launch();
const ctx = await b.newContext({ viewport: { width: 1440, height: 900 }, deviceScaleFactor: 2, locale: LANG === "de" ? "de-DE" : "en-US" });
await ctx.addInitScript((lang) => { localStorage.setItem("roster-lang", lang); localStorage.setItem("roster-theme", "light"); }, LANG);
const p = await ctx.newPage();
const wait = (ms) => p.waitForTimeout(ms);
const shot = async (name) => { await p.screenshot({ path: `${MODE}/${name}${SUFFIX}.png` }); console.log("shot", name, LANG); };
const api = (m, u, body) => p.evaluate(async ([m, u, body]) => {
  const r = await fetch("/api/v1" + u, { method: m, headers: { "Content-Type": "application/json" }, body: body === undefined ? undefined : JSON.stringify(body), credentials: "same-origin" });
  const t = await r.text();
  try { return JSON.parse(t); } catch { return t; }
}, [m, u, body]);

async function login() {
  await p.goto(BASE + "/");
  await p.waitForSelector("input");
  const i = await p.$$("input");
  await i[0].fill("admin");
  await i[1].fill("demo1234");
  await p.click("button[type=submit]");
  await wait(1800);
}

async function setTheme(theme) {
  await api("PUT", "/auth/me/theme", { theme });
  await p.evaluate((t) => localStorage.setItem("roster-theme", t), theme);
}

// Demo-Daten (einmalig): Firmen/Standorte, Rollen/Nutzer, Richtlinie mit Checks und
// Task, benutzerdefinierte Felder samt Werten, Dashboard-Kacheln, Listen-Spalten.
async function seed() {
  const devs = await api("GET", "/devices");
  const devId = devs[0] && devs[0].id;
  const acme = await api("POST", "/clients", { name: "ACME Corp" });
  const berlin = await api("POST", "/sites", { client_id: acme.id, name: "Berlin HQ" });
  await api("POST", "/sites", { client_id: acme.id, name: "Munich Office" });
  const globex = await api("POST", "/clients", { name: "Globex Industries" });
  await api("POST", "/sites", { client_id: globex.id, name: "Head Office" });
  if (devId) await api("PUT", "/devices/" + devId + "/site", { site_id: berlin.id });
  const support = await api("POST", "/roles", { name: "Support", permissions: ["page.dashboard", "page.devices", "devices.operate"] });
  await api("POST", "/roles", { name: "Auditor (read-only)", permissions: ["page.dashboard", "page.devices"] });
  await api("POST", "/users", { username: "support1", password: "Passw0rd!x", role: "technician" });
  await api("POST", "/users", { username: "acme-viewer", password: "Passw0rd!x", role: "viewer", custom_role_id: support.id });
  await api("POST", "/groups", { name: "Workstations", description: "" });
  await api("POST", "/groups", { name: "Servers", description: "" });

  // Richtlinie mit Checks aller Art; die Service-Prüfung schlägt bewusst fehl (sshd aus).
  const pol = await api("POST", "/policies", { name: "Baseline", description: "Default checks for every device" });
  const checks = {};
  for (const c of [
    { name: "Disk /", type: "disk", config: { threshold: 15 } },
    { name: "Memory", type: "memory", config: { threshold: 90 }, severity: "warning" },
    { name: "CPU load", type: "cpu", config: { threshold: 90 }, severity: "warning" },
    { name: "Pending updates", type: "updates", config: { threshold: 20 }, severity: "warning" },
    { name: "Ping gateway", type: "ping", config: { host: "127.0.0.1", warn: 50, crit: 200 }, severity: "warning" },
    { name: "Website", type: "http", config: { url: BASE + "/", expected_status: 200 } },
    { name: "Service sshd", type: "service", config: { name: "sshd" } },
    { name: "CPU temperature", type: "temperature", config: {}, severity: "warning" },
  ]) {
    const r = await api("POST", "/policies/" + pol.id + "/checks", { severity: "critical", ...c });
    if (r && r.id) checks[c.type] = r.id;
  }
  const script = await api("POST", "/scripts", { name: "Cleanup temp files", shell: "shell", platforms: ["linux", "darwin"], content: "#!/bin/sh\nfind /tmp -type f -mtime +7 -delete 2>/dev/null\necho \"temp cleaned\"\n" });
  await api("POST", "/scripts", { name: "Restart print spooler", shell: "powershell", platforms: ["windows"], content: "Restart-Service -Name Spooler\n" });
  if (script && script.id) await api("POST", "/policies/" + pol.id + "/tasks", { name: "Weekly temp cleanup", script_id: script.id, schedule_type: "interval", interval_minutes: 7 * 24 * 60, frequency: "weekly" });
  await api("POST", "/policies/" + pol.id + "/assignments", { target_type: "client", target_id: acme.id });
  const srv = await api("POST", "/policies", { name: "Servers", description: "Stricter checks for servers" });
  for (const c of [
    { name: "Disk /var", type: "disk", config: { threshold: 25 } },
    { name: "TLS certificate", type: "cert", config: { host: "example.com", port: 443, warn: 30, crit: 7 } },
    { name: "Backup port", type: "tcp", config: { host: "127.0.0.1", port: 22 } },
    { name: "Uptime", type: "uptime", config: { threshold: 90 }, severity: "warning" },
  ]) await api("POST", "/policies/" + srv.id + "/checks", { severity: "critical", ...c });
  await api("POST", "/policies/" + srv.id + "/assignments", { target_type: "client", target_id: globex.id });
  const ws = await api("POST", "/policies", { name: "Workstations", description: "User PCs: updates and security" });
  for (const c of [
    { name: "Pending updates", type: "updates", config: { threshold: 10 }, severity: "warning" },
    { name: "Antivirus", type: "av", config: {} },
    { name: "Disk encryption", type: "bitlocker", config: {} },
  ]) await api("POST", "/policies/" + ws.id + "/checks", { severity: "critical", ...c });

  // Benutzerdefinierte Felder + Werte am Gerät.
  const fields = {};
  for (const f of [
    { name: "Asset-Tag", type: "text" },
    { name: "Stage", type: "select", options: ["prod", "staging", "dev"] },
    { name: "Docker Image", type: "text" },
    { name: "Maintenance", type: "checkbox" },
  ]) {
    const r = await api("POST", "/custom-fields", { model: "device", required: false, options: [], default_value: "", ...f });
    if (r && r.id) fields[f.name] = r.id;
  }
  if (devId) await api("PUT", "/custom-field-values", { model: "device", entity_id: devId, values: {
    [fields["Asset-Tag"]]: "ACME-0042", [fields["Stage"]]: "prod", [fields["Docker Image"]]: "nginx:1.27.2", [fields["Maintenance"]]: "false",
  } });

  // Dashboard-Kacheln und Listen-Spalten (Benutzereinstellungen des Admins).
  await api("PUT", "/auth/me/prefs/dashboard.widgets", [
    { id: "w1", title: "", source: "summary", key: "device_status", chart: "donut", range: "7d", width: 1 },
    { id: "w2", title: "", source: "check", key: checks.disk, chart: "donut", range: "7d", width: 1 },
    { id: "w3", title: "", source: "summary", key: "failing_checks", chart: "line", range: "7d", width: 2 },
    { id: "w4", title: "", source: "events", key: "recent", chart: "list", range: "7d", width: 2 },
    { id: "w5", title: "", source: "field", key: fields["Stage"], chart: "bar", range: "7d", width: 2 },
  ]);
  await api("PUT", "/auth/me/prefs/devices.columns", ["status", "hostname", "users", "os", "ip", "cf:" + fields["Stage"], "cf:" + fields["Asset-Tag"], "checks", "updates", "last_seen"]);
  await api("PUT", "/auth/me/prefs/devices.saved_filters", [
    { name: "Prod servers", filter: { match: "all", conditions: [{ field: "cf:" + fields["Stage"], op: "eq", value: "prod" }] } },
  ]);
  await setTheme("light");
  return { devId, checks, fields, siteId: berlin.id };
}

// --- Navigation ---------------------------------------------------------------
let DEV_ID = "";
async function openDevice() {
  if (!DEV_ID) { const devs = await api("GET", "/devices"); DEV_ID = devs[0] && devs[0].id; }
  await p.goto(BASE + "/devices/" + DEV_ID); await wait(1800);
}
async function tab(group, name, ms = 1200) {
  await p.click(`button.tab-group:has-text("${L(group)}")`, { timeout: 5000 }); await wait(300);
  if (name) await p.click(`button.tab:has-text("${L(name)}")`, { timeout: 5000 });
  await wait(ms);
}
async function openSettings(areaLabel) {
  await p.goto(BASE + "/devices"); await wait(800);
  const dd = await p.$(".nav-dropdown button");
  if (!dd) return false;
  await dd.click({ timeout: 5000 }); await wait(300);
  const item = await p.$(`.nav-dropdown-item:has-text("${L(areaLabel)}")`);
  if (!item) return false;
  await item.click({ timeout: 5000 }); await wait(1200);
  return true;
}
async function tryShot(name, fn) {
  try { await fn(); await shot(name); } catch (e) { console.log(name + ":", e.message.split("\n")[0]); }
}

await login();
if (MODE === "seed") {
  const ids = await seed();
  writeFileSync(ARG, JSON.stringify(ids));
  console.log("seeded", ids);
  await b.close();
  process.exit(0);
}

await setTheme("light");
await p.goto(BASE + "/dashboard"); await wait(2000); await shot("dashboard");
await p.goto(BASE + "/devices"); await wait(1500); await shot("devices");
await tryShot("devices-filter", async () => {
  await p.click(`button:has-text("${L("Filter")}")`); await wait(600);
  await p.selectOption('select:has(option[value="Prod servers"])', "Prod servers", { timeout: 3000 }); await wait(600);
});
await p.keyboard.press("Escape");

await openDevice(); await shot("device-detail");
await tryShot("live-utilization", async () => {
  await tab("Übersicht", "Auslastung", 300);
  await p.waitForSelector(".metric-fill", { timeout: 30000 }); await wait(3000);
});
await tryShot("checks", () => tab("Zustand", "Checks"));
await tryShot("check-history", async () => {
  await p.click('table .btn-link:has-text("Disk /")'); await wait(900);
  await p.click(`label.chip:has-text("${L("7 Tage")}")`); await wait(900);
});
await p.keyboard.press("Escape"); await wait(300);
await tryShot("tasks", () => tab("Zustand", "Tasks"));
await tryShot("software", () => tab("Inventar", "Software"));
await tryShot("patches", () => tab("Inventar", "Patches"));
await tryShot("fields", () => tab("Inventar", "Felder"));
await tryShot("services", async () => {
  await tab("System", "Dienste/Prozesse", 200);
  await p.waitForSelector(".scroll-list table tbody tr", { timeout: 30000 }); await wait(800);
});
await tryShot("security", () => tab("System", "Sicherheit"));
await tryShot("files", () => tab("Zugriff", "Dateien", 2500));
await tryShot("terminal", async () => {
  await tab("Zugriff", "Terminal", 800);
  await p.click(`button:has-text("${L("Verbinden")}")`, { timeout: 5000 }); await wait(3500);
  await p.click(".xterm, .terminal, canvas", { timeout: 3000 }).catch(() => {});
  // fish fragt beim ersten Start nach der Prompt-Konfiguration (tide) – abbrechen.
  await p.keyboard.type("q"); await p.keyboard.press("Enter"); await wait(1200);
  await p.keyboard.type("clear; uname -a; df -h /"); await p.keyboard.press("Enter"); await wait(1500);
});
await tryShot("remote-control", () => tab("Zugriff", "Fernsteuern", 1500));

await tryShot("policies", async () => { await p.goto(BASE + "/policies"); await wait(1500); });
await tryShot("policy-detail", async () => {
  await p.click(`table tbody tr:first-child button:has-text("${L("Bearbeiten")}")`, { timeout: 5000 }); await wait(1500);
});
await tryShot("scripts", async () => { await p.goto(BASE + "/scripts"); await wait(1200); });

// Nav-Modals (auf einer Seite mit sichtbarer Topnav).
await p.goto(BASE + "/devices"); await wait(1000);
await tryShot("network-scan", async () => { await p.click(`button.navbtn:has-text("${L("Netzwerk-Scan")}")`); await wait(900); });
await p.keyboard.press("Escape"); await wait(400);
await tryShot("bulk-action", async () => { await p.click(`button.navbtn:has-text("${L("Sammelaktion")}")`); await wait(900); });
await p.keyboard.press("Escape"); await wait(400);

await tryShot("roles", async () => { if (!(await openSettings("Benutzer & Rollen"))) throw new Error("no settings"); });
await tryShot("data-scope", async () => {
  const sc = await p.$(`button:has-text("${L("Bereich")}")`);
  if (!sc) throw new Error("no scope button");
  await sc.click(); await wait(800);
  await p.evaluate(() => document.querySelector(".scope-editor")?.scrollIntoView({ block: "center" }));
  await wait(400);
});
await p.keyboard.press("Escape"); await wait(300);
await tryShot("custom-fields", async () => {
  if (!(await openSettings("Geräte-Verwaltung"))) throw new Error("no settings");
  await p.evaluate((h) => { for (const el of document.querySelectorAll("h2")) if (el.textContent.trim() === h) el.scrollIntoView({ block: "start" }); }, L("Benutzerdefinierte Felder"));
  await wait(400);
});
await tryShot("alerting", async () => { if (!(await openSettings("Benachrichtigungen"))) throw new Error("no settings"); });

// Dunkles Design für die Landing-Page.
await setTheme("dark");
await p.goto(BASE + "/dashboard"); await wait(2000); await shot("dashboard-dark");
await openDevice(); await shot("device-detail-dark");
await setTheme("light");

await b.close();
console.log("done", LANG);
