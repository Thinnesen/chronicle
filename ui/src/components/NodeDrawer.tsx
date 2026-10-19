import { X } from "lucide-react";
import { useEffect, useRef, useState, type PointerEvent } from "react";

import { nodeLabel, type NodeStatus, type Workflow } from "../lib/workflow";
import { Status } from "./Lamp";
import { NodePanel } from "./NodePanel";
import "./NodeDrawer.css";

const key = "chronicle-drawer-width";
const min = 320;

function readWidth(): number {
  try {
    return Number(localStorage.getItem(key)) || 400;
  } catch {
    return 400;
  }
}

interface DrawerProps {
  wf: Workflow;
  node: NodeStatus;
  nodes: Record<string, NodeStatus>;
  namespace: string;
  workflow: string;
  onSelect: (id: string) => void;
  onClose: () => void;
}

// NodeDrawer shows the selected node next to the graph. The user can drag its left edge to change the width.
export function NodeDrawer({ onClose, ...props }: DrawerProps) {
  const [width, setWidth] = useState(readWidth);
  // Escape closes the panel, but not while a dialog or the command palette is open: Escape closes those first.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape" && !e.defaultPrevented && !document.querySelector('[role="dialog"]')) onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);
  const drag = useRef<{ x: number; width: number } | null>(null);
  const onMove = (e: PointerEvent) => {
    if (!drag.current) return;
    const max = Math.max(min, window.innerWidth * 0.6);
    setWidth(Math.round(Math.min(max, Math.max(min, drag.current.width + drag.current.x - e.clientX))));
  };
  const onUp = () => {
    drag.current = null;
    try {
      localStorage.setItem(key, String(width));
    } catch {
      // The width then lasts only for this page.
    }
  };
  return (
    <aside className="drawer card" aria-label={`Step ${nodeLabel(props.node)}`} style={{ "--drawer-width": `${width}px` } as React.CSSProperties}>
      <div
        className="drawer-handle"
        role="separator"
        aria-orientation="vertical"
        aria-label="Change the width of the panel"
        aria-valuenow={width}
        tabIndex={0}
        onPointerDown={(e) => {
          drag.current = { x: e.clientX, width };
          e.currentTarget.setPointerCapture(e.pointerId);
        }}
        onPointerMove={onMove}
        onPointerUp={onUp}
        onKeyDown={(e) => {
          if (e.key === "ArrowLeft") setWidth((w) => Math.min(window.innerWidth * 0.6, w + 24));
          if (e.key === "ArrowRight") setWidth((w) => Math.max(min, w - 24));
        }}
      />
      <header className="drawer-head">
        <div className="drawer-title">
          <h2>{nodeLabel(props.node)}</h2>
          <Status phase={props.node.phase || "Pending"} />
        </div>
        <button type="button" className="icon-button" aria-label="Close the panel" onClick={onClose}>
          <X />
        </button>
      </header>
      <NodePanel {...props} />
    </aside>
  );
}
