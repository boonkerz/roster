import { ReactNode, useState } from "react";
import { NavLink } from "react-router-dom";
import { useAuth } from "../auth";
import { ThemeToggle } from "../theme";
import { LangSwitch, useI18n } from "../i18n";
import { Modal } from "./Modal";
import { Groups } from "../pages/Groups";
import { Settings } from "../pages/Settings";
import { Account } from "../pages/Account";
import { BulkActions } from "./BulkActions";
import { FleetVulnerabilities } from "./FleetVulnerabilities";

type ModalKey = "groups" | "settings" | "account" | "bulk" | "vulns";

export function Layout({ children }: { children: ReactNode }) {
  const { user, logout } = useAuth();
  const { t } = useI18n();
  const isAdmin = user?.role === "admin";
  const canOperate = user?.role === "admin" || user?.role === "technician";
  const [modal, setModal] = useState<ModalKey | null>(null);

  return (
    <div className="app">
      <header className="topbar">
        <div className="brand">
          <span className="brand-mark">▣</span> PC-Inventar
        </div>
        <nav className="topnav">
          <NavLink to="/dashboard">{t("Übersicht")}</NavLink>
          <NavLink to="/devices">{t("Geräte")}</NavLink>
          {isAdmin && <NavLink to="/policies">{t("Richtlinien")}</NavLink>}
          {isAdmin && <NavLink to="/scripts">{t("Skripte")}</NavLink>}
          <button className="navbtn" onClick={() => setModal("groups")}>{t("Tags")}</button>
          <button className="navbtn" onClick={() => setModal("vulns")}>{t("Schwachstellen")}</button>
          {canOperate && <button className="navbtn" onClick={() => setModal("bulk")}>{t("Sammelaktion")}</button>}
          {isAdmin && <button className="navbtn" onClick={() => setModal("settings")}>{t("Einstellungen")}</button>}
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
      {modal === "settings" && <Modal onClose={() => setModal(null)}><Settings /></Modal>}
      {modal === "account" && <Modal onClose={() => setModal(null)}><Account /></Modal>}
      {modal === "bulk" && <Modal onClose={() => setModal(null)}><BulkActions /></Modal>}
      {modal === "vulns" && <Modal onClose={() => setModal(null)}><FleetVulnerabilities /></Modal>}
    </div>
  );
}
