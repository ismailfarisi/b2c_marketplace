"use client";

import { useQuery, useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { use } from "react";

import { Action, Detail, Facts, Panel, Ref, plural } from "@/components/admin/detail";
import { Tag, money } from "@/components/admin/table";
import { getMealKitchen, setMealPlanActive } from "@/lib/admin-api";
import { SLOTS, WEEKDAYS } from "@/lib/vendor-api";

/**
 * One kitchen as staff see it: what it sells and whether it is ready to.
 * Read-only apart from taking a plan off sale — the menu and prices are the
 * kitchen's to change, from their own portal.
 */
export default function MealKitchenPage({
  params,
}: {
  params: Promise<{ locale: string; branchId: string }>;
}) {
  const { locale, branchId } = use(params);
  const qc = useQueryClient();
  const key = ["admin", "meals", "kitchen", branchId];
  const { data, isLoading, error } = useQuery({ queryKey: key, queryFn: () => getMealKitchen(branchId) });

  if (error) return <p className="danger">This kitchen could not be loaded.</p>;
  if (isLoading || !data) return <p className="muted">Loading kitchen…</p>;

  const { profile, overview, menu, plans, dishes } = data;
  const usedSlots = SLOTS.filter((s) => menu.slots.some((m) => m.slot === s));
  const cell = (weekday: number, slot: string) => menu.slots.find((m) => m.weekday === weekday && m.slot === slot);

  return (
    <Detail
      title={`${profile.vendor_name} · ${profile.name}`}
      subtitle={[profile.city, profile.emirate].filter(Boolean).join(", ") || "no city recorded"}
      tags={
        <>
          <Tag tone={profile.vendor_status === "active" ? "ok" : "danger"}>vendor {profile.vendor_status}</Tag>
          <Tag tone={profile.is_active ? "ok" : "warn"}>{profile.is_active ? "branch open" : "branch closed"}</Tag>
          {overview.menu_gaps.length > 0 ? (
            <Tag tone="warn">{plural(overview.menu_gaps.length, "menu gap")}</Tag>
          ) : null}
        </>
      }
      aside={
        <Panel title="Links">
          <Facts
            rows={[
              ["Vendor", <Ref locale={locale} kind="vendors" id={profile.vendor_id}>compliance & payouts</Ref>],
              [
                "Subscriptions",
                <Link className="ref" href={`/${locale}/admin/meals/subscriptions?branch=${branchId}`}>
                  {plural(overview.active_subscribers, "active subscriber")}
                </Link>,
              ],
            ]}
          />
        </Panel>
      }
    >
      <Panel title="Ready to sell" note={overview.profile_complete && profile.has_service_area ? undefined : "not yet"}>
        <Facts
          rows={[
            ["Profile", <Tag tone={overview.profile_complete ? "ok" : "warn"}>{overview.profile_complete ? "complete" : "incomplete"}</Tag>],
            ["Delivery area", <Tag tone={profile.has_service_area ? "ok" : "warn"}>{profile.has_service_area ? "set" : "none"}</Tag>],
            ["Delivery windows", profile.delivery_windows.map((w) => w.label).join(", ") || null],
            ["Cuisines", profile.cuisines.join(", ") || null],
            ["Dietary", profile.dietary_tags.join(", ") || null],
            ["Active dishes", overview.active_dishes],
            ["Subscribers", `${overview.active_subscribers} active · ${overview.paused_subscribers} paused · ${overview.pending_payment} awaiting payment`],
          ]}
        />
      </Panel>

      <Panel title="Plans" note={plural(plans.length, "plan")}>
        {plans.length === 0 ? (
          <p className="muted">No plans. Customers cannot subscribe to this kitchen.</p>
        ) : (
          <table className="money-table">
            <thead>
              <tr>
                <th>Plan</th>
                <th>Days</th>
                <th className="num">Cycle price</th>
                <th>Rotation</th>
                <th className="num">Subscribers</th>
                <th>Sale</th>
              </tr>
            </thead>
            <tbody>
              {plans.map((p) => (
                <tr key={p.id}>
                  <td>
                    {p.name}
                    <div className="faint">
                      {p.meal_slots.join(", ")} · {p.cycle_days} days · {plural(p.tiers.filter((t) => t.is_active).length, "tier")}
                    </div>
                  </td>
                  <td className="faint">{p.delivery_weekdays.map((d) => WEEKDAYS[d]).join(" ")}</td>
                  <td className="num">{money({ amount: p.price_minor, currency: p.currency })}</td>
                  <td>
                    {p.rotation_enabled && p.rotation_week_price_minor != null ? (
                      <Tag tone="ok">{money({ amount: p.rotation_week_price_minor, currency: p.currency })}/week</Tag>
                    ) : (
                      <span className="faint">no</span>
                    )}
                  </td>
                  <td className="num">{p.active_subscribers}</td>
                  <td>
                    <Tag tone={p.is_active ? "ok" : "warn"}>{p.is_active ? "on sale" : "withdrawn"}</Tag>{" "}
                    <Action
                      label={p.is_active ? "Withdraw" : "Put back on sale"}
                      verb={p.is_active ? "Withdraw plan" : "Restore plan"}
                      danger={p.is_active}
                      hint={
                        p.is_active
                          ? "Stops new sign-ups. Current subscribers keep their meals until they cancel."
                          : "Customers can subscribe again."
                      }
                      onConfirm={async (reason) => {
                        await setMealPlanActive(p.id, !p.is_active, reason);
                        await qc.invalidateQueries({ queryKey: key });
                      }}
                    />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Panel>

      <Panel title="Weekly menu" note={overview.menu_gaps.length > 0 ? plural(overview.menu_gaps.length, "gap") : undefined}>
        {usedSlots.length === 0 ? (
          <p className="muted">Empty. Nothing would be cooked for a subscriber.</p>
        ) : (
          <table className="money-table">
            <thead>
              <tr>
                <th />
                {WEEKDAYS.map((d) => (
                  <th key={d}>{d}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {usedSlots.map((slot) => (
                <tr key={slot}>
                  <td className="faint">{slot}</td>
                  {WEEKDAYS.map((_, wd) => {
                    const c = cell(wd, slot);
                    const gap = overview.menu_gaps.some((g) => g.weekday === wd && g.slot === slot);
                    return (
                      <td key={wd}>
                        {c?.default ? c.default.name : gap ? <Tag tone="warn">gap</Tag> : <span className="faint">—</span>}
                        {c?.alternative ? <div className="faint">or {c.alternative.name}</div> : null}
                      </td>
                    );
                  })}
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Panel>

      <Panel title="Dishes" note={plural(dishes.length, "dish", "dishes")}>
        {dishes.length === 0 ? (
          <p className="muted">No dishes.</p>
        ) : (
          <table className="money-table">
            <tbody>
              {dishes.map((d) => (
                <tr key={d.id}>
                  <td>{d.name}</td>
                  <td className="faint">{d.allergens.length > 0 ? `contains ${d.allergens.join(", ")}` : "no allergens declared"}</td>
                  <td className="faint">{d.calories != null ? `${d.calories} kcal` : ""}</td>
                  <td>{d.is_active ? <Tag tone="ok">active</Tag> : <Tag>archived</Tag>}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Panel>
    </Detail>
  );
}
