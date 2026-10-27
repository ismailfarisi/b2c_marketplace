"use client";

import { formatMoney } from "@marketplace/api-client";
import { useQuery } from "@tanstack/react-query";

import { Panel } from "@/components/admin/detail";
import { errorText, useBranch } from "@/components/kitchen/portal";
import { listStatements } from "@/lib/api";

/**
 * Weekly statements. Vendor-wide rather than per branch, because a payout goes
 * to the vendor's bank account, not to a kitchen.
 */
export default function PayoutsPage() {
  const branch = useBranch();
  const { data, error, isPending } = useQuery({
    queryKey: ["vendor", "statements", branch.vendor_id],
    queryFn: () => listStatements(branch.vendor_id),
  });
  const items = data?.items ?? [];
  const m = (amount: number, currency: string) => formatMoney({ amount, currency }, "en");

  return (
    <Panel title="Statements" note="Commission and VAT are deducted before payout">
      {error ? (
        <div className="state error">{errorText(error)}</div>
      ) : isPending ? (
        <div className="skeleton" style={{ inlineSize: "40%" }} />
      ) : items.length === 0 ? (
        <div className="state">No statements yet. The first closes at the end of your first week of deliveries.</div>
      ) : (
        <div className="table-wrap">
          <table className="data">
            <thead>
              <tr>
                <th>Reference</th>
                <th>Period</th>
                <th className="num">Gross</th>
                <th className="num">Commission</th>
                <th className="num">VAT</th>
                <th className="num">Adjustments</th>
                <th className="num">Net</th>
                <th>State</th>
              </tr>
            </thead>
            <tbody>
              {items.map((s) => (
                <tr key={s.id}>
                  <td className="mono">{s.reference_no}</td>
                  <td className="nowrap">
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
                    <span className={`tag ${s.state === "paid" ? "ok" : ""}`}>{s.state}</span>
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
