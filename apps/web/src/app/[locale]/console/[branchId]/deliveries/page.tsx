"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { formatMoney } from "@marketplace/api-client";
import { useState } from "react";

import { label } from "@/components/kitchen/copy";
import { ConsoleError, errorText, useBranch } from "@/components/kitchen/portal";
import { getBoard, getQueue, getSummary, planRoutes } from "@/lib/api";
import { branchToday } from "@/lib/vendor-api";

/**
 * The branch dashboard: today's numbers, the order queue, the dispatch board.
 *
 * Polls rather than streams. A vendor watching a delivery window wants the
 * screen to be current, and 20 seconds is well inside what a kitchen notices —
 * whereas a websocket per open dashboard is a connection to hold, a reconnect
 * path to get right, and an auth refresh story, for a screen that is idle
 * nineteen hours a day.
 *
 * "Today" is the kitchen's day (its timezone), not the browser's.
 */
export default function BranchDashboard() {
  const branch = useBranch();
  const { t, locale, branch_id: branchId } = branch;
  const d = t.deliveries;
  const loc = locale === "ar" ? "ar" : "en";
  const [date, setDate] = useState(() => branchToday(branch.timezone));
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

  const loadError = summary.error ?? queue.error ?? board.error;
  const retry = () => {
    if (summary.error) void summary.refetch();
    if (queue.error) void queue.refetch();
    if (board.error) void board.refetch();
  };

  return (
    <main className="page wide">
      <header className="row spread">
        <h1>{d.today}</h1>
        <input
          type="date"
          value={date}
          onChange={(e) => {
            plan.reset();
            setDate(e.target.value);
          }}
          aria-label={d.date}
        />
      </header>

      {loadError ? <ConsoleError error={loadError} onRetry={retry} locale={locale} /> : null}

      <section className="metrics">
        <Metric label={d.scheduled} value={summary.data?.scheduled} />
        <Metric label={d.inProgress} value={summary.data?.in_progress} />
        <Metric label={d.completed} value={summary.data?.completed} />
        <Metric label={d.failed} value={summary.data?.failed} tone={summary.data?.failed ? "bad" : undefined} />
        <Metric
          label={d.unassigned}
          value={summary.data?.unassigned}
          tone={summary.data?.unassigned ? "warn" : undefined}
        />
        <Metric
          label={d.cash}
          value={
            summary.data &&
            formatMoney({ amount: summary.data.cash_held_minor, currency: summary.data.currency }, loc)
          }
        />
        <Metric
          label={d.payable}
          value={
            summary.data && formatMoney({ amount: summary.data.payable_minor, currency: summary.data.currency }, loc)
          }
        />
      </section>

      <section>
        <div className="row spread">
          <h2>{d.dispatch}</h2>
          {/* Planning routes is a write: staff see the board but not the button
              (the server refuses them too, 403 role_read_only). */}
          {branch.editable ? (
            <button onClick={() => plan.mutate()} disabled={plan.isPending}>
              {plan.isPending ? d.planning : d.plan}
            </button>
          ) : null}
        </div>

        {plan.error ? (
          <p className="danger" role="alert">
            {errorText(plan.error, locale)}
          </p>
        ) : null}

        {plan.data && (
          <p className="muted">
            {d.planned(plan.data.routes_created, plan.data.stops_routed)}
            {plan.data.stops_already_late > 0 && (
              /* Surfaced, not buried. A plan containing stops that are already
                 past their window is a reassignment decision now — not a
                 customer discovery in three hours. */
              <strong className="bad">{d.late(plan.data.stops_already_late)}</strong>
            )}
          </p>
        )}

        {board.data && board.data.unassigned > 0 && <p className="warn">{d.noRoute(board.data.unassigned)}</p>}

        <div className="scroll">
          <table>
            <thead>
              <tr>
                <th>{d.driver}</th>
                <th>{d.status}</th>
                <th>{d.stops}</th>
                <th>{d.distance}</th>
                <th>{d.firstEta}</th>
              </tr>
            </thead>
            <tbody>
              {(board.data?.routes ?? []).map((r) => (
                <tr key={r.id}>
                  <td dir="auto">{r.driver_name || <span className="muted">—</span>}</td>
                  <td>
                    <span className="pill">{label(t.statuses, r.status)}</span>
                  </td>
                  <td>
                    {r.completed_stops}/{r.planned_stops}
                    {r.failed_stops > 0 && <span className="bad"> · {r.failed_stops}</span>}
                  </td>
                  <td>{d.km((r.planned_distance_m / 1000).toLocaleString(loc, { maximumFractionDigits: 1, minimumFractionDigits: 1 }))}</td>
                  <td>
                    {r.first_eta
                      ? new Date(r.first_eta).toLocaleTimeString(loc, branch.timezone ? { timeZone: branch.timezone } : {})
                      : "—"}
                  </td>
                </tr>
              ))}
              {board.data && board.data.routes.length === 0 ? (
                <tr>
                  <td colSpan={5} className="muted">
                    {d.noRoutes}
                  </td>
                </tr>
              ) : null}
            </tbody>
          </table>
        </div>
      </section>

      <section>
        <h2>{d.queue}</h2>
        <div className="scroll">
          <table>
            <thead>
              <tr>
                <th>{d.window}</th>
                <th>{d.order}</th>
                <th>{d.items}</th>
                <th>{d.recipient}</th>
                <th>{d.area}</th>
                <th>{d.driver}</th>
                <th>{d.status}</th>
              </tr>
            </thead>
            <tbody>
              {(queue.data?.items ?? []).map((q) => (
                <tr key={q.fulfillment_id}>
                  <td dir="auto">{q.window}</td>
                  <td className="mono">{q.order_no || q.payload_ref || "—"}</td>
                  <td dir="auto">{q.items || <span className="muted">—</span>}</td>
                  <td dir="auto">{q.recipient_name}</td>
                  <td dir="auto">{q.area}</td>
                  <td dir="auto">{q.driver_name || <span className="muted">—</span>}</td>
                  <td>
                    <span className={`pill ${q.status === "failed" ? "bad" : ""}`}>{label(t.statuses, q.status)}</span>
                    {q.attempt_count > 1 && <span className="warn"> ·{q.attempt_count}</span>}
                  </td>
                </tr>
              ))}
              {queue.data && queue.data.items.length === 0 ? (
                <tr>
                  <td colSpan={7} className="muted">
                    {d.noOrders}
                  </td>
                </tr>
              ) : null}
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
