"use client";

import { decideAppeal, getAppealQueue } from "@/lib/admin-api";
import { Decide, QueueState, useQueueData } from "@/components/admin/review";

/**
 * Appeals.
 *
 * The queue shows **overdue appeals only**, and that is the server's choice
 * rather than a filter this page applies. The failure mode of an appeals
 * process is not that appeals are decided wrongly — it is that they are never
 * decided, and a list of every open appeal buries the breached ones among them.
 *
 * Two things about this screen are not cosmetic:
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
  const items = data?.items ?? [];

  return (
    <>
      <h1>Appeals</h1>
      <p className="muted">
        Overdue only. Seven-day SLA — an appeals process with no deadline is a
        way of saying no slowly, and the app stores treat &ldquo;users cannot
        appeal&rdquo; as a policy violation rather than a product choice.
      </p>

      <QueueState
        isPending={isPending}
        error={error}
        count={items.length}
        empty="No appeal is past its deadline. Open appeals within SLA are not listed here — this queue exists to surface the ones nobody has reached."
      />

      <ul className="stack">
        {items.map((a) => (
          <li key={a.id} className="card">
            <div className="row spread">
              <div className="row">
                <span className="pill">{a.subject_type}</span>
                <span className="mono muted">{a.subject_id}</span>
              </div>
              <span className="pill bad">{a.overdue_by_hours}h over</span>
            </div>

            <p className="muted">
              Opened {new Date(a.created_at).toLocaleString("en-AE")} · due{" "}
              {new Date(a.due_at).toLocaleString("en-AE")} · appellant{" "}
              <span className="mono">{a.appellant_id}</span>
            </p>

            <Decide
              approveLabel="Overturn"
              rejectLabel="Uphold"
              // A note either way. An upheld appeal is a second refusal and the
              // author is entitled to know it was read; an overturned one voids
              // a strike, and the void carries a reason into the record that a
              // review of the moderation function later reads.
              onDecide={(overturn, note) => decideAppeal(a.id, overturn, note)}
            />
          </li>
        ))}
      </ul>
    </>
  );
}
