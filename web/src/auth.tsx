import { createContext, useContext, useEffect, useState, ReactNode } from "react";
import { api, ApiError } from "./api";
import type { User } from "./types";

export type LoginResult = { ok: true } | { totp: true; pending: string };

interface AuthState {
  user: User | null;
  loading: boolean;
  login: (username: string, password: string) => Promise<LoginResult>;
  completeTotp: (pending: string, code: string) => Promise<void>;
  reload: () => Promise<void>;
  logout: () => Promise<void>;
  // hasPerm prüft ein Recht; Admins haben implizit alles.
  hasPerm: (perm: string) => boolean;
}

const AuthContext = createContext<AuthState>(null!);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    api
      .get<User>("/auth/me")
      .then(setUser)
      .catch((e) => {
        if (!(e instanceof ApiError && e.status === 401)) console.error(e);
      })
      .finally(() => setLoading(false));
  }, []);

  const login = async (username: string, password: string): Promise<LoginResult> => {
    const res = await api.post<User & { totp_required?: boolean; pending?: string }>("/auth/login", { username, password });
    if (res?.totp_required) return { totp: true, pending: res.pending! };
    setUser(res);
    return { ok: true };
  };
  const completeTotp = async (pending: string, code: string) => {
    setUser(await api.post<User>("/auth/login/totp", { pending, code }));
  };
  const reload = async () => { setUser(await api.get<User>("/auth/me")); };
  const logout = async () => {
    await api.post("/auth/logout");
    setUser(null);
  };

  const hasPerm = (perm: string) =>
    !!user && (user.role === "admin" || (user.permissions ?? []).includes(perm));

  return <AuthContext.Provider value={{ user, loading, login, completeTotp, reload, logout, hasPerm }}>{children}</AuthContext.Provider>;
}

export const useAuth = () => useContext(AuthContext);
