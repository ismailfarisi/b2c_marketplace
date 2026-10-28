"use client";

import { formatMoney } from "@marketplace/api-client";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

import { Panel, plural } from "@/components/admin/detail";
import {
  fromLocalInput,
  toLocalInput,
  windowState,
} from "@/components/admin/merch";
import {
  deletePromo,
  getMealKitchens,
  getPromoRedemptions,
  getPromos,
  savePromo,
  type MerchPromo,
  type MerchPromoInput,
} from "@/lib/admin-api";
import { parseMajor } from "@/lib/vendor-api";

const EMPTY: MerchPromoInput = {
  code: null,
  name: "",
  name_ar: "",
  description: "",
  description_ar: "",
  discount_type: "percent",
  percent_bp: 1000,
  amount_minor: 0,
  max_discount_minor: null,
  currency: "AED",
  trial_days: null,
  applies_to: "subscriptions",
  audience: "everyone",
  kitchen_share_bp: 0,
  usage_limit: null,
  per_customer_limit: 1,
  is_public: false,
  is_active: true,
  starts_at: null,
  ends_at: null,
  branch_ids: [],
};

const aed = (n: number, currency = "AED") =>
  formatMoney({ amount: n, currency }, "en");
const major = (n: number | null) => (n == null ? "" : String(n / 100));
const intOrNull = (v: string) => {
  const n = parseInt(v, 10);
  return Number.isFinite(n) && n > 0 ? n : null;
};

function describe(p: MerchPromoInput) {
  const parts: string[] = [];
  if (p.trial_days) parts.push(`${p.trial_days}-day trial`);
  if (p.discount_type === "percent") parts.push(`${p.percent_bp / 100}% off`);
  if (p.discount_type === "fixed") parts.push(`${aed(p.amount_minor, p.currency)} off`);
  return parts.join(" · ") || "—";
}

