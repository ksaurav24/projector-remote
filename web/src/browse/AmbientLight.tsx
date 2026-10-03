import { AnimatePresence, motion } from "framer-motion";

/** The spill of light from the "screen": a heavily blurred copy of
 * the current artwork behind the page, so each title tints the room it
 * sits in. A CSS filter on the image itself, not canvas color
 * sampling, since imgcdn.kim sends no CORS headers. */
export function AmbientLight({ src, className = "" }: { src?: string; className?: string }) {
  return (
    <div aria-hidden className={`pointer-events-none overflow-hidden ${className}`}>
      <AnimatePresence>
        {src && (
          <motion.img
            key={src}
            src={src}
            alt=""
            initial={{ opacity: 0 }}
            animate={{ opacity: 0.6 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 1.1, ease: "easeInOut" }}
            className="absolute inset-0 size-full object-cover scale-125 blur-[72px] saturate-[1.6]"
          />
        )}
      </AnimatePresence>
      <div className="absolute inset-0 bg-gradient-to-b from-black/10 via-black/55 to-black" />
    </div>
  );
}
