import { Navigate, Route, Routes } from "react-router-dom";
import { AppShell } from "./components/layout/AppShell";
import { SessionView } from "./components/session/SessionView";
import { DashboardPage } from "./pages/DashboardPage";
import { HistoryPage } from "./pages/HistoryPage";
import { SettingsPage } from "./pages/SettingsPage";

export default function App() {
  return <Routes>
    <Route element={<AppShell />}>
      <Route index element={<DashboardPage />} />
      <Route path="history" element={<HistoryPage />} />
      <Route path="settings" element={<SettingsPage />} />
      <Route path="runs/:id" element={<SessionView />} />
      <Route path="runs/:id/report" element={<SessionView />} />
    </Route>
    <Route path="*" element={<Navigate to="/" replace />} />
  </Routes>;
}
