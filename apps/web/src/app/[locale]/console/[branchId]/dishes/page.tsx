"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useRef, useState } from "react";

import { Panel } from "@/components/admin/detail";
import { label, type ConsoleCopy } from "@/components/kitchen/copy";
import { ConsoleError, errorText, fieldErrors, useBranch } from "@/components/kitchen/portal";
import { useIdempotencyKey } from "@/lib/use-idempotency-key";
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
  const { t, locale } = branch;
  const c = t.dishes;
  const qc = useQueryClient();
  const key = ["vendor", "dishes", branch.branch_id];
  const { data, error, isPending, refetch } = useQuery({ queryKey: key, queryFn: () => listDishes(branch.branch_id) });
  const [editing, setEditing] = useState<{ id: string | null; input: DishInput } | null>(null);
  const [q, setQ] = useState("");
  const [showArchived, setShowArchived] = useState(false);

  // One key per open form: a double-click or a retry of the same new dish is
  // the same request to the server, so it cannot create the dish twice.
  const idem = useIdempotencyKey();
  const refresh = () => {
    qc.invalidateQueries({ queryKey: key });
    qc.invalidateQueries({ queryKey: ["vendor", "menu", branch.branch_id] });
    qc.invalidateQueries({ queryKey: ["vendor", "overview", branch.branch_id] });
  };
  const save = useMutation({
    mutationFn: (e: { id: string | null; input: DishInput }) =>
      e.id
        ? updateDish(branch.branch_id, e.id, e.input)
        : createDish(branch.branch_id, e.input, { headers: idem.headers() }),
    onSuccess: () => {
      idem.reset();
      setEditing(null);
      refresh();
    },
    // A rejected draft will be corrected and sent again as a different body,
    // which must not collide with the key of the one the server refused.
    onError: () => idem.reset(),
  });
  // Archive/restore from the list is its own mutation, so it never closes (or
  // discards the input of) a dish form that is open for a different dish.
  const archive = useMutation({
    mutationFn: (d: Dish) => updateDish(branch.branch_id, d.id, { ...toInput(d), is_active: !d.is_active }),
    onSuccess: refresh,
  });
  const open = (next: { id: string | null; input: DishInput } | null) => {
    save.reset();
    idem.reset();
    setEditing(next);
  };

  const all = data?.items ?? [];
  const items = all.filter(
    (d) =>
      (showArchived || d.is_active) &&
      (q === "" || `${d.name} ${d.name_ar ?? ""} ${d.tags.join(" ")}`.toLowerCase().includes(q.toLowerCase())),
  );

  const unphotographed = all.filter((d) => d.is_active && !d.image_url).length;

  return (
    <>
      {unphotographed > 0 && branch.editable ? (
        <div className="state photo-nudge">
          <strong>{c.noPhoto(unphotographed)}</strong> {c.noPhotoBody}
        </div>
      ) : null}
      {editing ? (
        <DishForm
          key={editing.id ?? "new"}
          title={editing.id ? c.edit : c.new}
          initial={editing.input}
          allergens={data?.allergens ?? []}
          busy={save.isPending}
          error={save.error}
          onCancel={() => open(null)}
          onSave={(input) => {
            if (save.isPending) return;
            save.mutate({ id: editing.id, input });
          }}
        />
      ) : null}

      <section className="panel">
        <header className="panel-head">
          <h3>{c.title}</h3>
          <input type="search" placeholder={c.search} value={q} onChange={(e) => setQ(e.target.value)} />
          <label className="muted">
            <input type="checkbox" checked={showArchived} onChange={(e) => setShowArchived(e.target.checked)} />{" "}
            {c.showArchived}
          </label>
          <div className="spacer" />
          {branch.editable ? (
            <button
              className="btn primary"
              type="button"
              onClick={() => open({ id: null, input: blank })}
            >
              {c.new}
            </button>
          ) : null}
        </header>
        {archive.error ? (
          <div className="state error" role="alert">
            {errorText(archive.error, locale)}
          </div>
        ) : null}
        {error ? (
          <ConsoleError error={error} onRetry={() => void refetch()} locale={locale} />
        ) : isPending ? (
          <div className="panel-body">
            <div className="skeleton" style={{ inlineSize: "40%" }} />
          </div>
        ) : items.length === 0 ? (
          <div className="state">{all.length === 0 ? c.empty : c.noMatch}</div>
        ) : (
          <div className="table-wrap">
            <table className="data">
              <thead>
                <tr>
                  <th className="grow">{c.colDish}</th>
                  <th>{c.colAllergens}</th>
                  <th>{c.colTags}</th>
                  <th className="num">{c.colKcal}</th>
                  <th className="num">{c.colOnMenu}</th>
                  <th>{c.colStatus}</th>
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
                        <span className="muted">{c.none}</span>
                      ) : (
                        d.allergens.map((a) => (
                          <span key={a} className="tag warn">
                            {label(t.allergens, a)}
                          </span>
                        ))
                      )}
                    </td>
                    <td>
                      {d.tags.map((t) => (
                        <span key={t} className="tag" dir="auto">
                          {t}
                        </span>
                      ))}
                    </td>
                    <td className="num">{d.calories ?? "—"}</td>
                    <td className="num">{d.menu_slots}</td>
                    <td>
                      <span className={`tag ${d.is_active ? "ok" : ""}`}>{d.is_active ? c.active : c.archived}</span>
                    </td>
                    {branch.editable ? (
                      <td className="nowrap">
                        <button
                          className="btn link"
                          type="button"
                          onClick={() => open({ id: d.id, input: toInput(d) })}
                        >
                          {t.edit}
                        </button>
                        <button
                          className="btn link"
                          type="button"
                          disabled={archive.isPending}
                          title={d.is_active && d.menu_slots > 0 ? c.archiveHint(d.menu_slots) : undefined}
                          onClick={() => {
                            if (
                              d.is_active &&
                              d.menu_slots > 0 &&
                              !window.confirm(c.archiveConfirm(d.name, d.menu_slots))
                            )
                              return;
                            archive.mutate(d);
                          }}
                        >
                          {d.is_active ? c.archive : c.restore}
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
  const { t, locale } = useBranch();
  const c: ConsoleCopy["dishes"] = t.dishes;
  const [d, setD] = useState<DishInput>(initial);
  const [tags, setTags] = useState(initial.tags.join(", "));
  const errs = fieldErrors(error, locale);
  // React disables the button only after a re-render; a fast double-click
  // lands both clicks before that. The ref closes the gap synchronously.
  const submitting = useRef(false);
  if (!busy) submitting.current = false;
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
          if (busy || submitting.current) return;
          submitting.current = true;
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
          {c.name}
          <input className="field" dir="auto" value={d.name} onChange={(e) => setD({ ...d, name: e.target.value })} required autoFocus />
          {errs.name ? <span className="err">{errs.name}</span> : null}
        </label>
        <label>
          {c.nameAr}
          <input className="field" dir="rtl" {...text("name_ar")} />
        </label>
        <label className="wide">
          {c.description}
          <textarea className="field" dir="auto" rows={2} {...text("description")} />
        </label>
        <label className="wide">
          {c.descriptionAr}
          <textarea className="field" dir="rtl" rows={2} {...text("description_ar")} />
        </label>
        <label>
          {c.calories}
          <input className="field" {...num("calories")} />
          {errs.calories ? <span className="err">{errs.calories}</span> : null}
        </label>
        <label>
          {c.protein}
          <input className="field" {...num("protein_g")} />
        </label>
        <label>
          {c.carbs}
          <input className="field" {...num("carbs_g")} />
        </label>
        <label>
          {c.fat}
          <input className="field" {...num("fat_g")} />
        </label>
        <div className="vp-label wide">
          {c.allergensLabel} <span className="hint">{c.allergensHint}</span>
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
                  {label(t.allergens, a)}
                </button>
              );
            })}
          </div>
          {errs.allergens ? <span className="err">{errs.allergens}</span> : null}
        </div>
        <label>
          {c.tags} <span className="hint">{c.tagsHint}</span>
          <input className="field" dir="auto" value={tags} onChange={(e) => setTags(e.target.value)} />
          {errs.tags ? <span className="err">{errs.tags}</span> : null}
        </label>
        <label>
          {c.imageUrl}
          <input className="field" dir="ltr" placeholder="https://" {...text("image_url")} />
          {errs.image_url ? <span className="err">{errs.image_url}</span> : null}
        </label>
        <div className="vp-actions wide">
          <button className="btn primary" type="submit" disabled={busy}>
            {busy ? t.saving : c.save}
          </button>
          <button className="btn" type="button" onClick={onCancel} disabled={busy}>
            {t.neverMind}
          </button>
          {error && Object.keys(errs).length === 0 ? <span className="danger">{errorText(error, locale)}</span> : null}
        </div>
      </form>
    </Panel>
  );
}
