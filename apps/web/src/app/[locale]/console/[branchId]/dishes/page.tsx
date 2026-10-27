"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

import { Panel } from "@/components/admin/detail";
import { errorText, fieldErrors, useBranch } from "@/components/kitchen/portal";
import { createDish, listDishes, updateDish, type Dish, type DishInput } from "@/lib/vendor-api";

const blank: DishInput = {
  name: "",
  name_ar: null,
  description: null,
  description_ar: null,
  image_url: null,
  calories: null,
  protein_g: null,
  carbs_g: null,
  fat_g: null,
  allergens: [],
  tags: [],
  is_active: true,
};

const toInput = (d: Dish): DishInput => ({
  name: d.name,
  name_ar: d.name_ar,
  description: d.description,
  description_ar: d.description_ar,
  image_url: d.image_url,
  calories: d.calories,
  protein_g: d.protein_g,
  carbs_g: d.carbs_g,
  fat_g: d.fat_g,
  allergens: d.allergens,
  tags: d.tags,
  is_active: d.is_active,
});

/**
 * The dishes a kitchen cooks. A dish is defined once and placed on the weekly
 * menu as many times as it is served. Archiving rather than deleting, because
 * past deliveries and customers' meal choices still name the dish.
 */
export default function DishesPage() {
  const branch = useBranch();
  const qc = useQueryClient();
  const key = ["vendor", "dishes", branch.branch_id];
  const { data, error, isPending } = useQuery({ queryKey: key, queryFn: () => listDishes(branch.branch_id) });
  const [editing, setEditing] = useState<{ id: string | null; input: DishInput } | null>(null);
  const [q, setQ] = useState("");
  const [showArchived, setShowArchived] = useState(false);

  const save = useMutation({
    mutationFn: (e: { id: string | null; input: DishInput }) =>
      e.id ? updateDish(branch.branch_id, e.id, e.input) : createDish(branch.branch_id, e.input),
    onSuccess: () => {
      setEditing(null);
      qc.invalidateQueries({ queryKey: key });
      qc.invalidateQueries({ queryKey: ["vendor", "menu", branch.branch_id] });
      qc.invalidateQueries({ queryKey: ["vendor", "overview", branch.branch_id] });
    },
  });

  const all = data?.items ?? [];
  const items = all.filter(
    (d) =>
      (showArchived || d.is_active) &&
      (q === "" || `${d.name} ${d.name_ar ?? ""} ${d.tags.join(" ")}`.toLowerCase().includes(q.toLowerCase())),
  );

  return (
    <>
      {editing ? (
        <DishForm
          key={editing.id ?? "new"}
          title={editing.id ? "Edit dish" : "New dish"}
          initial={editing.input}
          allergens={data?.allergens ?? []}
          busy={save.isPending}
          error={save.error}
          onCancel={() => {
            save.reset();
            setEditing(null);
          }}
          onSave={(input) => save.mutate({ id: editing.id, input })}
        />
      ) : null}

      <section className="panel">
        <header className="panel-head">
          <h3>Dishes</h3>
          <input type="search" placeholder="Search dishes" value={q} onChange={(e) => setQ(e.target.value)} />
          <label className="muted">
            <input type="checkbox" checked={showArchived} onChange={(e) => setShowArchived(e.target.checked)} /> Show
            archived
          </label>
          <div className="spacer" />
          {branch.editable ? (
            <button
              className="btn primary"
              type="button"
              onClick={() => {
                save.reset();
                setEditing({ id: null, input: blank });
              }}
            >
              New dish
            </button>
          ) : null}
        </header>
        {error ? (
          <div className="state error">{errorText(error)}</div>
        ) : isPending ? (
          <div className="panel-body">
            <div className="skeleton" style={{ inlineSize: "40%" }} />
          </div>
        ) : items.length === 0 ? (
          <div className="state">{all.length === 0 ? "No dishes yet. Add the first dish you cook." : "Nothing matches."}</div>
        ) : (
          <div className="table-wrap">
            <table className="data">
              <thead>
                <tr>
                  <th className="grow">Dish</th>
                  <th>Allergens</th>
                  <th>Tags</th>
                  <th className="num">kcal</th>
                  <th className="num">On menu</th>
                  <th>Status</th>
                  {branch.editable ? <th /> : null}
                </tr>
              </thead>
              <tbody>
                {items.map((d) => (
                  <tr key={d.id}>
                    <td className="grow">
                      <strong dir="auto">{d.name}</strong>
                      {d.name_ar ? (
                        <div className="muted" dir="rtl">
                          {d.name_ar}
                        </div>
                      ) : null}
                    </td>
                    <td>
                      {d.allergens.length === 0 ? (
                        <span className="muted">none</span>
                      ) : (
                        d.allergens.map((a) => (
                          <span key={a} className="tag warn">
                            {a.replace("_", " ")}
                          </span>
                        ))
                      )}
                    </td>
                    <td>
                      {d.tags.map((t) => (
                        <span key={t} className="tag">
                          {t}
                        </span>
                      ))}
                    </td>
                    <td className="num">{d.calories ?? "—"}</td>
                    <td className="num">{d.menu_slots}</td>
                    <td>
                      <span className={`tag ${d.is_active ? "ok" : ""}`}>{d.is_active ? "active" : "archived"}</span>
                    </td>
                    {branch.editable ? (
                      <td className="nowrap">
                        <button
                          className="btn link"
                          type="button"
                          onClick={() => {
                            save.reset();
                            setEditing({ id: d.id, input: toInput(d) });
                          }}
                        >
                          Edit
                        </button>
                        <button
                          className="btn link"
                          type="button"
                          disabled={save.isPending}
                          title={
                            d.is_active && d.menu_slots > 0
                              ? `On ${d.menu_slots} menu slot(s) — archiving leaves them showing a dish you no longer cook`
                              : undefined
                          }
                          onClick={() => {
                            if (
                              d.is_active &&
                              d.menu_slots > 0 &&
                              !window.confirm(
                                `${d.name} is on ${d.menu_slots} menu slot(s). Archive it anyway? Replace it on the weekly menu afterwards.`,
                              )
                            )
                              return;
                            save.mutate({ id: d.id, input: { ...toInput(d), is_active: !d.is_active } });
                          }}
                        >
                          {d.is_active ? "Archive" : "Restore"}
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

function DishForm({
  title,
  initial,
  allergens,
  busy,
  error,
  onSave,
  onCancel,
}: {
  title: string;
  initial: DishInput;
  allergens: string[];
  busy: boolean;
  error: unknown;
  onSave: (d: DishInput) => void;
  onCancel: () => void;
}) {
  const [d, setD] = useState<DishInput>(initial);
  const [tags, setTags] = useState(initial.tags.join(", "));
  const errs = fieldErrors(error);
  const text = (k: "name_ar" | "description" | "description_ar" | "image_url") => ({
    value: d[k] ?? "",
    onChange: (e: { target: { value: string } }) => setD({ ...d, [k]: e.target.value === "" ? null : e.target.value }),
  });
  const num = (k: "calories" | "protein_g" | "carbs_g" | "fat_g") => ({
    value: d[k] ?? "",
    type: "number",
    min: 0,
    step: k === "calories" ? 1 : 0.1,
    onChange: (e: { target: { value: string } }) =>
      setD({ ...d, [k]: e.target.value === "" ? null : Number(e.target.value) }),
  });

  return (
    <Panel title={title}>
      <form
        className="vp-form"
        onSubmit={(e) => {
          e.preventDefault();
          onSave({
            ...d,
            tags: tags
              .split(",")
              .map((t) => t.trim())
              .filter(Boolean),
          });
        }}
      >
        <label>
          Name
          <input className="field" value={d.name} onChange={(e) => setD({ ...d, name: e.target.value })} required autoFocus />
          {errs.name ? <span className="err">{errs.name}</span> : null}
        </label>
        <label>
          Name in Arabic
          <input className="field" dir="rtl" {...text("name_ar")} />
        </label>
        <label className="wide">
          Description
          <textarea className="field" rows={2} {...text("description")} />
        </label>
        <label className="wide">
          Description in Arabic
          <textarea className="field" dir="rtl" rows={2} {...text("description_ar")} />
        </label>
        <label>
          Calories
          <input className="field" {...num("calories")} />
          {errs.calories ? <span className="err">{errs.calories}</span> : null}
        </label>
        <label>
          Protein (g)
          <input className="field" {...num("protein_g")} />
        </label>
        <label>
          Carbs (g)
          <input className="field" {...num("carbs_g")} />
        </label>
        <label>
          Fat (g)
          <input className="field" {...num("fat_g")} />
        </label>
        <div className="vp-label wide">
          Allergens <span className="hint">customers filter by these, so declare every one</span>
          <div className="vp-chips">
            {allergens.map((a) => {
              const on = d.allergens.includes(a);
              return (
                <button
                  key={a}
                  type="button"
                  className="vp-chip"
                  aria-pressed={on}
                  onClick={() =>
                    setD({ ...d, allergens: on ? d.allergens.filter((x) => x !== a) : [...d.allergens, a] })
                  }
                >
                  {a.replace("_", " ")}
                </button>
              );
            })}
          </div>
          {errs.allergens ? <span className="err">{errs.allergens}</span> : null}
        </div>
        <label>
          Tags <span className="hint">comma-separated, e.g. spicy, vegetarian</span>
          <input className="field" value={tags} onChange={(e) => setTags(e.target.value)} />
          {errs.tags ? <span className="err">{errs.tags}</span> : null}
        </label>
        <label>
          Image URL
          <input className="field" placeholder="https://" {...text("image_url")} />
          {errs.image_url ? <span className="err">{errs.image_url}</span> : null}
        </label>
        <div className="vp-actions wide">
          <button className="btn primary" type="submit" disabled={busy}>
            {busy ? "Saving…" : "Save dish"}
          </button>
          <button className="btn" type="button" onClick={onCancel} disabled={busy}>
            Never mind
          </button>
          {error && Object.keys(errs).length === 0 ? <span className="danger">{errorText(error)}</span> : null}
        </div>
      </form>
    </Panel>
  );
}
