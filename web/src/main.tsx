import React from "react";
import ReactDOM from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { AuthProvider } from "./auth";
import { PrefsProvider } from "./prefs";
import { ThemeProvider, applyTheme, resolveInitialTheme } from "./theme";
import { I18nProvider } from "./i18n";
import App from "./App";
import "./styles.css";

// Theme vor dem ersten Render setzen, um ein Aufblitzen des falschen Designs zu vermeiden.
applyTheme(resolveInitialTheme());

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: false, refetchOnWindowFocus: false } },
});

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <I18nProvider>
          <AuthProvider>
            <PrefsProvider>
              <ThemeProvider>
                <App />
              </ThemeProvider>
            </PrefsProvider>
          </AuthProvider>
        </I18nProvider>
      </BrowserRouter>
    </QueryClientProvider>
  </React.StrictMode>
);
