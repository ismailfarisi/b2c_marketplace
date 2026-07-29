"use client";

import { decideReel, getReelQueue } from "@/lib/admin-api";
import { CardState, Decide, Tag, useQueueData, waited } from "@/components/admin/table";

/**
 * Reel review.
 *
 * **Cards, not a table**, and that is the density decision this screen makes
 * against the one next door. A comment is a line of text and a report count —
 * sixty of those belong in a column. A reel is a video, and a table row of it is
 * a filename: the reviewer's whole job is *looking at the thing*, so the thing
 * gets the space.
 *
 * Sorted oldest first rather than by report count, because this queue has a
 * publish SLA on it: an author whose video is stuck is a supply problem, and
 * supply is the existential risk this product actually has.
 *
 * **The video is not playable here, and neither is the poster visible.**
 * `poster_key` is an object-storage key and there is no endpoint that turns one
 * into a URL a browser can load, so the key is rendered as text. A moderator
 * cannot watch what they are judging — which means this screen is not yet
 * sufficient for the decision it asks for. Stated here rather than hidden behind
 * a grey box that looks like a loading image. See PHASE-10 §7.3.
 */
export default function ReelQueue() {
  const { data, isPending, error } = useQueueData("reels", getReelQueue);
  const items = data?.items ?? [];

  return (
    <>
      <p className="console-lede">
        Two-hour SLA, oldest first — a stuck author is a supply problem, and
        supply is the risk this product actually has.
      </p>

      <CardState
        isPending={isPending}
        error={error}
        count={items.length}
        empty="Nothing waiting. Every submitted reel has been decided."
      />

      {items.map((it) => (
        <section key={it.reel_id} className="panel">
          <div className="panel-head">
            <Tag>{it.author_type}</Tag>
            <Tag tone={it.tier === "new" ? "warn" : undefined}>{it.tier}</Tag>
            {it.reports > 0 && (
              <Tag tone="danger">
                {it.reports} report{it.reports === 1 ? "" : "s"}
              </Tag>
            )}
            <div style={{ flex: 1 }} />
            <Tag tone={it.sla_breached ? "danger" : undefined}>
              waiting {waited(it.waiting_seconds)}
              {it.sla_breached ? " · late" : ""}
            </Tag>
          </div>

          <div className="panel-body">
            {/* dir="auto" and not the layout's ltr. The chrome is English; the
                caption is whatever the author wrote, and an Arabic caption
                rendered left-to-right is unreadable in exactly the way that
                makes a moderator misjudge it. */}
            <p dir="auto" style={{ fontSize: 15, marginBlockStart: 0 }}>
              {it.caption || <span className="faint">no caption</span>}
            </p>

            {it.categories && it.categories.length > 0 && (
              <div className="decide row" style={{ padding: 0, borderInlineStart: "none" }}>
                {it.categories.map((c) => (
                  <Tag key={c}>{c}</Tag>
                ))}
              </div>
            )}

            <p className="note" style={{ marginBlockStart: 10 }}>
              <span className="mono">{it.poster_key || "no poster"}</span>
              {" · "}
              <span className="mono">{it.reel_id}</span>
            </p>

            <p className="note danger">
              The video cannot be played here yet — there is no signed-URL
              endpoint, so this shows the storage key. Judging a reel without
              watching it is not a decision this screen can support.
            </p>
          </div>

          <Decide
            onDecide={(approve, reason) =>
              decideReel(it.reel_id, approve ? "approve" : "reject", reason)
            }
          />
        </section>
      ))}
    </>
  );
}
