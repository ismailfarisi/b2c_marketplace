"use client";

import { formatMoney } from "@marketplace/api-client";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useRef, useState } from "react";

import { Panel } from "@/components/admin/detail";
import { label, type ConsoleCopy } from "@/components/kitchen/copy";
import { ConsoleError, errorText, fieldErrors, useBranch } from "@/components/kitchen/portal";
import { useIdempotencyKey } from "@/lib/use-idempotency-key";
import {
  createPlan,
  listPlans,
  parseMajor,
  SLOTS,
  toMajorInput,
  updatePlan,
  type Plan,
  type PlanInput,
  type Slot,
} from "@/lib/vendor-api";

type TierRow = { key: string; name: string; portion: "" | "small" | "regular" | "large"; days: string; meals: string; price: string };

type Draft = {
  name: string;
  name_ar: string;
  description: string;
  cycle_days: number;
  // Numeric inputs are kept as typed and converted on submit, so clearing a
  // field to retype it does not turn it into 0.
  days_per_week: string;
  meal_slots: Slot[];
  delivery_weekdays: number[];
  cutoff_hours: string;
  allowed_pause_days: string;
  price: string;
  rotation_enabled: boolean;
  rotation_price: string;
  is_active: boolean;
  tiers: TierRow[];
};

const fresh: Draft = {
  name: "",
  name_ar: "",
  description: "",
  cycle_days: 28,
  days_per_week: "5",
  meal_slots: ["lunch"],
  delivery_weekdays: [0, 1, 2, 3, 4],
  cutoff_hours: "20",
  allowed_pause_days: "7",
  price: "",
  rotation_enabled: false,
  rotation_price: "",
  is_active: true,
  tiers: [],
};

function toDraft(p: Plan): Draft {
  return {
    name: p.name,
    name_ar: p.name_ar ?? "",
    description: p.description ?? "",
    cycle_days: p.cycle_days,
    days_per_week: String(p.days_per_week),
    meal_slots: p.meal_slots,
    delivery_weekdays: p.delivery_weekdays,
    cutoff_hours: String(p.cutoff_hours),
    allowed_pause_days: String(p.allowed_pause_days),
    price: toMajorInput(p.price_minor, p.currency),
    rotation_enabled: p.rotation_enabled,
    rotation_price: toMajorInput(p.rotation_week_price_minor, p.currency),
    is_active: p.is_active,
    tiers: p.tiers
      .filter((t) => t.is_active)
      .map((t) => ({
        key: t.key,
        name: t.name,
        portion: t.portion_size ?? "",
        days: t.days_per_week?.toString() ?? "",
        meals: t.meals_per_day?.toString() ?? "",
        price: toMajorInput(t.price_minor, p.currency),
      })),
  };
}

function shape(
  t: ConsoleCopy,
  p: { days_per_week: number; meal_slots: string[]; delivery_weekdays: number[]; cycle_days: number },
) {
  const days = p.delivery_weekdays.map((d) => t.weekdays[d]).join(" ");
  return t.plans.shape(p.days_per_week, days, p.meal_slots.map((s) => label(t.slots, s)).join(" + "), p.cycle_days);
}

/** A whole number as typed, or null when the field is empty or not a whole number. */
function wholeNumber(raw: string): number | null {
  const s = raw.trim();
  return /^\d+$/.test(s) ? Number(s) : null;
}

/**
 * What a kitchen sells. A plan fixes the shape of a subscription — which days
 * the kitchen delivers, which meals, how many days a week the customer picks —
 * and its price per billing cycle. Tiers are variants of the same plan (a
 * large portion, fewer days) at their own price.
 *
 * Joining the rotation lets customers who pick four kitchens for a month get
 * one week from this kitchen. Each kitchen names its own weekly price for
 * that; the customer pays the sum of the four, and the usual commission
 * applies to each week.
 */
