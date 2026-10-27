"use client";

import { useQuery } from "@tanstack/react-query";
import { useState } from "react";

import { Panel } from "@/components/admin/detail";
import { errorText, useBranch } from "@/components/kitchen/portal";
import { getPrepSheet } from "@/lib/vendor-api";

const SLOT_LABEL: Record<string, string> = {
  breakfast: "Breakfast",
  lunch: "Lunch",
  dinner: "Dinner",
  snack: "Snack",
  suhoor: "Suhoor",
  iftar: "Iftar",
};

const today = () => {
  const d = new Date();
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
};

/**
 * What to cook. One line per dish and meal: every subscriber's pick where they
 * made one before the cutoff, otherwise the day's default dish. Skipped and
 * paused deliveries are already gone from the count.
 */
export default function PrepPage() {
  const branch = useBranch();
  const [date, setDate] = useState(today);
  const { data, error, isPending } = useQuery({
    queryKey: ["vendor", "prep", branch.branch_id, date],
    queryFn: () => getPrepSheet(branch.branch_id, date),
    enabled: date !== "",
  });
  const items = data?.items ?? [];
  const slots = [...new Set(items.map((i) => i.meal_slot))];
  const total = items.reduce((n, i) => n + i.portions, 0);

  return (
    <Panel
      title="Prep sheet"
      note="Counts lock at each plan's cutoff; until then customers can still switch between the two dishes."
    >
      <div className="row spread">
        <label>
          <span className="muted">Day </span>
          <input
            className="field"
            type="date"
            value={date}
            onChange={(e) => setDate(e.target.value)}
          />
        </label>
        {data && <span className="muted">{total} portions</span>}
      </div>
      {error ? (
        <div className="state error">{errorText(error)}</div>
      ) : isPending ? (
        <div className="skeleton" style={{ inlineSize: "40%" }} />
      ) : items.length === 0 ? (
        <div className="state">No subscription deliveries on this day.</div>
      ) : (
        <div className="table-wrap">
          <table className="data">
            <thead>
              <tr>
                <th>Meal</th>
                <th>Dish</th>
                <th className="num">Portions</th>
                <th className="num">Picked by customer</th>
              </tr>
            </thead>
            <tbody>
              {slots.flatMap((slot) =>
                items
                  .filter((i) => i.meal_slot === slot)
                  .map((i, idx) => (
                    <tr key={`${slot}-${i.dish_id ?? "none"}`}>
                      <td>{idx === 0 ? (SLOT_LABEL[slot] ?? slot) : ""}</td>
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
