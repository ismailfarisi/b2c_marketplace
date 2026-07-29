"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

import { getDisputeQueue, resolveDispute } from "@/lib/admin-api";
import { Evidence, QueuePanel, Tag, since, useQueueData } from "@/components/admin/table";

/**
 * Disputes.
 *
 * The only queue here that does not use the shared `Decide` control, and the
 * reason is the shape of the decision rather than the styling. Every other queue
 * is approve-or-reject; a dispute resolves into one of seven outcomes, some of
 * which move money.
 *
 * The closed list is not a UI convenience. *"How often do we refund"* is a
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

function ResolveForm({ disputeId, onDone }: { disputeId: string; onDone: () => void }) {
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
        // refund twelve fils, which is the class of bug that only surfaces in a
        // reconciliation weeks later.
        amount_minor: needsAmount && amount ? Math.round(Number(amount) * 100) : undefined,
        note,
      }),
    onSuccess: () => {
      setNote("");
      setAmount("");
      setError(null);
      void queryClient.invalidateQueries({ queryKey: ["admin"] });
      onDone();
    },
    onError: (e: unknown) => setError(e instanceof Error ? e.message : "Failed"),
  });

  return (
    <div className="decide">
      <div className="row">
        <select
          className="field"
          value={resolution}
          onChange={(e) => setResolution(e.target.value)}
          aria-label="Resolution"
        >
          {RESOLUTIONS.map((r) => (
            <option key={r.value} value={r.value}>
              {r.label}
            </option>
          ))}
        </select>

        {needsAmount && (
          <input
            className="field"
            value={amount}
            onChange={(e) => setAmount(e.target.value)}
            inputMode="decimal"
            placeholder="Amount (AED)"
            aria-label="Amount in AED"
            style={{ minInlineSize: "14ch" }}
          />
        )}

        <input
          className="field"
          value={note}
          onChange={(e) => setNote(e.target.value)}
          placeholder="Note (required)"
          aria-label="Note"
        />

        <button
          className="btn primary"
          onClick={() => mutation.mutate()}
          disabled={mutation.isPending || note.trim().length === 0}
          title={
            note.trim().length === 0
              ? "A resolution with no stated reason cannot be audited"
              : undefined
          }
        >
          Resolve
        </button>
      </div>
      {error && <p className="note danger">{error}</p>}
    </div>
  );
}

export default function DisputeQueue() {
  const { data, isPending, error } = useQueueData("disputes", getDisputeQueue);
  const rows = data?.items ?? [];

  return (
    <>
      <p className="console-lede">
        Three-day SLA. Resolutions come from a closed list because &ldquo;how
        often do we refund&rdquo; is a weekly question and free text cannot answer
        it. The audit row is written in the same transaction as the decision.
      </p>

      <QueuePanel
        title="Open"
        columns={[
          { label: "Reference" },
          { label: "Kind" },
          { label: "Subject", className: "grow" },
          { label: "State" },
          { label: "Opened", className: "num" },
          { label: "Due", className: "num" },
        ]}
        rows={rows}
        isPending={isPending}
        error={error}
        empty="No open disputes."
        rowKey={(d) => d.id}
        renderRow={(d) => (
          <>
            <td className="mono nowrap">{d.reference_no}</td>
            <td className="nowrap">
              <Tag>{d.kind}</Tag>
            </td>
            <td className="grow mono">
              {d.subject_type} {d.subject_id}
            </td>
            <td className="nowrap">{d.state}</td>
            <td className="num nowrap">{since(d.created_at)}</td>
            <td className="num nowrap">
              {d.overdue ? (
                <Tag tone="danger">overdue</Tag>
              ) : d.due_at ? (
                new Date(d.due_at).toLocaleDateString("en-AE")
              ) : (
                <span className="faint">—</span>
              )}
            </td>
          </>
        )}
        renderDetail={(d, close) => (
          <>
            <div className="decide" style={{ paddingBlockEnd: 0 }}>
              <Evidence
                rows={[
                  ["Reference", <span className="mono">{d.reference_no}</span>],
                  ["Subject", `${d.subject_type} ${d.subject_id}`],
                  ["Opened", new Date(d.created_at).toLocaleString("en-AE")],
                  [
                    "Assigned",
                    d.assigned_to ? (
                      <span className="mono">{d.assigned_to}</span>
                    ) : (
                      <span className="faint">nobody</span>
                    ),
                  ],
                ]}
              />
            </div>
            <ResolveForm disputeId={d.id} onDone={close} />
          </>
        )}
      />
    </>
  );
}
