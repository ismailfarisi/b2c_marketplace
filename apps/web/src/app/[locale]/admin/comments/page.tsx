"use client";

import { decideComment, getCommentQueue } from "@/lib/admin-api";
import {
  Decide,
  Evidence,
  QueuePanel,
  Tag,
  since,
  useQueueData,
} from "@/components/admin/table";

/**
 * Comment review.
 *
 * A table, not cards, and that is the density decision this screen exists to
 * make. Phase 4 gave comments their own endpoint because they arrive at a rate
 * orders of magnitude higher than reels and are judged in seconds rather than
 * minutes. A reviewer clears sixty of these in the time it takes to watch four
 * reels, so vertical space is the thing that costs them — every extra rem is one
 * fewer item on screen and one more scroll between decisions.
 */
export default function CommentQueue() {
  const { data, isPending, error } = useQueueData("comments", getCommentQueue);
  const rows = data?.items ?? [];

  return (
    <>
      <p className="console-lede">
        Thirty-minute SLA. Reported comments first — an unreported one in this
        queue was held by the classifier, and the classifier is more often wrong
        than a person who took the trouble to report.
      </p>

      <QueuePanel
        title="Awaiting review"
        hint="judged in seconds"
        columns={[
          { label: "Author" },
          { label: "Comment", className: "grow" },
          { label: "Reports", className: "num" },
          { label: "Waiting", className: "num" },
        ]}
        rows={rows}
        isPending={isPending}
        error={error}
        empty="Nothing held."
        rowKey={(c) => c.id}
        renderRow={(c) => (
          <>
            <td className="nowrap">{c.author_name || <span className="faint">unknown</span>}</td>
            {/* dir="auto" rather than inheriting the LTR chrome. A comment is
                written in whichever language its author chose, and an Arabic one
                forced left-to-right is unreadable in exactly the way that makes
                a moderator misjudge it. */}
            <td className="grow" dir="auto">
              {c.body}
            </td>
            <td className="num">
              {c.reports > 0 ? <Tag tone="danger">{c.reports}</Tag> : <span className="faint">—</span>}
            </td>
            <td className="num nowrap">{since(c.created_at)}</td>
          </>
        )}
        renderDetail={(c, close) => (
          <Decide
            approveLabel="Publish"
            rejectLabel="Remove"
            onDone={close}
            onDecide={(approve, reason) =>
              decideComment(c.id, approve ? "approve" : "reject", reason)
            }
          >
            <Evidence
              rows={[
                ["Comment", <span dir="auto">{c.body}</span>],
                ["Author", <span className="mono">{c.author_id}</span>],
                ["On reel", <span className="mono">{c.reel_id}</span>],
                ["State", c.state],
              ]}
            />
          </Decide>
        )}
      />
    </>
  );
}
