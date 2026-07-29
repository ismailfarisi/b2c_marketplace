"use client";

import { useQuery } from "@tanstack/react-query";
import { use } from "react";

import { Detail, Facts, Panel, Ref, TimelinePanel, plural } from "@/components/admin/detail";
import { Tag, money } from "@/components/admin/table";
import { getCustomerDetail, getCustomerTimeline } from "@/lib/admin-api";

/**
 * The customer page.
 *
 * Ordered by what an agent needs before they can help: identity and standing
 * first, then history, then addresses, then what was sent to them. Standing is
 * near the top on purpose — an agent who apologises warmly for a delay and then
 * discovers the account has three active strikes has already given away the
 * platform's position.
 *
 * Nothing here is editable, and that is deliberate for now. Every mutation the
 * console offers goes through the service that owns the data, and the customer
 * mutations that matter — merging duplicate accounts, forcing a phone change,
 * deletion under a data request — do not have safe methods behind them yet.
 * A form that half-worked would be worse than no form.
 */

export default function CustomerPage({
  params,
}: {
  params: Promise<{ locale: string; userId: string }>;
}) {
  const { locale, userId } = use(params);

  const { data, isLoading, error } = useQuery({
    queryKey: ["admin", "customer", userId],
    queryFn: () => getCustomerDetail(userId),
  });

  if (error) return <p className="danger">This customer could not be loaded.</p>;
  if (isLoading || !data) return <p className="muted">Loading customer…</p>;

  const spend = data.orders
    .filter((o) => o.status !== "cancelled")
    .reduce((sum, o) => sum + o.total.amount, 0);
  const currency = data.orders[0]?.total.currency ?? "AED";

  return (
    <Detail
      title={data.display_name ?? data.phone ?? "Customer"}
      subtitle={`joined ${new Date(data.created_at).toLocaleDateString()} · ${data.country} · ${data.locale}`}
      tags={
        <>
          <Tag tone={data.status === "active" ? "ok" : "danger"}>{data.status}</Tag>
          {data.standing.active_strikes > 0 ? (
            <Tag tone="danger">{data.standing.active_strikes} strikes</Tag>
          ) : null}
          {data.standing.open_appeals > 0 ? (
            <Tag tone="warn">{data.standing.open_appeals} appeals open</Tag>
          ) : null}
        </>
      }
      aside={
        <TimelinePanel
          queryKey={["admin", "customer", userId, "timeline"]}
          fetcher={() => getCustomerTimeline(userId)}
        />
      }
    >
      <Panel title="Identity">
        <Facts
          rows={[
            ["Phone", data.phone ? <span className="mono">{data.phone}</span> : null],
            ["Email", data.email],
            ["App language", data.locale],
            // Two locales, because they diverge and the divergence is the bug:
            // an app set to Arabic and notifications still going out in English
            // is a customer who cannot read their delivery code.
            [
              "Message language",
              data.notification_locale && data.notification_locale !== data.locale ? (
                <Tag tone="warn" title="Messages go out in a different language to the app">
                  {data.notification_locale}
                </Tag>
              ) : (
                (data.notification_locale ?? data.locale)
              ),
            ],
            ["Blocked by", data.blocked_count > 0 ? `${data.blocked_count} people` : null],
            ["User id", <span className="mono faint">{data.id}</span>],
          ]}
        />
      </Panel>

      <Panel
        title="Orders"
        note={`${plural(data.orders.length, "order")} · ${money({ amount: spend, currency })} not cancelled`}
      >
        {data.orders.length === 0 ? (
          <p className="muted">Never ordered.</p>
        ) : (
          <table className="money-table">
            <tbody>
              {data.orders.map((o) => (
                <tr key={o.id}>
                  <td>
                    <Ref locale={locale} kind="orders" id={o.id}>
                      {o.order_no}
                    </Ref>
                  </td>
                  <td>
                    <Tag tone={o.status === "cancelled" ? "danger" : o.status === "completed" ? "ok" : undefined}>
                      {o.status}
                    </Tag>
                  </td>
                  <td className="faint">{o.branch}</td>
                  <td className="faint">{new Date(o.placed_at).toLocaleDateString()}</td>
                  <td className="num">{money(o.total)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Panel>

      {data.subscriptions && data.subscriptions.length > 0 ? (
        <Panel title="Subscriptions">
          <table className="money-table">
            <tbody>
              {data.subscriptions.map((s) => (
                <tr key={s.id}>
                  <td>{s.plan}</td>
                  <td>
                    <Tag tone={s.status === "active" ? "ok" : undefined}>{s.status}</Tag>
                  </td>
                  <td className="faint">
                    {s.next ? `scheduled through ${new Date(s.next).toLocaleDateString()}` : "not scheduled"}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </Panel>
      ) : null}

      <Panel title="Addresses" note={plural(data.addresses.length, "address", "addresses")}>
        {data.addresses.length === 0 ? (
          <p className="muted">No saved addresses.</p>
        ) : (
          <ul className="addresses">
            {data.addresses.map((a) => (
              <li key={a.id}>
                <div>
                  <strong>{a.label ?? "Address"}</strong>
                  {a.is_default ? <Tag tone="ok">default</Tag> : null}
                </div>
                <div>{a.line}</div>
                <div className="faint">
                  {[a.area, a.city].filter(Boolean).join(", ")}
                  {a.recipient ? ` · for ${a.recipient}` : ""}
                </div>
              </li>
            ))}
          </ul>
        )}
      </Panel>

      <Panel title="What we sent them" note={plural(data.notifications.length, "message")}>
        {data.notifications.length === 0 ? (
          <p className="muted">Nothing has been sent to this customer.</p>
        ) : (
          <table className="money-table">
            <tbody>
              {data.notifications.map((n, i) => (
                <tr key={`${n.at}-${i}`}>
                  <td className="mono">{n.key}</td>
                  <td>{n.channel}</td>
                  <td className="faint">{n.destination}</td>
                  <td>
                    {n.suppressed ? (
                      <Tag tone="danger" title={n.suppressed}>
                        suppressed
                      </Tag>
                    ) : (
                      <Tag tone={n.status === "sent" ? "ok" : undefined}>{n.status}</Tag>
                    )}
                  </td>
                  <td className="faint">{new Date(n.at).toLocaleString()}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Panel>
    </Detail>
  );
}
