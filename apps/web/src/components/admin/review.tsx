"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState, type ReactNode } from "react";

/**
 * The pieces every review queue shares.
 *
 * What is shared is deliberately narrow: the polling behaviour, the "how long
 * has this waited" rendering, and the shape of a decision. What is *not* shared
 * is the card itself — a reel is judged by watching it, a bank account by
 * comparing two names, a chat message by reading what the scanner found. A
 * generic review component would render all three as a list of fields, which is
 * the same mistake as showing a kitchen a list of addresses.
 */

/** How often a queue refetches while a moderator has it open.
 *
 *  Thirty seconds. Slow enough that a room full of moderators is not a load
 *  problem, fast enough that work arriving mid-shift is seen without a manual
 *  refresh — and the manual refresh is what people stop doing after an hour.
 */
export const POLL_MS = 30_000;

export function useQueueData<T>(key: string, fn: () => Promise<{ items: T[] }>) {
  return useQuery({
    queryKey: ["admin", key],
    queryFn: fn,
    refetchInterval: POLL_MS,
    // Keep showing the previous list while refetching. A queue that blanks
    // every thirty seconds is one a reviewer cannot read a caption from.
    placeholderData: (previous) => previous,
  });
}

/** Formats a wait as something a person reads at a glance. */
export function waited(seconds: number): string {
  if (seconds < 60) return `${seconds}s`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m`;
  if (seconds < 86_400) return `${Math.floor(seconds / 3600)}h`;
  return `${Math.floor(seconds / 86_400)}d`;
}

export function Waiting({ seconds, breached }: { seconds: number; breached: boolean }) {
  return (
    <span className={breached ? "pill bad" : "pill"} title={`${seconds}s`}>
      {waited(seconds)}
      {breached ? " · late" : ""}
    </span>
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
 * **The buttons disable while the mutation is in flight and the row leaves on
 * success.** A double-tap on "reject" against a queue that has not yet refetched
 * decides the *next* item too, and under queue pressure that is not a rare
 * event — it is what a fast reviewer does.
 */
export function Decide({
  onDecide,
  approveLabel = "Approve",
  rejectLabel = "Reject",
  reasonRequired = true,
  extra,
}: {
  onDecide: (approve: boolean, reason: string) => Promise<unknown>;
  approveLabel?: string;
  rejectLabel?: string;
  /** False where a rejection carries no free text — releasing a held chat
   *  message, for instance, where the decision is binary and the scanner
   *  finding is already the reason. */
  reasonRequired?: boolean;
  extra?: ReactNode;
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
    },
    onError: (e: unknown) => setError(e instanceof Error ? e.message : "Failed"),
  });

  const rejectBlocked = reasonRequired && reason.trim().length === 0;

  return (
    <div className="actions">
      {extra}
      <input
        value={reason}
        onChange={(e) => setReason(e.target.value)}
        placeholder={reasonRequired ? "Reason (required to reject)" : "Note (optional)"}
        aria-label="Reason"
      />
      <div className="row">
        <button
          onClick={() => mutation.mutate({ approve: true })}
          disabled={mutation.isPending}
        >
          {approveLabel}
        </button>
        <button
          className="ghost"
          onClick={() => mutation.mutate({ approve: false })}
          disabled={mutation.isPending || rejectBlocked}
          title={rejectBlocked ? "A rejection needs a reason — it can be appealed" : undefined}
        >
          {rejectLabel}
        </button>
      </div>
      {error && <p className="bad">{error}</p>}
    </div>
  );
}

/**
 * Empty state.
 *
 * Says the queue is empty rather than rendering nothing. A blank panel is
 * indistinguishable from a failed fetch, and the difference between "there is no
 * work" and "the tool is broken" is the whole reason someone is looking.
 */
export function QueueState({
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
  if (isPending) return <p className="muted">Loading…</p>;
  if (error) {
    return (
      <p className="bad">
        Could not load. This account may not have staff access — every queue here
        checks `platform_admins` on the server, on every call.
      </p>
    );
  }
  if (count === 0) return <p className="muted">{empty}</p>;
  return null;
}
