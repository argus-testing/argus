export function Spinner({ label = "Loading" }: { label?: string }) {
  return <div className="spinner-wrap"><span className="spinner" /><span className="sr-only">{label}</span></div>;
}
