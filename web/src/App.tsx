import { Navigate, Route, Routes } from "react-router-dom";
import { useAuth } from "./auth";
import { Layout } from "./components/Layout";
import { Login } from "./pages/Login";
import { Dashboard } from "./pages/Dashboard";
import { Devices } from "./pages/Devices";
import { DeviceDetail } from "./pages/DeviceDetail";
import { Policies } from "./pages/Policies";
import { Scripts } from "./pages/Scripts";
import { TwoFactorSetup } from "./pages/TwoFactorSetup";
import { TerminalPopout } from "./pages/TerminalPopout";
import { RemotePopout } from "./pages/RemotePopout";

export default function App() {
  const { user, loading, hasPerm } = useAuth();

  if (loading) return <div className="center muted">Lädt…</div>;
  if (!user) return <Login />;
  // 2FA-Pflicht: ohne aktivierten zweiten Faktor zuerst die Einrichtung erzwingen.
  if (user.require_2fa && !user.totp_enabled) return <TwoFactorSetup />;

  // Erste für den Nutzer zugängliche Seite (Fallback/Startziel).
  const home = hasPerm("page.dashboard") ? "/dashboard" : hasPerm("page.devices") ? "/devices"
    : hasPerm("page.policies") ? "/policies" : hasPerm("page.scripts") ? "/scripts" : "/dashboard";
  // guard rendert die Seite nur mit Recht, sonst Weiterleitung aufs Startziel.
  const guard = (perm: string, el: JSX.Element) => (hasPerm(perm) ? el : <Navigate to={home} replace />);

  return (
    <Routes>
      {/* Popout-Terminal/Fernsteuerung: eigenes Vollfenster ohne Layout/Sidebar. */}
      <Route path="/devices/:id/terminal" element={<TerminalPopout />} />
      <Route path="/devices/:id/remote" element={<RemotePopout />} />
      <Route path="*" element={
        <Layout>
          <Routes>
            <Route path="/" element={<Navigate to={home} replace />} />
            <Route path="/dashboard" element={guard("page.dashboard", <Dashboard />)} />
            <Route path="/devices" element={guard("page.devices", <Devices />)} />
            <Route path="/devices/:id" element={guard("page.devices", <DeviceDetail />)} />
            <Route path="/policies" element={guard("page.policies", <Policies />)} />
            <Route path="/scripts" element={guard("page.scripts", <Scripts />)} />
            <Route path="*" element={<Navigate to={home} replace />} />
          </Routes>
        </Layout>
      } />
    </Routes>
  );
}
