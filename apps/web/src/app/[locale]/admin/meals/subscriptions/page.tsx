"use client";

import { useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { use, useState } from "react";

import { Panel, plural } from "@/components/admin/detail";
import { Tag, money } from "@/components/admin/table";
import { SUBSCRIPTION_STATUSES, getMealSubscriptions, statusTone } from "@/lib/admin-api";

export default function MealSubscriptionsPage({
  params,
  searchParams,
}: {
  params: Promise<{ locale: string }>;
  searchParams: Promise<{ branch?: string }>;
}) {
  const { locale } = use(params);
  const branch = use(searchParams).branch ?? "";
  const [q, setQ] = useState("");
  const [status, setStatus] = useState("");
  const { data, isLoading, error } = useQuery({
    queryKey: ["admin", "meals", "subscriptions", q, status, branch],
    queryFn: () => getMealSubscriptions({ q, status, branch_id: branch }),
  });

  if (error) {
    return (
      <p className="danger">
        Subscriptions could not be loaded. If this reads &ldquo;forbidden&rdquo;, the account is missing
        <span className="mono"> commerce.read</span>.
      </p>
    );
  }
  const rows = data?.items ?? [];

  return (
    <div className="detail">
      <div className="detail-head">
        <div>
          <h2>Meal subscriptions</h2>
          <p className="console-lede">
            {branch ? "One kitchen. " : ""}
            {rows.length > 0 ? `Newest ${plural(rows.length, "subscription")}.` : "Nothing matches."}
          </p>
        </div>
        <div className="spacer" />
        <div className="filters">
          <label className="field">
            <span>Search</span>
            <input
              type="search"
              value={q}
              placeholder="number, kitchen, customer id"
              onChange={(e) => setQ(e.target.value)}
            />
          </label>
          <label className="field">
            <span>Status</span>
            <select value={status} onChange={(e) => setStatus(e.target.value)}>
              <option value="">any</option>
              {SUBSCRIPTION_STATUSES.map((s) => (
                <option key={s} value={s}>
                  {s.replace("_", " ")}
                </option>
              ))}
            </select>
          </label>
        </div>
      </div>

      <Panel title="Subscriptions" note={plural(rows.length, "subscription")}>
        {isLoading ? (
          <p className="muted">Loading…</p>
        ) : rows.length === 0 ? (
          <p className="muted">No subscriptions match.</p>
        ) : (
          <table className="money-table">
            <thead>
              <tr>
                <th>Number</th>
                <th>Kitchen</th>
                <th>Plan</th>
                <th>Status</th>
                <th>Started</th>
                <th className="num">Current cycle</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((s) => (
                <tr key={s.id}>
                  <td>
                    <Link className="ref mono" href={`/${locale}/admin/meals/subscriptions/${s.id}`}>
                      {s.subscription_no}
                    </Link>
                  </td>
                  <td>
                    <Link className="ref" href={`/${locale}/admin/meals/kitchens/${s.branch_id}`}>
                      {s.vendor_name} · {s.branch_name}
                    </Link>
                  </td>
                  <td className="faint">{s.plan_name}</td>
                  <td>
                    <Tag tone={statusTone(s.status)}>{s.status.replace("_", " ")}</Tag>
                  </td>
                  <td className="faint">{s.start_date.slice(0, 10)}</td>
                  <td className="num">
                    {s.cycle_no != null && s.cycle_amount_minor != null ? (
                      <>
                        #{s.cycle_no} {money({ amount: s.cycle_amount_minor, currency: s.currency ?? "AED" })}{" "}
                        <span className="faint">{s.cycle_status}</span>
                      </>
                    ) : (
                      <span className="faint">none</span>
                    )}
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
