"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { useState } from "react";

import { Panel } from "@/components/admin/detail";
import { errorText, useBranch } from "@/components/vendor/portal";
import { getMenu, listDishes, listPlans, setMenuSlot, SLOTS, WEEKDAYS, type MenuSlot, type Slot } from "@/lib/vendor-api";

/**
 * The weekly menu: for each weekday and meal, one default dish and optionally
 * one alternative. "Biryani every Monday lunch" is one cell; "biryani or
 * vegetable biryani" is the same cell with an alternative. Customers who do
 * not choose before the cutoff get the default.
 *
 * Cells a live plan delivers but the menu leaves empty are highlighted: those
 * are meals a subscriber is paying for with nothing to cook.
 */
export default function MenuPage() {
  const branch = useBranch();
  const qc = useQueryClient();
  const id = branch.branch_id;
  const menu = useQuery({ queryKey: ["vendor", "menu", id], queryFn: () => getMenu(id) });
  const dishes = useQuery({ queryKey: ["vendor", "dishes", id], queryFn: () => listDishes(id) });
  const plans = useQuery({ queryKey: ["vendor", "plans", id], queryFn: () => listPlans(id) });
  const [selected, setSelected] = useState<{ weekday: number; slot: Slot } | null>(null);
  const [showAll, setShowAll] = useState(false);

  const cells = new Map<string, MenuSlot>();
  for (const s of menu.data?.slots ?? []) cells.set(`${s.weekday}:${s.slot}`, s);

  const needed = new Set<string>();
  const planSlots = new Set<Slot>();
  for (const p of plans.data?.items ?? []) {
    if (!p.is_active) continue;
    for (const s of p.meal_slots) {
      planSlots.add(s);
      for (const d of p.delivery_weekdays) needed.add(`${d}:${s}`);
    }
  }
  const used = new Set<Slot>([...planSlots, ...(menu.data?.slots ?? []).map((s) => s.slot)]);
  const rows = showAll ? [...SLOTS] : SLOTS.filter((s) => used.has(s));
  if (rows.length === 0) rows.push("lunch");

  const save = useMutation({
    mutationFn: (v: { weekday: number; slot: Slot; def: string | null; alt: string | null }) =>
      setMenuSlot(id, v.weekday, v.slot, { default_dish_id: v.def, alternative_dish_id: v.alt }),
    onSuccess: (m) => {
      qc.setQueryData(["vendor", "menu", id], m);
      qc.invalidateQueries({ queryKey: ["vendor", "overview", id] });
      qc.invalidateQueries({ queryKey: ["vendor", "dishes", id] });
      setSelected(null);
    },
  });

  if (menu.error) return <div className="state error">{errorText(menu.error)}</div>;
  const activeDishes = (dishes.data?.items ?? []).filter((d) => d.is_active);
  const gapCount = [...needed].filter((k) => !cells.get(k)?.default).length;
  const sel = selected ? cells.get(`${selected.weekday}:${selected.slot}`) : undefined;

  return (
    <>
      <section className="panel">
        <header className="panel-head">
          <h3>This week, every week</h3>
          {gapCount > 0 ? <span className="tag warn">{gapCount} empty slot{gapCount === 1 ? "" : "s"} on live plans</span> : null}
          <div className="spacer" />
          <label className="muted">
            <input type="checkbox" checked={showAll} onChange={(e) => setShowAll(e.target.checked)} /> Show all meals
          </label>
        </header>
        <div className="panel-body table-wrap">
          {menu.isPending ? (
            <div className="skeleton" style={{ inlineSize: "40%" }} />
          ) : (
            <table className="menu-grid">
              <thead>
                <tr>
                  <th className="slot" />
                  {WEEKDAYS.map((d) => (
                    <th key={d}>{d}</th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {rows.map((slot) => (
                  <tr key={slot}>
                    <th className="slot">{slot}</th>
                    {WEEKDAYS.map((_, weekday) => {
                      const k = `${weekday}:${slot}`;
                      const c = cells.get(k);
                      const isSel = selected?.weekday === weekday && selected.slot === slot;
                      const cls = [
                        "menu-cell",
                        c?.default ? "filled" : "",
                        !c?.default && needed.has(k) ? "gap" : "",
                        c?.default && !c.default.is_active ? "archived" : "",
                        isSel ? "selected" : "",
                      ].join(" ");
                      return (
                        <td key={weekday}>
                          <button
                            type="button"
                            className={cls}
                            disabled={!branch.editable}
                            onClick={() => {
                              save.reset();
                              setSelected({ weekday, slot });
                            }}
                          >
                            {c?.default ? (
                              <>
                                <span className="default" dir="auto">
                                  {c.default.name}
                                </span>
                                {c.alternative ? (
                                  <span className="alt" dir="auto">
                                    or {c.alternative.name}
                                  </span>
                                ) : null}
                              </>
                            ) : (
                              <span className="empty">{needed.has(k) ? "Needs a dish" : branch.editable ? "+ Add" : "—"}</span>
                            )}
                          </button>
                        </td>
                      );
                    })}
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      </section>

      {selected && branch.editable ? (
        <SlotEditor
          key={`${selected.weekday}:${selected.slot}`}
          title={`${WEEKDAYS[selected.weekday] ?? ""} ${selected.slot}`}
          dishes={activeDishes}
          initialDefault={sel?.default?.dish_id ?? ""}
          initialAlt={sel?.alternative?.dish_id ?? ""}
          busy={save.isPending}
          error={save.error}
          dishesHref={`${branch.base}/dishes`}
          onCancel={() => setSelected(null)}
          onSave={(def, alt) => save.mutate({ ...selected, def, alt })}
        />
      ) : null}
    </>
  );
}

function SlotEditor({
  title,
  dishes,
  initialDefault,
  initialAlt,
  busy,
  error,
  dishesHref,
  onSave,
  onCancel,
}: {
  title: string;
  dishes: Array<{ id: string; name: string }>;
  initialDefault: string;
  initialAlt: string;
  busy: boolean;
  error: unknown;
  dishesHref: string;
  onSave: (def: string | null, alt: string | null) => void;
  onCancel: () => void;
}) {
  const [def, setDef] = useState(initialDefault);
  const [alt, setAlt] = useState(initialAlt);
  return (
    <Panel title={title} note="Customers who do not choose get the default">
      {dishes.length === 0 ? (
        <p className="muted">
          No active dishes yet. <Link href={dishesHref}>Add dishes</Link> first.
        </p>
      ) : (
        <form
          className="vp-form"
          onSubmit={(e) => {
            e.preventDefault();
            onSave(def || null, def && alt ? alt : null);
          }}
        >
          <label>
            Default dish
            <select className="field" value={def} onChange={(e) => setDef(e.target.value)}>
              <option value="">— none —</option>
              {dishes.map((d) => (
                <option key={d.id} value={d.id}>
                  {d.name}
                </option>
              ))}
            </select>
          </label>
          <label>
            Alternative <span className="hint">optional second choice</span>
            <select className="field" value={alt} onChange={(e) => setAlt(e.target.value)} disabled={!def}>
              <option value="">— no choice, default only —</option>
              {dishes
                .filter((d) => d.id !== def)
                .map((d) => (
                  <option key={d.id} value={d.id}>
                    {d.name}
                  </option>
                ))}
            </select>
          </label>
          <div className="vp-actions wide">
            <button className="btn primary" type="submit" disabled={busy}>
              {busy ? "Saving…" : def ? "Save" : "Clear slot"}
            </button>
            <button className="btn" type="button" onClick={onCancel} disabled={busy}>
              Never mind
            </button>
            {error ? <span className="danger">{errorText(error)}</span> : null}
          </div>
        </form>
      )}
    </Panel>
  );
}
