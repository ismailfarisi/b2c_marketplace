"use client";

import { ApiError } from "@marketplace/api-client";
import Link from "next/link";

import { useWhoami } from "@/components/admin/session";

/**
 * How the console says "this did not load".
 *
 * The console chrome is English by design (see the admin layout), so this copy
 * is English only. What matters is telling apart the three failures an operator
 * acts on differently: a 403 (ask for the permission, named), a 404 (the record
 * is not there), and everything else, which is an outage and gets a request id
 * and a Retry button.
 */

type Kind = "forbidden" | "notFound" | "offline" | "server";

export function consoleErrorKind(error: unknown): Kind {
  if (error instanceof ApiError) {
    if (error.status === 403) return "forbidden";
    if (error.status === 404) return "notFound";
    return "server";
  }
  return "offline";
}

/** The server's 403 detail already names the missing permission; fall back to ours. */
function forbiddenText(error: unknown, perm?: string | undefined): string {
  const detail = error instanceof ApiError ? error.problem?.detail : undefined;
  if (detail) return detail;
  return perm
    ? `This account is missing ${perm}.`
    : "This account does not have the permission this needs.";
}

export function ConsoleError({
  error,
  what,
  perm,
  onRetry,
  back,
}: {
  error: unknown;
  /** "this queue", "this subscription", … */
  what: string;
  /** The permission the endpoint checks, named when the server does not. */
  perm?: string | undefined;
  onRetry?: (() => void) | undefined;
  back?: { href: string; label: string };
}) {
  const kind = consoleErrorKind(error);
  const requestId =
    error instanceof ApiError
      ? (error.requestId ?? error.problem?.request_id ?? null)
      : null;

  let headline: string;
  let body: string;
  switch (kind) {
    case "forbidden":
      headline = `No access to ${what}.`;
      body = `${forbiddenText(error, perm)} Someone holding staff.admin can grant it.`;
      break;
    case "notFound":
      headline = "No such record.";
      body = `There is nothing at this id. It may have been mistyped, or ${what} was removed.`;
      break;
    case "offline":
      headline = `Could not load ${what}.`;
      body =
        "The request did not reach the API. Check the connection, then try again.";
      break;
    default:
      headline = `Could not load ${what}.`;
      body =
        "Service error — the API failed to answer. Try again; if it keeps failing, quote the request id.";
  }
  const retryable = kind === "offline" || kind === "server";

  return (
    <div className="state error" role="alert">
      <div className="headline">{headline}</div>
      <p>{body}</p>
      {requestId && kind !== "notFound" ? (
        <p className="muted mono">request {requestId}</p>
      ) : null}
      {(retryable && onRetry) || back ? (
        <div className="action-buttons">
          {retryable && onRetry ? (
            <button type="button" className="btn primary" onClick={onRetry}>
              Retry
            </button>
          ) : null}
          {back ? (
            <Link className="btn" href={back.href}>
              {back.label}
            </Link>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}

/**
 * A background refresh failed while the last good data is still on screen.
 *
 * Small and non-blocking on purpose: replacing the table with the error would
 * unmount an expanded row and the reply or reason typed into it.
 */
export function RefreshFailed({
  error,
  onRetry,
}: {
  error: unknown;
  onRetry?: (() => void) | undefined;
}) {
  const kind = consoleErrorKind(error);
  const why =
    kind === "offline"
      ? "the API did not answer"
      : kind === "forbidden"
        ? "this account no longer has access"
        : "the API returned an error";
  return (
    <div className="refresh-failed" role="status">
      <span>
        Couldn&rsquo;t refresh ({why}). Showing what loaded last.
      </span>
      {onRetry ? (
        <button type="button" className="btn link" onClick={onRetry}>
          Retry
        </button>
      ) : null}
    </div>
  );
}

/** A detail page that did not load: the error, and always a way back to its list. */
export function DetailError(props: {
  error: unknown;
  what: string;
  perm?: string | undefined;
  onRetry?: (() => void) | undefined;
  back: { href: string; label: string };
}) {
  return (
    <div className="panel">
      <ConsoleError {...props} />
    </div>
  );
}

/**
 * Whether the signed-in staff member holds a permission. Undefined while whoami
 * is loading, so callers can avoid flashing a disabled state.
 */
export function useCan(): (perm: string) => boolean | undefined {
  const { data } = useWhoami();
  return (perm) => (data ? data.permissions.includes(perm) : undefined);
}
