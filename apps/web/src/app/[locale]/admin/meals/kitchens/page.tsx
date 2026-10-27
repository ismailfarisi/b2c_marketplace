"use client";

import { useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { use, useState } from "react";

import { Panel, plural } from "@/components/admin/detail";
import { Tag } from "@/components/admin/table";
import { getMealKitchens } from "@/lib/admin-api";

/** Every branch that has started selling meals: a dish or a plan. */
export default function MealKitchensPage({ params }: { params: Promise<{ locale: string }> }) {
  const { locale } = use(params);
  const [q, setQ] = useState("");
  const [rotation, setRotation] = useState(false);
  const { data, isLoading, error } = useQuery({
    queryKey: ["admin", "meals", "kitchens", q, rotation],
    queryFn: () => getMealKitchens({ q, rotation }),
  });

  if (error) {
    return (
      <p className="danger">
        Kitchens could not be loaded. If this reads &ldquo;forbidden&rdquo;, the account is missing
        <span className="mono"> commerce.read</span>.
      </p>
    );
  }
  const rows = data?.items ?? [];

  return (
    <div className="detail">
      <div className="detail-head">
        <div>
          <h2>Meal kitchens</h2>
          <p className="console-lede">
            {rows.length > 0
              ? `${plural(rows.length, "kitchen")}, ${rows.filter((k) => k.rotation_plans > 0).length} in the rotation.`
              : "No kitchen matches."}
          </p>
        </div>
        <div className="spacer" />
        <div className="filters">
          <label className="field">
            <span>Search</span>
            <input type="search" value={q} placeholder="kitchen or branch" onChange={(e) => setQ(e.target.value)} />
          </label>
          <label className="field">
            <span>Rotation only</span>
            <input type="checkbox" checked={rotation} onChange={(e) => setRotation(e.target.checked)} />
          </label>
        </div>
      </div>

      <Panel title="Kitchens" note={plural(rows.length, "kitchen")}>
        {isLoading ? (
          <p className="muted">Loading…</p>
        ) : rows.length === 0 ? (
          <p className="muted">Nothing here. A branch appears once it adds its first dish or plan.</p>
        ) : (
          <table className="money-table">
            <thead>
              <tr>
                <th>Kitchen</th>
                <th>City</th>
                <th>State</th>
                <th className="num">Dishes</th>
                <th className="num">Live plans</th>
                <th className="num">Rotation</th>
                <th className="num">Subscribers</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((k) => (
                <tr key={k.branch_id}>
                  <td>
                    <Link className="ref" href={`/${locale}/admin/meals/kitchens/${k.branch_id}`}>
                      {k.vendor_name} · {k.branch_name}
                    </Link>
                  </td>
                  <td className="faint">{k.city ?? "—"}</td>
                  <td>
                    {k.vendor_status !== "active" ? (
                      <Tag tone="danger">vendor {k.vendor_status}</Tag>
                    ) : !k.branch_active ? (
                      <Tag tone="warn">branch closed</Tag>
                    ) : k.live_plans === 0 ? (
                      <Tag tone="warn">no live plan</Tag>
                    ) : (
                      <Tag tone="ok">selling</Tag>
                    )}
                  </td>
                  <td className="num">{k.dishes}</td>
                  <td className="num">{k.live_plans}</td>
                  <td className="num">{k.rotation_plans > 0 ? <Tag tone="ok">{k.rotation_plans}</Tag> : 0}</td>
                  <td className="num">{k.active_subscribers}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Panel>
    </div>
  );
}
