import { ChevronLeft, ChevronRight, Image, Maximize2, X } from "lucide-react";
import { useState } from "react";
import { EventType } from "../../constants";
import type { RunEvent } from "../../types";

export function EvidenceGallery({ events }: { events: RunEvent[] }) {
  const screenshots = events.filter((event) => event.type === EventType.BROWSER_SCREENSHOT && typeof event.data.path === "string");
  const [selected, setSelected] = useState(Math.max(0, screenshots.length - 1));
  const [lightbox, setLightbox] = useState(false);
  if (!screenshots.length) return <div className="stage-empty"><span><Image size={20} /></span><p>Screenshots will appear here</p></div>;
  const current = screenshots[Math.min(selected, screenshots.length - 1)];
  return <>
    <div className="evidence-gallery">
      <button className="evidence-preview" type="button" onClick={() => setLightbox(true)}><img src={String(current.data.path)} alt={String(current.data.label ?? "Browser evidence")} /><span><Maximize2 size={16} /></span><small>{Math.min(selected + 1, screenshots.length)} / {screenshots.length}</small></button>
      {screenshots.length > 1 && <div className="evidence-thumbs">{screenshots.map((event, index) => <button type="button" className={index === selected ? "active" : ""} onClick={() => setSelected(index)} key={event.id}><img src={String(event.data.path)} alt="" /></button>)}</div>}
    </div>
    {lightbox && <div className="lightbox" role="dialog" aria-modal="true" onClick={() => setLightbox(false)}>
      <button className="lightbox-close" type="button"><X size={18} /></button>
      {selected > 0 && <button className="lightbox-prev" type="button" onClick={(event) => { event.stopPropagation(); setSelected(selected - 1); }}><ChevronLeft size={22} /></button>}
      <img src={String(current.data.path)} alt={String(current.data.label ?? "Browser evidence")} onClick={(event) => event.stopPropagation()} />
      {selected < screenshots.length - 1 && <button className="lightbox-next" type="button" onClick={(event) => { event.stopPropagation(); setSelected(selected + 1); }}><ChevronRight size={22} /></button>}
    </div>}
  </>;
}
