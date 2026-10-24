# Übersicht

Die Übersichtsseite zeigt die Flotte auf einen Blick: Geräteanzahl, fehlschlagende Checks
und Tasks, ausstehende Patches und Schwachstellen. Alles unterhalb dieser Kacheln ist ein
**Widget-Raster, das du selbst konfigurierst** – es startet mit einem Geräte-Status-Ring und
den letzten Status-Wechseln der Checks; beide kannst du wie jedes andere Widget entfernen, in
der Größe ändern oder wieder hinzufügen.

## Eigene Diagramme

Klicke auf **+ Diagramm** und wähle eine Quelle, eine Darstellung und (für den Verlauf) einen
Zeitraum:

![Übersicht mit eigenen Diagrammen](../screenshots/dashboard.png){ .shadow }

| Quelle | Was gezählt wird |
|---|---|
| **Kennzahl** | die Zahlen der Übersicht – Geräte gesamt/online/offline, fehlschlagende Checks und Tasks, ausstehende Patches, Schwachstellen – oder die Verteilung des Geräte-Status |
| **Check** | ein Richtlinien-Check über alle Geräte: wie viele bestehen, warnen, fehlschlagen |
| **Feld** | ein benutzerdefiniertes Gerätefeld: wie viele Geräte jeden Wert tragen – z. B. die Docker-Image-Versionen, die ein Collector-Task schreibt, siehe [Inventar → Benutzerdefinierte Felder](inventory.md#benutzerdefinierte-felder). Listenfelder zählen jeden Eintrag |
| **Status-Wechsel** | die Liste der letzten Status-Übergänge von Checks (keine Darstellung zu wählen) |

Darstellungen:

- **Balken** – die aktuelle Verteilung, ein Balken pro Wert (bis zu 12, der Rest
  zusammengefasst unter *Andere*). Am besten für viele Werte wie Versionen.
- **Ring** – die aktuellen Anteile, bis zu 7 Segmente plus *Andere*. Am besten für eine
  Handvoll Kategorien wie den Status eines Checks.
- **Verlauf** – eine Linie pro Wert über 24 Stunden bis zu einem Jahr, aus **stündlichen
  Snapshots**, die der Server von jeder Kennzahl, jedem Check-Status und jedem Feldwert
  nimmt. Der erste Snapshot entsteht etwa eine Minute nach dem Serverstart, danach einer zu
  jeder vollen Stunde; Snapshots werden ein Jahr lang aufbewahrt.

Fahre mit der Maus über einen Balken, ein Segment oder einen Punkt, um exakte Zahlen zu
sehen. Diagramme aktualisieren sich jede Minute, und die Statusfarben (grün/orange/rot) sind
für Check- und Geräte-Status reserviert; andere Reihen verwenden eine feste,
farbenblind-sichere Reihenfolge.

Jedes Widget hat eine **Breite** – ein Viertel, die Hälfte, drei Viertel oder die volle Zeile
eines vierspaltigen Rasters (zwei Spalten auf schmaleren Bildschirmen, eine auf Smartphones).
**Ziehe** ein Widget an seinem Griff auf ein anderes, um die Reihenfolge zu ändern; der Stift
bearbeitet Quelle, Darstellung, Breite und Titel, das Kreuz entfernt es.

Deine Widgets werden in deinem **Benutzerkonto** gespeichert, genau wie die Spalten der
Geräteliste und gespeicherte Filter – sie folgen dir also in jeden Browser.

Benutzer mit eingeschränktem Datenbereich (siehe [Rollen & Berechtigungen](permissions.md))
sehen nur die Zahlen ihrer Standorte – Snapshots werden pro Standort genommen und innerhalb
des Datenbereichs des Benutzers summiert.
