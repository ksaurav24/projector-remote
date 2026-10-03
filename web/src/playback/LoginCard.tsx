import { useState } from "react";
import type { FormEvent } from "react";
import { useApi } from "../catalog/api";
import { Img } from "../components/Img";
import { tick } from "../format";

/** NetMirror's OTP login, run by the projector for the phone: shows the
 * picture NetMirror publishes the code in, and trades the code for a
 * fresh token. onLoggedIn retries whatever needed it. */
export function LoginCard({ onLoggedIn, onDismiss }: { onLoggedIn: () => void; onDismiss: () => void }) {
  const hint = useApi<{ image: string }>("/api/catalog/login");
  const [otp, setOtp] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    tick();
    setBusy(true);
    setError(null);
    try {
      const res = await fetch("/api/catalog/login", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ otp: otp.trim() }),
      });
      if (res.ok) {
        onLoggedIn();
        return;
      }
      const body = await res.json().catch(() => null);
      setError(body?.error ?? `Login failed (${res.status})`);
    } catch {
      setError("Can't reach the projector");
    } finally {
      setBusy(false);
    }
  };

  return (
    <form onSubmit={submit} className="mt-7 rounded-3xl bg-white/[0.07] p-5 ring-1 ring-white/10">
      <p className="text-[16px] font-semibold">Log in to NetMirror</p>
      <p className="mt-1 text-[14px] leading-relaxed text-bulb/75">Enter the code shown below.</p>
      {hint.status === "ok" && hint.data.image && (
        <div className="mt-4 aspect-[1040/280] overflow-hidden rounded-2xl bg-white/[0.06]">
          <Img src={hint.data.image} loading="eager" className="size-full object-cover" />
        </div>
      )}
      <input
        value={otp}
        onChange={(e) => setOtp(e.target.value.replace(/\D/g, ""))}
        inputMode="numeric"
        autoComplete="one-time-code"
        placeholder="Code"
        aria-label="Login code"
        className="mt-4 w-full rounded-2xl bg-white/[0.08] px-4 py-3 text-center text-[20px] font-semibold tracking-[0.3em] outline-none ring-1 ring-white/10 placeholder:tracking-normal placeholder:text-haze focus:ring-spot/70"
      />
      {error && <p className="mt-2 text-[13px] text-red-300">{error}</p>}
      <div className="mt-4 flex gap-2.5">
        <button
          type="submit"
          disabled={busy || !otp}
          className="flex-1 rounded-2xl bg-spot py-3 text-[15px] font-bold text-spot-ink disabled:opacity-50"
        >
          {busy ? "Logging in…" : "Log in and play"}
        </button>
        <button type="button" onClick={onDismiss} className="flex-1 rounded-2xl bg-white/10 py-3 text-[15px] font-semibold ring-1 ring-white/10">
          Dismiss
        </button>
      </div>
    </form>
  );
}
