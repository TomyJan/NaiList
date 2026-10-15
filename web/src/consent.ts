import { useEffect, useState } from "react";

export const POLICY_VERSION = "2026-10-15";
export const AGREEMENT_KEY = "nailist:agreement";
export const COOKIE_NOTICE_KEY = "nailist:cookie-notice";

export function useAcknowledgement(key: string) {
  const read = () => {
    try {
      return localStorage.getItem(key) === POLICY_VERSION;
    } catch {
      return false;
    }
  };
  const [accepted, setAccepted] = useState(read);
  useEffect(() => {
    const sync = (event: StorageEvent) => {
      if (event.key === key || event.key === null) setAccepted(read());
    };
    window.addEventListener("storage", sync);
    return () => window.removeEventListener("storage", sync);
  }, [key]);
  const update = (value: boolean) => {
    setAccepted(value);
    try {
      localStorage.setItem(key, value ? POLICY_VERSION : "");
    } catch {
      // Confirmation still works for this visit when browser storage is blocked.
    }
  };
  return [accepted, update] as const;
}
