package api

import "net"

// parseCIDR kapselt net.ParseCIDR (eigene Datei, damit secmon_handlers ohne net-Import bleibt).
func parseCIDR(s string) (net.IP, *net.IPNet, error) { return net.ParseCIDR(s) }
