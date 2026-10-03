import { motion } from "framer-motion";
import { ArrowClockwise } from "@phosphor-icons/react";

export function ErrorState({ message, onRetry }: { message: string; onRetry: () => void }) {
  return (
    <div className="flex flex-col items-center px-8 py-20 text-center">
      <p className="text-[15px] leading-relaxed text-bulb/80">{message}</p>
      <motion.button
        whileTap={{ scale: 0.96 }}
        onClick={onRetry}
        className="mt-5 flex items-center gap-2 rounded-full bg-white/10 px-5 py-2.5 text-[14px] font-semibold text-bulb ring-1 ring-white/10"
      >
        <ArrowClockwise weight="bold" className="size-4" />
        Try again
      </motion.button>
    </div>
  );
}

export function HomeSkeleton() {
  return (
    <div aria-busy aria-label="Loading">
      <div className="px-4">
        <div className="aspect-[4/3] rounded-[28px] shimmer" />
        <div className="mt-4 h-3.5 w-4/5 rounded-full shimmer" />
        <div className="mt-2 h-3.5 w-3/5 rounded-full shimmer" />
      </div>
      {[0, 1, 2].map((r) => (
        <div key={r} className="mt-9">
          <div className="mx-4 mb-3 h-4 w-40 rounded-full shimmer" />
          <div className="flex gap-3 overflow-hidden px-4">
            {[0, 1, 2].map((c) => (
              <div key={c} className="aspect-video w-[44vw] max-w-[220px] shrink-0 rounded-2xl shimmer" />
            ))}
          </div>
        </div>
      ))}
    </div>
  );
}