export default function PlansPage() {
  const branch = useBranch();
  const { t, locale } = branch;
  const c = t.plans;
  const money = locale === "ar" ? "ar" : "en";
  const qc = useQueryClient();
  const key = ["vendor", "plans", branch.branch_id];
  const { data, error, isPending, refetch } = useQuery({ queryKey: key, queryFn: () => listPlans(branch.branch_id) });
  const [editing, setEditing] = useState<{ plan: Plan | null } | null>(null);
  const plans = data?.items ?? [];

  return (
    <>
      {editing ? (
        <PlanForm
          key={editing.plan?.id ?? "new"}
          plan={editing.plan}
          currency={editing.plan?.currency ?? plans[0]?.currency ?? "AED"}
          onDone={() => {
            setEditing(null);
            qc.invalidateQueries({ queryKey: key });
            qc.invalidateQueries({ queryKey: ["vendor", "overview", branch.branch_id] });
          }}
          onCancel={() => setEditing(null)}
        />
      ) : null}

      <section className="panel">
        <header className="panel-head">
          <h3>{c.title}</h3>
          <div className="spacer" />
          {branch.editable ? (
            <button className="btn primary" type="button" onClick={() => setEditing({ plan: null })}>
              {c.new}
            </button>
          ) : null}
        </header>
        {error ? (
          <ConsoleError error={error} onRetry={() => void refetch()} locale={locale} />
        ) : isPending ? (
          <div className="panel-body">
            <div className="skeleton" style={{ inlineSize: "40%" }} />
          </div>
        ) : plans.length === 0 ? (
          <div className="state">{c.empty}</div>
        ) : (
          <div className="table-wrap">
            <table className="data">
              <thead>
                <tr>
                  <th className="grow">{c.colPlan}</th>
                  <th className="num">{c.colPrice}</th>
                  <th className="num">{c.colRotation}</th>
                  <th>{c.colTiers}</th>
                  <th className="num">{c.colSubscribers}</th>
                  <th>{c.colStatus}</th>
                  {branch.editable ? <th /> : null}
                </tr>
              </thead>
              <tbody>
                {plans.map((p) => (
                  <tr key={p.id}>
                    <td className="grow">
                      <strong dir="auto">{p.name}</strong>
                      <div className="muted">{shape(t, p)}</div>
                    </td>
                    <td className="num">{formatMoney({ amount: p.price_minor, currency: p.currency }, money)}</td>
                    <td className="num">
                      {p.rotation_enabled && p.rotation_week_price_minor != null ? (
                        formatMoney({ amount: p.rotation_week_price_minor, currency: p.currency }, money)
                      ) : (
                        <span className="muted">{c.notInRotation}</span>
                      )}
                    </td>
                    <td>
                      {p.tiers
                        .filter((t) => t.is_active)
                        .map((t) => (
                          <span key={t.id} className="tag" dir="auto">
                            {t.name}
                          </span>
                        ))}
                    </td>
                    <td className="num">{p.active_subscribers}</td>
                    <td>
                      <span className={`tag ${p.is_active ? "ok" : ""}`}>{p.is_active ? c.live : c.paused}</span>
                    </td>
                    {branch.editable ? (
                      <td>
                        <button className="btn link" type="button" onClick={() => setEditing({ plan: p })}>
                          {t.edit}
                        </button>
                      </td>
                    ) : null}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>
    </>
  );
}

function PlanForm({
  plan,
  currency,
  onDone,
  onCancel,
}: {
  plan: Plan | null;
  currency: string;
  onDone: () => void;
  onCancel: () => void;
}) {
  const branch = useBranch();
  const { locale } = branch;
  const T = branch.t;
  const c = T.plans;
  const [d, setD] = useState<Draft>(plan ? toDraft(plan) : fresh);
  const [local, setLocal] = useState<Record<string, string>>({});
  const locked = (plan?.active_subscribers ?? 0) > 0;

  // One key per open form, so a double-click on "Create plan" is one plan.
  const idem = useIdempotencyKey();
  const submitting = useRef(false);
  const save = useMutation({
    mutationFn: (body: PlanInput) =>
      plan
        ? updatePlan(branch.branch_id, plan.id, body)
        : createPlan(branch.branch_id, body, { headers: idem.headers() }),
    onSuccess: () => {
      idem.reset();
      onDone();
    },
    onError: () => idem.reset(),
    onSettled: () => {
      submitting.current = false;
    },
  });
  const errs = { ...fieldErrors(save.error, locale), ...local };

  function submit() {
    if (save.isPending || submitting.current) return;
    const bad: Record<string, string> = {};
    const price = parseMajor(d.price, currency);
    if (price == null || price <= 0) bad.price_minor = c.priceRequired;
    const rotation = d.rotation_enabled ? parseMajor(d.rotation_price, currency) : null;
    if (d.rotation_enabled && (rotation == null || rotation <= 0)) bad.rotation_week_price_minor = c.rotationRequired;
    const daysPerWeek = wholeNumber(d.days_per_week);
    if (daysPerWeek == null) bad.days_per_week = c.wholeNumber;
    const cutoffHours = wholeNumber(d.cutoff_hours);
    if (cutoffHours == null) bad.cutoff_hours = c.wholeNumber;
    const pauseDays = wholeNumber(d.allowed_pause_days);
    if (pauseDays == null) bad.allowed_pause_days = c.wholeNumber;
    const tiers = d.tiers.map((t, i) => {
      const tp = parseMajor(t.price, currency);
      if (tp == null || tp <= 0) bad[`tiers[${i}].price_minor`] = c.tierPriceRequired;
      return {
        key: t.key || t.name.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, ""),
        name: t.name,
        name_ar: null,
        days_per_week: t.days ? Number(t.days) : null,
        meals_per_day: t.meals ? Number(t.meals) : null,
        portion_size: t.portion === "" ? null : t.portion,
        price_minor: tp ?? 0,
      };
    });
    setLocal(bad);
    if (Object.keys(bad).length > 0) return;
    submitting.current = true;
    save.mutate({
      name: d.name,
      name_ar: d.name_ar.trim() || null,
      description: d.description.trim() || null,
      description_ar: plan?.description_ar ?? null,
      image_url: plan?.image_url ?? null,
      cycle_days: d.cycle_days,
      days_per_week: daysPerWeek ?? 0,
      meal_slots: d.meal_slots,
      delivery_weekdays: d.delivery_weekdays,
      allowed_pause_days: pauseDays ?? 0,
      cutoff_hours: cutoffHours ?? 0,
      price_minor: price ?? 0,
      rotation_enabled: d.rotation_enabled,
      rotation_week_price_minor: rotation,
      is_active: d.is_active,
      tiers,
    });
  }

  const toggle = <T,>(list: T[], v: T) => (list.includes(v) ? list.filter((x) => x !== v) : [...list, v]);
  const err = (k: string) => (errs[k] ? <span className="err">{errs[k]}</span> : null);

  return (
    <Panel title={plan ? c.editTitle(plan.name) : c.new} note={currency}>
      <form
        className="vp-form"
        onSubmit={(e) => {
          e.preventDefault();
          submit();
        }}
      >
        <label>
          {c.name}
          <input className="field" dir="auto" value={d.name} onChange={(e) => setD({ ...d, name: e.target.value })} required autoFocus />
          {err("name")}
        </label>
        <label>
          {c.nameAr}
          <input className="field" dir="rtl" value={d.name_ar} onChange={(e) => setD({ ...d, name_ar: e.target.value })} />
        </label>
        <label className="wide">
          {c.description}
          <textarea className="field" dir="auto" rows={2} value={d.description} onChange={(e) => setD({ ...d, description: e.target.value })} />
        </label>

        {locked ? (
          <div className="vp-readonly wide">
            {c.locked(plan?.active_subscribers ?? 0)}
          </div>
        ) : null}

        <div className="vp-label wide">
          {c.deliveryDays} <span className="hint">{c.deliveryDaysHint}</span>
          <div className="vp-chips">
            {T.weekdays.map((name, i) => (
              <button
                key={name}
                type="button"
                className="vp-chip"
                aria-pressed={d.delivery_weekdays.includes(i)}
                onClick={() => setD({ ...d, delivery_weekdays: toggle(d.delivery_weekdays, i).sort() })}
              >
                {name}
              </button>
            ))}
          </div>
          {err("delivery_weekdays")}
        </div>
        <div className="vp-label wide">
          {c.meals}
          <div className="vp-chips">
            {SLOTS.map((s) => (
              <button
                key={s}
                type="button"
                className="vp-chip"
                disabled={locked}
                aria-pressed={d.meal_slots.includes(s)}
                onClick={() => setD({ ...d, meal_slots: SLOTS.filter((x) => (x === s ? !d.meal_slots.includes(s) : d.meal_slots.includes(x))) })}
              >
                {label(T.slots, s)}
              </button>
            ))}
          </div>
          {err("meal_slots")}
        </div>
        <label>
          {c.daysPerWeek} <span className="hint">{c.daysPerWeekHint}</span>
          <input
            className="field"
            type="number"
            min={1}
            max={7}
            value={d.days_per_week}
            disabled={locked}
            onChange={(e) => setD({ ...d, days_per_week: e.target.value })}
          />
          {err("days_per_week")}
        </label>
        <label>
          {c.cycle}
          <select className="field" value={d.cycle_days} disabled={locked} onChange={(e) => setD({ ...d, cycle_days: Number(e.target.value) })}>
            {[7, 14, 28, 30].map((n) => (
              <option key={n} value={n}>
                {c.cycleDays(n)}
              </option>
            ))}
          </select>
          {err("cycle_days")}
        </label>
        <label>
          {c.price(currency)} <span className="hint">{c.beforeVat}</span>
          <input className="field" dir="ltr" inputMode="decimal" value={d.price} onChange={(e) => setD({ ...d, price: e.target.value })} placeholder="1200.00" />
          {err("price_minor")}
        </label>
        <label>
          {c.cutoff}
          <input
            className="field"
            type="number"
            min={0}
            max={168}
            value={d.cutoff_hours}
            onChange={(e) => setD({ ...d, cutoff_hours: e.target.value })}
          />
          {err("cutoff_hours")}
        </label>
        <label>
          {c.pauseDays}
          <input
            className="field"
            type="number"
            min={0}
            value={d.allowed_pause_days}
            onChange={(e) => setD({ ...d, allowed_pause_days: e.target.value })}
          />
          {err("allowed_pause_days")}
        </label>
        <label>
          {c.status}
          <select className="field" value={d.is_active ? "live" : "paused"} onChange={(e) => setD({ ...d, is_active: e.target.value === "live" })}>
            <option value="live">{c.statusLive}</option>
            <option value="paused">{c.statusPaused}</option>
          </select>
        </label>

        <div className="vp-label wide">
          <label style={{ flexDirection: "row", alignItems: "center", gap: 8 }}>
            <input
              type="checkbox"
              checked={d.rotation_enabled}
              onChange={(e) => setD({ ...d, rotation_enabled: e.target.checked })}
            />
            {c.rotation}
          </label>
          <span className="hint">{c.rotationHint}</span>
        </div>
        {d.rotation_enabled ? (
          <label>
            {c.rotationPrice(currency)}
            <input
              className="field"
              dir="ltr"
              inputMode="decimal"
              value={d.rotation_price}
              onChange={(e) => setD({ ...d, rotation_price: e.target.value })}
              placeholder="320.00"
            />
            {err("rotation_week_price_minor")}
          </label>
        ) : null}

        <div className="vp-label wide">
          {c.tiers} <span className="hint">{c.tiersHint}</span>
          {d.tiers.length > 0 ? (
            <table className="data">
              <thead>
                <tr>
                  <th>{c.tierName}</th>
                  <th>{c.portion}</th>
                  <th>{c.tierDays}</th>
                  <th>{c.tierMeals}</th>
                  <th>{c.tierPrice}</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {d.tiers.map((t, i) => {
                  const set = (patch: Partial<TierRow>) =>
                    setD({ ...d, tiers: d.tiers.map((x, j) => (j === i ? { ...x, ...patch } : x)) });
                  return (
                    <tr key={i}>
                      <td>
                        <input className="field" dir="auto" value={t.name} onChange={(e) => set({ name: e.target.value })} required />
                        {err(`tiers[${i}].name`)}
                        {err(`tiers[${i}].key`)}
                      </td>
                      <td>
                        <select className="field" value={t.portion} onChange={(e) => set({ portion: e.target.value as TierRow["portion"] })}>
                          <option value="">—</option>
                          <option value="small">{c.portions.small}</option>
                          <option value="regular">{c.portions.regular}</option>
                          <option value="large">{c.portions.large}</option>
                        </select>
                      </td>
                      <td>
                        <input className="field" type="number" min={1} max={7} value={t.days} placeholder={c.planDefault} onChange={(e) => set({ days: e.target.value })} />
                        {err(`tiers[${i}].days_per_week`)}
                      </td>
                      <td>
                        <input className="field" type="number" min={1} max={6} value={t.meals} placeholder={c.planDefault} onChange={(e) => set({ meals: e.target.value })} />
                        {err(`tiers[${i}].meals_per_day`)}
                      </td>
                      <td>
                        <input className="field" inputMode="decimal" value={t.price} onChange={(e) => set({ price: e.target.value })} />
                        {err(`tiers[${i}].price_minor`)}
                      </td>
                      <td>
                        <button className="btn link" type="button" onClick={() => setD({ ...d, tiers: d.tiers.filter((_, j) => j !== i) })}>
                          {c.remove}
                        </button>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          ) : null}
          <div>
            <button
              className="btn"
              type="button"
              onClick={() => setD({ ...d, tiers: [...d.tiers, { key: "", name: "", portion: "", days: "", meals: "", price: "" }] })}
            >
              {c.addTier}
            </button>
          </div>
        </div>

        <div className="vp-actions wide">
          <button className="btn primary" type="submit" disabled={save.isPending}>
            {save.isPending ? T.saving : plan ? c.save : c.create}
          </button>
          <button className="btn" type="button" onClick={onCancel} disabled={save.isPending}>
            {T.neverMind}
          </button>
          {save.error && Object.keys(fieldErrors(save.error, locale)).length === 0 ? (
            <span className="danger">{errorText(save.error, locale)}</span>
          ) : null}
        </div>
      </form>
    </Panel>
  );
}
