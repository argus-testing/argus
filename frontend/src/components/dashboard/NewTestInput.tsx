import { useRef, useState, type FormEvent, type KeyboardEvent } from "react";
import { ArrowUp, Globe } from "lucide-react";
import { useNavigate } from "react-router-dom";
import { api } from "../../lib/api";
import type { Run } from "../../types";

const suggestions = [
  "Check that navigation links work across all pages",
  "Verify the responsive layout on mobile",
  "Test the signup form validation",
  "Inspect the main user flow for visual issues",
];

export function NewTestInput() {
  const navigate = useNavigate();
  const textarea = useRef<HTMLTextAreaElement>(null);
  const [instructions, setInstructions] = useState("");
  const [url, setUrl] = useState("");
  const [focused, setFocused] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState("");

  const submit = async (event?: FormEvent) => {
    event?.preventDefault();
    if (!instructions.trim() || !url.trim() || submitting) return;
    setSubmitting(true);
    setError("");
    try {
      const run = await api<Run>("/api/runs", {
        method: "POST",
        body: JSON.stringify({ url: url.trim(), instructions: instructions.trim() }),
      });
      navigate(`/runs/${run.id}`);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Could not start test");
      setSubmitting(false);
    }
  };

  const onKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key === "Enter" && !event.shiftKey) {
      event.preventDefault();
      void submit();
    }
  };

  return <form className={`composer ${focused ? "focused" : ""}`} onSubmit={(event) => void submit(event)}>
    <textarea
      ref={textarea}
      rows={3}
      required
      value={instructions}
      onChange={(event) => setInstructions(event.target.value)}
      onKeyDown={onKeyDown}
      onFocus={() => setFocused(true)}
      onBlur={() => setFocused(false)}
      placeholder={'Describe a test to run… e.g. "Test the checkout flow on mobile"'}
      aria-label="Test description"
    />
    <div className="url-field">
      <Globe size={13} />
      <input type="url" required value={url} onChange={(event) => setUrl(event.target.value)} placeholder="https://example.com" aria-label="Target URL" />
    </div>
    <div className="composer-toolbar">
      <span><Globe size={13} />Target URL</span>
      <span className="key-hint">Enter to run · Shift + Enter for a new line</span>
      <button type="submit" className="send-button" disabled={!instructions.trim() || !url.trim() || submitting} title="Start test">
        <ArrowUp size={15} />
      </button>
    </div>
    {error && <div className="inline-error">{error}</div>}
    {!instructions && <div className="suggestions">{suggestions.map((suggestion) => <button type="button" key={suggestion} onMouseDown={(event) => {
      event.preventDefault();
      setInstructions(suggestion);
      textarea.current?.focus();
    }}>{suggestion}</button>)}</div>}
  </form>;
}
