import { ReactNode, useEffect, useRef, useState } from "react";
import { NavLink } from "react-router-dom";
import { useAuth } from "../auth";
import { ThemeToggle } from "../theme";
import { LangSwitch, useI18n } from "../i18n";
import { Modal } from "./Modal";
import { Groups } from "../pages/Groups";
import { Settings, type SettingsArea } from "../pages/Settings";
import { Account } from "../pages/Account";
import { BulkActions } from "./BulkActions";
import { FleetVulnerabilities } from "./FleetVulnerabilities";
import { NetworkScan } from "./NetworkScan";

type ModalKey = "groups" | "settings" | "account" | "bulk" | "vulns" | "netscan";

// NavDropdown zeigt ein klickbares Menü in der Topnav (schließt bei Klick außerhalb).
function NavDropdown({ label, items }: { label: string; items: { label: string; onClick: () => void }[] }) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!open) return;
    const onDoc = (e: MouseEvent) => { if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false); };
    document.addEventListener("mousedown", onDoc);
    return () => document.removeEventListener("mousedown", onDoc);
  }, [open]);
  return (
    <div className="nav-dropdown" ref={ref}>
      <button className={`navbtn ${open ? "active" : ""}`} onClick={() => setOpen((o) => !o)}>{label} ▾</button>
      {open && (
        <div className="nav-dropdown-menu">
          {items.map((it) => (
            <button key={it.label} className="nav-dropdown-item" onClick={() => { it.onClick(); setOpen(false); }}>{it.label}</button>
          ))}
        </div>
      )}
    </div>
  );
}

export function Layout({ children }: { children: ReactNode }) {
  const { user, logout, hasPerm } = useAuth();
  const { t } = useI18n();
  const isAdmin = user?.role === "admin";
  const canOperate = hasPerm("devices.operate");
  const [modal, setModal] = useState<ModalKey | null>(null);
  const [settingsArea, setSettingsArea] = useState<SettingsArea>();

  const openSettings = (area: SettingsArea) => { setSettingsArea(area); setModal("settings"); };

  return (
    <div className="app">
      <header className="topbar">
        <div className="brand">
          <span className="brand-mark">▣</span> Roster
        </div>
        <nav className="topnav">
          {hasPerm("page.dashboard") && <NavLink to="/dashboard">{t("Übersicht")}</NavLink>}
          {hasPerm("page.devices") && <NavLink to="/devices">{t("Geräte")}</NavLink>}
          {hasPerm("page.policies") && <NavLink to="/policies">{t("Richtlinien")}</NavLink>}
          {hasPerm("page.scripts") && <NavLink to="/scripts">{t("Skripte")}</NavLink>}
          {hasPerm("page.devices") && <button className="navbtn" onClick={() => setModal("groups")}>{t("Tags")}</button>}
          {hasPerm("page.devices") && <button className="navbtn" onClick={() => setModal("vulns")}>{t("Schwachstellen")}</button>}
          {canOperate && <button className="navbtn" onClick={() => setModal("bulk")}>{t("Sammelaktion")}</button>}
          {canOperate && <button className="navbtn" onClick={() => setModal("netscan")}>{t("Netzwerk-Scan")}</button>}
          {hasPerm("page.settings") && (
            <NavDropdown label={t("Einstellungen")} items={[
              ...(isAdmin ? [{ label: t("Benutzer & Rollen"), onClick: () => openSettings("users") }] : []),
              { label: t("Benachrichtigungen"), onClick: () => openSettings("notify") },
              { label: t("Geräte-Verwaltung"), onClick: () => openSettings("devices") },
              { label: t("Sicherheit & Protokoll"), onClick: () => openSettings("security") },
            ]} />
          )}
        </nav>
        <div className="topbar-right">
          <button className="topuser topuser-btn" onClick={() => setModal("account")} title={t("Mein Konto")}>
            <div className="user-name">{user?.username}</div>
            <div className="user-role">{user?.role}</div>
          </button>
          <button className="btn ghost" onClick={() => logout()}>{t("Abmelden")}</button>
          <LangSwitch />
          <ThemeToggle />
        </div>
      </header>

      <main className="content">{children}</main>

      {modal === "groups" && <Modal onClose={() => setModal(null)}><Groups /></Modal>}
      {modal === "settings" && <Modal onClose={() => setModal(null)}><Settings initialArea={settingsArea} /></Modal>}
      {modal === "account" && <Modal onClose={() => setModal(null)}><Account /></Modal>}
      {modal === "bulk" && <Modal onClose={() => setModal(null)}><BulkActions /></Modal>}
      {modal === "vulns" && <Modal onClose={() => setModal(null)}><FleetVulnerabilities /></Modal>}
      {modal === "netscan" && <Modal onClose={() => setModal(null)}><NetworkScan /></Modal>}
    </div>
  );
}
