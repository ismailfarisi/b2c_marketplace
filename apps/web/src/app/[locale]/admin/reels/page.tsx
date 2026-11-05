"use client";

import Link from "next/link";
import { useParams } from "next/navigation";

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
 * The card shows the caption and storage keys; the video itself plays on the
 * reel's detail page (signed URL from `getReelMedia`), linked as "Watch".
 */
export default function ReelQueue() {
  const { locale } = useParams<{ locale: string }>();
  const { data, error, refetch } = useQueueData("reels", getReelQueue);
  const items = data?.items ?? [];

  return (
    <>
      <p className="console-lede">
        Two-hour SLA, oldest first — a stuck author is a supply problem, and
        supply is the risk this product actually has.
      </p>

      <CardState
        loaded={data !== undefined}
        error={error}
        perm="content.review"
        onRetry={() => void refetch()}
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

            <p className="note">
              <Link className="btn" href={`/${locale}/admin/reels/${it.reel_id}`}>
                Watch
              </Link>{" "}
              Plays the video on the reel&rsquo;s page before you decide.
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
