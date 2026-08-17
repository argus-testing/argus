import { Outlet } from "react-router-dom";
import { Sidebar } from "./Sidebar";

export function AppShell() {
  return <div className="app-shell"><Sidebar /><div className="page-shell"><Outlet /></div></div>;
}
