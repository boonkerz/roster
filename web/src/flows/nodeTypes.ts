import type { FlowNodeType, FlowTemplate, Flow } from "../types";

// Feldbeschreibungen je Baustein für das Eigenschaften-Panel des Editors.
export interface FieldDef {
  key: string;
  label: string;
  kind: "text" | "textarea" | "select" | "bool" | "number";
  options?: [string, string][];
  placeholder?: string;
  help?: string;
  show?: (cfg: Record<string, unknown>) => boolean;
}

export interface NodeTypeDef {
  type: FlowNodeType;
  label: string;
  icon: string;
  prefix: string;
  help: string;
  fields: FieldDef[];
  defaults: Record<string, string | number | boolean>;
}

const isCompose = (c: Record<string, unknown>) => c.mode === "compose_image";

export const NODE_TYPES: NodeTypeDef[] = [
  {
    type: "find_files", label: "Dateien finden", icon: "🔎", prefix: "find",
    help: "Findet Dateien per Pfadmuster oder die Compose-Dateien der Container mit einem Image. Eigene Backups (.roster-bak-…) werden nie gefunden. Keine Treffer = Gerät nicht betroffen, der Ablauf endet ohne Fehler.",
    defaults: { mode: "compose_image", image: "" },
    fields: [
      { key: "mode", label: "Modus", kind: "select", options: [["compose_image", "Compose-Dateien der Container mit Image"], ["paths", "Pfade / Muster"]] },
      { key: "image", label: "Image (ohne Tag = alle Tags)", kind: "text", placeholder: "jrcs/letsencrypt-nginx-proxy-companion", show: isCompose },
      { key: "paths", label: "Pfade, eine je Zeile (Glob)", kind: "textarea", placeholder: "/opt/*/docker-compose.yml", show: (c) => !isCompose(c) },
      { key: "contains", label: "Nur Dateien, die enthalten (optional)", kind: "text" },
    ],
  },
  {
    type: "replace", label: "Text ersetzen", icon: "✏️", prefix: "repl",
    help: "Ersetzt Text in den Dateien des vorigen Bausteins (oder in angegebenen Dateien). Vor dem Schreiben wird je Datei ein Backup angelegt; die Vorschau zeigt den Diff. Ohne Treffer endet der Ablauf hier ohne Fehler (Gerät schon umgestellt).",
    defaults: { search: "", replace: "" },
    fields: [
      { key: "search", label: "Suchen", kind: "text", placeholder: "8000" },
      { key: "replace", label: "Ersetzen durch", kind: "text", placeholder: "80" },
      { key: "regex", label: "Regulärer Ausdruck ($1 … für Gruppen)", kind: "bool" },
      { key: "line_filter", label: "Nur in Zeilen, die enthalten", kind: "text", placeholder: "image:" },
      { key: "files", label: "Dateien (leer = vom vorigen Baustein)", kind: "textarea" },
      { key: "fail_if_unchanged", label: "Fehler, wenn nichts ersetzt wurde", kind: "bool" },
      { key: "continue_if_unchanged", label: "Weitermachen, auch wenn nichts ersetzt wurde", kind: "bool", help: "Standard: Ohne Treffer endet der Ablauf hier (Gerät schon umgestellt)." },
    ],
  },
  {
    type: "compose", label: "Docker Compose", icon: "🐳", prefix: "compose",
    help: "pull, up -d, restart oder down – für die Projekte des vorigen „Dateien finden“ (nur betroffene Dienste) oder ein Verzeichnis.",
    defaults: { action: "pull" },
    fields: [
      { key: "action", label: "Aktion", kind: "select", options: [["pull", "pull"], ["up", "up -d"], ["restart", "restart"], ["down", "down"]] },
      { key: "dir", label: "Verzeichnis (leer = vom vorigen Baustein)", kind: "text", placeholder: "/opt/shop" },
      { key: "compose_files", label: "Compose-Dateien (optional, eine je Zeile)", kind: "textarea" },
      { key: "services", label: "Dienste (optional, kommagetrennt)", kind: "text" },
      { key: "all_services", label: "Alle Dienste des Projekts", kind: "bool" },
    ],
  },
  {
    type: "command", label: "Befehl / Skript", icon: "❯_", prefix: "cmd",
    help: "Führt einen Befehl oder ein Skript aus der Bibliothek aus. Exit ≠ 0 = Fehler. Ausgabe als {{steps.<id>.out}}. In der Vorschau wird nichts ausgeführt.",
    defaults: { shell: "shell", script: "" },
    fields: [
      { key: "script_id", label: "Skript aus der Bibliothek (optional)", kind: "select", options: [] },
      { key: "shell", label: "Shell", kind: "select", options: [["shell", "sh / bash"], ["powershell", "PowerShell"]], show: (c) => !c.script_id },
      { key: "script", label: "Befehl", kind: "textarea", placeholder: "docker image prune -f", show: (c) => !c.script_id },
      { key: "timeout_s", label: "Zeitlimit (Sekunden)", kind: "number", placeholder: "600" },
    ],
  },
  {
    type: "http", label: "API abfragen", icon: "🌐", prefix: "api",
    help: "HTTP-Anfrage; JSON-Antwort als {{steps.<id>.json.pfad}}, der Wert unter „JSON-Pfad“ als {{steps.<id>.value}}. Vorschau führt nur GET/HEAD aus.",
    defaults: { method: "GET", url: "" },
    fields: [
      { key: "method", label: "Methode", kind: "select", options: [["GET", "GET"], ["POST", "POST"], ["PUT", "PUT"], ["PATCH", "PATCH"], ["DELETE", "DELETE"], ["HEAD", "HEAD"]] },
      { key: "url", label: "URL", kind: "text", placeholder: "https://api.example.com/v1/version" },
      { key: "headers", label: "Header, einer je Zeile (Name: Wert)", kind: "textarea", placeholder: "Authorization: Bearer …" },
      { key: "body", label: "Body", kind: "textarea", show: (c) => c.method !== "GET" && c.method !== "HEAD" },
      { key: "json_path", label: "JSON-Pfad für den Wert", kind: "text", placeholder: "data.0.version" },
      { key: "require_value", label: "Fehler, wenn kein Wert", kind: "bool" },
      { key: "expect_status", label: "Erwarteter Status (leer = 2xx)", kind: "text", placeholder: "200,201" },
      { key: "insecure", label: "Zertifikat nicht prüfen", kind: "bool" },
    ],
  },
  {
    type: "check", label: "Prüfen", icon: "✓", prefix: "check",
    help: "Prüft einen Zustand und verzweigt über ok/fail. Ohne Container-Namen werden die betroffenen Compose-Dienste geprüft.",
    defaults: { kind: "container", retries: 3, interval_s: 10 },
    fields: [
      { key: "kind", label: "Art", kind: "select", options: [["container", "Container läuft"], ["http", "HTTP-Status"], ["tcp", "TCP-Port erreichbar"], ["file_contains", "Datei enthält Text"], ["expr", "Wert vergleichen"]] },
      { key: "container", label: "Container-Name enthält (leer = betroffene Dienste)", kind: "text", show: (c) => c.kind === "container" },
      { key: "healthy", label: "Muss healthy sein", kind: "bool", show: (c) => c.kind === "container" },
      { key: "url", label: "URL", kind: "text", show: (c) => c.kind === "http" },
      { key: "expect_status", label: "Erwarteter Status (leer = 2xx)", kind: "text", show: (c) => c.kind === "http" },
      { key: "host", label: "Host", kind: "text", placeholder: "127.0.0.1", show: (c) => c.kind === "tcp" },
      { key: "port", label: "Port", kind: "text", placeholder: "80", show: (c) => c.kind === "tcp" },
      { key: "path", label: "Datei", kind: "text", show: (c) => c.kind === "file_contains" },
      { key: "text", label: "Text", kind: "text", show: (c) => c.kind === "file_contains" },
      { key: "value", label: "Wert", kind: "text", placeholder: "{{steps.api.value}}", show: (c) => c.kind === "expr" },
      { key: "op", label: "Vergleich", kind: "select", options: [["eq", "gleich"], ["ne", "ungleich"], ["contains", "enthält"], ["gt", "größer"], ["lt", "kleiner"], ["version_lt", "Version kleiner als"], ["version_le", "Version kleiner/gleich"], ["version_gt", "Version größer als"], ["version_ge", "Version größer/gleich"], ["version_eq", "Version gleich"], ["notempty", "nicht leer"], ["empty", "leer"]], show: (c) => c.kind === "expr" },
      { key: "expect", label: "Vergleichswert", kind: "text", show: (c) => c.kind === "expr" },
      { key: "false_ends", label: "Nicht erfüllt: Ablauf ohne Fehler beenden (nicht betroffen)", kind: "bool" },
      { key: "retries", label: "Wiederholungen", kind: "number" },
      { key: "interval_s", label: "Abstand (Sekunden)", kind: "number" },
    ],
  },
  {
    type: "read_value", label: "Wert lesen", icon: "🔍", prefix: "val",
    help: "Liest einen Wert aus YAML/JSON (Pfad wie services.php-fpm.image) oder aus einer .env (Schlüssel). Ergebnis als {{steps.<id>.value}}; image_tag/image_repo schneidet Tag oder Repository aus einer Image-Angabe.",
    defaults: { path: "", extract: "none" },
    fields: [
      { key: "path", label: "Pfad bzw. Schlüssel", kind: "text", placeholder: "services.php-fpm.image" },
      { key: "extract", label: "Teil", kind: "select", options: [["none", "ganzer Wert"], ["image_tag", "Image-Tag (Version)"], ["image_repo", "Image-Repository"]] },
      { key: "files", label: "Dateien (leer = vom vorigen Baustein)", kind: "textarea" },
      { key: "optional", label: "Fehlt der Pfad: leerer Wert statt Fehler", kind: "bool" },
    ],
  },
  {
    type: "yaml", label: "YAML ändern", icon: "🧩", prefix: "yaml",
    help: "Ändert YAML-Dateien gezielt (Formatierung und Kommentare bleiben): set pfad = wert, append pfad = listeneintrag (nur wenn neu), ensure pfad = wert (nur wenn fehlt), delete pfad. {a,b} im Pfad gilt für mehrere Dienste, {{value.repo}} ist das Repository des aktuellen Werts. Backup, Diff und Vorschau wie bei „Text ersetzen“.",
    defaults: { ops: "set services.{web,php-fpm,cron}.image = {{value.repo}}:2.5.0\nappend services.php-fpm.volumes = shop_media:/var/www/media\nensure volumes.shop_media = {}" },
    fields: [
      { key: "ops", label: "Operationen (eine je Zeile)", kind: "textarea" },
      { key: "files", label: "Dateien (leer = vom vorigen Baustein)", kind: "textarea" },
      { key: "fail_if_unchanged", label: "Fehler, wenn nichts ersetzt wurde", kind: "bool" },
      { key: "continue_if_unchanged", label: "Weitermachen, auch wenn nichts ersetzt wurde", kind: "bool" },
    ],
  },
  {
    type: "rollback", label: "Zurückrollen", icon: "↩", prefix: "rb",
    help: "Stellt alle in diesem Lauf geänderten Dateien aus den Backups wieder her. Unbehandelte Fehler rollen automatisch zurück.",
    defaults: { compose_up: true },
    fields: [{ key: "compose_up", label: "Danach betroffene Dienste neu starten (up -d)", kind: "bool" }],
  },
  {
    type: "wait", label: "Warten", icon: "⏱", prefix: "wait",
    help: "Wartet eine Zeit (max. 10 Minuten), z. B. bis Container hochgefahren sind.",
    defaults: { seconds: 15 },
    fields: [{ key: "seconds", label: "Sekunden", kind: "number" }],
  },
];

