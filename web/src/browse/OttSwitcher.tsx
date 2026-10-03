import { motion } from "framer-motion";
import { OTTS } from "../catalog/types";
import type { Ott } from "../catalog/types";

/** Platform picker. Brand color appears only on the active pill;
 * everywhere else the app keeps its own orange. layoutGroup keeps two
 * switchers on screen at once (browse and search) from sharing a pill. */
export function OttSwitcher({
  value,
  onChange,
  layoutGroup,
}: {
  value: Ott;
  onChange: (o: Ott) => void;
  layoutGroup: string;
}) {
  return (
    <div role="tablist" className="flex rounded-full bg-white/[0.07] p-1 ring-1 ring-white/10">
      {OTTS.map((o) => {
        const active = o.id === value;
        return (
          <button
            key={o.id}
            role="tab"
            aria-selected={active}
            onClick={() => onChange(o.id)}
            className={
              "relative px-3.5 py-1.5 text-[13px] font-semibold transition-colors " +
              (active ? "text-white" : "text-haze active:text-bulb")
            }
          >
            {active && (
              <motion.span
                layoutId={`ott-pill-${layoutGroup}`}
                className="absolute inset-0 rounded-full"
                style={{ backgroundColor: o.tint }}
                transition={{ type: "spring", stiffness: 400, damping: 32 }}
              />
            )}
            <span className="relative">{o.label}</span>
          </button>
        );
      })}
    </div>
  );
}
