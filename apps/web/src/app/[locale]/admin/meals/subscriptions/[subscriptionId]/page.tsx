"use client";

import { useQuery, useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { use } from "react";

import { Action, Detail, Facts, Panel, Ref, plural } from "@/components/admin/detail";
import { Tag, money } from "@/components/admin/table";
import { cancelMealSubscription, getMealSubscription, statusTone } from "@/lib/admin-api";

const day = (d: string) => d.slice(0, 10);

/**
 * One subscription. Cancelling here does what the customer's cancel does —
 * future deliveries stop and the unearned part of a paid cycle is credited —
 * but does not refund money to a card; that stays a separate, deliberate act
 * on the cycle's order.
 */
export default function MealSubscriptionPage({
  params,
}: {
  params: Promise<{ locale: string; subscriptionId: string }>;
}) {
  const { locale, subscriptionId } = use(params);
  const qc = useQueryClient();
  const key = ["admin", "meals", "subscription", subscriptionId];
  const { data, isLoading, error } = useQuery({ queryKey: key, queryFn: () => getMealSubscription(subscriptionId) });

  if (error) return <p className="danger">This subscription could not be loaded.</p>;
  if (isLoading || !data) return <p className="muted">Loading subscription…</p>;

  const closed = data.status === "cancelled" || data.status === "completed";

  return (
    <Detail
      title={data.subscription_no}
      subtitle={`${data.plan_name} · ${data.vendor_name} · ${data.branch_name}`}
      tags={<Tag tone={statusTone(data.status)}>{data.status.replace("_", " ")}</Tag>}
      aside={
        <Panel title="Act">
          <Action
            label="Cancel subscription"
            verb="Cancel subscription"
            danger
            disabled={closed}
            disabledReason={`Already ${data.status}.`}
            hint="Stops future deliveries and credits the unearned part of the paid cycle. Delivered meals stay delivered. No card refund is issued."
            onConfirm={async (reason) => {
              await cancelMealSubscription(subscriptionId, reason);
              await qc.invalidateQueries({ queryKey: key });
            }}
          />
        </Panel>
      }
    >
      <Panel title="Subscription">
        <Facts
          rows={[
            ["Customer", <Ref locale={locale} kind="users" id={data.user_id}>open customer</Ref>],
            [
              "Kitchen",
              <Link className="ref" href={`/${locale}/admin/meals/kitchens/${data.branch_id}`}>
                {data.vendor_name} · {data.branch_name}
              </Link>,
            ],
            ["Started", day(data.start_date)],
            ["Ends", data.end_date ? day(data.end_date) : "renews"],
            ["Pause days", `${data.pause_days_used} of ${data.allowed_pause_days} used`],
            ["Cancelled", data.cancelled_at ? new Date(data.cancelled_at).toLocaleString() : null],
            ["Cancellation reason", data.cancellation_reason],
          ]}
        />
      </Panel>

      <Panel title="Billing cycles" note={plural(data.cycles.length, "cycle")}>
        {data.cycles.length === 0 ? (
          <p className="muted">No cycle yet. The first one opens at checkout.</p>
        ) : (
          <table className="money-table">
            <thead>
              <tr>
                <th>#</th>
                <th>Dates</th>
                <th>Status</th>
                <th className="num">Charged</th>
                <th className="num">Earned so far</th>
                <th>Order</th>
              </tr>
            </thead>
            <tbody>
              {data.cycles.map((c) => (
                <tr key={c.cycle_no}>
                  <td>{c.cycle_no}</td>
                  <td className="faint">
                    {day(c.starts_on)} → {day(c.ends_on)}
                  </td>
                  <td>
                    <Tag tone={c.status === "paid" ? "ok" : undefined}>{c.status}</Tag>
                  </td>
                  <td className="num">{money({ amount: c.amount_minor, currency: c.currency })}</td>
                  <td className="num">{money({ amount: c.recognised_minor, currency: c.currency })}</td>
                  <td>
                    {c.order_id ? (
                      <Ref locale={locale} kind="orders" id={c.order_id}>
                        order
                      </Ref>
                    ) : (
                      <span className="faint">—</span>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Panel>

      <Panel title="Pauses and skips" note={`${plural(data.pauses.length, "pause")} · ${plural(data.skips.length, "skip")}`}>
        {data.pauses.length === 0 && data.skips.length === 0 ? (
          <p className="muted">Never paused or skipped.</p>
        ) : (
          <table className="money-table">
            <tbody>
              {data.pauses.map((p) => (
                <tr key={`p-${p.from_date}`}>
                  <td>pause</td>
                  <td className="faint">
                    {day(p.from_date)} → {day(p.to_date)}
                  </td>
                  <td className="faint">
                    {p.suppressed_days} days · {p.credit_mode}
                  </td>
                  <td>
                    <Tag>{p.state}</Tag>
                  </td>
                </tr>
              ))}
              {data.skips.map((s) => (
                <tr key={`s-${s.skip_date}-${s.meal_slot ?? ""}`}>
                  <td>skip</td>
                  <td className="faint">{day(s.skip_date)}</td>
                  <td className="faint">{s.meal_slot ?? "all meals"}</td>
                  <td className="faint">{s.reason ?? ""}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Panel>

      <Panel title="History" note={plural(data.events.length, "event")}>
        {data.events.length === 0 ? (
          <p className="muted">No events.</p>
        ) : (
          <table className="money-table">
            <tbody>
              {data.events.map((e, i) => (
                <tr key={`${e.occurred_at}-${i}`}>
                  <td className="faint">{new Date(e.occurred_at).toLocaleString()}</td>
                  <td>{e.event_type}</td>
                  <td className="faint">
                    {e.from_status && e.to_status && e.from_status !== e.to_status ? `${e.from_status} → ${e.to_status}` : ""}
                  </td>
                  <td>
                    <Tag>{e.actor_type}</Tag>
                  </td>
                  <td className="faint">{typeof e.metadata?.reason === "string" ? e.metadata.reason : ""}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Panel>
    </Detail>
  );
}
