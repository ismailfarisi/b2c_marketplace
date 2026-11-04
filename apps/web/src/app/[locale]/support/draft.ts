"use client";

import { useCallback, useEffect, useState } from "react";

/**
 * A support draft that survives the page being torn down: an expired session
 * hard-navigates to sign-in, and the person comes back to the same words.
 * sessionStorage, like the tokens, so it never outlives the tab.
 */
export function useDraft(key: string): [string, (v: string) => void, () => void] {
  const storageKey = `support-draft:${key}`;
  const [value, setValue] = useState("");

  useEffect(() => {
    try {
      const saved = window.sessionStorage.getItem(storageKey);
      if (saved) setValue(saved);
    } catch {
      // Storage blocked: the draft just lives in memory.
    }
  }, [storageKey]);

  const set = useCallback(
    (v: string) => {
      setValue(v);
      try {
        if (v) window.sessionStorage.setItem(storageKey, v);
        else window.sessionStorage.removeItem(storageKey);
      } catch {}
    },
    [storageKey],
  );
  const clear = useCallback(() => set(""), [set]);
  return [value, set, clear];
}

/** Activity time in Dubai, so a 01:00 reply isn't dated the day before. */
export function formatWhen(locale: "en" | "ar", iso: string, withTime = false) {
  return new Intl.DateTimeFormat(locale === "ar" ? "ar-AE" : "en-AE", {
    dateStyle: "medium",
    ...(withTime ? { timeStyle: "short" } : {}),
    timeZone: "Asia/Dubai",
  }).format(new Date(iso));
}
