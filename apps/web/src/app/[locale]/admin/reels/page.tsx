"use client";

import { decideReel, getReelQueue } from "@/lib/admin-api";
import { Decide, QueueState, Waiting, useQueueData } from "@/components/admin/review";

/**
 * Reel review.
 *
 * The queue with a publish SLA on it, which is why it sorts by wait rather than
 * by report count: an author whose video is stuck is a supply problem, and
 * supply is the existential risk this product has.
 *
 * **The poster is shown, the video is not.** That is a gap and it is stated
 * here rather than hidden: `poster_key` is an object-storage key and there is no
 * signed-URL endpoint to turn it into something an <img> can load, so this
 * renders the key. A moderator cannot currently watch the reel they are judging,
 * which means this screen is not yet sufficient for the decision it asks for.
 * The missing piece is one endpoint — see docs/PHASE-10 §5.2.
 */
export default function ReelQueue() {
  const { data, isPending, error } = useQueueData("reels", getReelQueue);
  const items = data?.items ?? [];

  return (
    <>
      <h1>Reels</h1>
      <p className="muted">
        Two-hour SLA. Oldest first — a stuck author is a supply problem, and
        supply is the risk this product actually has.
      </p>

      <QueueState
        isPending={isPending}
        error={error}
        count={items.length}
        empty="Nothing waiting. Every submitted reel has been decided."
      />

      <ul className="stack">
        {items.map((it) => (
          <li key={it.reel_id} className="card">
            <div className="row spread">
              <div className="row">
                <span className="pill">{it.author_type}</span>
                <span className="pill">{it.tier}</span>
                {it.reports > 0 && (
                  <span className="pill bad">
                    {it.reports} report{it.reports === 1 ? "" : "s"}
                  </span>
                )}
              </div>
              <Waiting seconds={it.waiting_seconds} breached={it.sla_breached} />
            </div>

            {/* dir="auto" and not the layout's ltr. The chrome is English; the
                caption is whatever the author wrote, and an Arabic caption
                rendered left-to-right is unreadable in exactly the way that
                makes a moderator misjudge it. */}
            <p dir="auto">{it.caption || <span className="muted">no caption</span>}</p>

            <p className="mono muted">
              {it.poster_key || "no poster"}
              {" · "}
              {it.reel_id}
            </p>

            {it.categories && it.categories.length > 0 && (
              <div className="row">
                {it.categories.map((c) => (
                  <span key={c} className="pill">
                    {c}
                  </span>
                ))}
              </div>
            )}

            <Decide
              onDecide={(approve, reason) =>
                decideReel(it.reel_id, approve ? "approve" : "reject", reason)
              }
            />
          </li>
        ))}
      </ul>
    </>
  );
}
