import { useEffect, useId, useRef, useState, type KeyboardEvent } from "react";
import { Check, CircleDashed, Copy, Server, Settings2, XCircle } from "lucide-react";
import { getMcpConfig, type McpClient } from "../../lib/mcpConfig";
import type { Settings } from "../../types";

const clients: Array<{ id: McpClient; label: string; location: string }> = [
  { id: "claude-code", label: "Claude Code", location: ".mcp.json" },
  { id: "cursor", label: "Cursor", location: "~/.cursor/mcp.json" },
  { id: "generic", label: "Generic", location: "Your MCP client config" },
];

export function McpIntegration({ settings, error }: { settings: Settings | null; error: string }) {
  const [client, setClient] = useState<McpClient>("claude-code");
  const [copyStatus, setCopyStatus] = useState<"idle" | "copied" | "error">("idle");
  const timeout = useRef<number | undefined>(undefined);
  const tabRefs = useRef<Array<HTMLButtonElement | null>>([]);
  const tabsId = useId();
  const config = getMcpConfig(client);
  const activeClient = clients.find(({ id }) => id === client)!;

  useEffect(() => () => window.clearTimeout(timeout.current), []);

  async function copyConfig() {
    try {
      if (!navigator.clipboard) throw new Error("Clipboard is unavailable");
      await navigator.clipboard.writeText(config);
      setCopyStatus("copied");
    } catch {
      setCopyStatus("error");
    }
    window.clearTimeout(timeout.current);
    timeout.current = window.setTimeout(() => setCopyStatus("idle"), 3000);
  }

  function selectClient(index: number) {
    setClient(clients[index].id);
    tabRefs.current[index]?.focus();
  }

  function handleTabKeyDown(event: KeyboardEvent<HTMLButtonElement>, currentIndex: number) {
    const lastIndex = clients.length - 1;
    let nextIndex: number | undefined;
    switch (event.key) {
      case "ArrowLeft": nextIndex = (currentIndex + lastIndex) % clients.length; break;
      case "ArrowRight": nextIndex = (currentIndex + 1) % clients.length; break;
      case "Home": nextIndex = 0; break;
      case "End": nextIndex = lastIndex; break;
      default: return;
    }
    event.preventDefault();
    selectClient(nextIndex);
  }

  return <section className="mcp-setup" aria-labelledby="mcp-setup-title">
    <div className="settings-title"><h2 id="mcp-setup-title">MCP setup</h2><p>Let a local MCP client start and review Argus tests.</p></div>
    <div className="card mcp-card">
      <section className="mcp-step" aria-labelledby="mcp-step-server">
        <span className="mcp-step-number">1</span>
        <div className="mcp-step-content">
          <h3 id="mcp-step-server">Confirm Argus is running</h3>
          <p>The local MCP adapter connects to the Argus server already running on this device.</p>
          <div className="mcp-statuses">
            <span className={`mcp-status ${settings ? "ready" : error ? "error" : "checking"}`}>
              {settings ? <Check size={13} /> : error ? <XCircle size={13} /> : <CircleDashed size={13} className="spin" />}{settings ? "Server running" : error ? "Server unavailable" : "Checking server"}
            </span>
            {settings && <><span className="mcp-status"><Server size={13} />{settings.model}</span><span className={`mcp-status ${settings.gemini_configured ? "ready" : "checking"}`}><Settings2 size={13} />Gemini {settings.gemini_configured ? "configured" : "not configured"}</span></>}
          </div>
          {error && <p className="mcp-server-error">Start Argus and refresh this page to verify the connection.</p>}
        </div>
      </section>

      <section className="mcp-step" aria-labelledby="mcp-step-client">
        <span className="mcp-step-number">2</span>
        <div className="mcp-step-content">
          <h3 id="mcp-step-client">Choose your MCP client</h3>
          <div className="mcp-tabs" role="tablist" aria-label="MCP client">
            {clients.map(({ id, label }, index) => <button key={id} ref={(tab) => { tabRefs.current[index] = tab; }} type="button" role="tab" id={`${tabsId}-${id}`} aria-selected={client === id} aria-controls={`${tabsId}-${id}-panel`} tabIndex={client === id ? 0 : -1} onClick={() => setClient(id)} onKeyDown={(event) => handleTabKeyDown(event, index)}>{label}</button>)}
          </div>
        </div>
      </section>

      <section className="mcp-step" aria-labelledby="mcp-step-config">
        <span className="mcp-step-number">3</span>
        <div className="mcp-step-content">
          <div className="mcp-code-heading"><div><h3 id="mcp-step-config">Add the local stdio configuration</h3><p>Add this to <code>{activeClient.location}</code>, then restart {activeClient.label}.</p></div><button type="button" className="mcp-copy" onClick={() => void copyConfig()} aria-label="Copy MCP configuration">{copyStatus === "copied" ? <Check size={15} /> : <Copy size={15} />}{copyStatus === "copied" ? "Copied" : "Copy"}</button></div>
          {clients.map(({ id }) => <div key={id} id={`${tabsId}-${id}-panel`} role="tabpanel" aria-labelledby={`${tabsId}-${id}`} hidden={client !== id} className="mcp-code"><pre><code>{getMcpConfig(id)}</code></pre></div>)}
          <p className={`mcp-copy-feedback ${copyStatus === "error" ? "error" : ""}`} aria-live="polite">{copyStatus === "error" ? "Could not copy configuration. Select and copy it manually." : "Uses the default local server at http://127.0.0.1:8000. No keys are needed."}</p>
        </div>
      </section>

      <section className="mcp-step mcp-step-tools" aria-labelledby="mcp-step-tools">
        <span className="mcp-step-number">4</span>
        <div className="mcp-step-content"><h3 id="mcp-step-tools">Available tools</h3><ul className="mcp-tools">{["start_test", "get_test_run", "list_test_runs", "cancel_test", "get_test_evidence"].map((tool) => <li key={tool}><code>{tool}</code></li>)}</ul></div>
      </section>
    </div>
  </section>;
}
