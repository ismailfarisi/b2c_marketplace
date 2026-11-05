"use client";

import { ApiError } from "@marketplace/api-client";

type Locale = "en" | "ar";

const COPY = {
  en: {
    offline: "You're offline. Check your connection and try again.",
    unreachable: "Can't reach the server right now. Try again in a moment.",
    server: "Something went wrong on our side. Try again shortly.",
    notFound: "We couldn't find that. It may have been removed.",
    forbidden: "You don't have access to this.",
    generic: "This didn't load. Try again.",
    retry: "Try again",
    ref: "Reference",
  },
  ar: {
    offline: "أنت غير متصل بالإنترنت. تحقق من الاتصال وحاول مرة أخرى.",
    unreachable: "تعذر الوصول إلى الخادم حالياً. حاول مرة أخرى بعد قليل.",
    server: "حدث خطأ من جهتنا. حاول مرة أخرى بعد قليل.",
    notFound: "لم نعثر على هذا. ربما تمت إزالته.",
    forbidden: "ليس لديك صلاحية للوصول إلى هذا.",
    generic: "تعذر التحميل. حاول مرة أخرى.",
    retry: "حاول مرة أخرى",
    ref: "المرجع",
  },
};

type Kind =
  | "offline"
  | "unreachable"
  | "server"
  | "notFound"
  | "forbidden"
  | "generic";

/** A query React Query paused because the browser reports no connection. */
export class OfflineError extends Error {
  constructor() {
    super("offline");
    this.name = "OfflineError";
  }
}

export function loadErrorKind(error: unknown): Kind {
  if (typeof navigator !== "undefined" && navigator.onLine === false) return "offline";
  if (error instanceof OfflineError) return "offline";
  // fetch rejects with a TypeError when the request never got a response (or
  // timed out). The browser says it is online, so it is the server (or the
  // path to it) that is down, not the reader's connection.
  if (error instanceof TypeError) return "unreachable";
  if (error instanceof ApiError) {
    if (error.status === 404) return "notFound";
    if (error.status === 403) return "forbidden";
    if (error.status >= 500) return "server";
  }
  return "generic";
}

/**
 * The one way a page says "this didn't load": the reason in the reader's
 * language, the request id for a server failure (what support asks for), and
 * a Try again that re-runs the query. A 404 or 403 will not change on retry, so
 * it gets no button.
 */
export function LoadError({
  error,
  onRetry,
  locale,
}: {
  error: unknown;
  onRetry?: () => void;
  locale: Locale;
}) {
  const t = COPY[locale];
  const kind = loadErrorKind(error);
  const requestId =
    kind === "server" && error instanceof ApiError
      ? (error.requestId ?? error.problem?.request_id ?? null)
      : null;
  const retryable = kind !== "notFound" && kind !== "forbidden";
  return (
    <div className="ms-empty" role="alert">
      <p>{t[kind]}</p>
      {requestId ? (
        <p className="ms-note">
          {t.ref}: <span dir="ltr">{requestId}</span>
        </p>
      ) : null}
      {onRetry && retryable ? (
        <button type="button" className="ms-btn" onClick={onRetry}>
          {t.retry}
        </button>
      ) : null}
    </div>
  );
}
