"use client";

import { useSyncExternalStore } from "react";

const COPY = {
  en: "You're offline. This page will update when you're back online.",
  ar: "أنت غير متصل بالإنترنت. ستتحدث الصفحة عند عودة الاتصال.",
};

function subscribe(onChange: () => void) {
  window.addEventListener("online", onChange);
  window.addEventListener("offline", onChange);
  return () => {
    window.removeEventListener("online", onChange);
    window.removeEventListener("offline", onChange);
  };
}

/**
 * One app-wide "you're offline" strip. React Query's onlineManager listens to
 * the same events, so queries pause while this shows and refetch on reconnect.
 * The server snapshot is "online": nothing to say before hydration.
 */
export function OfflineBanner({ locale }: { locale: "en" | "ar" }) {
  const online = useSyncExternalStore(
    subscribe,
    () => navigator.onLine,
    () => true,
  );
  if (online) return null;
  return (
    <div
      role="status"
      aria-live="polite"
      style={{
        position: "sticky",
        insetBlockStart: 0,
        zIndex: 50,
        padding: "0.5rem 1rem",
        textAlign: "center",
        fontSize: "0.875rem",
        background: "var(--ms-saffron-soft, #fff1cc)",
        color: "var(--ms-saffron-ink, #7a5200)",
      }}
    >
      {COPY[locale]}
    </div>
  );
}
