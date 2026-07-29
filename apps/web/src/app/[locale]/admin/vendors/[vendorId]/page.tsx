"use client";

import { useQuery } from "@tanstack/react-query";
import { use } from "react";

import { Detail, Facts, Panel, TimelinePanel, plural } from "@/components/admin/detail";
import { Tag, money } from "@/components/admin/table";
import { getVendorDetail, getVendorTimeline } from "@/lib/admin-api";

/**
 * The vendor page.
 *
 * Compliance first, and everything else after. The order is not stylistic: the
 * question that brings someone to this page is usually "can we pay them", and
 * that is answered by three things — is the trade licence current, has KYC been
 * approved, is there a payout account — which are otherwise spread across three
 * screens and a database.
 *
 * The payable is read from `ledger_entries` at request time and never from a
 * cached column. A stored balance that drifts from the journal is the one
 * number in this system nobody can afford to have wrong, and the way it drifts
 * is somebody optimising exactly this page.
 */

export default function VendorPage({
  params,
}: {
  params: Promise<{ locale: string; vendorId: string }>;
}) {
  const { vendorId } = use(params);

  const { data, isLoading, error } = useQuery({
    queryKey: ["admin", "vendor", vendorId],
    queryFn: () => getVendorDetail(vendorId),
  });

  if (error) return <p className="danger">This vendor could not be loaded.</p>;
  if (isLoading || !data) return <p className="muted">Loading vendor…</p>;

  const licenceExpired = data.trade_licence?.expired ?? false;
  const payable = data.compliance.payout_account === "approved";

  return (
    <Detail
      title={data.trade_name}
      subtitle={`${data.legal_name ?? "no legal name recorded"} · ${data.country} · joined ${new Date(
        data.created_at,
      ).toLocaleDateString()}`}
      tags={
        <>
          <Tag tone={data.status === "active" ? "ok" : "danger"}>{data.status}</Tag>
          {licenceExpired ? <Tag tone="danger">licence expired</Tag> : null}
          {data.open_disputes > 0 ? <Tag tone="warn">{data.open_disputes} disputes</Tag> : null}
        </>
      }
      aside={
        <TimelinePanel
          queryKey={["admin", "vendor", vendorId, "timeline"]}
          fetcher={() => getVendorTimeline(vendorId)}
        />
      }
    >
      <Panel
        title="Can we pay them"
        note={payable ? undefined : "not yet"}
      >
        <Facts
          rows={[
            [
              "Owed to them",
              <strong className={data.payable.amount > 0 ? "" : "faint"}>{money(data.payable)}</strong>,
            ],
            [
              "Identity check",
              <Tag tone={data.compliance.kyc_state === "approved" ? "ok" : "warn"}>
                {data.compliance.kyc_state ?? "never started"}
              </Tag>,
            ],
            [
              "Payout account",
              <Tag tone={payable ? "ok" : "warn"}>{data.compliance.payout_account ?? "none"}</Tag>,
            ],
            [
              "Trade licence",
              data.trade_licence?.number ? (
                <>
                  <span className="mono">{data.trade_licence.number}</span>{" "}
                  {licenceExpired ? (
                    <Tag tone="danger">expired {data.trade_licence.expires}</Tag>
                  ) : (
                    <span className="faint">until {data.trade_licence.expires}</span>
                  )}
                </>
              ) : null,
            ],
            ["Tax registration", data.tax_registration ? <span className="mono">{data.tax_registration}</span> : null],
            ["Settlement currency", data.currency],
          ]}
        />
        {!payable ? (
          <p className="muted">
            A payout run will skip this vendor until the account is approved. The money is still theirs — it
            accrues in the ledger either way.
          </p>
        ) : null}
      </Panel>

      <Panel title="Statements" note={data.statements ? plural(data.statements.length, "statement") : undefined}>
        {!data.statements || data.statements.length === 0 ? (
          <p className="muted">Never paid out. Expected for a vendor whose account is not approved yet.</p>
        ) : (
          <table className="money-table">
            <tbody>
              {data.statements.map((s) => (
                <tr key={s.id}>
                  <td className="mono">{s.reference_no}</td>
                  <td>
                    <Tag tone={s.state === "paid" ? "ok" : undefined}>{s.state}</Tag>
                  </td>
                  <td className="faint">to {new Date(s.period_end).toLocaleDateString()}</td>
                  <td className="num">{money(s.net)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Panel>

      <Panel title="Branches" note={plural(data.branches.length, "branch", "branches")}>
        {data.branches.length === 0 ? (
          <p className="muted">
            No branches. A vendor without one cannot receive an order — nothing has capacity to reserve
            against.
          </p>
        ) : (
          <table className="money-table">
            <tbody>
              {data.branches.map((b) => (
                <tr key={b.id}>
                  <td>{b.name}</td>
                  <td className="faint">{b.city}</td>
                  <td>
                    <Tag tone={b.is_active ? "ok" : "danger"}>{b.is_active ? "open" : "closed"}</Tag>
                  </td>
                  <td className="faint">
                    {b.drivers > 0 ? `${b.drivers} drivers` : "no drivers of their own"}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Panel>

      <Panel title="Reach">
        <Facts
          rows={[
            ["Published reels", data.reels.published],
            [
              "Awaiting review",
              data.reels.pending_review > 0 ? (
                <Tag tone="warn">{data.reels.pending_review}</Tag>
              ) : (
                0
              ),
            ],
            [
              "Last published",
              data.reels.last_published ? new Date(data.reels.last_published).toLocaleString() : null,
            ],
            ["Rating", data.rating ?? null],
            ["Storefront", <span className="mono faint">/{data.slug}</span>],
          ]}
        />
      </Panel>

      <Panel title="Who can act for them" note={plural(data.staff.length, "person", "people")}>
        {data.staff.length === 0 ? (
          <p className="muted">Nobody. This vendor cannot log in to their own console.</p>
        ) : (
          <table className="money-table">
            <tbody>
              {data.staff.map((s) => (
                <tr key={s.user_id}>
                  <td>{s.name ?? "—"}</td>
                  <td className="mono faint">{s.phone}</td>
                  <td>
                    <Tag>{s.role}</Tag>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Panel>
    </Detail>
  );
}
