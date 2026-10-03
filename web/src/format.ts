/** 83_000 -> "1:23", 3_723_000 -> "1:02:03". */
export function clock(ms: number): string {
  const s = Math.max(0, Math.floor(ms / 1000));
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const sec = String(s % 60).padStart(2, "0");
  return h > 0 ? `${h}:${String(m).padStart(2, "0")}:${sec}` : `${m}:${sec}`;
}

/** Time left, rounded up to whole minutes: "12m left", "1h 5m left". */
export function timeLeft(pos: number, dur: number): string {
  const mins = Math.max(1, Math.ceil((dur - pos) / 60000));
  const h = Math.floor(mins / 60);
  return h > 0 ? `${h}h ${mins % 60}m left` : `${mins}m left`;
}

export function progressRatio(pos: number, dur: number): number {
  return dur > 0 ? Math.min(1, Math.max(0, pos / dur)) : 0;
}

/** A short haptic tick on phones that support it (Android); a no-op
 * elsewhere. */
export function tick() {
  navigator.vibrate?.(8);
}
