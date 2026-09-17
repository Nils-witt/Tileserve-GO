import type { MouseEvent, ReactNode } from "react";

interface ModalProps {
  open: boolean;
  title: string;
  onClose: () => void;
  headerExtra?: ReactNode;
  /** "auto" (default) sizes the modal to its content, up to 88vh — used by
   * every modal except the map Preview, which needs a fixed, tall canvas
   * for MapLibre. */
  variant?: "auto" | "full";
  noPadding?: boolean;
  children: ReactNode;
}

export default function Modal({ open, title, onClose, headerExtra, variant = "auto", noPadding, children }: ModalProps) {
  if (!open) return null;

  const onOverlayClick = (e: MouseEvent<HTMLDivElement>) => {
    if (e.target === e.currentTarget) onClose();
  };

  return (
    <div className="modal-overlay" onClick={onOverlayClick}>
      <div className="modal" style={variant === "auto" ? { height: "auto", maxHeight: "88vh" } : undefined}>
        <div className="modal-header">
          <h2>{title}</h2>
          <div className="actions">
            {headerExtra}
            <button type="button" className="secondary" onClick={onClose}>
              Close
            </button>
          </div>
        </div>
        {noPadding ? children : <div style={{ padding: "1rem", overflow: "auto" }}>{children}</div>}
      </div>
    </div>
  );
}
