"use client";

import { decideMessage, getMessageQueue } from "@/lib/admin-api";
import { Decide, QueueState, useQueueData } from "@/components/admin/review";

/**
 * Held chat messages.
 *
 * The most time-critical queue in the product and the one with the smallest
 * screen. A held message is a *stalled conversation* — a buyer asked a question
 * about a car and the seller has not seen it — so fifteen minutes is the SLA and
 * anything that slows a decision here costs a sale.
 *
 * The scanner's findings are shown next to the body rather than as a badge,
 * because they are the reason the message is here and the reviewer is
 * effectively checking the scanner's work. A message held for
 * `advance_fee_language` and a message held for a phone number are opposite
 * decisions: the first is usually a scam, the second is usually two people
 * trying to close a deal.
 */
export default function MessageQueue() {
  const { data, isPending, error } = useQueueData("messages", getMessageQueue);
  const items = data?.items ?? [];

  return (
    <>
      <h1>Chat messages</h1>
      <p className="muted">
        Fifteen-minute SLA. A held message is a stalled conversation, not a
        stalled post.
      </p>

      <QueueState
        isPending={isPending}
        error={error}
        count={items.length}
        empty="Nothing held."
      />

      <ul className="stack tight">
        {items.map((it) => (
          <li key={it.id} className="card">
            <div className="row spread">
              <span className="muted mono">{it.sender_id}</span>
              <span className="pill bad">{it.reason || "held"}</span>
            </div>

            {/* The message as the scanner saw it. Arabic-Indic digits fold to
                ASCII before matching, so a body that looks clean here may
                legitimately have tripped the phone detector — the findings say
                which rule fired. */}
            <p dir="auto">{it.body}</p>

            {it.findings && it.findings.length > 0 && (
              <div className="row">
                <span className="muted">scanner:</span>
                {it.findings.map((f) => (
                  <span key={f} className="pill">
                    {f}
                  </span>
                ))}
              </div>
            )}

            {it.listing_id && (
              <p className="muted mono">listing {it.listing_id}</p>
            )}

            <Decide
              // Binary. The scanner finding is already the reason, and asking a
              // reviewer to retype it fifteen times an hour produces "spam" as
              // the reason on every row.
              reasonRequired={false}
              approveLabel="Release"
              rejectLabel="Block"
              onDecide={(approve) => decideMessage(it.id, approve)}
            />
          </li>
        ))}
      </ul>
    </>
  );
}
