"use client";

import { formatMoney } from "@marketplace/api-client";
import { useQuery } from "@tanstack/react-query";

import { Panel } from "@/components/admin/detail";
import { label } from "@/components/kitchen/copy";
import { ConsoleError, useBranch } from "@/components/kitchen/portal";
import { listStatements } from "@/lib/api";

/**
 * Weekly statements. Vendor-wide rather than per branch, because a payout goes
 * to the vendor's bank account, not to a kitchen.
 */
export default function PayoutsPage() {
  const branch = useBranch();
  const { t, locale } = branch;
  const c = t.payouts;
  const { data, error, isPending, refetch } = useQuery({
    queryKey: ["vendor", "statements", branch.vendor_id],
    queryFn: () => listStatements(branch.vendor_id),
  });
  const items = data?.items ?? [];
  const m = (amount: number, currency: string) => formatMoney({ amount, currency }, locale === "ar" ? "ar" : "en");

  return (
    <Panel title={c.title} note={c.note}>
      {error ? (
        <ConsoleError error={error} onRetry={() => void refetch()} locale={locale} />
      ) : isPending ? (
        <div className="skeleton" style={{ inlineSize: "40%" }} />
      ) : items.length === 0 ? (
        <div className="state">{c.empty}</div>
      ) : (
        <div className="table-wrap">
          <table className="data">
            <thead>
              <tr>
                <th>{c.reference}</th>
                <th>{c.period}</th>
                <th className="num">{c.gross}</th>
                <th className="num">{c.commission}</th>
                <th className="num">{c.vat}</th>
                <th className="num">{c.adjustments}</th>
                <th className="num">{c.net}</th>
                <th>{c.state}</th>
              </tr>
            </thead>
            <tbody>
              {items.map((s) => (
                <tr key={s.id}>
                  <td className="mono">{s.reference_no}</td>
                  <td className="nowrap" dir="ltr">
                    {s.period_start} – {s.period_end}
                  </td>
                  <td className="num">{m(s.gross_minor, s.currency)}</td>
                  <td className="num">{m(-s.commission_minor, s.currency)}</td>
                  <td className="num">{m(-s.vat_minor, s.currency)}</td>
                  <td className="num">{m(s.adjustments_minor, s.currency)}</td>
                  <td className="num">
                    <strong>{m(s.net_minor, s.currency)}</strong>
                  </td>
                  <td>
                    <span className={`tag ${s.state === "paid" ? "ok" : ""}`}>{label(t.statuses, s.state)}</span>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </Panel>
  );
}
