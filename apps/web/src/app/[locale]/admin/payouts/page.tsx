"use client";

import { useQuery } from "@tanstack/react-query";
import { use, useState } from "react";

import { Panel, Ref, plural } from "@/components/admin/detail";
import { Tag, money } from "@/components/admin/table";
import { getPayoutRuns, getStatements } from "@/lib/admin-api";

/**
 * Payout runs, and who is not getting paid.
 *
 * The blocked list is the point of this screen. "Why has this vendor not been
 * paid" is otherwise a join across `vendors`, `payout_accounts`, `kyc_cases` and
 * a licence expiry date, and nobody performs it at 5pm on a Thursday — so the
 * vendor is told "it's being looked into" and it is not.
 *
 * No issue or pay buttons, and their absence is deliberate. Paying a run moves
 * real money to real bank accounts, and `payouts.Service` has no method today
 * that takes an actor, records the action, and refuses a vendor whose licence
 * expired yesterday. A button wired to the method that exists would give this
 * console the ability to pay a blocked vendor in one click, which is worse than
 * not being able to pay anyone.
 */

export default function PayoutsPage({ params }: { params: Promise<{ locale: string }> }) {
  const { locale } = use(params);
  const [runId, setRunId] = useState<string>("");

  const runs = useQuery({ queryKey: ["admin", "payout-runs"], queryFn: getPayoutRuns });
  const statements = useQuery({
    queryKey: ["admin", "statements", runId],
    queryFn: () => getStatements(runId ? { run_id: runId } : {}),
  });

  if (runs.error) {
    return (
      <p className="danger">
        Payouts could not be loaded. If this reads &ldquo;forbidden&rdquo;, the account is missing
        <span className="mono"> money.read</span>.
      </p>
    );
  }

  const rows = statements.data?.items ?? [];
  const blocked = rows.filter((s) => s.blocked);
  const owedBlocked = blocked.reduce((n, s) => n + s.net.amount, 0);
  const currency = rows[0]?.net.currency ?? "AED";

  return (
    <div className="detail">
      <div className="detail-head">
        <div>
          <h2>
            Payouts
            {blocked.length > 0 ? (
              <Tag tone="warn">{plural(blocked.length, "vendor")} blocked</Tag>
            ) : null}
          </h2>
          <p className="console-lede">
            {blocked.length > 0
              ? `${money({ amount: owedBlocked, currency })} is owed to vendors who cannot currently be paid.`
              : "Settlement runs and the statements inside them."}
          </p>
        </div>
      </div>

      <Panel title="Runs" note={runs.data ? plural(runs.data.items.length, "run") : undefined}>
        {runs.isLoading ? (
          <p className="muted">Loading…</p>
        ) : (runs.data?.items ?? []).length === 0 ? (
          <p className="muted">No payout run has been created yet.</p>
        ) : (
          <table className="money-table">
            <tbody>
              {runs.data!.items.map((r) => (
                <tr
                  key={r.id}
                  className={runId === r.id ? "selected" : ""}
                  onClick={() => setRunId(runId === r.id ? "" : r.id)}
                  style={{ cursor: "pointer" }}
                >
                  <td>
                    {new Date(r.period_start).toLocaleDateString()} –{" "}
                    {new Date(r.period_end).toLocaleDateString()}
                  </td>
                  <td>
                    <Tag tone={r.state === "paid" ? "ok" : r.state === "draft" ? undefined : "warn"}>
                      {r.state}
                    </Tag>
                  </td>
                  <td className="faint">{plural(r.statements, "statement")}</td>
                  <td className="faint">
                    {r.approved_by ? `approved by ${r.approved_by}` : "not approved"}
                  </td>
                  <td className="num">{money(r.total)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
        {runId ? (
          <p className="muted">
            Showing one run. <button className="linkish" onClick={() => setRunId("")}>Show all statements</button>
          </p>
        ) : null}
      </Panel>

      <Panel
        title={blocked.length > 0 ? "Blocked" : "Nothing blocked"}
        note={blocked.length > 0 ? money({ amount: owedBlocked, currency }) : undefined}
      >
        {blocked.length === 0 ? (
          <p className="muted">
            Every statement in view can be paid. This panel stays on the page when it is empty on
            purpose — an absent panel and a passing one look identical, and only one of them means
            the check ran.
          </p>
        ) : (
          <table className="money-table">
            <tbody>
              {blocked.map((s) => (
                <tr key={s.id}>
                  <td>
                    <Ref locale={locale} kind="vendors" id={s.vendor_id}>
                      {s.vendor}
                    </Ref>
                  </td>
                  <td className="danger">{s.blocked}</td>
                  <td className="faint">{s.reference_no}</td>
                  <td className="num">{money(s.net)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Panel>

      <Panel title="Statements" note={plural(rows.length, "statement")}>
        {statements.isLoading ? (
          <p className="muted">Loading…</p>
        ) : rows.length === 0 ? (
          <p className="muted">No statements in this run.</p>
        ) : (
          <table className="money-table">
            <thead>
              <tr>
                <th>Vendor</th>
                <th>Period</th>
                <th className="num">Gross</th>
                <th className="num">Commission</th>
                <th className="num">VAT</th>
                <th className="num">Net</th>
                <th>State</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((s) => (
                <tr key={s.id}>
                  <td>
                    <Ref locale={locale} kind="vendors" id={s.vendor_id}>
                      {s.vendor}
                    </Ref>
                    {s.blocked ? <Tag tone="danger" title={s.blocked}>blocked</Tag> : null}
                  </td>
                  <td className="faint">{new Date(s.period_end).toLocaleDateString()}</td>
                  <td className="num">{money(s.gross)}</td>
                  <td className="num faint">−{money(s.commission)}</td>
                  <td className="num faint">−{money(s.vat)}</td>
                  <td className="num">{money(s.net)}</td>
                  <td>
                    <Tag tone={s.state === "paid" ? "ok" : undefined}>{s.state}</Tag>
                    {s.payment_ref ? <span className="faint mono"> {s.payment_ref}</span> : null}
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
