"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Fragment, useState, type ReactNode } from "react";

import { POLL_MS } from "@/components/admin/shell";

/**
 * Table primitives for the queues that are read rather than watched.
 *
 * The split is the same one that governs the API: **a table suits an item you
 * scan, a card suits an item you look at.** A comment is one line of text and a
 * count of reports — sixty of those belong in a table where the eye runs down a
 * column. A reel is a video; a table row of it is a filename.
 *
 * So `reels` and `kyc` keep cards, and everything else here gets a table. That
 * is a decision per screen, not a component that tries to do both.
 */

export function useQueueData<T>(key: string, fn: () => Promise<{ items: T[] }>) {
  return useQuery({
    queryKey: ["admin", key],
    queryFn: fn,
    refetchInterval: POLL_MS,
    // Keep the previous list on screen while refetching. A queue that blanks
    // every thirty seconds is one a reviewer cannot read a caption from.
    placeholderData: (previous) => previous,
  });
}

/** Formats a wait as something read at a glance. */
export function waited(seconds: number): string {
  if (seconds < 60) return `${seconds}s`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m`;
  if (seconds < 86_400) return `${Math.floor(seconds / 3600)}h`;
  return `${Math.floor(seconds / 86_400)}d`;
}

export function since(iso: string): string {
  return waited(Math.max(0, Math.floor((Date.now() - new Date(iso).getTime()) / 1000)));
}

export function money(m: { amount: number; currency: string }): string {
  return `${(m.amount / 100).toLocaleString("en-AE", {
    minimumFractionDigits: 0,
    maximumFractionDigits: 2,
  })} ${m.currency}`;
}

export function Tag({
  children,
  tone,
  code,
  title,
}: {
  children: ReactNode;
  tone?: "danger" | "warn" | "ok" | undefined;
  code?: boolean;
  title?: string;
}) {
  return (
    <span className={`tag ${tone ?? ""} ${code ? "code" : ""}`} title={title}>
      {children}
    </span>
  );
}

/**
 * A panel with a sticky-headed table inside it, and the three states a queue can
 * be in.
 *
 * The empty state says the queue is empty rather than rendering nothing. A blank
 * panel is indistinguishable from a failed fetch, and the difference between
 * "there is no work" and "the tool is broken" is the whole reason someone is
 * looking at it.
 */
export function QueuePanel<T>({
  title,
  hint,
  columns,
  rows,
  isPending,
  error,
  empty,
  rowKey,
  renderRow,
  renderDetail,
}: {
  title: string;
  hint?: string;
  columns: Array<{ label: string; className?: string }>;
  rows: T[];
  isPending: boolean;
  error: unknown;
  empty: string;
  rowKey: (row: T) => string;
  renderRow: (row: T) => ReactNode;
  /** The decision form, expanded inside the table. */
  renderDetail: (row: T, close: () => void) => ReactNode;
}) {
  const [openKey, setOpenKey] = useState<string | null>(null);

  return (
    <section className="panel">
      <div className="panel-head">
        <h2>{title}</h2>
        {hint && <span className="hint">{hint}</span>}
        <div style={{ flex: 1 }} />
        {!isPending && !error && (
          <span className="hint">
            {rows.length} {rows.length === 1 ? "item" : "items"}
          </span>
        )}
      </div>

      {error ? (
        <div className="state error">
          <div className="headline">Could not load this queue.</div>
          This account may not have staff access — every queue checks{" "}
          <code>platform_admins</code> on the server, on every call.
        </div>
      ) : isPending ? (
        <SkeletonRows columns={columns.length} />
      ) : rows.length === 0 ? (
        <div className="state">{empty}</div>
      ) : (
        <div className="table-wrap">
          <table className="data">
            <thead>
              <tr>
                {columns.map((c) => (
                  <th key={c.label} className={c.className}>
                    {c.label}
                  </th>
                ))}
                <th />
              </tr>
            </thead>
            <tbody>
              {rows.map((row) => {
                const key = rowKey(row);
                const open = openKey === key;
                return (
                  <Fragment key={key}>
                    <tr className={open ? "open" : ""}>
                      {renderRow(row)}
                      <td className="num">
                        <button
                          className="btn link"
                          onClick={() => setOpenKey(open ? null : key)}
                          aria-expanded={open}
                        >
                          {open ? "Close" : "Review"}
                        </button>
                      </td>
                    </tr>
                    {open && (
                      <tr className="detail">
                        <td colSpan={columns.length + 1}>
                          {renderDetail(row, () => setOpenKey(null))}
                        </td>
                      </tr>
                    )}
                  </Fragment>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}

/**
 * Skeleton rows rather than a spinner.
 *
 * A spinner says "wait". Skeleton rows say "this is a table with about this many
 * rows in it", which is what stops a reviewer navigating away and back.
 */
function SkeletonRows({ columns }: { columns: number }) {
  return (
    <div className="table-wrap">
      <table className="data">
        <tbody>
          {[0, 1, 2].map((i) => (
            <tr key={i}>
              {Array.from({ length: columns }).map((_, j) => (
                <td key={j}>
                  <div className="skeleton" style={{ inlineSize: `${40 + ((i + j) % 4) * 15}%` }} />
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

/**
 * The decision control.
 *
 * Two properties are load-bearing and neither is obvious from the markup.
 *
 * **Rejecting requires a reason and approving does not.** Every decide endpoint
 * in this API enforces the same asymmetry, and it is not bureaucracy: a
 * rejection is appealable, and an appeal reviewer with no stated reason is
 * reviewing a verdict rather than an argument.
 *
 * **The buttons disable while the mutation is in flight, and the row closes on
 * success.** A double-tap on "reject" against a queue that has not yet refetched
 * decides the *next* item too — and under queue pressure that is not a rare
 * event, it is what a fast reviewer does.
 */
export function Decide({
  onDecide,
  onDone,
  approveLabel = "Approve",
  rejectLabel = "Reject",
  reasonRequired = true,
  children,
}: {
  onDecide: (approve: boolean, reason: string) => Promise<unknown>;
  onDone?: (() => void) | undefined;
  approveLabel?: string;
  rejectLabel?: string;
  /** False where the decision is binary and the reason already exists — a held
   *  chat message, where the scanner finding *is* the reason and asking a
   *  reviewer to retype it fifteen times an hour produces "spam" on every row. */
  reasonRequired?: boolean;
  /** Evidence rendered above the controls. */
  children?: ReactNode;
}) {
  const [reason, setReason] = useState("");
  const [error, setError] = useState<string | null>(null);
  const queryClient = useQueryClient();

  const mutation = useMutation({
    mutationFn: ({ approve }: { approve: boolean }) => onDecide(approve, reason),
    onSuccess: () => {
      setReason("");
      setError(null);
      void queryClient.invalidateQueries({ queryKey: ["admin"] });
      onDone?.();
    },
    onError: (e: unknown) => setError(e instanceof Error ? e.message : "Failed"),
  });

  const rejectBlocked = reasonRequired && reason.trim().length === 0;

  return (
    <div className="decide">
      {children}
      <div className="row">
        <input
          className="field"
          value={reason}
          onChange={(e) => setReason(e.target.value)}
          placeholder={reasonRequired ? "Reason (required to reject)" : "Note (optional)"}
          aria-label="Reason"
        />
        <button
          className="btn primary"
          onClick={() => mutation.mutate({ approve: true })}
          disabled={mutation.isPending}
        >
          {approveLabel}
        </button>
        <button
          className="btn danger"
          onClick={() => mutation.mutate({ approve: false })}
          disabled={mutation.isPending || rejectBlocked}
          title={rejectBlocked ? "A rejection needs a reason — it can be appealed" : undefined}
        >
          {rejectLabel}
        </button>
      </div>
      {error && <p className="note danger">{error}</p>}
    </div>
  );
}

/** A definition list for the evidence above a decision. */
export function Evidence({ rows }: { rows: Array<[string, ReactNode]> }) {
  return (
    <dl className="evidence">
      {rows.map(([k, v]) => (
        <Fragment key={k}>
          <dt>{k}</dt>
          <dd>{v}</dd>
        </Fragment>
      ))}
    </dl>
  );
}

/** Card-shaped states, for the two screens that are not tables. */
export function CardState({
  isPending,
  error,
  count,
  empty,
}: {
  isPending: boolean;
  error: unknown;
  count: number;
  empty: string;
}) {
  if (error) {
    return (
      <div className="panel">
        <div className="state error">
          <div className="headline">Could not load this queue.</div>
          This account may not have staff access.
        </div>
      </div>
    );
  }
  if (isPending) {
    return (
      <div className="panel">
        <div className="panel-body">
          <div className="skeleton" style={{ inlineSize: "40%" }} />
        </div>
      </div>
    );
  }
  if (count === 0) {
    return (
      <div className="panel">
        <div className="state">{empty}</div>
      </div>
    );
  }
  return null;
}
