"use client";

import { useQuery } from "@tanstack/react-query";
import { useState } from "react";

import { Panel } from "@/components/admin/detail";
import { label } from "@/components/kitchen/copy";
import { ConsoleError, useBranch } from "@/components/kitchen/portal";
import { branchToday, getPrepSheet } from "@/lib/vendor-api";

/**
 * What to cook. One line per dish and meal: every subscriber's pick where they
 * made one before the cutoff, otherwise the day's default dish. Skipped and
 * paused deliveries are already gone from the count.
 */
export default function PrepPage() {
  const branch = useBranch();
  const { t, locale } = branch;
  const c = t.prep;
  // The kitchen's today (its timezone), not the browser's.
  const [date, setDate] = useState(() => branchToday(branch.timezone));
  const { data, error, isPending, refetch } = useQuery({
    queryKey: ["vendor", "prep", branch.branch_id, date],
    queryFn: () => getPrepSheet(branch.branch_id, date),
    enabled: date !== "",
  });
  const items = data?.items ?? [];
  const slots = [...new Set(items.map((i) => i.meal_slot))];
  const total = items.reduce((n, i) => n + i.portions, 0);

  return (
    <Panel title={c.title} note={c.note}>
      <div className="row spread">
        <label>
          <span className="muted">{c.day} </span>
          <input
            className="field"
            type="date"
            value={date}
            onChange={(e) => setDate(e.target.value)}
          />
        </label>
        {data && <span className="muted">{c.portions(total)}</span>}
      </div>
      {error ? (
        <ConsoleError error={error} onRetry={() => void refetch()} locale={locale} />
      ) : isPending ? (
        <div className="skeleton" style={{ inlineSize: "40%" }} />
      ) : items.length === 0 ? (
        <div className="state">{c.empty}</div>
      ) : (
        <div className="table-wrap">
          <table className="data">
            <thead>
              <tr>
                <th>{c.colMeal}</th>
                <th>{c.colDish}</th>
                <th className="num">{c.colPortions}</th>
                <th className="num">{c.colPicked}</th>
              </tr>
            </thead>
            <tbody>
              {slots.flatMap((slot) =>
                items
                  .filter((i) => i.meal_slot === slot)
                  .map((i, idx) => (
                    <tr key={`${slot}-${i.dish_id ?? "none"}`}>
                      <td>{idx === 0 ? label(t.slots, slot) : ""}</td>
                      <td dir="auto">
                        {i.name}
                        {i.name_ar && (
                          <span className="muted"> · {i.name_ar}</span>
                        )}
                      </td>
                      <td className="num">
                        <strong>{i.portions}</strong>
                      </td>
                      <td className="num">{i.chosen_by_customer}</td>
                    </tr>
                  )),
              )}
            </tbody>
          </table>
        </div>
      )}
    </Panel>
  );
}
