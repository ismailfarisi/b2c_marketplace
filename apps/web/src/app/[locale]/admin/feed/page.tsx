"use client";

import { useMutation } from "@tanstack/react-query";
import { use, useState } from "react";

import { Panel, Ref, messageOf, plural } from "@/components/admin/detail";
import { Tag } from "@/components/admin/table";
import { inspectFeed, type FeedInspection } from "@/lib/admin-api";

/**
 * The feed inspector — "the feed looked wrong at 14:20".
 *
 * Runs the *real* retrieval for a point and shows the score with its terms kept,
 * because the alternatives are both bad: "the ranker is working as designed", or
 * a re-implementation of the ranker in a spreadsheet that is always subtly
 * different from the one actually serving.
 *
 * It shows the **ranked order, not a page**. Exploration slots come from a
 * seeded shuffle of the viewer key and page number, so a reel that appeared at
 * position 4 for a customer will not appear there for an operator. Marking
 * explore-eligible candidates and saying what that means is honest;
 * reconstructing a page and implying somebody saw it is not.
 *
 * On demand rather than on load. Every run is a full candidate generation with a
 * geo scan, and a page that fires one on mount turns an idle console tab into a
 * recurring load against the same query path the feed serves from.
 */

const TERMS = [
  "proximity",
  "recency",
  "engagement",
  "conversion",
  "intent",
  "affinity",
  "followed",
  "author",
] as const;

export default function FeedInspectorPage({
  params,
}: {
  params: Promise<{ locale: string }>;
}) {
  const { locale } = use(params);
  const [lat, setLat] = useState("25.11");
  const [lng, setLng] = useState("55.20");
  const [viewer, setViewer] = useState("");
  const [result, setResult] = useState<FeedInspection | null>(null);
  const [error, setError] = useState<string | null>(null);

  const run = useMutation({
    mutationFn: () =>
      inspectFeed({
        lat: Number(lat),
        lng: Number(lng),
        ...(viewer.trim() ? { viewer_id: viewer.trim() } : {}),
        limit: 25,
      }),
    onSuccess: (d) => {
      setResult(d);
      setError(null);
    },
    onError: (e) => setError(messageOf(e)),
  });

  return (
    <div className="detail">
      <div className="detail-head">
        <div>
          <h2>
            Feed inspector
            {result ? <Tag>{result.profile}</Tag> : null}
          </h2>
          <p className="console-lede">
            What the ranker would return for a point, and why. This runs the same retrieval the feed
            runs — it does not serve a page, record an impression, or spend a boost budget.
          </p>
        </div>
        <div className="spacer" />
        <div className="filters">
          <label className="field">
            <span>Latitude</span>
            <input value={lat} onChange={(e) => setLat(e.target.value)} className="mono" />
          </label>
          <label className="field">
            <span>Longitude</span>
            <input value={lng} onChange={(e) => setLng(e.target.value)} className="mono" />
          </label>
          <label className="field">
            <span>Viewer id (optional)</span>
            <input
              value={viewer}
              onChange={(e) => setViewer(e.target.value)}
              placeholder="anonymous"
              className="mono"
            />
          </label>
          <button className="btn primary" disabled={run.isPending} onClick={() => run.mutate()}>
            {run.isPending ? "Running…" : "Run"}
          </button>
        </div>
      </div>

      {error ? <p className="alarm">{error}</p> : null}

      {!result ? (
        <Panel title="Nothing run yet">
          <p className="muted">
            Enter a point and press Run. A viewer id is optional — without one this inspects the
            feed an anonymous visitor would get, which is the case most worth checking because it
            has no history to fall back on.
          </p>
        </Panel>
      ) : (
        <>
          <Panel
            title="Retrieval"
            note={`${result.retrieved} candidates · showing ${result.shown}`}
          >
            {result.retrieved === 0 ? (
              <p className="alarm">
                Nothing was retrieved at this point. That is a coverage problem rather than a
                ranking one: no published reel has a coverage area containing it, so no amount of
                weight tuning will put anything in this feed.
              </p>
            ) : result.retrieved <= 5 ? (
              <p className="muted">
                Only {plural(result.retrieved, "candidate")} to choose from. Below about five, the
                feed repeats itself within a session regardless of how it ranks — this is the
                supply problem wearing a ranking costume.
              </p>
            ) : (
              <p className="muted">
                {result.retrieved} candidates before the page limit. Ten drawn from eleven and ten
                drawn from four hundred look identical in the list below and are entirely different
                situations.
              </p>
            )}
          </Panel>

          <Panel title="Ranked" note={plural(result.items.length, "candidate")}>
            <div className="table-wrap">
              <table className="money-table">
                <thead>
                  <tr>
                    <th />
                    <th>Reel</th>
                    <th className="num">Score</th>
                    {TERMS.map((t) => (
                      <th key={t} className="num">
                        {t.slice(0, 4)}
                      </th>
                    ))}
                    <th className="num">Dist</th>
                    <th className="num">Impr</th>
                  </tr>
                </thead>
                <tbody>
                  {result.items.map((c) => (
                    <tr key={c.reel_id}>
                      <td className="num faint">{c.rank}</td>
                      <td>
                        <Ref locale={locale} kind="reels" id={c.reel_id}>
                          <span className="reel-link">{c.caption ?? "(no caption)"}</span>
                        </Ref>
                        <div className="faint">{c.author}</div>
                        {c.explore_eligible ? (
                          // The usual answer to "why is this at position 4",
                          // and invisible in the score by construction: a reel
                          // with no signal cannot win on score, which is
                          // exactly why exploration exists.
                          <Tag tone="warn" title="Can be interleaved regardless of score">
                            explore
                          </Tag>
                        ) : null}
                      </td>
                      <td className="num">{c.score.toFixed(3)}</td>
                      {TERMS.map((t) => (
                        <td
                          key={t}
                          className={`num ${c.terms[t] === 0 ? "faint" : ""}`}
                          title={`${t}: ${c.terms[t]}`}
                        >
                          {c.terms[t].toFixed(2)}
                        </td>
                      ))}
                      <td className="num faint">
                        {c.features.distance_km < 0 ? "?" : c.features.distance_km.toFixed(1)}
                      </td>
                      <td className="num faint">{c.features.impressions}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            {/* Every term is shown including the zeroes, because a zero is the
                answer to a question. "Why is a reel from another emirate here"
                is answered by proximity contributing nothing, and a table that
                hid empty columns would hide it. */}
            <p className="muted">
              Every term is shown, zeroes included — a zero proximity term is the answer to
              &ldquo;why is something from another emirate here&rdquo;, and hiding it would hide
              that. Distance is <span className="mono">?</span> when the reel has no origin point
              recorded, which is treated as neutral rather than distant so a data-collection failure
              does not demote content.
            </p>
          </Panel>
        </>
      )}
    </div>
  );
}
