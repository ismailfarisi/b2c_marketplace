"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { useState } from "react";

import { Panel } from "@/components/admin/detail";
import { label } from "@/components/kitchen/copy";
import {
  ConsoleError,
  errorText,
  useBranch,
} from "@/components/kitchen/portal";
import {
  copyMenuWeek,
  getMenu,
  listDishes,
  listPlans,
  MAX_MENU_WEEKS,
  setMenuSlot,
  setMenuWeeks,
  SLOTS,
  type MenuSlot,
  type Slot,
} from "@/lib/vendor-api";

/**
 * The weekly menu: for each weekday and meal, one default dish and optionally
 * one alternative. "Biryani every Monday lunch" is one cell; "biryani or
 * vegetable biryani" is the same cell with an alternative. Customers who do
 * not choose before the cutoff get the default.
 *
 * Cells a live plan delivers but the menu leaves empty are highlighted: those
 * are meals a subscriber is paying for with nothing to cook.
 *
 * A menu can rotate through 1–4 weeks so subscribers do not eat the same
 * Monday lunch forever. Weeks beyond the live count are a draft the kitchen
 * fills (often by copying week 1) before it serves them; serving N weeks is
 * refused until every delivery day of every week has a dish.
 */
export default function MenuPage() {
  const branch = useBranch();
  const { t, locale } = branch;
  const c = t.menu;
  const qc = useQueryClient();
  const id = branch.branch_id;
  const menu = useQuery({
    queryKey: ["vendor", "menu", id],
    queryFn: () => getMenu(id),
  });
  const dishes = useQuery({
    queryKey: ["vendor", "dishes", id],
    queryFn: () => listDishes(id),
  });
  const plans = useQuery({
    queryKey: ["vendor", "plans", id],
    queryFn: () => listPlans(id),
  });
  const [selected, setSelected] = useState<{
    weekday: number;
    slot: Slot;
  } | null>(null);
  const [showAll, setShowAll] = useState(false);
  const [week, setWeek] = useState(1);
  const [draftWeeks, setDraftWeeks] = useState<number | null>(null);
  const w = t.menuWeeks;
  const liveWeeks = Math.max(menu.data?.weeks ?? 1, 1);
  const storedWeeks = Math.max(
    liveWeeks,
    ...(menu.data?.slots ?? []).map((s) => s.week || 1),
  );
  const targetWeeks = draftWeeks ?? liveWeeks;
  const tabWeeks = Math.min(MAX_MENU_WEEKS, Math.max(targetWeeks, storedWeeks));
  const viewWeek = Math.min(week, tabWeeks);

  const allCells = new Map<string, MenuSlot>();
  for (const s of menu.data?.slots ?? [])
    allCells.set(`${s.week || 1}:${s.weekday}:${s.slot}`, s);
  const cells = new Map<string, MenuSlot>();
  for (const s of menu.data?.slots ?? [])
    if ((s.week || 1) === viewWeek) cells.set(`${s.weekday}:${s.slot}`, s);

  const needed = new Set<string>();
  const planSlots = new Set<Slot>();
  for (const p of plans.data?.items ?? []) {
    if (!p.is_active) continue;
    for (const s of p.meal_slots) {
      planSlots.add(s);
      for (const d of p.delivery_weekdays) needed.add(`${d}:${s}`);
    }
  }
  const used = new Set<Slot>([
    ...planSlots,
    ...(menu.data?.slots ?? []).map((s) => s.slot),
  ]);
  const rows = showAll ? [...SLOTS] : SLOTS.filter((s) => used.has(s));
  if (rows.length === 0) rows.push("lunch");

  const save = useMutation({
    mutationFn: (v: {
      week: number;
      weekday: number;
      slot: Slot;
      def: string | null;
      alt: string | null;
    }) =>
      setMenuSlot(id, v.weekday, v.slot, {
        week: v.week,
        default_dish_id: v.def,
        alternative_dish_id: v.alt,
      }),
    onSuccess: (m) => {
      qc.setQueryData(["vendor", "menu", id], m);
      qc.invalidateQueries({ queryKey: ["vendor", "overview", id] });
      qc.invalidateQueries({ queryKey: ["vendor", "dishes", id] });
      setSelected(null);
    },
  });
  const copy = useMutation({
    mutationFn: (to: number) => copyMenuWeek(id, to, 1),
    onSuccess: (m) => qc.setQueryData(["vendor", "menu", id], m),
  });
  const serve = useMutation({
    mutationFn: (n: number) => setMenuWeeks(id, n),
    onSuccess: (m) => {
      qc.setQueryData(["vendor", "menu", id], m);
      qc.invalidateQueries({ queryKey: ["vendor", "overview", id] });
      setDraftWeeks(null);
    },
  });

  if (menu.error)
    return (
      <ConsoleError
        error={menu.error}
        onRetry={() => void menu.refetch()}
        locale={locale}
      />
    );
  const activeDishes = (dishes.data?.items ?? []).filter((d) => d.is_active);
  const gapCount = [...needed].filter((k) => !cells.get(k)?.default).length;
  const sel = selected
    ? cells.get(`${selected.weekday}:${selected.slot}`)
    : undefined;
  // Every cell a live plan delivers, in every week about to be served.
  const targetGaps: Array<{ week: number; key: string }> = [];
  for (let n = 1; n <= targetWeeks; n++) {
    for (const k of needed)
      if (!allCells.get(`${n}:${k}`)?.default?.is_active)
        targetGaps.push({ week: n, key: k });
  }
  const weekEmpty = viewWeek > 1 && cells.size === 0;
  const pickWeek = (n: number) => {
    save.reset();
    setSelected(null);
    setWeek(n);
  };

  return (
    <>
      <section className="panel">
        <header className="panel-head">
          <h3>{w.title}</h3>
          <div className="spacer" />
          <select
            className="field"
            aria-label={w.title}
            value={targetWeeks}
            disabled={!branch.editable || serve.isPending}
            onChange={(e) => {
              serve.reset();
              const n = Number(e.target.value);
              setDraftWeeks(n === liveWeeks ? null : n);
              if (n > tabWeeks) pickWeek(n);
            }}
          >
            {Array.from({ length: MAX_MENU_WEEKS }, (_, i) => i + 1).map(
              (n) => (
                <option key={n} value={n}>
                  {w.weeks(n)}
                </option>
              ),
            )}
          </select>
        </header>
        <div className="panel-body">
          <div className="vp-chips" role="tablist" aria-label={w.title}>
            {Array.from({ length: tabWeeks }, (_, i) => i + 1).map((n) => (
              <button
                key={n}
                type="button"
                role="tab"
                className="vp-chip"
                aria-selected={n === viewWeek}
                aria-pressed={n === viewWeek}
                onClick={() => pickWeek(n)}
              >
                {w.week(n)} · {n <= liveWeeks ? w.live : w.draft}
                {n <= liveWeeks &&
                liveWeeks > 1 &&
                n === menu.data?.current_week
                  ? ` · ${w.thisWeek}`
                  : ""}
              </button>
            ))}
          </div>
          {weekEmpty && branch.editable ? (
            <p className="muted">
              {w.empty}{" "}
              <button
                className="btn"
                type="button"
                disabled={copy.isPending}
                onClick={() => copy.mutate(viewWeek)}
              >
                {copy.isPending ? w.copying : w.copy(1)}
              </button>
              {copy.error ? (
                <span className="danger"> {errorText(copy.error, locale)}</span>
              ) : null}
            </p>
          ) : null}
          {draftWeeks !== null && branch.editable ? (
            <div className="vp-actions wide">
              {targetGaps.length > 0 ? (
                <span className="tag warn">
                  {w.incomplete(targetGaps.length)}
                </span>
              ) : null}
              {targetGaps.length > 0 ? (
                <span className="vp-chips">
                  {[...new Set(targetGaps.map((g) => g.week))].map((n) => (
                    <button
                      key={n}
                      type="button"
                      className="vp-chip"
                      onClick={() => pickWeek(n)}
                    >
                      {w.gapsIn(n)}
                    </button>
                  ))}
                </span>
              ) : null}
              <button
                className="btn primary"
                type="button"
                disabled={targetGaps.length > 0 || serve.isPending}
                onClick={() => serve.mutate(targetWeeks)}
              >
                {serve.isPending ? t.saving : w.save(targetWeeks)}
              </button>
              <button
                className="btn"
                type="button"
                disabled={serve.isPending}
                onClick={() => setDraftWeeks(null)}
              >
                {t.neverMind}
              </button>
              {serve.error ? (
                <span className="danger">{errorText(serve.error, locale)}</span>
              ) : null}
            </div>
          ) : null}
        </div>
      </section>

      <section className="panel">
        <header className="panel-head">
          <h3>{liveWeeks > 1 || tabWeeks > 1 ? w.week(viewWeek) : c.title}</h3>
          {gapCount > 0 ? (
            <span className="tag warn">{c.gaps(gapCount)}</span>
          ) : null}
          <div className="spacer" />
          <label className="muted">
            <input
              type="checkbox"
              checked={showAll}
              onChange={(e) => setShowAll(e.target.checked)}
            />{" "}
            {c.showAll}
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
                  {t.weekdays.map((d) => (
                    <th key={d}>{d}</th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {rows.map((slot) => (
                  <tr key={slot}>
                    <th className="slot">{label(t.slots, slot)}</th>
                    {t.weekdays.map((_, weekday) => {
                      const k = `${weekday}:${slot}`;
                      const c = cells.get(k);
                      const isSel =
                        selected?.weekday === weekday && selected.slot === slot;
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
                                    {t.menu.or(c.alternative.name)}
                                  </span>
                                ) : null}
                              </>
                            ) : (
                              <span className="empty">
                                {needed.has(k)
                                  ? t.menu.needsDish
                                  : branch.editable
                                    ? t.menu.add
                                    : "—"}
                              </span>
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
          key={`${viewWeek}:${selected.weekday}:${selected.slot}`}
          title={`${tabWeeks > 1 ? `${w.week(viewWeek)} · ` : ""}${t.weekdays[selected.weekday] ?? ""} ${label(t.slots, selected.slot)}`}
          dishes={activeDishes}
          initialDefault={sel?.default?.dish_id ?? ""}
          initialAlt={sel?.alternative?.dish_id ?? ""}
          busy={save.isPending}
          error={save.error}
          dishesHref={`${branch.base}/dishes`}
          onCancel={() => setSelected(null)}
          onSave={(def, alt) =>
            save.mutate({ ...selected, week: viewWeek, def, alt })
          }
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
  const { t, locale } = useBranch();
  const c = t.menu;
  const [def, setDef] = useState(initialDefault);
  const [alt, setAlt] = useState(initialAlt);
  return (
    <Panel title={title} note={c.note}>
      {dishes.length === 0 ? (
        <p className="muted">
          {c.noDishes} <Link href={dishesHref}>{c.addDishes}</Link> {c.first}
        </p>
      ) : (
        <form
          className="vp-form"
          onSubmit={(e) => {
            e.preventDefault();
            onSave(def || null, def && alt && alt !== def ? alt : null);
          }}
        >
          <label>
            {c.defaultDish}
            <select
              className="field"
              value={def}
              onChange={(e) => {
                const next = e.target.value;
                setDef(next);
                // The alternative list hides the default, so a kept alt equal to
                // the new default would be invisible yet sent (and refused, 422).
                if (alt === next) setAlt("");
              }}
            >
              <option value="">{c.none}</option>
              {dishes.map((d) => (
                <option key={d.id} value={d.id}>
                  {d.name}
                </option>
              ))}
            </select>
          </label>
          <label>
            {c.alternative} <span className="hint">{c.alternativeHint}</span>
            <select
              className="field"
              value={alt}
              onChange={(e) => setAlt(e.target.value)}
              disabled={!def}
            >
              <option value="">{c.noChoice}</option>
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
              {busy ? t.saving : def ? c.save : c.clear}
            </button>
            <button
              className="btn"
              type="button"
              onClick={onCancel}
              disabled={busy}
            >
              {t.neverMind}
            </button>
            {error ? (
              <span className="danger">{errorText(error, locale)}</span>
            ) : null}
          </div>
        </form>
      )}
    </Panel>
  );
}
