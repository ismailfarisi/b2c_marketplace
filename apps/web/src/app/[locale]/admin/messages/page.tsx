"use client";

import { decideMessage, getMessageQueue } from "@/lib/admin-api";
import {
  Decide,
  Evidence,
  QueuePanel,
  Tag,
  since,
  useQueueData,
} from "@/components/admin/table";

/**
 * Held chat messages.
 *
 * The most time-critical queue in the product. A held message is a *stalled
 * conversation* — a buyer asked a question about a car and the seller has not
 * seen it — so fifteen minutes is the SLA and anything that slows a decision
 * costs a sale.
 *
 * The scanner's findings sit in their own column rather than hiding in the
 * detail, because they are the reason the message is here and the reviewer is
 * effectively checking the scanner's work. A message held for
 * `advance_fee_language` and one held for a phone number are opposite decisions:
 * the first is usually a scam, the second is usually two people trying to close
 * a deal.
 */
export default function MessageQueue() {
  const { data, isPending, error } = useQueueData("messages", getMessageQueue);
  const rows = data?.items ?? [];

  return (
    <>
      <p className="console-lede">
        Fifteen-minute SLA. A held message is a stalled conversation, not a
        stalled post — the buyer is waiting on a reply that is sitting here.
      </p>

      <QueuePanel
        title="Held"
        hint="release or block"
        columns={[
          { label: "Message", className: "grow" },
          { label: "Scanner" },
          { label: "Held for" },
          { label: "Waiting", className: "num" },
        ]}
        rows={rows}
        isPending={isPending}
        error={error}
        empty="Nothing held."
        rowKey={(m) => m.id}
        renderRow={(m) => (
          <>
            {/* The body as the scanner saw it. Arabic-Indic digits fold to ASCII
                before matching, so a body that looks clean here may legitimately
                have tripped the phone detector — the findings column says which
                rule fired. */}
            <td className="grow" dir="auto">
              {m.body}
            </td>
            <td className="nowrap">
              {(m.findings ?? []).map((f) => (
                <Tag key={f} code>
                  {f}
                </Tag>
              ))}
            </td>
            <td className="nowrap">
              <Tag tone="danger">{m.reason || "held"}</Tag>
            </td>
            <td className="num nowrap">{since(m.created_at)}</td>
          </>
        )}
        renderDetail={(m, close) => (
          <Decide
            // Binary. The scanner finding is already the reason, and asking a
            // reviewer to retype it fifteen times an hour produces "spam" as the
            // reason on every row.
            reasonRequired={false}
            approveLabel="Release"
            rejectLabel="Block"
            onDone={close}
            onDecide={(approve) => decideMessage(m.id, approve)}
          >
            <Evidence
              rows={[
                ["Message", <span dir="auto">{m.body}</span>],
                ["Sender", <span className="mono">{m.sender_id}</span>],
                ["Conversation", <span className="mono">{m.conversation_id}</span>],
                [
                  "Listing",
                  m.listing_id ? (
                    <span className="mono">{m.listing_id}</span>
                  ) : (
                    <span className="faint">not about a listing</span>
                  ),
                ],
              ]}
            />
          </Decide>
        )}
      />
    </>
  );
}
