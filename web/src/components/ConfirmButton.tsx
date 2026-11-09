import { ReactNode, useEffect, useState } from "react";
import { useI18n } from "../i18n";

// ConfirmButton fragt im Knopf selbst nach (erster Klick „Sicher?“, zweiter führt aus) – statt
// window.confirm, das Browser nach „Weitere Dialoge verhindern“ stumm mit „Abbrechen“ beantworten.
export function ConfirmButton({ onConfirm, children, confirmLabel, className = "btn ghost sm danger", disabled, title, style }: {
  onConfirm: () => void; children: ReactNode; confirmLabel?: string; className?: string; disabled?: boolean; title?: string; style?: React.CSSProperties;
}) {
  const { t } = useI18n();
  const [armed, setArmed] = useState(false);
  useEffect(() => {
    if (!armed) return;
    const h = setTimeout(() => setArmed(false), 4000);
    return () => clearTimeout(h);
  }, [armed]);
  return (
    <button className={className + (armed ? " armed" : "")} style={style} disabled={disabled} title={title}
      onClick={(e) => { e.stopPropagation(); if (armed) { setArmed(false); onConfirm(); } else setArmed(true); }}>
      {armed ? (confirmLabel ?? t("Sicher? Nochmal klicken")) : children}
    </button>
  );
}
