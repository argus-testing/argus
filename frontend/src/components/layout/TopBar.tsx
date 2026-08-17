import { Moon, Plus, Sun } from "lucide-react";
import { useNavigate } from "react-router-dom";
import { useTheme } from "../../contexts/theme";
import { Button } from "../ui/Button";

export function TopBar({ title, showNewTest = false }: { title: string; showNewTest?: boolean }) {
  const navigate = useNavigate();
  const { theme, toggleTheme } = useTheme();
  return <header className="topbar">
    <h1>{title}</h1>
    <div>
      {showNewTest && <Button size="compact" onClick={() => navigate("/")}><Plus size={14} />New Test</Button>}
      <button className="icon-button" type="button" onClick={toggleTheme} title={theme === "dark" ? "Switch to light" : "Switch to dark"}>
        {theme === "dark" ? <Sun size={16} /> : <Moon size={16} />}
      </button>
    </div>
  </header>;
}
