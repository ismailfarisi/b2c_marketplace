"use client";

import { useQuery } from "@tanstack/react-query";
import { use, useState } from "react";

import { Panel, Ref, plural } from "@/components/admin/detail";
import { Tag, since } from "@/components/admin/table";
import { getCoverage, getSupplyGaps } from "@/lib/admin-api";

/**
 * Content supply — the risk the video-first plan calls out, made countable.
 *
 * A feed in a district with four reels shows the same four reels to everyone,
 * and from inside the product that is indistinguishable from a broken ranker.
 * The only way to tell them apart is to count, and nothing counted until now.
 *
 * Two lists, and the split matters. Vendors who have **never** published are a
 * different conversation to vendors who have **stopped** — the first is an
 * onboarding that did not finish, the second is a relationship going cold — so
 * they are not merged and sorted together by treating "never" as a very large
 * number of quiet days.
 */

export default function SupplyPage({ params }: { params: Promise<{ locale: string }> }) {
  const { locale } = use(params);
  const [quietDays, setQuietDays] = useState(14);

  const gaps = useQuery({
    queryKey: ["admin", "supply", "gaps", quietDays],
    queryFn: () => getSupplyGaps(quietDays),
  });
  const coverage = useQuery({ queryKey: ["admin", "supply", "coverage"], queryFn: getCoverage });

  if (gaps.error) return <p className="danger">Supply data could not be loaded.</p>;

  const rows = gaps.data?.items ?? [];
  const never = rows.filter((g) => g.days_quiet === null);
  const stopped = rows.filter((g) => g.days_quiet !== null);
  const areas = coverage.data?.items ?? [];
  const thin = areas.filter((a) => a.reels < 5);

  return (
    <div className="detail">
      <div className="detail-head">
        <div>
          <h2>
            Supply
            {never.length > 0 ? <Tag tone="danger">{never.length} never published</Tag> : null}
            {thin.length > 0 ? <Tag tone="warn">{plural(thin.length, "thin area")}</Tag> : null}
          </h2>
          <p className="console-lede">
            Who has content, who has stopped, and which districts a feed would struggle to fill.
          </p>
        </div>
        <div className="spacer" />
        <label className="field">
          <span>Quiet for at least</span>
          <select value={quietDays} onChange={(e) => setQuietDays(Number(e.target.value))}>
            <option value={7}>7 days</option>
            <option value={14}>14 days</option>
            <option value={30}>30 days</option>
            <option value={90}>90 days</option>
          </select>
        </label>
      </div>

      <Panel title="Onboarded and never published" note={plural(never.length, "vendor")}>
        {never.length === 0 ? (
          <p className="muted">
            Every active vendor has published at least once. This is the panel that should stay
            empty — a vendor here was signed up and then left alone.
          </p>
        ) : (
          <table className="money-table">
            <tbody>
              {never.map((g) => (
                <tr key={g.vendor_id}>
                  <td>
                    <Ref locale={locale} kind="vendors" id={g.vendor_id}>
                      {g.vendor}
                    </Ref>
                  </td>
                  <td className="faint">{g.city}</td>
                  <td>
                    {g.pending_review > 0 ? (
                      // Not idle — waiting on us. Worth separating, because the
                      // fix is a moderator rather than a phone call.
                      <Tag tone="warn">{plural(g.pending_review, "reel")} awaiting review</Tag>
                    ) : (
                      <span className="faint">nothing uploaded</span>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Panel>

      <Panel title={`Quiet for ${quietDays}+ days`} note={plural(stopped.length, "vendor")}>
        {stopped.length === 0 ? (
          <p className="muted">Nobody who has published has gone quiet for that long.</p>
        ) : (
          <table className="money-table">
            <tbody>
              {stopped.map((g) => (
                <tr key={g.vendor_id}>
                  <td>
                    <Ref locale={locale} kind="vendors" id={g.vendor_id}>
                      {g.vendor}
                    </Ref>
                  </td>
                  <td className="faint">{g.city}</td>
                  <td className="faint">{plural(g.published, "reel")} published</td>
                  <td className={g.days_quiet && g.days_quiet > 30 ? "danger" : "faint"}>
                    last {g.last_published ? `${since(g.last_published)} ago` : "—"}
                  </td>
                  <td>
                    {g.pending_review > 0 ? (
                      <Tag tone="warn">{plural(g.pending_review, "reel")} awaiting review</Tag>
                    ) : null}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Panel>

      <Panel title="Coverage" note={plural(areas.length, "area")}>
        {coverage.isLoading ? (
          <p className="muted">Loading…</p>
        ) : areas.length === 0 ? (
          <p className="muted">No branches with a city recorded.</p>
        ) : (
          <table className="money-table">
            <thead>
              <tr>
                <th>Emirate</th>
                <th>City</th>
                <th className="num">Published reels</th>
                <th className="num">Vendors</th>
                <th>Last published</th>
              </tr>
            </thead>
            <tbody>
              {areas.map((a) => (
                <tr key={`${a.emirate}-${a.city}`}>
                  <td>{a.emirate}</td>
                  <td>{a.city}</td>
                  {/* Fewer than five is the threshold at which a feed starts
                      repeating itself to the same viewer within a session. */}
                  <td className={`num ${a.reels < 5 ? "danger" : ""}`}>{a.reels}</td>
                  <td className="num faint">{a.vendors}</td>
                  <td className="faint">
                    {a.last_published ? `${since(a.last_published)} ago` : "never"}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Panel>
    </div>
  );
}
