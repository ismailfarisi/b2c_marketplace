"use client";

import { useQuery } from "@tanstack/react-query";

import { Panel, plural } from "@/components/admin/detail";
import { Tag, money, since } from "@/components/admin/table";
import { getDriverCash, getRemittances } from "@/lib/admin-api";

/**
 * Cash reconciliation — who is carrying the platform's money.
 *
 * Ordered by what is held, descending, because the question is always "who has
 * the most of our cash" and never "who is alphabetically first". The age of the
 * oldest held collection sits next to it: a driver holding 200 dirhams from this
 * morning and a driver holding 200 dirhams from eleven days ago are the same
 * number and completely different problems.
 *
 * Shortfalls are shown for every driver including the zeroes. A reconciliation
 * page that lists only discrepancies cannot be distinguished from a broken query
 * on a day when nothing is wrong, and that is the day it silently stops working.
 */

export default function CashPage() {
  const cash = useQuery({ queryKey: ["admin", "cod", "drivers"], queryFn: getDriverCash });
  const remittances = useQuery({
    queryKey: ["admin", "cod", "remittances"],
    queryFn: () => getRemittances(),
  });

  if (cash.error) {
    return (
      <p className="danger">
        Cash positions could not be loaded. If this reads &ldquo;forbidden&rdquo;, the account is
        missing <span className="mono">money.read</span>.
      </p>
    );
  }

  const drivers = cash.data?.items ?? [];
  const carrying = drivers.filter((d) => d.held.amount > 0);
  const short = drivers.filter((d) => d.shortfall.amount !== 0);

  return (
    <div className="detail">
      <div className="detail-head">
        <div>
          <h2>
            Cash
            {cash.data ? <Tag>{money(cash.data.total_held)} held</Tag> : null}
          </h2>
          <p className="console-lede">
            {carrying.length > 0
              ? `${plural(carrying.length, "driver")} carrying money that has not been handed in.`
              : "Nothing outstanding — every collection has been remitted."}
          </p>
        </div>
      </div>

      {short.length > 0 ? (
        <p className="alarm">
          {plural(short.length, "driver")} settled for an amount that differs from what was
          collected. A shortfall is not automatically theft — a customer who paid 240 on a 250 order
          produces one — but every one of them should have an explanation attached before the next
          settlement.
        </p>
      ) : null}

      <Panel title="Per driver" note={plural(drivers.length, "driver")}>
        {cash.isLoading ? (
          <p className="muted">Loading…</p>
        ) : drivers.length === 0 ? (
          <p className="muted">No drivers.</p>
        ) : (
          <table className="money-table">
            <thead>
              <tr>
                <th>Driver</th>
                <th className="num">Held</th>
                <th>Oldest</th>
                <th className="num">Remitted</th>
                <th className="num">Settled</th>
                <th className="num">Variance</th>
              </tr>
            </thead>
            <tbody>
              {drivers.map((d) => (
                <tr key={d.driver_id}>
                  <td>
                    {d.driver}
                    {d.phone ? <span className="faint mono"> {d.phone}</span> : null}
                  </td>
                  <td className={`num ${d.held.amount > 0 ? "" : "faint"}`}>
                    {money(d.held)}
                    {d.held_stops > 0 ? (
                      <span className="faint"> · {plural(d.held_stops, "stop")}</span>
                    ) : null}
                  </td>
                  {/* Age, not just amount. The same 200 dirhams held for a day
                      and held for eleven days are different problems. */}
                  <td className={d.oldest_held && ageDays(d.oldest_held) > 2 ? "danger" : "faint"}>
                    {d.oldest_held ? `${since(d.oldest_held)} ago` : "—"}
                  </td>
                  <td className="num faint">{money(d.remitted)}</td>
                  <td className="num faint">{money(d.settled)}</td>
                  <td className={`num ${d.shortfall.amount !== 0 ? "danger" : "faint"}`}>
                    {money(d.shortfall)}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Panel>

      <Panel
        title="Hand-ins"
        note={remittances.data ? plural(remittances.data.items.length, "remittance") : undefined}
      >
        {remittances.isLoading ? (
          <p className="muted">Loading…</p>
        ) : (remittances.data?.items ?? []).length === 0 ? (
          <p className="muted">
            No cash has been handed in yet. If drivers are holding money above, that is the gap.
          </p>
        ) : (
          <table className="money-table">
            <thead>
              <tr>
                <th>Reference</th>
                <th>Driver</th>
                <th className="num">Declared</th>
                <th className="num">Counted</th>
                <th className="num">Collections</th>
                <th className="num">Variance</th>
                <th>State</th>
              </tr>
            </thead>
            <tbody>
              {remittances.data!.items.map((r) => (
                <tr key={r.id}>
                  <td className="mono">{r.reference_no}</td>
                  <td>{r.driver}</td>
                  {/* Three numbers from three parties: what the driver said,
                      what was counted, and what the collections add up to.
                      Shown side by side because a disagreement between any two
                      of them is the finding. */}
                  <td className="num">{money(r.declared)}</td>
                  <td className="num">{r.counted ? money(r.counted) : <span className="faint">not counted</span>}</td>
                  <td className="num faint">
                    {money(r.expected)}
                    <span className="faint"> · {r.collections}</span>
                  </td>
                  <td className={`num ${r.variance.amount !== 0 ? "danger" : "faint"}`}>
                    {money(r.variance)}
                  </td>
                  <td>
                    <Tag tone={r.state === "settled" ? "ok" : undefined}>{r.state}</Tag>
                    <span className="faint"> {r.method}</span>
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

function ageDays(iso: string): number {
  return (Date.now() - new Date(iso).getTime()) / 86_400_000;
}
