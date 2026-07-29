"use client";

import { useQuery } from "@tanstack/react-query";
import { use, useState } from "react";

import { Panel, Ref, plural } from "@/components/admin/detail";
import { Tag, money } from "@/components/admin/table";
import { getInvoices, getRefunds } from "@/lib/admin-api";

/**
 * Refunds and invoices — the two documents that get asked about after the fact.
 *
 * The refund list is the one nobody wants to look at and everybody eventually
 * does, usually because someone outside the company has asked a question. Its
 * value is entirely in two columns: the reason, and who authorised it.
 *
 * A refund with no actor is shown, loudly. It means no `admin_actions` row
 * matched — the refund was issued by something other than a person: a webhook, a
 * script, a migration. That may be completely fine and it is always the row
 * worth asking about, so hiding it to keep the table tidy would remove the only
 * reason to open the page.
 */

export default function RefundsPage({ params }: { params: Promise<{ locale: string }> }) {
  const { locale } = use(params);
  const [from, setFrom] = useState("");
  const [to, setTo] = useState("");
  const [invoiceQuery, setInvoiceQuery] = useState("");

  const refunds = useQuery({
    queryKey: ["admin", "refunds", from, to],
    queryFn: () => getRefunds({ from, to }),
  });

  const invoices = useQuery({
    queryKey: ["admin", "invoices", invoiceQuery, from, to],
    queryFn: () => getInvoices({ q: invoiceQuery, from, to }),
  });

  if (refunds.error) {
    return (
      <p className="danger">
        Refunds could not be loaded. If this reads &ldquo;forbidden&rdquo;, the account is missing
        <span className="mono"> money.read</span>.
      </p>
    );
  }

  const rows = refunds.data?.items ?? [];
  const unattributed = rows.filter((r) => !r.actor);
  const total = rows.reduce((n, r) => n + r.amount.amount, 0);
  const currency = rows[0]?.amount.currency ?? "AED";

  return (
    <div className="detail">
      <div className="detail-head">
        <div>
          <h2>
            Refunds
            {unattributed.length > 0 ? (
              <Tag tone="warn">{unattributed.length} with no named actor</Tag>
            ) : null}
          </h2>
          <p className="console-lede">
            {rows.length > 0
              ? `${money({ amount: total, currency })} returned across ${plural(rows.length, "refund")}.`
              : "Nothing has been refunded in this range."}
          </p>
        </div>
        <div className="spacer" />
        <div className="filters">
          <label className="field">
            <span>From</span>
            <input type="date" value={from} onChange={(e) => setFrom(e.target.value)} />
          </label>
          <label className="field">
            <span>To</span>
            <input type="date" value={to} onChange={(e) => setTo(e.target.value)} />
          </label>
        </div>
      </div>

      <Panel title="Refunds" note={plural(rows.length, "refund")}>
        {refunds.isLoading ? (
          <p className="muted">Loading…</p>
        ) : rows.length === 0 ? (
          <p className="muted">Nothing refunded in this range.</p>
        ) : (
          <table className="money-table">
            <thead>
              <tr>
                <th>Order</th>
                <th>Vendor</th>
                <th className="num">Amount</th>
                <th>Reason</th>
                <th>Authorised by</th>
                <th>When</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((r) => (
                <tr key={r.id} className={r.actor ? "" : "unattributed"}>
                  <td>
                    <Ref locale={locale} kind="orders" id={r.order_id}>
                      {r.order_no}
                    </Ref>
                  </td>
                  <td className="faint">{r.vendor}</td>
                  <td className="num">{money(r.amount)}</td>
                  <td>{r.reason ?? <span className="faint">none recorded</span>}</td>
                  <td>
                    {r.actor ? (
                      <span className="mono">{r.actor}</span>
                    ) : (
                      <Tag tone="warn" title="No admin action matched this refund">
                        not a person
                      </Tag>
                    )}
                  </td>
                  <td className="faint">{new Date(r.at).toLocaleString()}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Panel>

      <Panel
        title="Invoices"
        note={
          <input
            type="search"
            value={invoiceQuery}
            placeholder="number, order no, or buyer"
            onChange={(e) => setInvoiceQuery(e.target.value)}
          />
        }
      >
        {invoices.isLoading ? (
          <p className="muted">Loading…</p>
        ) : (invoices.data?.items ?? []).length === 0 ? (
          <p className="muted">
            No invoices match. An order only produces one once the invoicing job has run for it.
          </p>
        ) : (
          <table className="money-table">
            <thead>
              <tr>
                <th>Number</th>
                <th>Kind</th>
                <th>Buyer</th>
                <th className="num">Net</th>
                <th className="num">Tax</th>
                <th className="num">Gross</th>
                <th>Issued</th>
              </tr>
            </thead>
            <tbody>
              {invoices.data!.items.map((i) => (
                <tr key={i.id}>
                  <td className="mono">
                    {i.number}
                    {/* A credit note that does not name what it corrects is not
                        a credit note — it is a second invoice with a negative
                        total. So the link is shown, always. */}
                    {i.corrects_number ? (
                      <span className="faint"> corrects {i.corrects_number}</span>
                    ) : null}
                  </td>
                  <td>
                    <Tag tone={i.kind === "credit_note" ? "warn" : undefined}>{i.kind}</Tag>
                  </td>
                  <td className="faint">{i.buyer ?? "—"}</td>
                  <td className="num">{money(i.net)}</td>
                  <td className="num faint">{money(i.tax)}</td>
                  <td className="num">{money(i.gross)}</td>
                  <td className="faint">
                    {new Date(i.issued_at).toLocaleDateString()}
                    {!i.has_pdf ? <span className="danger"> · no PDF</span> : null}
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
