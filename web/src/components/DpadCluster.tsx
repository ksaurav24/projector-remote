import { motion } from "framer-motion";
import type { ReactNode } from "react";
import {
  ArrowLeft,
  CaretUp,
  CaretDown,
  CaretLeft,
  CaretRight,
  House,
  Power,
  SpeakerX,
  PlayPause,
} from "@phosphor-icons/react";
import { useKeyRepeat } from "../hooks/useKeyRepeat";

function UtilButton({ code, icon, danger = false }: { code: string; icon: ReactNode; danger?: boolean }) {
  const handlers = useKeyRepeat(code);
  return (
    <motion.button
      {...handlers}
      whileTap={{ scale: 0.9 }}
      className={
        "flex-1 rounded-2xl py-3.5 flex items-center justify-center border transition-colors " +
        (danger
          ? "bg-red-50 dark:bg-red-950/40 border-red-200 dark:border-red-900 text-red-600 dark:text-red-400 active:bg-red-100 dark:active:bg-red-900/50"
          : "bg-white dark:bg-neutral-900 border-neutral-200 dark:border-neutral-800 text-neutral-700 dark:text-neutral-300 active:bg-neutral-100 dark:active:bg-neutral-800")
      }
    >
      {icon}
    </motion.button>
  );
}

function DpadButton({
  code,
  children,
  center = false,
}: {
  code: string;
  children: ReactNode;
  center?: boolean;
}) {
  const handlers = useKeyRepeat(code);
  return (
    <motion.button
      {...handlers}
      whileTap={{ scale: 0.9 }}
      className={
        "flex items-center justify-center " +
        (center
          ? "m-2.5 rounded-full bg-orange-500 text-white text-xs font-bold shadow-[0_2px_8px_rgba(249,115,22,0.4)] active:bg-orange-600"
          : "text-neutral-600 dark:text-neutral-300 active:bg-black/5 dark:active:bg-white/5")
      }
    >
      {children}
    </motion.button>
  );
}

export function DpadCluster() {
  return (
    <div className="flex flex-col gap-3">
      <div className="flex gap-2.5">
        <UtilButton code="KEYCODE_BACK" icon={<ArrowLeft weight="bold" className="size-5" />} />
        <UtilButton code="KEYCODE_POWER" icon={<Power weight="bold" className="size-5" />} danger />
        <UtilButton code="KEYCODE_HOME" icon={<House weight="bold" className="size-5" />} />
      </div>

      <div className="mx-auto w-full max-w-[248px] aspect-square rounded-full bg-neutral-100 dark:bg-neutral-800 border border-neutral-200 dark:border-neutral-700 grid grid-cols-3 grid-rows-3 overflow-hidden">
        <div />
        <DpadButton code="KEYCODE_DPAD_UP">
          <CaretUp weight="bold" className="size-6" />
        </DpadButton>
        <div />
        <DpadButton code="KEYCODE_DPAD_LEFT">
          <CaretLeft weight="bold" className="size-6" />
        </DpadButton>
        <DpadButton code="KEYCODE_DPAD_CENTER" center>
          OK
        </DpadButton>
        <DpadButton code="KEYCODE_DPAD_RIGHT">
          <CaretRight weight="bold" className="size-6" />
        </DpadButton>
        <div />
        <DpadButton code="KEYCODE_DPAD_DOWN">
          <CaretDown weight="bold" className="size-6" />
        </DpadButton>
        <div />
      </div>

      <div className="flex gap-2.5">
        <UtilButton code="KEYCODE_VOLUME_MUTE" icon={<SpeakerX weight="bold" className="size-5" />} />
        <UtilButton code="KEYCODE_MEDIA_PLAY_PAUSE" icon={<PlayPause weight="bold" className="size-5" />} />
      </div>
    </div>
  );
}
