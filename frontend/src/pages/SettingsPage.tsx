import { useEffect, useState } from "react";
import { CheckCircle2, KeyRound, Server, XCircle } from "lucide-react";
import { McpIntegration } from "../components/integrations/McpIntegration";
import { TopBar } from "../components/layout/TopBar";
import { Card } from "../components/ui/Card";
import { Spinner } from "../components/ui/Spinner";
import { api } from "../lib/api";
import type { Settings } from "../types";

export function SettingsPage() {
  const [settings, setSettings] = useState<Settings | null>(null);
  const [error, setError] = useState("");
  useEffect(() => {
    void api<Settings>("/api/settings").then(setSettings).catch((reason: unknown) => setError(reason instanceof Error ? reason.message : "Could not load settings"));
  }, []);

  return <>
    <TopBar title="Settings" />
    <main className="page-content settings-page">
      <div className="settings-title"><h2>Local runtime</h2><p>Read-only configuration loaded from the environment.</p></div>
      {!settings && !error ? <Spinner /> : error ? <div className="inline-error">{error}</div> : settings && <Card className="settings-card">
        <div className="setting-item">
          <span className="setting-icon"><KeyRound size={18} /></span>
          <div><h3>Gemini provider</h3><p>Argus reads the API key from your environment. Secrets are never stored by this app.</p></div>
          <span className={`configuration-state ${settings.gemini_configured ? "configured" : "missing"}`}>
            {settings.gemini_configured ? <CheckCircle2 size={14} /> : <XCircle size={14} />}{settings.gemini_configured ? "Configured" : "Not configured"}
          </span>
        </div>
        <div className="environment-code"><small>ENVIRONMENT SETUP</small><code>GEMINI_API_KEY=your-api-key</code><p>Add this value to <b>.env</b> or your shell, then restart Argus.</p></div>
        <div className="setting-item compact-setting"><span className="setting-icon"><Server size={18} /></span><div><h3>Model</h3><p>Override with <code>GEMINI_MODEL</code>.</p></div><code>{settings.model}</code></div>
        <div className="settings-note">Target-site credentials are not stored or managed in this release.</div>
      </Card>}
      <McpIntegration settings={settings} error={error} />
    </main>
  </>;
}
