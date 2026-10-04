import { useEffect, useRef } from "react";

/** Keep keyboard navigation in the topmost panel and restore its opener. */
export function useDialogFocus(onClose: () => void, active = true) {
  const ref = useRef<HTMLDivElement>(null);
  const close = useRef(onClose);
  useEffect(() => { close.current = onClose; }, [onClose]);
  useEffect(() => {
    if (!active) return;
    const panel = ref.current;
    if (!panel) return;
    const opener = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    const selector = 'button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), a[href], [tabindex="0"]';
    const focusable = () => [...Array.from(panel.querySelectorAll<HTMLElement>(selector)), ...Array.from(document.querySelectorAll<HTMLElement>(`[data-playback-mini] ${selector.split(", ").join(", [data-playback-mini] ")}`))].filter(el => el.getClientRects().length > 0 && !el.closest('[inert]'));
    const frame = requestAnimationFrame(() => {
      if (!panel.contains(document.activeElement)) (focusable()[0] ?? panel).focus();
    });
    function keydown(event: KeyboardEvent) {
      const dialogs = document.querySelectorAll('[role="dialog"]');
      if (dialogs[dialogs.length - 1] !== panel) return;
      if (event.key === "Escape") { event.preventDefault(); event.stopPropagation(); close.current(); }
      if (event.key !== "Tab") return;
      const items = focusable();
      if (!items.length) { event.preventDefault(); panel!.focus(); return; }
      const first = items[0], last = items[items.length - 1];
      if (event.shiftKey && (document.activeElement === first || document.activeElement === panel)) { event.preventDefault(); last.focus(); }
      else if (!event.shiftKey && (document.activeElement === last || !items.includes(document.activeElement as HTMLElement))) { event.preventDefault(); first.focus(); }
    }
    document.addEventListener("keydown", keydown);
    return () => { cancelAnimationFrame(frame); document.removeEventListener("keydown", keydown); if (opener?.isConnected) opener.focus(); };
  }, [active]);
  return ref;
}
