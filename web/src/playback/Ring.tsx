import { motion } from "framer-motion";

const COUNTDOWN_FROM = 10;

/** The next-episode countdown: an orange ring draining toward zero,
 * matching the card on the projector. */
export function Ring({ seconds, size = 40 }: { seconds: number; size?: number }) {
  const r = size / 2 - 3;
  const c = 2 * Math.PI * r;
  return (
    <span className="relative inline-grid shrink-0 place-items-center" style={{ width: size, height: size }}>
      <svg width={size} height={size} className="-rotate-90">
        <circle cx={size / 2} cy={size / 2} r={r} fill="none" stroke="rgb(255 255 255 / 0.15)" strokeWidth={3} />
        <motion.circle
          cx={size / 2}
          cy={size / 2}
          r={r}
          fill="none"
          stroke="#f97316"
          strokeWidth={3}
          strokeLinecap="round"
          strokeDasharray={c}
          initial={false}
          animate={{ strokeDashoffset: c * (1 - Math.max(0, seconds - 1) / COUNTDOWN_FROM) }}
          transition={{ duration: 1, ease: "linear" }}
        />
      </svg>
      <span className="absolute text-[13px] font-bold tabular-nums text-bulb">{seconds}</span>
    </span>
  );
}
