# Network & discovery

Roster can look beyond managed devices and map what's on the network.

## Network discovery

An agent (or the server itself) scans a CIDR range via TCP / ARP / DNS and imports the
hosts it finds as **assets** into a site. Hostnames are resolved via reverse DNS, **NetBIOS**
and **mDNS**. You can then **adopt** an asset as an unmanaged device, and later merge it
with the real agent once one is installed on that host.

## SNMP

Query **SNMP** printer information — model, serial, firmware, page count and toner levels —
for discovered or unmanaged devices.

## Vulnerability scan (CVE)

Match installed software against **OSV.dev**: a per-device tab, a fleet-wide overview, and
a daily background scan (best coverage for Linux packages).

## Wake-on-LAN

Wake an offline device by MAC: Roster broadcasts the magic packet **from the server**
(best effort) and, if available, also asks an **online neighbor agent** in the same site to
send it — so waking works even across the server's own segment.
