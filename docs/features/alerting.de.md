# Alarme & Berichte

## Alarme

Modulare Benachrichtigungskanäle – **E-Mail, Webhook, Pushover, Telegram, ntfy** – eingegrenzt auf
Firma/Standort/Gerät mit einem Schwere-Filter. Roster sendet, wenn ein Check neu fehlschlägt, und
erneut bei **Erholung**, und respektiert **Wartungsfenster**, damit geplante Arbeiten niemanden
alarmieren. Checks mit [Selbstheilung](checks-tasks.md#selbstheilung) bleiben still, bis die
Remediation gelaufen ist – nur ein Fehler, der sie überlebt, wird gemeldet.

### Software-Änderungsalarme

Roster kann melden, wenn sich die installierte Software eines Geräts ändert. Der globale
Schalter unter *Einstellungen → Alarme* ist der **Standard**; der Bereich
**Benachrichtigungen** einer Richtlinie kann ihn mit *Ja* oder *Nein* überstimmen und bei
*Ja* eingrenzen, was gemeldet wird:

- **Änderungsarten** – installiert, entfernt, aktualisiert (beliebig kombinierbar).
- **Ignorierte Programme** – ein Glob-Muster je Zeile, ohne Rücksicht auf Groß-/Kleinschreibung
  gegen den Programmnamen geprüft (z. B. `Microsoft Edge*`, `lib*`).

Bei mehreren Richtlinien gewinnt die am **Gerät** zugewiesene vor der am **Standort** vor
der an der **Firma**; eine Richtlinie auf *Standard* bleibt transparent. Die Einstellung am
Gerät selbst (*Wie Richtlinie / Melden / Nicht melden* auf der Geräteseite) hat das letzte
Wort über den Schalter, die Filter kommen weiterhin aus der gewinnenden Richtlinie.

![Benachrichtigungseinstellungen](../screenshots/alerting.png){ .shadow }

## Berichte

Geplante **Gesundheitsberichte**, die die Flotte zusammenfassen, zugestellt nach einem
cron-ähnlichen Zeitplan.

## Audit-Protokoll

Jede verändernde Aktion wird in einem **Audit-Protokoll** festgehalten (wer, was, wann, von wo),
für Admins verfügbar unter **Einstellungen → Sicherheit & Protokoll**.
