"use client";

import { useQuery, useQueryClient } from "@tanstack/react-query";
import { use, useState } from "react";

import { Action, Detail, Facts, Panel, Ref, TimelinePanel, plural } from "@/components/admin/detail";
import { Tag, money } from "@/components/admin/table";
import {
  changePhone,
  eraseAccount,
  getCustomerDetail,
  getCustomerTimeline,
  mergeAccounts,
  type MergeResult,
} from "@/lib/admin-api";

/**
 * The customer page.
 *
 * Ordered by what an agent needs before they can help: identity and standing
 * first, then history, then addresses, then what was sent to them. Standing is
 * near the top on purpose — an agent who apologises warmly for a delay and then
 * discovers the account has three active strikes has already given away the
 * platform's position.
 *
 * The three account actions live in the header, and each is mostly a refusal.
 * A merge will not run against an account with an order in flight; an erasure
 * will not run against one with an open dispute that might owe money; a phone
 * change will not silently absorb a number that already belongs to someone.
 * Those refusals are the feature — the happy paths are three UPDATEs.
 *
 * A merge deliberately leaves orders and invoices where they are. An invoice
 * names a buyer, and one whose buyer changed after issue is a different document
 * from the one the customer holds and the one the tax authority has. The count
 * of what stayed is shown after the merge rather than hidden, because the
 * customer will ask.
 */

export default function CustomerPage({
  params,
}: {
  params: Promise<{ locale: string; userId: string }>;
}) {
  const { locale, userId } = use(params);
  const queryClient = useQueryClient();
  const [newPhone, setNewPhone] = useState("");
  const [survivor, setSurvivor] = useState("");
  const [merged, setMerged] = useState<MergeResult | null>(null);

  const invalidate = () => {
    void queryClient.invalidateQueries({ queryKey: ["admin", "customer", userId] });
    void queryClient.invalidateQueries({ queryKey: ["admin", "customer", userId, "timeline"] });
  };

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
      actions={
        <div className="detail-actions">
          <Action
            label="Change number"
            verb="Change it"
            hint="The new number is unverified until they sign in with it, and every existing session is revoked — they will be signed out on every device."
            extra={(busy) => (
              <label className="field">
                <span>New number, international format</span>
                <input
                  value={newPhone}
                  onChange={(e) => setNewPhone(e.target.value)}
                  placeholder="+9715XXXXXXXX"
                  disabled={busy}
                  className="mono"
                />
              </label>
            )}
            onConfirm={async (reason) => {
              await changePhone(userId, { phone: newPhone.trim(), reason });
              setNewPhone("");
              invalidate();
            }}
          />
          <Action
            label="Merge away"
            verb="Merge this account into the other"
            danger
            hint="This account is the one that stops. Orders and invoices stay with it — only addresses, favourites and saved searches move."
            extra={(busy) => (
              <label className="field">
                <span>Surviving account id</span>
                <input
                  value={survivor}
                  onChange={(e) => setSurvivor(e.target.value)}
                  placeholder="the account that keeps going"
                  disabled={busy}
                  className="mono"
                />
              </label>
            )}
            onConfirm={async (reason) => {
              setMerged(await mergeAccounts(userId, { survivor_id: survivor.trim(), reason }));
              setSurvivor("");
              invalidate();
            }}
          />
          <Action
            label="Erase"
            verb="Erase this person"
            danger
            hint="Name, email, phone, addresses and device tokens go and cannot be recovered. Invoices and ledger entries are retained as the law requires, attached to a subject that no longer resolves to a person."
            onConfirm={async (reason) => {
              await eraseAccount(userId, reason);
              invalidate();
            }}
          />
        </div>
      }
      aside={
        <TimelinePanel
          queryKey={["admin", "customer", userId, "timeline"]}
          fetcher={() => getCustomerTimeline(userId)}
        />
      }
    >
      {merged ? (
        <p className="alarm">
          Merged. {merged.moved.addresses} address(es), {merged.moved.favourites} favourite(s) and{" "}
          {merged.moved.searches} saved search(es) moved across.{" "}
          <strong>
            {merged.stayed.orders} order(s) and {merged.stayed.invoices} invoice(s) stayed here
          </strong>{" "}
          — an invoice whose buyer changed after issue is a different document from the one the
          customer holds. Their full history is visible in this console, not in the app.
        </p>
      ) : null}
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
