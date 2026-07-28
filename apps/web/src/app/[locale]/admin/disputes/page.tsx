"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

import { getDisputeQueue, resolveDispute } from "@/lib/admin-api";
import { QueueState, useQueueData } from "@/components/admin/review";

/**
 * Disputes.
 *
 * The only queue on this panel that does not use the shared `Decide` control,
 * and the reason is the shape of the decision rather than the styling. Every
 * other queue is approve-or-reject; a dispute resolves into one of seven
 * outcomes, some of which move money.
 *
 * The closed list is not a UI convenience. "How often do we refund" is a
 * question ops asks weekly and free text cannot answer it — and a refund with no
 * stated reason is indistinguishable from an unauthorised one six months later,
 * which is why the note is required and the audit row is written in the same
 * transaction as the decision.
 */
const RESOLUTIONS = [
  { value: "refunded", label: "Refunded in full", money: true },
  { value: "partially_refunded", label: "Partially refunded", money: true },
  { value: "redelivered", label: "Redelivered", money: false },
  { value: "credited", label: "Credited to wallet", money: true },
  { value: "no_action", label: "No action", money: false },
  { value: "vendor_charged", label: "Charged to the vendor", money: true },
  { value: "driver_penalised", label: "Driver penalised", money: false },
] as const;

function ResolveForm({ disputeId }: { disputeId: string }) {
  const [resolution, setResolution] = useState<string>(RESOLUTIONS[0].value);
  const [amount, setAmount] = useState("");
  const [note, setNote] = useState("");
  const [error, setError] = useState<string | null>(null);
  const queryClient = useQueryClient();

  const needsAmount = RESOLUTIONS.find((r) => r.value === resolution)?.money ?? false;

  const mutation = useMutation({
    mutationFn: () =>
      resolveDispute(disputeId, {
        resolution,
        // Sent in minor units. A form that took 12.50 and posted 12.50 would
        // refund twelve fils, which is the class of bug that only surfaces in
        // a reconciliation weeks later.
        amount_minor: needsAmount && amount ? Math.round(Number(amount) * 100) : undefined,
        note,
      }),
    onSuccess: () => {
      setNote("");
      setAmount("");
      setError(null);
      void queryClient.invalidateQueries({ queryKey: ["admin"] });
    },
    onError: (e: unknown) => setError(e instanceof Error ? e.message : "Failed"),
  });

  return (
    <div className="actions">
      <select value={resolution} onChange={(e) => setResolution(e.target.value)}>
        {RESOLUTIONS.map((r) => (
          <option key={r.value} value={r.value}>
            {r.label}
          </option>
        ))}
      </select>

      {needsAmount && (
        <input
          value={amount}
          onChange={(e) => setAmount(e.target.value)}
          inputMode="decimal"
          placeholder="Amount (AED)"
          aria-label="Amount in AED"
        />
      )}

      <input
        value={note}
        onChange={(e) => setNote(e.target.value)}
        placeholder="Note (required)"
        aria-label="Note"
      />

      <button
        onClick={() => mutation.mutate()}
        disabled={mutation.isPending || note.trim().length === 0}
        title={note.trim().length === 0 ? "A resolution with no stated reason cannot be audited" : undefined}
      >
        Resolve
      </button>

      {error && <p className="bad">{error}</p>}
    </div>
  );
}

export default function DisputeQueue() {
  const { data, isPending, error } = useQueueData("disputes", getDisputeQueue);
  const items = data?.items ?? [];

  return (
    <>
      <h1>Disputes</h1>
      <p className="muted">
        Three-day SLA. Resolutions come from a closed list because &ldquo;how
        often do we refund&rdquo; is a weekly question and free text cannot
        answer it.
      </p>

      <QueueState
        isPending={isPending}
        error={error}
        count={items.length}
        empty="No open disputes."
      />

      <ul className="stack">
        {items.map((d) => (
          <li key={d.id} className="card">
            <div className="row spread">
              <div className="row">
                <span className="pill">{d.kind}</span>
                <span className="pill">{d.state}</span>
              </div>
              {d.overdue && <span className="pill bad">overdue</span>}
            </div>

            <p className="mono muted">
              {d.reference_no} · {d.subject_type} {d.subject_id}
            </p>

            <p className="muted">
              Opened {new Date(d.created_at).toLocaleString("en-AE")}
              {d.due_at && ` · due ${new Date(d.due_at).toLocaleString("en-AE")}`}
              {d.assigned_to && ` · assigned`}
            </p>

            <ResolveForm disputeId={d.id} />
          </li>
        ))}
      </ul>
    </>
  );
}
