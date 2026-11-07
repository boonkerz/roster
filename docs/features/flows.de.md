# Abläufe (Flow-Designer)

Wiederkehrende Änderungen auf vielen Servern – ein Image in Compose-Dateien tauschen, einen Port
von `8000` auf `80` umstellen, eine Version aus einer API in eine `.env` schreiben – baust du als
**Ablauf** aus Bausteinen, die du per Linie verbindest. Ausgeführt wird er vom Agent selbst
(ohne `sed`, `bash` oder Zusatzwerkzeuge), mit **Vorschau**, **Backup** und **Zurückrollen**.

![Flow-Designer mit Bausteinen und Verbindungen](../screenshots/flows-editor.png){ .shadow }

## Editor

Unter **Abläufe** legst du einen neuen Ablauf an oder startest **aus einer Vorlage**. Links stehen
die Bausteine, in der Mitte die Zeichenfläche, rechts die Eigenschaften des ausgewählten
Bausteins. Jeder Baustein hat einen Eingang oben und zwei Ausgänge unten:

- **ok** (grün) – weiter, wenn der Baustein erfolgreich war,
- **Fehler** (rot) – Zweig für den Fehlerfall, z. B. auf *Zurückrollen*.

Ein Ablauf hat genau einen Startbaustein (ohne eingehende Linie) und darf keine Kreise
enthalten. Ist der Fehler-Ausgang eines Bausteins **nicht verbunden**, bricht der Ablauf dort ab
und stellt **automatisch** alle bis dahin geänderten Dateien wieder her.

**Parameter** (z. B. `FROM`, `TO`) fragt der Assistent vor dem Start ab; im Ablauf stehen sie als
`{{param.FROM}}`.

## Bausteine

| Baustein | Was er tut | In der Vorschau |
|---|---|---|
| **Dateien finden** | Pfade/Globs (eine je Zeile, optional „enthält …“) **oder** die Compose-Dateien aller Container mit einem Image (über die Compose-Labels; ohne Tag = alle Tags; dazu die `.env`, falls das Image dort steht). Keine Treffer = Gerät nicht betroffen, der Ablauf endet ohne Fehler. | läuft |
| **Text ersetzen** | Literal oder regulärer Ausdruck (`$1` …), optional nur in Zeilen mit einem Text (z. B. `image:`). Je Datei wird vorher ein Backup `<datei>.roster-bak-<lauf>` angelegt. | zeigt den Diff, ändert nichts |
| **Docker Compose** | `pull`, `up -d`, `restart`, `down` – für die gefundenen Projekte (nur betroffene Dienste) oder ein Verzeichnis; `docker compose` mit Rückfall auf `docker-compose`. | zeigt die Befehle |
| **Befehl / Skript** | Eigener Befehl oder ein Skript aus der Bibliothek; Exit ≠ 0 = Fehler. | wird nicht ausgeführt |
| **API abfragen** | HTTP-Anfrage mit Headern/Body; JSON-Pfad liefert einen Wert (`{{steps.api.value}}`). | nur GET/HEAD |
| **Prüfen** | Container läuft (ohne Namen: die betroffenen Compose-Dienste), HTTP-Status, TCP-Port, Datei enthält Text, Wert vergleichen – mit Wiederholungen. | wird übersprungen |
| **Zurückrollen** | Stellt alle in diesem Lauf geänderten Dateien wieder her, optional danach `up -d`. | wird übersprungen |
| **Warten** | Wartet bis zu 10 Minuten. | wird übersprungen |

**Platzhalter:** `{{param.NAME}}`, `{{steps.ID.value}}`, `{{steps.ID.out}}` (Befehlsausgabe),
`{{steps.ID.json.pfad.0.feld}}`, `{{steps.ID.files}}` und die Gerätefelder `{{agent.feld}}`,
`{{client.feld}}`, `{{site.feld}}`.

## Ausführen: Vorschau → Testserver → Rollout

![Assistent mit Vorschau und Diff](../screenshots/flows-run.png){ .shadow }

1. **Parameter und Vorschau-Server** wählen, *Vorschau starten*: der Agent dieses Servers führt den
   Ablauf im Trockenlauf aus und zeigt je Baustein, was passieren würde – samt Diff jeder Datei.
2. **Auf Testserver ausführen**: derselbe Server, diesmal echt (mit Backups).
3. **Ausrollen** auf eine Firma, einen Standort, einen Tag, alle Geräte oder – vom Container-Tab
   aus – die betroffenen Geräte. Die Tabelle zeigt den Stand je Gerät; Geräte ohne Treffer
   erscheinen als *nicht betroffen*.

Unter **Abläufe → Läufe** stehen frühere Läufe mit ihren Ergebnissen; einzelne Läufe lassen sich
löschen. **Kopieren** (in der Liste oder im Editor) legt einen Ablauf als Ausgangspunkt für eine
Variante an – im Editor auch mit noch ungespeicherten Änderungen.

## Vorlagen

- **Image in Compose ersetzen** – Compose-Dateien mit altem Image finden, in der `image:`-Zeile
  ersetzen, `pull`, `up -d`, kurz warten, prüfen; bei Fehler zurückrollen und alten Stand
  hochfahren. Unter **Sicherheit → Container** öffnet *Image ersetzen …* diese Vorlage mit dem
  Image und den betroffenen Geräten vorbelegt (z. B. `jrcs/letsencrypt-nginx-proxy-companion` →
  `nginxproxy/acme-companion`).
- **Wert in Datei ersetzen und Dienste neu starten** – z. B. Port `8000` → `80`, Projekt neu
  starten, Port prüfen.
- **API abfragen und Wert eintragen** – Wert per JSON-Pfad lesen und per regulärem Ausdruck in
  eine Datei schreiben.

!!! note "Rechte"
    Abläufe bearbeiten erfordert das Recht *Skript-Bibliothek*, ausführen *Geräte bedienen*.
    Jeder Start steht im Audit-Log. Der Agent führt Abläufe mit seinen Rechten aus (root bzw.
    SYSTEM) – Backups bleiben neben den Dateien liegen.
