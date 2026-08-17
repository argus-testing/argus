import type { RunStatusValue } from "./constants";

export interface RunEvent {
  id: number;
  run_id: string;
  type: string;
  created_at: string;
  data: Record<string, unknown>;
}

export interface Finding {
  severity: string;
  title: string;
  detail: string;
}

export interface RunReport {
  verdict: "passed" | "failed" | "inconclusive";
  summary: string;
  plan?: string;
  findings: Finding[];
  recommendations: string[];
}

export interface Run {
  id: string;
  url: string;
  instructions: string;
  status: RunStatusValue;
  created_at: string;
  updated_at: string;
  error: string | null;
  report: RunReport | null;
  events?: RunEvent[];
}

export interface Settings {
  gemini_configured: boolean;
  model: string;
}
