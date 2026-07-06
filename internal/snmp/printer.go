// Package snmp liest Netzwerkdrucker per SNMP (v2c) über die standardisierte
// Printer-MIB aus – Modell, Seriennummer, Seitenzähler, Status und Toner-/Trommel-
// Füllstände. Firmware/genaue Modellbezeichnung stecken meist in der Beschreibung.
package snmp

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/gosnmp/gosnmp"

	"github.com/thomaspeterson/pc-inventory/internal/shared"
)

const (
	oidSysDescr    = "1.3.6.1.2.1.1.1.0"           // Beschreibung (enthält oft Modell + Firmware)
	oidModel       = "1.3.6.1.2.1.25.3.2.1.3.1"    // hrDeviceDescr
	oidSerial      = "1.3.6.1.2.1.43.5.1.1.17.1"   // prtGeneralSerialNumber
	oidPageCount   = "1.3.6.1.2.1.43.10.2.1.4.1.1" // prtMarkerLifeCount
	oidStatus      = "1.3.6.1.2.1.25.3.5.1.1.1"    // hrPrinterStatus
	oidSupplyDesc  = "1.3.6.1.2.1.43.11.1.1.6"     // prtMarkerSuppliesDescription (Tabelle)
	oidSupplyMax   = "1.3.6.1.2.1.43.11.1.1.8"     // prtMarkerSuppliesMaxCapacity
	oidSupplyLevel = "1.3.6.1.2.1.43.11.1.1.9"     // prtMarkerSuppliesLevel
)

var printerStatus = map[int]string{1: "sonstiger", 2: "unbekannt", 3: "bereit", 4: "druckt", 5: "aufwärmen"}

// Query liest die Druckerdaten von ip (Community meist "public").
func Query(ctx context.Context, ip, community string) (*shared.PrinterInfo, error) {
	if community == "" {
		community = "public"
	}
	g := &gosnmp.GoSNMP{
		Target:    ip,
		Port:      161,
		Community: community,
		Version:   gosnmp.Version2c,
		Timeout:   3 * time.Second,
		Retries:   1,
		Context:   ctx,
	}
	if err := g.Connect(); err != nil {
		return nil, err
	}
	defer g.Conn.Close()

	info := &shared.PrinterInfo{IP: ip}
	if res, err := g.Get([]string{oidSysDescr, oidModel, oidSerial, oidPageCount, oidStatus}); err == nil {
		for _, v := range res.Variables {
			switch {
			case strings.HasSuffix(v.Name, oidSysDescr):
				info.Description = str(v)
			case strings.HasSuffix(v.Name, oidModel):
				info.Model = str(v)
			case strings.HasSuffix(v.Name, oidSerial):
				info.Serial = str(v)
			case strings.HasSuffix(v.Name, oidPageCount):
				info.PageCount = toInt(v)
			case strings.HasSuffix(v.Name, oidStatus):
				if s, ok := printerStatus[toInt(v)]; ok {
					info.Status = s
				}
			}
		}
	}

	// Verbrauchsmaterialien (Tabelle) einsammeln.
	descs := walkStr(g, oidSupplyDesc)
	maxes := walkInt(g, oidSupplyMax)
	levels := walkInt(g, oidSupplyLevel)
	for i, d := range descs {
		s := shared.PrinterSupply{Name: d, Level: -2, Max: -2}
		if i < len(maxes) {
			s.Max = maxes[i]
		}
		if i < len(levels) {
			s.Level = levels[i]
		}
		info.Supplies = append(info.Supplies, s)
	}
	return info, nil
}

func str(v gosnmp.SnmpPDU) string {
	if b, ok := v.Value.([]byte); ok {
		return strings.TrimSpace(string(b))
	}
	if s, ok := v.Value.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

func toInt(v gosnmp.SnmpPDU) int {
	switch n := v.Value.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case uint:
		return int(n)
	case uint64:
		return int(n)
	}
	if s := str(v); s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			return n
		}
	}
	return 0
}

func walkStr(g *gosnmp.GoSNMP, oid string) []string {
	var out []string
	_ = g.Walk(oid, func(v gosnmp.SnmpPDU) error { out = append(out, str(v)); return nil })
	return out
}

func walkInt(g *gosnmp.GoSNMP, oid string) []int {
	var out []int
	_ = g.Walk(oid, func(v gosnmp.SnmpPDU) error { out = append(out, toInt(v)); return nil })
	return out
}
