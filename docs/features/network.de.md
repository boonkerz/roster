# Netzwerk & Erkennung

Roster kann über verwaltete Geräte hinausblicken und erfassen, was im Netzwerk unterwegs ist.

## Netzwerk-Erkennung

Ein Agent (oder der Server selbst) scannt einen CIDR-Bereich per TCP / ARP / DNS und importiert die
gefundenen Hosts als **Assets** in einen Standort. Hostnamen werden per Reverse-DNS, **NetBIOS**
und **mDNS** aufgelöst. Ein Asset kannst du anschließend als nicht verwaltetes Gerät **übernehmen**
und später mit dem echten Agent zusammenführen, sobald einer auf diesem Host installiert ist.

![Netzwerk-Scan](../screenshots/network-scan.png){ .shadow }

## SNMP

**SNMP**-Druckerinformationen abfragen – Modell, Seriennummer, Firmware, Seitenzähler und
Tonerstände – für erkannte oder nicht verwaltete Geräte.

## Schwachstellen-Scan (CVE)

Installierte Software mit **OSV.dev** abgleichen: ein Tab pro Gerät, eine flottenweite Übersicht
und ein täglicher Hintergrund-Scan (beste Abdeckung für Linux-Pakete).

## Wake-on-LAN

Ein Offline-Gerät per MAC aufwecken: Roster sendet das Magic Packet **vom Server** aus
(best effort) und bittet, falls vorhanden, zusätzlich einen **Online-Nachbar-Agent** am selben
Standort, es zu senden – so klappt das Aufwecken auch über das eigene Segment des Servers hinaus.
