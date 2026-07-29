"use client";

import { useQuery, useQueryClient } from "@tanstack/react-query";
import { use, useState } from "react";

import {
  Action,
  Detail,
  Facts,
  Panel,
  Ref,
  TimelinePanel,
  plural,
  type Fact,
} from "@/components/admin/detail";
import { Tag, money } from "@/components/admin/table";
import {
  cancelOrder,
  getOrderDetail,
  getOrderTimeline,
  refundOrder,
  type OrderDetail,
} from "@/lib/admin-api";

/**
 * The order detail page — the one an agent opens with a customer on the phone.
 *
 * Laid out in the order the questions actually arrive. Not "what is in the
 * database about this order", which would put the uuid first, but: what was
 * ordered, what was charged, where is it, and did we tell them anything. An
 * agent reading top to bottom has answered the call by the time they reach the
 * bottom of the first panel.
 *
 * The delivery panel is the one that earns its space. It carries the pair
 * `otp_issued` / `otp_sent` separately, because those differing is the exact
 * failure that strands a driver at a door with a customer who never got a code
 * — and it is invisible in every other view of the same order.
 */

export default function OrderPage({
  params,
}: {
  params: Promise<{ locale: string; orderId: string }>;
}) {
  const { locale, orderId } = use(params);
  const queryClient = useQueryClient();
  const [amount, setAmount] = useState("");

  const { data, isLoading, error } = useQuery({
    queryKey: ["admin", "order", orderId],
    queryFn: () => getOrderDetail(orderId),
  });

  const invalidate = () => {
    void queryClient.invalidateQueries({ queryKey: ["admin", "order", orderId] });
    void queryClient.invalidateQueries({ queryKey: ["admin", "order", orderId, "timeline"] });
  };

  if (error) return <p className="danger">This order could not be loaded.</p>;
  if (isLoading || !data) return <p className="muted">Loading order…</p>;

  const settled = data.status === "cancelled" || data.status === "completed";
  const captured = data.payments.some((p) => p.kind === "capture" && p.status === "succeeded");

  return (
    <Detail
      title={data.order_no}
      subtitle={`${data.service_type.replace(/_/g, " ")} · placed ${new Date(data.placed_at).toLocaleString()}`}
      tags={
        <>
          <Tag tone={statusTone(data.status)}>{data.status}</Tag>
          {data.kind !== "one_off" ? <Tag>{data.kind}</Tag> : null}
        </>
      }
      actions={
        <div className="detail-actions">
          <Action
            label="Refund"
            verb="Refund this order"
            danger
            disabled={!captured}
            disabledReason="Nothing has been captured on this order yet."
            hint="Leave the amount blank to refund everything still refundable. The platform commission comes back proportionally."
            extra={(busy) => (
              <label className="field">
                <span>Amount ({data.currency}) — blank refunds the rest</span>
                <input
                  type="number"
                  min="0"
                  step="0.01"
                  value={amount}
                  onChange={(event) => setAmount(event.target.value)}
                  disabled={busy}
                  placeholder="everything"
                />
              </label>
            )}
            onConfirm={async (reason) => {
              // Parsed from the major unit the operator typed. Rounding here
              // rather than trusting a float: 12.35 * 100 is 1234.9999… and a
              // refund one fils short of the capture never clears the "fully
              // refunded" check, leaving the quote stuck in `captured` forever.
              const major = amount.trim() ? Number(amount) : 0;
              await refundOrder(orderId, {
                ...(major > 0 ? { amount_minor: Math.round(major * 100) } : {}),
                reason,
              });
              setAmount("");
              invalidate();
            }}
          />
          <Action
            label="Cancel"
            verb="Cancel this order"
            danger
            disabled={settled}
            disabledReason={`A ${data.status} order cannot be cancelled.`}
            hint="Releases the delivery slot and drops any stop that has not happened. This does not refund anything — refund separately if it is owed."
            onConfirm={async (reason) => {
              await cancelOrder(orderId, reason);
              invalidate();
            }}
          />
        </div>
      }
      aside={
        <TimelinePanel
          queryKey={["admin", "order", orderId, "timeline"]}
          fetcher={() => getOrderTimeline(orderId)}
        />
      }
    >
      <Panel title="Who and where">
        <Facts rows={whoRows(locale, data)} />
      </Panel>

      <Panel title="What was ordered">
        {data.lines.length === 0 ? (
          <p className="muted">No lines recorded.</p>
        ) : (
          <ul className="lines">
            {data.lines.map((line, i) => (
              <li key={`${line.name}-${i}`}>
                <span className="qty">×{line.quantity}</span>
                <span>{line.name}</span>
                {line.options ? <span className="faint mono">{line.options}</span> : null}
              </li>
            ))}
          </ul>
        )}
        {data.notes ? <p className="note">“{data.notes}”</p> : null}
      </Panel>

      {data.quote ? (
        <Panel
          title="What was charged"
          note={data.quote.tax_inclusive ? "prices include VAT" : "VAT added on top"}
        >
          <table className="money-table">
            <tbody>
              {data.quote.lines.map((line, i) => (
                <tr key={`${line.kind}-${i}`}>
                  <td>{line.label}</td>
                  <td className="faint">{(line.tax_rate_bp / 100).toFixed(0)}% VAT</td>
                  <td className="num">{money(line.amount)}</td>
                </tr>
              ))}
              {data.quote.discount.amount > 0 ? (
                <tr>
                  <td colSpan={2}>Discount</td>
                  <td className="num">−{money(data.quote.discount)}</td>
                </tr>
              ) : null}
              <tr className="total">
                <td colSpan={2}>Total</td>
                <td className="num">{money(data.quote.total)}</td>
              </tr>
              <tr>
                <td colSpan={2} className="faint">
                  of which VAT
                </td>
                <td className="num faint">{money(data.quote.tax)}</td>
              </tr>
            </tbody>
          </table>
          <p className="muted">
            Quote state <Tag code>{data.quote.state}</Tag>
          </p>
        </Panel>
      ) : null}

      <Panel title="Money movements" note={plural(data.payments.length, "movement")}>
        {data.payments.length === 0 ? (
          <p className="muted">Nothing has been charged. A cash order stays like this until delivery.</p>
        ) : (
          <table className="money-table">
            <tbody>
              {data.payments.map((p) => (
                <tr key={p.id}>
                  <td>
                    <Tag tone={p.status === "succeeded" ? "ok" : p.status === "failed" ? "danger" : undefined}>
                      {p.kind}
                    </Tag>
                  </td>
                  <td>
                    {p.provider}
                    {p.failure_code ? <span className="danger"> · {p.failure_code}</span> : null}
                    {p.three_ds ? <span className="faint"> · 3DS {p.three_ds}</span> : null}
                  </td>
                  <td className="faint">
                    {p.settled_at ? `settled ${new Date(p.settled_at).toLocaleString()}` : "unsettled"}
                  </td>
                  <td className="num">{money(p.amount)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Panel>

      <Panel title="Delivery" note={plural(data.stops.length, "stop")}>
        {data.stops.length === 0 ? (
          <p className="muted">No stops scheduled.</p>
        ) : (
          <ol className="stops">
            {data.stops.map((stop) => (
              <li key={stop.id}>
                <div className="stop-head">
                  <Tag tone={stopTone(stop.status)}>{stop.kind}</Tag>
                  <strong>{stop.status}</strong>
                  <span className="faint">
                    {stop.date} · {stop.window}
                  </span>
                  {stop.attempts > 1 ? <Tag tone="warn">{stop.attempts} attempts</Tag> : null}
                </div>
                <Facts
                  rows={[
                    ["Address", stop.address],
                    ["Recipient", stop.recipient ? `${stop.recipient}${stop.phone ? ` · ${stop.phone}` : ""}` : null],
                    ["Driver", stop.driver_name],
                    ["Cash to collect", stop.cod_minor > 0 ? money({ amount: stop.cod_minor, currency: data.currency }) : null],
                    ["Proof", stop.proof],
                    // The pair, not a single flag. Issued-but-not-sent is a
                    // driver standing at a door and a customer with no code,
                    // and it looks identical to "no OTP" everywhere else.
                    [
                      "Delivery code",
                      stop.otp_issued ? (
                        stop.otp_sent ? (
                          <Tag tone="ok">issued and sent</Tag>
                        ) : (
                          <Tag tone="danger" title="The customer never received it">
                            issued but never sent
                          </Tag>
                        )
                      ) : null,
                    ],
                    ["Failure", stop.failure_reason ? <span className="danger">{stop.failure_reason}</span> : null],
                  ]}
                />
              </li>
            ))}
          </ol>
        )}
      </Panel>

      <Panel title="What we told them" note={plural(data.notifications.length, "message")}>
        {data.notifications.length === 0 ? (
          <p className="muted">
            Nothing was sent about this order. Worth knowing before promising a customer they were notified.
          </p>
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

function whoRows(locale: string, data: OrderDetail): Fact[] {
  return [
    [
      "Customer",
      data.customer ? (
        <Ref locale={locale} kind="users" id={data.customer.id}>
          {data.customer.name ?? data.customer.phone ?? data.customer.id}
        </Ref>
      ) : null,
    ],
    ["Phone", data.customer?.phone],
    [
      "Vendor",
      data.branch ? (
        <Ref locale={locale} kind="vendors" id={data.branch.vendor_id}>
          {data.branch.vendor}
        </Ref>
      ) : null,
    ],
    ["Branch", data.branch?.name],
    [
      "Invoice",
      data.invoice ? (
        <>
          <span className="mono">{data.invoice.number}</span> <Tag>{data.invoice.status}</Tag>
        </>
      ) : null,
    ],
    [
      "Came from",
      data.attribution && data.attribution.model !== "none" ? (
        <>
          {data.attribution.model}
          {data.attribution.reel_id ? <span className="faint mono"> · {data.attribution.reel_id}</span> : null}
        </>
      ) : null,
    ],
    ["Order id", <span className="mono faint">{data.id}</span>],
  ];
}

function statusTone(status: string): "danger" | "warn" | "ok" | undefined {
  if (status === "cancelled" || status === "failed") return "danger";
  if (status === "completed") return "ok";
  if (status === "pending") return "warn";
  return undefined;
}

function stopTone(status: string): "danger" | "warn" | "ok" | undefined {
  if (status === "completed") return "ok";
  if (status === "failed" || status === "cancelled") return "danger";
  return undefined;
}
