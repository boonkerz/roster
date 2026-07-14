package model

// Permission-Keys (Seiten/Funktionen). Diese Liste ist die einzige Wahrheit; das
// Frontend spiegelt sie. „page.*" steuert Sichtbarkeit/Zugriff einer Seite,
// „devices.operate" die Geräte-Bedienung (Terminal, Fernsteuerung, Skripte, Neustart …).
const (
	PermDashboard      = "page.dashboard" // Übersicht
	PermDevices        = "page.devices"   // Geräte (Liste + Detail, lesend)
	PermPolicies       = "page.policies"  // Richtlinien
	PermScripts        = "page.scripts"   // Skript-Bibliothek
	PermSettings       = "page.settings"  // Einstellungen (Org, Alerts, Tokens-Ansicht …)
	PermDevicesOperate = "devices.operate" // Geräte bedienen (nicht nur ansehen)
)

// AllPermissions ist der vollständige Katalog (für die Rollen-Verwaltung im UI und
// als „alles" für Admins).
var AllPermissions = []string{
	PermDashboard, PermDevices, PermPolicies, PermScripts, PermSettings, PermDevicesOperate,
}

// defaultRolePermissions bildet die eingebauten Rollen auf Rechte ab (Verhalten wie
// bisher: admin = alles, technician = ansehen + bedienen, viewer = nur ansehen).
func defaultRolePermissions(r Role) []string {
	switch r {
	case RoleAdmin:
		return AllPermissions
	case RoleTech:
		return []string{PermDashboard, PermDevices, PermDevicesOperate}
	default: // viewer
		return []string{PermDashboard, PermDevices}
	}
}

// EffectivePermissions liefert die tatsächlichen Rechte eines Benutzers: Admins sind
// immer Superuser (alle Rechte); sonst gelten – falls gesetzt – die Rechte der
// Custom-Rolle, andernfalls die Default-Rechte der eingebauten Rolle. customPerms ist
// das (bereits geladene) Permission-Set der Custom-Rolle oder nil.
func EffectivePermissions(u *User, customPerms []string) []string {
	if u == nil {
		return nil
	}
	if u.Role == RoleAdmin {
		return AllPermissions // Superuser, unabhängig von Custom-Rolle
	}
	if u.CustomRoleID != "" && customPerms != nil {
		return customPerms
	}
	return defaultRolePermissions(u.Role)
}

// HasPermission prüft, ob perms den Key enthält.
func HasPermission(perms []string, key string) bool {
	for _, p := range perms {
		if p == key {
			return true
		}
	}
	return false
}
