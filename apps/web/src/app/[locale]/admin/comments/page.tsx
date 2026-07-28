"use client";

import { decideComment, getCommentQueue } from "@/lib/admin-api";
import { Decide, QueueState, useQueueData } from "@/components/admin/review";

/**
 * Comment review.
 *
 * Its own screen rather than a tab on the reel queue, for the reason Phase 4
 * gave it its own endpoint: comments arrive at a rate orders of magnitude
 * higher and are judged in seconds rather than minutes. Interleaving them would
 * bury the reel queue, which is the one with a publish SLA on it.
 *
 * The consequence for this page is that it is dense on purpose. No cards with
 * generous padding — a reviewer clears sixty of these in the time it takes to
 * watch four reels, and vertical space is the thing that costs them.
 */
export default function CommentQueue() {
  const { data, isPending, error } = useQueueData("comments", getCommentQueue);
  const items = data?.items ?? [];

  return (
    <>
      <h1>Comments</h1>
      <p className="muted">
        Judged in seconds. Reported comments first — an unreported one in this
        queue was held by the classifier, and the classifier is more often wrong
        than a person who took the trouble to report.
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
              <span className="muted">{it.author_name || it.author_id}</span>
              <div className="row">
                {it.reports > 0 && (
                  <span className="pill bad">
                    {it.reports} report{it.reports === 1 ? "" : "s"}
                  </span>
                )}
                <span className="pill">{it.state}</span>
              </div>
            </div>

            <p dir="auto">{it.body}</p>

            <Decide
              onDecide={(approve, reason) =>
                decideComment(it.id, approve ? "approve" : "reject", reason)
              }
              approveLabel="Publish"
              rejectLabel="Remove"
            />
          </li>
        ))}
      </ul>
    </>
  );
}
