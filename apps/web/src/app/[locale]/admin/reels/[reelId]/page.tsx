"use client";

import { useQuery } from "@tanstack/react-query";
import { use, useState } from "react";

import { Detail, Facts, Panel, Ref, TimelinePanel, messageOf, plural } from "@/components/admin/detail";
import { Tag } from "@/components/admin/table";
import { getReelDetail, getReelMedia, getReelTimeline, type ReelDetail } from "@/lib/admin-api";

/**
 * The reel page, and the reason it exists: **you can watch the video.**
 *
 * Until this page, a moderator was asked to approve or reject content they could
 * not see. The queue gave them a caption, a poster key and a report count, and a
 * button marked Reject. That is not moderation — it is a coin toss with an audit
 * trail attached, and the audit trail makes it worse rather than better because
 * it records a decision nobody could have made properly.
 *
 * The player is opt-in, one click. Not because loading is expensive but because
 * fetching the URL is a recorded event: "who watched this video" is asked after
 * a complaint, and it should not answer "everyone who opened the page".
 *
 * Below it, the pipeline. Four timestamps in the order they happen, so "why is
 * this stuck" is answered by looking rather than by reading worker logs — a reel
 * that never probed failed somewhere different to one that probed and never
 * transcoded, and the state name is the same unhelpful word in both cases.
 */

