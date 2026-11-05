"use client";

import { ApiError } from "@marketplace/api-client";
import Link from "next/link";

import { networkKind, problemFields, problemMessage } from "@/lib/problem";

import { SUPPORT_COPY } from "./copy";

/** Why a send failed, in the reader's language, with the draft kept. */
export function SendError({
  error,
  paused,
  locale,
  next,
  fields = [],
}: {
  error: unknown;
  paused?: boolean;
  locale: "en" | "ar";
  next: string;
  /** Fields rendered by the form itself; their errors are shown there. */
  fields?: string[];
}) {
  const t = SUPPORT_COPY[locale];
  const network = paused ? "offline" : networkKind(error);
  if (network) return <p className="ms-error" role="alert">{t[network]}</p>;
  if (!error) return null;
  if (error instanceof ApiError && error.status === 401) {
    return (
      <p className="ms-error" role="alert">
        {t.signInAgain}{" "}
        <Link href={`/${locale}/signin?next=${encodeURIComponent(next)}`}>{t.signIn}</Link>
      </p>
    );
  }
  if (!(error instanceof ApiError) || error.status >= 500) {
    return <p className="ms-error" role="alert">{t.sendFailed}</p>;
  }
  const byField = problemFields(error, locale);
  if (Object.keys(byField).length && Object.keys(byField).every((f) => fields.includes(f))) {
    return null;
  }
  return <p className="ms-error" role="alert">{problemMessage(error, locale)}</p>;
}

/** The server's complaint about one field, under that field. */
export function FieldError({
  error,
  field,
  local,
  locale,
}: {
  error: unknown;
  field: string;
  local?: string | null;
  locale: "en" | "ar";
}) {
  const msg = local || problemFields(error, locale)[field];
  return msg ? <small className="ms-error">{msg}</small> : null;
}
