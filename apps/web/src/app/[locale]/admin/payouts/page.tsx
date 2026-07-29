"use client";

import { useQuery, useQueryClient } from "@tanstack/react-query";
import { use, useState } from "react";

import { Action, Panel, Ref, plural } from "@/components/admin/detail";
import { Tag, money } from "@/components/admin/table";
import {
  approveRun,
  getPayoutRuns,
  getStatements,
  issueRun,
  issueStatement,
  payStatement,
  type IssueRunResult,
  type PayoutStatement,
} from "@/lib/admin-api";

/**
 * Payout runs, and who is not getting paid.
 *
 * The blocked list is the point of this screen. "Why has this vendor not been
 * paid" is otherwise a join across `vendors`, `payout_accounts`, `kyc_cases` and
 * a licence expiry date, and nobody performs it at 5pm on a Thursday — so the
 * vendor is told "it's being looked into" and it is not.
 *
 * The actions come in a deliberate order: approve the run, then issue the
 * statements, then pay each one. Approval does not issue anything and says so —
 * it records that a person looked at the totals, which is the review step, and
 * collapsing it into issuing would mean either reviewing two hundred statements
 * one at a time or reviewing none of them.
 *
 * Every action re-checks eligibility server-side at the moment it runs. A
 * statement issued on Monday and paid on Friday can belong to a vendor whose
 * licence lapsed on Wednesday, and nothing writes to their record when the date
 * passes — so the gate that ran at issue is stale by the time money moves.
 */

export default function PayoutsPage({ params }: { params: Promise<{ locale: string }> }) {
  const { locale } = use(params);
  const [runId, setRunId] = useState<string>("");
  const [lastIssue, setLastIssue] = useState<IssueRunResult | null>(null);
  const queryClient = useQueryClient();

  const invalidate = () => {
    void queryClient.invalidateQueries({ queryKey: ["admin", "payout-runs"] });
    void queryClient.invalidateQueries({ queryKey: ["admin", "statements"] });
  };

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
                  <td onClick={(e) => e.stopPropagation()}>
                    {r.state === "draft" ? (
                      <Action
                        label="Approve"
                        verb="Approve these totals"
                        hint="Approval issues nothing. It records that a person reviewed the numbers before anything was sent to a vendor."
                        onConfirm={async (reason) => {
                          await approveRun(r.id, reason);
                          invalidate();
                        }}
                      />
                    ) : r.state === "approved" ? (
                      <Action
                        label="Issue all"
                        verb="Issue every eligible statement"
                        hint="Blocked vendors are skipped and listed rather than failing the batch — holding nineteen vendors because one licence lapsed teaches people to clear blockers carelessly."
                        onConfirm={async (reason) => {
                          setLastIssue(await issueRun(r.id, reason));
                          invalidate();
                        }}
                      />
                    ) : null}
                  </td>
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

      {lastIssue ? (
        <Panel
          title={`Issued ${lastIssue.issued}`}
          note={lastIssue.skipped.length > 0 ? `${lastIssue.skipped.length} skipped` : "none skipped"}
        >
          {lastIssue.skipped.length === 0 ? (
            <p className="muted">Every draft in the run was issued.</p>
          ) : (
            <table className="money-table">
              <tbody>
                {lastIssue.skipped.map((sk) => (
                  <tr key={sk.statement_id}>
                    <td>
                      <Ref locale={locale} kind="vendors" id={sk.vendor_id}>
                        {sk.vendor}
                      </Ref>
                    </td>
                    <td className="danger">{sk.reason}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </Panel>
      ) : null}

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
                <th />
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
                  <td>
                    <StatementActions statement={s} onDone={invalidate} />
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

/**
 * The per-statement actions.
 *
 * A blocked statement shows no button at all rather than a disabled one. The
 * reason is already in the row beside it, and a greyed-out Pay next to a stated
 * reason invites someone to go looking for the override — which is exactly the
 * behaviour the block exists to prevent.
 */
function StatementActions({
  statement,
  onDone,
}: {
  statement: PayoutStatement;
  onDone: () => void;
}) {
  const [bankRef, setBankRef] = useState("");

  if (statement.blocked) return null;

  if (statement.state === "draft") {
    return (
      <Action
        label="Issue"
        verb="Issue this statement"
        hint="The vendor can reconcile against it from this point. Correcting an issued statement means a credit note, not an edit."
        onConfirm={async (reason) => {
          await issueStatement(statement.id, reason);
          onDone();
        }}
      />
    );
  }

  if (statement.state === "issued") {
    return (
      <Action
        label="Pay"
        verb="Record this payment"
        danger
        hint="This posts the ledger. The transfer itself happens in the bank — record the reference the bank gave you, not one you invent."
        extra={(busy) => (
          <label className="field">
            <span>Bank reference</span>
            <input
              value={bankRef}
              onChange={(e) => setBankRef(e.target.value)}
              disabled={busy}
              placeholder="e.g. FTS-99201"
              className="mono"
            />
          </label>
        )}
        onConfirm={async (reason) => {
          await payStatement(statement.id, { payment_ref: bankRef, reason });
          setBankRef("");
          onDone();
        }}
      />
    );
  }

  return null;
}