/** Discount codes and trials. A trial shortens the first cycle and does not renew. */
export default function PromosPage() {
  const qc = useQueryClient();
  const { data, error } = useQuery({
    queryKey: ["admin", "promos"],
    queryFn: getPromos,
  });
  const kitchens = useQuery({
    queryKey: ["admin", "meal-kitchens", "promo-picker"],
    queryFn: () => getMealKitchens(),
  });
  const [editing, setEditing] = useState<string | null>(null);
  const [form, setForm] = useState<MerchPromoInput>(EMPTY);
  const [problem, setProblem] = useState<string | null>(null);
  const rows = data?.items ?? [];
  const redemptions = useQuery({
    queryKey: ["admin", "promos", editing, "redemptions"],
    queryFn: () => getPromoRedemptions(editing!),
    enabled: editing != null && editing !== "new",
  });

  const save = useMutation({
    mutationFn: () => savePromo(editing === "new" ? null : editing, form),
    onSuccess: async (p) => {
      setProblem(null);
      setEditing(p.id);
      await qc.invalidateQueries({ queryKey: ["admin", "promos"] });
    },
    onError: (e: Error) => setProblem(e.message),
  });
  const remove = useMutation({
    mutationFn: (id: string) => deletePromo(id),
    onSuccess: async () => {
      setEditing(null);
      await qc.invalidateQueries({ queryKey: ["admin", "promos"] });
    },
  });

  const open = (p: MerchPromo | null) => {
    setProblem(null);
    if (!p) {
      setEditing("new");
      setForm(EMPTY);
      return;
    }
    const { id: _id, updated_at: _u, usage_count: _c, ...rest } = p;
    setEditing(p.id);
    setForm(rest);
  };
  const set = (patch: Partial<MerchPromoInput>) => setForm({ ...form, ...patch });
  const text = (
    k: "name" | "name_ar" | "description" | "description_ar",
    label: string,
    dir?: "rtl",
  ) => (
    <label className="field">
      <span>{label}</span>
      <input dir={dir} value={form[k]} onChange={(e) => set({ [k]: e.target.value })} />
    </label>
  );

  if (error)
    return (
      <p className="danger">Promos could not be loaded. Needs commerce.read.</p>
    );

  const isTrial = form.trial_days != null;

  return (
    <div className="detail">
      <div className="detail-head">
        <div>
          <h2>Promos &amp; trials</h2>
          <p className="console-lede">
            Codes customers type at checkout, and public offers shown on kitchen
            pages. The platform funds the discount unless a kitchen share is set.
          </p>
        </div>
        <div className="spacer" />
        <button type="button" className="btn primary" onClick={() => open(null)}>
          New promo
        </button>
      </div>

      <Panel title="Promos" note={plural(rows.length, "promo")}>
        {rows.length === 0 ? (
          <p className="muted">No promos yet.</p>
        ) : (
          <table className="money-table">
            <thead>
              <tr>
                <th>Name</th>
                <th>Code</th>
                <th>Gives</th>
                <th>For</th>
                <th>Used</th>
                <th>State</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((p) => (
                <tr key={p.id}>
                  <td>
                    <button type="button" className="btn link" onClick={() => open(p)}>
                      {p.name}
                    </button>
                  </td>
                  <td className="mono">{p.code ?? (p.is_public ? "public offer" : "—")}</td>
                  <td>{describe(p)}</td>
                  <td className="faint">
                    {p.applies_to}
                    {p.audience === "new_customers" ? " · new customers" : ""}
                    {p.branch_ids.length > 0 ? ` · ${plural(p.branch_ids.length, "kitchen")}` : ""}
                  </td>
                  <td>
                    {p.usage_count}
                    {p.usage_limit ? ` / ${p.usage_limit}` : ""}
                  </td>
                  <td>{windowState(p)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Panel>

      {editing && (
        <Panel title={editing === "new" ? "New promo" : "Edit promo"}>
          <form
            className="merch-form"
            onSubmit={(e) => {
              e.preventDefault();
              save.mutate();
            }}
          >
            {text("name", "Name (English)")}
            {text("name_ar", "Name (Arabic)", "rtl")}
            {text("description", "Description (English)")}
            {text("description_ar", "Description (Arabic)", "rtl")}
            <label className="field">
              <span>Code (blank for a public offer only)</span>
              <input
                className="mono"
                value={form.code ?? ""}
                onChange={(e) => set({ code: e.target.value.toUpperCase() || null })}
              />
            </label>
            <label className="field merch-check">
              <input
                type="checkbox"
                checked={form.is_public}
                onChange={(e) => set({ is_public: e.target.checked })}
              />
              <span>Show on kitchen pages as an offer</span>
            </label>

            <label className="field merch-check">
              <input
                type="checkbox"
                checked={isTrial}
                onChange={(e) =>
                  set(
                    e.target.checked
                      ? { trial_days: 3, applies_to: "subscriptions" }
                      : {
                          trial_days: null,
                          discount_type:
                            form.discount_type === "none" ? "percent" : form.discount_type,
                          percent_bp: form.percent_bp || 1000,
                        },
                  )
                }
              />
              <span>Trial: shorten the first cycle (no renewal)</span>
            </label>
            {isTrial && (
              <label className="field">
                <span>Trial length (days, 1–14)</span>
                <input
                  type="number"
                  min={1}
                  max={14}
                  value={form.trial_days ?? ""}
                  onChange={(e) => set({ trial_days: intOrNull(e.target.value) })}
                />
              </label>
            )}

            <label className="field">
              <span>Discount</span>
              <select
                value={form.discount_type}
                onChange={(e) =>
                  set({ discount_type: e.target.value as MerchPromoInput["discount_type"] })
                }
              >
                <option value="percent">Percent off meals</option>
                <option value="fixed">Fixed amount off</option>
                {isTrial && <option value="none">None (trial at pro-rata price)</option>}
              </select>
            </label>
            {form.discount_type === "percent" && (
              <>
                <label className="field">
                  <span>Percent off (delivery is never discounted)</span>
                  <input
                    type="number"
                    min={0.01}
                    max={100}
                    step={0.01}
                    value={form.percent_bp / 100}
                    onChange={(e) =>
                      set({ percent_bp: Math.round(parseFloat(e.target.value || "0") * 100) })
                    }
                  />
                </label>
                <label className="field">
                  <span>Cap ({form.currency}, optional)</span>
                  <input
                    inputMode="decimal"
                    value={major(form.max_discount_minor)}
                    onChange={(e) =>
                      set({ max_discount_minor: parseMajor(e.target.value, form.currency) || null })
                    }
                  />
                </label>
              </>
            )}
            {form.discount_type === "fixed" && (
              <label className="field">
                <span>Amount off ({form.currency})</span>
                <input
                  inputMode="decimal"
                  value={major(form.amount_minor)}
                  onChange={(e) => set({ amount_minor: parseMajor(e.target.value, form.currency) ?? 0 })}
                />
              </label>
            )}

            <label className="field">
              <span>Applies to</span>
              <select
                value={form.applies_to}
                disabled={isTrial}
                onChange={(e) =>
                  set({ applies_to: e.target.value as MerchPromoInput["applies_to"] })
                }
              >
                <option value="subscriptions">Single-kitchen subscriptions</option>
                <option value="rotations">4-kitchen rotations</option>
                <option value="both">Both</option>
              </select>
            </label>
            <label className="field">
              <span>Who can use it</span>
              <select
                value={form.audience}
                onChange={(e) =>
                  set({ audience: e.target.value as MerchPromoInput["audience"] })
                }
              >
                <option value="everyone">Everyone</option>
                <option value="new_customers">First-time subscribers only</option>
              </select>
            </label>
            <label className="field">
              <span>Kitchen pays (% of the discount)</span>
              <input
                type="number"
                min={0}
                max={100}
                value={form.kitchen_share_bp / 100}
                onChange={(e) =>
                  set({
                    kitchen_share_bp: Math.round(parseFloat(e.target.value || "0") * 100),
                  })
                }
              />
            </label>
            <label className="field">
              <span>Total uses (blank = unlimited)</span>
              <input
                type="number"
                min={1}
                value={form.usage_limit ?? ""}
                onChange={(e) => set({ usage_limit: intOrNull(e.target.value) })}
              />
            </label>
            <label className="field">
              <span>Uses per customer (blank = unlimited)</span>
              <input
                type="number"
                min={1}
                value={form.per_customer_limit ?? ""}
                onChange={(e) => set({ per_customer_limit: intOrNull(e.target.value) })}
              />
            </label>
            <label className="field">
              <span>Starts</span>
              <input
                type="datetime-local"
                value={toLocalInput(form.starts_at)}
                onChange={(e) => set({ starts_at: fromLocalInput(e.target.value) })}
              />
            </label>
            <label className="field">
              <span>Ends</span>
              <input
                type="datetime-local"
                value={toLocalInput(form.ends_at)}
                onChange={(e) => set({ ends_at: fromLocalInput(e.target.value) })}
              />
            </label>

            <fieldset className="field">
              <legend>Kitchens (none ticked = every kitchen)</legend>
              <div className="merch-picker">
                {(kitchens.data?.items ?? []).map((k) => (
                  <label key={k.branch_id} className="merch-check">
                    <input
                      type="checkbox"
                      checked={form.branch_ids.includes(k.branch_id)}
                      onChange={(e) =>
                        set({
                          branch_ids: e.target.checked
                            ? [...form.branch_ids, k.branch_id]
                            : form.branch_ids.filter((b) => b !== k.branch_id),
                        })
                      }
                    />
                    <span>
                      {k.branch_name} <span className="faint">· {k.vendor_name}</span>
                    </span>
                  </label>
                ))}
              </div>
            </fieldset>

            <label className="field merch-check">
              <input
                type="checkbox"
                checked={form.is_active}
                onChange={(e) => set({ is_active: e.target.checked })}
              />
              <span>Active</span>
            </label>
            {problem && <p className="danger">{problem}</p>}
            <div className="merch-actions">
              <button type="submit" className="btn primary" disabled={save.isPending}>
                {save.isPending ? "Saving…" : "Save promo"}
              </button>
              {editing !== "new" && (
                <button
                  type="button"
                  className="btn danger"
                  onClick={() =>
                    window.confirm("End and delete this promo? Checkouts already paid keep their discount.") &&
                    remove.mutate(editing)
                  }
                >
                  Delete
                </button>
              )}
            </div>
          </form>
        </Panel>
      )}

      {editing && editing !== "new" && (
        <Panel
          title="Redemptions"
          note={plural(redemptions.data?.items.length ?? 0, "redemption")}
        >
          {(redemptions.data?.items ?? []).length === 0 ? (
            <p className="muted">Not used yet.</p>
          ) : (
            <table className="money-table">
              <thead>
                <tr>
                  <th>When</th>
                  <th>For</th>
                  <th>Discount</th>
                  <th>Platform pays</th>
                  <th>Status</th>
                </tr>
              </thead>
              <tbody>
                {redemptions.data!.items.map((r) => (
                  <tr key={r.id}>
                    <td className="faint">{new Date(r.created_at).toLocaleString()}</td>
                    <td className="mono">{r.reference}</td>
                    <td>{aed(r.discount_minor, r.currency)}</td>
                    <td>{aed(r.platform_funded_minor, r.currency)}</td>
                    <td>{r.status}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </Panel>
      )}
    </div>
  );
}
