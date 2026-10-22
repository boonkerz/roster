import { createContext, useCallback, useContext, useEffect, useRef, useState, ReactNode } from "react";
import { api } from "./api";
import { useAuth } from "./auth";

// Benutzereinstellungen der Web-UI, serverseitig je Benutzer gespeichert
// (GET /auth/me/prefs, PUT /auth/me/prefs/{key}) – Spaltenauswahl, gespeicherte Filter,
// Export-Spalten … sind damit auch nach dem Anmelden an einem anderen Browser da.
// Schreibzugriffe werden je Schlüssel kurz gebündelt (debounced).

type Prefs = Record<string, unknown>;

interface PrefsCtx {
  prefs: Prefs | null; // null = noch nicht geladen (oder nicht angemeldet)
  set: (key: string, value: unknown) => void;
}

const Ctx = createContext<PrefsCtx>({ prefs: null, set: () => {} });

export function PrefsProvider({ children }: { children: ReactNode }) {
  const { user } = useAuth();
  const [prefs, setPrefs] = useState<Prefs | null>(null);
  const timers = useRef<Record<string, number>>({});

  useEffect(() => {
    if (!user) { setPrefs(null); return; }
    let cancelled = false;
    api.get<Prefs>("/auth/me/prefs")
      .then((p) => { if (!cancelled) setPrefs(p ?? {}); })
      .catch(() => { if (!cancelled) setPrefs({}); }); // älterer Server: nur Sitzungs-Zustand
    return () => { cancelled = true; };
  }, [user?.id]); // eslint-disable-line react-hooks/exhaustive-deps

  const set = useCallback((key: string, value: unknown) => {
    setPrefs((p) => ({ ...(p ?? {}), [key]: value }));
    window.clearTimeout(timers.current[key]);
    timers.current[key] = window.setTimeout(() => {
      api.put(`/auth/me/prefs/${encodeURIComponent(key)}`, value).catch(() => {});
    }, 400);
  }, []);

  return <Ctx.Provider value={{ prefs, set }}>{children}</Ctx.Provider>;
}

// usePref liefert [Wert, Setzer, geladen]. Solange der Server-Stand nicht da ist, gilt
// der Standardwert. legacyKey: alter localStorage-Schlüssel, der einmalig übernommen
// wird, wenn der Server für diesen Benutzer noch nichts gespeichert hat.
export function usePref<T>(key: string, def: T, legacyKey?: string): [T, (v: T) => void, boolean] {
  const { prefs, set } = useContext(Ctx);
  const loaded = prefs !== null;
  const has = loaded && key in prefs;

  useEffect(() => {
    if (!loaded || has || !legacyKey) return;
    try {
      const raw = localStorage.getItem(legacyKey);
      if (raw) {
        set(key, JSON.parse(raw));
        localStorage.removeItem(legacyKey);
      }
    } catch { /* kaputter Altwert: ignorieren */ }
  }, [loaded, has, legacyKey, key, set]);

  const value = has ? (prefs[key] as T) : def;
  const setValue = useCallback((v: T) => set(key, v), [key, set]);
  return [value, setValue, loaded];
}
