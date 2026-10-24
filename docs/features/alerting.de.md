# Alarme & Berichte

## Alarme

Modulare Benachrichtigungskanäle – **E-Mail, Webhook, Pushover, Telegram, ntfy** – eingegrenzt auf
Firma/Standort/Gerät mit einem Schwere-Filter. Roster sendet, wenn ein Check neu fehlschlägt, und
erneut bei **Erholung**, und respektiert **Wartungsfenster**, damit geplante Arbeiten niemanden
alarmieren. Checks mit [Selbstheilung](checks-tasks.md#selbstheilung) bleiben still, bis die
Remediation gelaufen ist – nur ein Fehler, der sie überlebt, wird gemeldet.
Optionale Software-Änderungsalarme benachrichtigen dich, wenn sich installierte Software ändert.

![Benachrichtigungseinstellungen](../screenshots/alerting.png){ .shadow }

## Berichte

Geplante **Gesundheitsberichte**, die die Flotte zusammenfassen, zugestellt nach einem
cron-ähnlichen Zeitplan.

## Audit-Protokoll

Jede verändernde Aktion wird in einem **Audit-Protokoll** festgehalten (wer, was, wann, von wo),
für Admins verfügbar unter **Einstellungen → Sicherheit & Protokoll**.
