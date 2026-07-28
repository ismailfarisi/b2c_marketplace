"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { formatMoney } from "@marketplace/api-client";
import { use, useState } from "react";

import { getBoard, getQueue, getSummary, planRoutes } from "@/lib/api";

function today(): string {
  return new Date().toISOString().slice(0, 10);
}

/**
 * The branch dashboard: today's numbers, the order queue, the dispatch board.
 *
 * Polls rather than streams. A vendor watching a delivery window wants the
 * screen to be current, and 20 seconds is well inside what a kitchen notices —
 * whereas a websocket per open dashboard is a connection to hold, a reconnect
 * path to get right, and an auth refresh story, for a screen that is idle
 * nineteen hours a day.
 */
export default function BranchDashboard({
  params,
}: {
  params: Promise<{ locale: string; branchId: string }>;
}) {
  const { locale, branchId } = use(params);
  const isAr = locale === "ar";
  const loc = isAr ? "ar" : "en";
  const [date, setDate] = useState(today());
  const queryClient = useQueryClient();

  const summary = useQuery({
    queryKey: ["summary", branchId, date],
    queryFn: () => getSummary(branchId, date),
    refetchInterval: 20_000,
  });

  const queue = useQuery({
    queryKey: ["queue", branchId, date],
    queryFn: () => getQueue(branchId, date),
    refetchInterval: 20_000,
  });

  const board = useQuery({
    queryKey: ["board", branchId, date],
    queryFn: () => getBoard(branchId, date),
    refetchInterval: 20_000,
  });

  const plan = useMutation({
    mutationFn: () => planRoutes(branchId, date),
    onSuccess: () => {
      // Both, because planning changes the board *and* stamps a driver onto
      // rows in the queue. Invalidating one leaves the other showing work that
      // is already routed.
      queryClient.invalidateQueries({ queryKey: ["board", branchId, date] });
      queryClient.invalidateQueries({ queryKey: ["queue", branchId, date] });
      queryClient.invalidateQueries({ queryKey: ["summary", branchId, date] });
    },
  });

  return (
    <main className="page wide">
      <header className="row spread">
        <h1>{isAr ? "اليوم" : "Today"}</h1>
        <input
          type="date"
          value={date}
          onChange={(e) => setDate(e.target.value)}
          aria-label={isAr ? "التاريخ" : "Date"}
        />
      </header>

      <section className="metrics">
        <Metric label={isAr ? "مجدول" : "Scheduled"} value={summary.data?.scheduled} />
        <Metric label={isAr ? "قيد التنفيذ" : "In progress"} value={summary.data?.in_progress} />
        <Metric label={isAr ? "مكتمل" : "Completed"} value={summary.data?.completed} />
        <Metric
          label={isAr ? "فشل" : "Failed"}
          value={summary.data?.failed}
          tone={summary.data?.failed ? "bad" : undefined}
        />
        <Metric
          label={isAr ? "بدون سائق" : "Unassigned"}
          value={summary.data?.unassigned}
          tone={summary.data?.unassigned ? "warn" : undefined}
        />
        <Metric
          label={isAr ? "نقد لدى السائقين" : "Cash with drivers"}
          value={
            summary.data &&
            formatMoney(
              { amount: summary.data.cash_held_minor, currency: summary.data.currency },
              loc,
            )
          }
        />
        <Metric
          label={isAr ? "المستحق" : "Payable"}
          value={
            summary.data &&
            formatMoney(
              { amount: summary.data.payable_minor, currency: summary.data.currency },
              loc,
            )
          }
        />
      </section>

      <section>
        <div className="row spread">
          <h2>{isAr ? "التوزيع" : "Dispatch"}</h2>
          <button onClick={() => plan.mutate()} disabled={plan.isPending}>
            {plan.isPending
              ? isAr
                ? "جارٍ التخطيط…"
                : "Planning…"
              : isAr
                ? "خطّط المسارات"
                : "Plan routes"}
          </button>
        </div>

        {plan.data && (
          <p className="muted">
            {isAr
              ? `${plan.data.routes_created} مسار، ${plan.data.stops_routed} محطة`
              : `${plan.data.routes_created} routes, ${plan.data.stops_routed} stops`}
            {plan.data.stops_already_late > 0 && (
              /* Surfaced, not buried. A plan containing stops that are already
                 past their window is a reassignment decision now — not a
                 customer discovery in three hours. */
              <strong className="bad">
                {isAr
                  ? ` · ${plan.data.stops_already_late} متأخرة بالفعل`
                  : ` · ${plan.data.stops_already_late} already late`}
              </strong>
            )}
          </p>
        )}

        {board.data && board.data.unassigned > 0 && (
          <p className="warn">
            {isAr
              ? `${board.data.unassigned} طلب بدون مسار`
              : `${board.data.unassigned} stops have no route`}
          </p>
        )}

        <div className="scroll">
          <table>
            <thead>
              <tr>
                <th>{isAr ? "السائق" : "Driver"}</th>
                <th>{isAr ? "الحالة" : "Status"}</th>
                <th>{isAr ? "محطات" : "Stops"}</th>
                <th>{isAr ? "المسافة" : "Distance"}</th>
                <th>{isAr ? "أول وصول" : "First ETA"}</th>
              </tr>
            </thead>
            <tbody>
              {(board.data?.routes ?? []).map((r) => (
                <tr key={r.id}>
                  <td>{r.driver_name || <span className="muted">—</span>}</td>
                  <td>
                    <span className="pill">{r.status}</span>
                  </td>
                  <td>
                    {r.completed_stops}/{r.planned_stops}
                    {r.failed_stops > 0 && <span className="bad"> · {r.failed_stops}</span>}
                  </td>
                  <td>{(r.planned_distance_m / 1000).toFixed(1)} km</td>
                  <td>{r.first_eta ? new Date(r.first_eta).toLocaleTimeString(loc) : "—"}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>

      <section>
        <h2>{isAr ? "قائمة الطلبات" : "Order queue"}</h2>
        <div className="scroll">
          <table>
            <thead>
              <tr>
                <th>{isAr ? "النافذة" : "Window"}</th>
                <th>{isAr ? "الطلب" : "Order"}</th>
                <th>{isAr ? "الأصناف" : "Items"}</th>
                <th>{isAr ? "المستلم" : "Recipient"}</th>
                <th>{isAr ? "المنطقة" : "Area"}</th>
                <th>{isAr ? "السائق" : "Driver"}</th>
                <th>{isAr ? "الحالة" : "Status"}</th>
              </tr>
            </thead>
            <tbody>
              {(queue.data?.items ?? []).map((q) => (
                <tr key={q.fulfillment_id}>
                  <td>{q.window}</td>
                  <td className="mono">{q.order_no || q.payload_ref || "—"}</td>
                  <td>{q.items || <span className="muted">—</span>}</td>
                  <td>{q.recipient_name}</td>
                  <td>{q.area}</td>
                  <td>{q.driver_name || <span className="muted">—</span>}</td>
                  <td>
                    <span className={`pill ${q.status === "failed" ? "bad" : ""}`}>{q.status}</span>
                    {q.attempt_count > 1 && (
                      <span className="warn"> ·{q.attempt_count}</span>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>
    </main>
  );
}

function Metric({
  label,
  value,
  tone,
}: {
  label: string;
  value: number | string | undefined;
  // `| undefined` explicitly: exactOptionalPropertyTypes is on, so an optional
  // prop and a prop that may be undefined are different types.
  tone?: "warn" | "bad" | undefined;
}) {
  return (
    <div className={`metric ${tone ?? ""}`}>
      <span className="metric-label">{label}</span>
      <span className="metric-value">{value ?? "—"}</span>
    </div>
  );
}
