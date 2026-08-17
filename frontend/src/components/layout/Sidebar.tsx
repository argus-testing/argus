import { useState } from "react";
import { NavLink } from "react-router-dom";
import { ChevronsLeft, ChevronsRight, Clock, LayoutDashboard, Settings } from "lucide-react";

const items = [
  { to: "/", label: "Dashboard", icon: LayoutDashboard },
  { to: "/history", label: "History", icon: Clock },
  { to: "/settings", label: "Settings", icon: Settings },
];

function initialCollapsed() {
  try {
    return localStorage.getItem("sidebar-collapsed") === "true";
  } catch {
    return false;
  }
}

export function Sidebar() {
  const [collapsed, setCollapsed] = useState(initialCollapsed);
  const toggle = () => setCollapsed((current) => {
    const next = !current;
    try { localStorage.setItem("sidebar-collapsed", String(next)); } catch { /* no-op */ }
    return next;
  });

  return <aside className={`sidebar ${collapsed ? "collapsed" : ""}`}>
    <button type="button" className="brand" onClick={toggle} title={collapsed ? "Expand sidebar" : "Collapse sidebar"}>
      <span className="brand-mark" aria-hidden="true"><svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="10"/><circle cx="12" cy="12" r="4" className="iris"/></svg></span>
      {!collapsed && <><strong>Argus</strong><ChevronsLeft size={18} className="collapse-icon" /></>}
      {collapsed && <ChevronsRight size={18} className="expand-icon" />}
    </button>
    <nav>
      {items.map(({ to, label, icon: Icon }) => <NavLink key={to} to={to} end={to === "/"} aria-label={label} title={collapsed ? label : undefined}>
        <Icon size={20} /><span>{label}</span>
      </NavLink>)}
    </nav>
    <div className="sidebar-footer" title="Local workspace">
      <span className="local-avatar">A</span>
      {!collapsed && <div><strong>Local workspace</strong><small>Data stays on this device</small></div>}
    </div>
  </aside>;
}