export default function ReelPage({
  params,
}: {
  params: Promise<{ locale: string; reelId: string }>;
}) {
  const { locale, reelId } = use(params);

  const { data, isLoading, error } = useQuery({
    queryKey: ["admin", "reel", reelId],
    queryFn: () => getReelDetail(reelId),
  });

  if (error) return <p className="danger">This reel could not be loaded.</p>;
  if (isLoading || !data) return <p className="muted">Loading reel…</p>;

  return (
    <Detail
      title={data.caption ?? "(no caption)"}
      subtitle={`${data.author} · ${(data.duration_ms / 1000).toFixed(1)}s · created ${new Date(
        data.created_at,
      ).toLocaleString()}`}
      tags={
        <>
          <Tag tone={stateTone(data.state)}>{data.state}</Tag>
          {data.visibility !== "public" ? <Tag tone="warn">{data.visibility}</Tag> : null}
          {data.reports > 0 ? <Tag tone="danger">{plural(data.reports, "report")}</Tag> : null}
        </>
      }
      aside={
        <TimelinePanel
          queryKey={["admin", "reel", reelId, "timeline"]}
          fetcher={() => getReelTimeline(reelId)}
        />
      }
    >
      <Panel title="The video">
        {data.asset.has_source ? (
          <Player reelId={reelId} mime={data.asset.mime_type} />
        ) : (
          <p className="muted">
            No source object is attached to this reel. That is usually the answer to &ldquo;why is
            this draft empty&rdquo; — the upload never completed, so there is nothing to play and
            nothing the pipeline could have processed.
          </p>
        )}
      </Panel>

      <Panel title="Why it is in this state">
        <Facts rows={pipelineRows(data)} />
        {data.pipeline.failure_code ? (
          <p className="alarm">
            <strong>{data.pipeline.failure_code}</strong>
            {data.pipeline.failure_detail ? ` — ${data.pipeline.failure_detail}` : ""}
          </p>
        ) : null}
      </Panel>

      <Panel title="What was uploaded">
        <Facts
          rows={[
            ["Format", data.asset.mime_type],
            [
              "Dimensions",
              data.asset.width && data.asset.height
                ? `${data.asset.width} × ${data.asset.height}${
                    data.asset.height > data.asset.width ? " (portrait)" : " — not portrait"
                  }`
                : null,
            ],
            ["Size", data.asset.bytes ? `${(data.asset.bytes / 1_048_576).toFixed(1)} MB` : null],
            ["Duration", `${(data.duration_ms / 1000).toFixed(1)}s`],
            ["Caption language", data.locale],
            [
              "Author",
              data.author_type === "vendor_branch" ? (
                <span>{data.author}</span>
              ) : (
                <Ref locale={locale} kind="users" id={data.author_id}>
                  {data.author}
                </Ref>
              ),
            ],
            [
              "Coverage",
              data.coverage.resolved ? (
                <>
                  <Tag tone="ok">resolved</Tag>
                  <span className="faint"> {data.coverage.kind}</span>
                </>
              ) : (
                // Unresolved coverage means the reel cannot enter a
                // location-ranked feed. It will look published and reach
                // nobody, which is the hardest failure to notice.
                <Tag tone="warn" title="This reel cannot appear in a location-ranked feed">
                  not resolved
                </Tag>
              ),
            ],
          ]}
        />
      </Panel>

      <Panel title="Renditions" note={plural(data.renditions.length, "rendition")}>
        {data.renditions.length === 0 ? (
          <p className="muted">
            {data.state === "published"
              ? "Published with no renditions — playback will fall back to the source, which is a large download on a phone."
              : "Nothing transcoded yet."}
          </p>
        ) : (
          <table className="money-table">
            <tbody>
              {data.renditions.map((r) => (
                <tr key={r.label}>
                  <td className="mono">{r.label}</td>
                  <td className="faint">
                    {r.width} × {r.height}
                  </td>
                  <td className="faint">{r.bitrate_kbps} kbps</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Panel>

      <Panel title="Reach">
        <Facts
          rows={[
            ["Views", data.engagement.views],
            ["Likes", data.engagement.likes],
            ["Comments", data.engagement.comments],
            ["Shares", data.engagement.shares],
            ["Saves", data.engagement.saves],
          ]}
        />
      </Panel>
    </Detail>
  );
}

/**
 * The player.
 *
 * One click to fetch the URL, because that fetch is recorded. The countdown is
 * shown rather than hidden: the URL expires in two minutes, and a viewer whose
 * video stops mid-way should know why rather than conclude the console is
 * broken.
 */
function Player({ reelId, mime }: { reelId: string; mime: string | null }) {
  const [url, setUrl] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  if (url) {
    return (
      <div className="player">
        <video src={url} controls playsInline preload="metadata" />
        <p className="muted">
          This link expires two minutes after it was issued, and the access has been recorded
          against your name. Click again for a fresh one.
        </p>
        <button className="btn" onClick={() => setUrl(null)}>
          Close
        </button>
      </div>
    );
  }

  return (
    <div>
      <button
        className="btn primary"
        disabled={busy}
        onClick={async () => {
          setBusy(true);
          setError(null);
          try {
            const media = await getReelMedia(reelId);
            setUrl(media.url);
          } catch (err) {
            setError(messageOf(err));
          } finally {
            setBusy(false);
          }
        }}
      >
        {busy ? "Getting a link…" : "Watch this reel"}
      </button>
      <p className="muted">
        {mime ? `${mime}. ` : ""}Fetching the link is recorded against your name — this is
        unpublished content belonging to someone else.
      </p>
      {error ? <p className="danger">{error}</p> : null}
    </div>
  );
}

function pipelineRows(data: ReelDetail) {
  const p = data.pipeline;
  return [
    ["Bytes received", stamp(p.bytes_ready_at)],
    ["Probed", stamp(p.probed_at)],
    ["Metadata ready", stamp(p.metadata_ready_at)],
    ["Published", stamp(data.published_at)],
    ["Removed", stamp(data.removed_at)],
    [
      "Upload window",
      p.upload_expires_at ? (
        new Date(p.upload_expires_at) < new Date() && !p.bytes_ready_at ? (
          <Tag tone="danger" title="The upload window closed before the bytes arrived">
            expired {new Date(p.upload_expires_at).toLocaleString()}
          </Tag>
        ) : (
          new Date(p.upload_expires_at).toLocaleString()
        )
      ) : null,
    ],
    ["Attempts", p.attempts > 1 ? <Tag tone="warn">{p.attempts}</Tag> : p.attempts],
  ] as Array<[string, React.ReactNode]>;
}

/** A timestamp, or an explicit "not yet" — which is the informative half. */
function stamp(iso: string | null): React.ReactNode {
  return iso ? new Date(iso).toLocaleString() : <span className="faint">not yet</span>;
}

function stateTone(state: string): "danger" | "warn" | "ok" | undefined {
  if (state === "published" || state === "approved") return "ok";
  if (state === "failed" || state === "rejected" || state === "removed" || state === "expired")
    return "danger";
  if (state === "review_pending" || state === "moderating") return "warn";
  return undefined;
}