export const NODE_TYPE: Record<string, NodeTypeDef> = Object.fromEntries(NODE_TYPES.map((n) => [n.type, n]));

// nodeSummary fasst die Konfiguration für die Kachel im Editor zusammen.
export function nodeSummary(type: string, c: Record<string, unknown>): string {
  const s = (k: string) => String(c[k] ?? "");
  switch (type) {
    case "find_files": return c.mode === "compose_image" ? `Image ${s("image")}` : s("paths").split("\n")[0];
    case "replace": return `${s("search")} → ${s("replace")}${c.line_filter ? ` (Zeilen mit „${s("line_filter")}“)` : ""}`;
    case "compose": return `${s("action") === "up" ? "up -d" : s("action")}${c.dir ? ` · ${s("dir")}` : ""}`;
    case "command": return c.script_id ? "Bibliotheks-Skript" : s("script").split("\n")[0];
    case "http": return `${s("method") || "GET"} ${s("url")}`;
    case "check": return c.kind === "expr" ? `${s("value")} ${s("op")} ${s("expect")}${c.false_ends ? " · sonst Ende" : ""}` : s("kind");
    case "wait": return `${s("seconds")} s`;
    case "read_value": return `${s("path")}${c.extract && c.extract !== "none" ? ` → ${s("extract")}` : ""}`;
    case "yaml": return s("ops").split("\n")[0];
    case "rollback": return c.compose_up ? "Dateien + up -d" : "Dateien";
  }
  return "";
}

// localizeTemplate übersetzt Name, Beschreibung, Knoten- und Parameterbeschriftungen einer Vorlage.
export function localizeTemplate(tpl: FlowTemplate, t: (s: string) => string): Partial<Flow> {
  return {
    name: t(tpl.name), description: t(tpl.description),
    graph: { nodes: tpl.graph.nodes.map((n) => ({ ...n, label: n.label ? t(n.label) : n.label })), edges: tpl.graph.edges },
    params: tpl.params.map((p) => ({ ...p, label: p.label ? t(p.label) : p.label })),
  };
}
