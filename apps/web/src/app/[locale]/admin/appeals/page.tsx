"use client";

import { decideAppeal, getAppealQueue } from "@/lib/admin-api";
import {
  Decide,
  Evidence,
  QueuePanel,
  Tag,
  since,
  useQueueData,
} from "@/components/admin/table";

/**
 * Appeals.
 *
 * The queue shows **overdue appeals only**, and that is the server's choice
 * rather than a filter this page applies. The failure mode of an appeals process
 * is not that appeals are decided wrongly — it is that they are never decided,
 * and a list of every open appeal buries the breached ones among them.
 *
 * Two things here are not cosmetic:
 *
 * **An appeal cannot be decided by the person whose decision is being
 * appealed.** Enforced by a database CHECK, not by rota discipline. In a team of
 * four that person is the obvious next reviewer most of the time, so the API
 * returns 403 and this page surfaces it rather than swallowing it — a moderator
 * who does not know why their decision failed will try again.
 *
 * **"Overturn" is the primary action.** Not because appeals should usually
 * succeed, but because upholding is the outcome that requires no work and
 * therefore needs no encouragement from the layout.
 */
export default function AppealQueue() {
  const { data, isPending, error } = useQueueData("appeals", getAppealQueue);
  const rows = data?.items ?? [];

  return (
    <>
      <p className="console-lede">
        Overdue only. Seven-day SLA — an appeals process with no deadline is a way
        of saying no slowly, and the app stores treat &ldquo;users cannot
        appeal&rdquo; as a policy violation rather than a product choice.
      </p>

      <QueuePanel
        title="Past their deadline"
        columns={[
          { label: "Subject" },
          { label: "Reference", className: "grow" },
          { label: "Appellant" },
          { label: "Opened", className: "num" },
          { label: "Overdue by", className: "num" },
        ]}
        rows={rows}
        isPending={isPending}
        error={error}
        empty="No appeal is past its deadline. Open appeals within SLA are not listed here — this queue exists to surface the ones nobody has reached."
        rowKey={(a) => a.id}
        renderRow={(a) => (
          <>
            <td className="nowrap">
              <Tag>{a.subject_type}</Tag>
            </td>
            <td className="grow mono">{a.subject_id}</td>
            <td className="mono nowrap">{a.appellant_id.slice(0, 8)}…</td>
            <td className="num nowrap">{since(a.created_at)}</td>
            <td className="num nowrap">
              <Tag tone="danger">{a.overdue_by_hours}h</Tag>
            </td>
          </>
        )}
        renderDetail={(a, close) => (
          <Decide
            approveLabel="Overturn"
            rejectLabel="Uphold"
            onDone={close}
            // A note either way. An upheld appeal is a second refusal and the
            // author is entitled to know it was read; an overturned one voids a
            // strike, and the void carries a reason into the record that a
            // later review of the moderation function reads.
            onDecide={(overturn, note) => decideAppeal(a.id, overturn, note)}
          >
            <p className="note">
              This appeal cannot be decided by whoever made the original
              decision — the database refuses it. If this returns 403, hand it to
              someone else rather than retrying.
            </p>
            <Evidence
              rows={[
                ["Subject", `${a.subject_type} ${a.subject_id}`],
                ["Appellant", <span className="mono">{a.appellant_id}</span>],
                ["Opened", new Date(a.created_at).toLocaleString("en-AE")],
                ["Due", new Date(a.due_at).toLocaleString("en-AE")],
              ]}
            />
          </Decide>
        )}
      />
    </>
  );
}
