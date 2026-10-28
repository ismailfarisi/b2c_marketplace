"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";

import { Panel, plural } from "@/components/admin/detail";
import {
  fromLocalInput,
  toLocalInput,
  windowState,
} from "@/components/admin/merch";
import {
  deleteMerchCollection,
  getMealKitchens,
  getMerchCollections,
  previewMerchCollection,
  saveMerchCollection,
  setMerchCollectionItems,
  type MerchCollection,
  type MerchCollectionInput,
} from "@/lib/admin-api";

const SLOTS = [
  "breakfast",
  "lunch",
  "dinner",
  "snack",
  "suhoor",
  "iftar",
] as const;
const SORTS = {
  rating: "Rating",
  price_asc: "Price, low first",
  price_desc: "Price, high first",
  newest: "Newest",
};

const EMPTY: MerchCollectionInput = {
  slug: "",
  title: "",
  title_ar: null,
  subtitle: null,
  subtitle_ar: null,
  rules: {},
  sort: "rating",
  max_items: 12,
  starts_at: null,
  ends_at: null,
  is_active: true,
};

const list = (v: string) =>
  v
    .split(",")
    .map((s) => s.trim().toLowerCase())
    .filter(Boolean);
const opt = (v: string) => (v.trim() ? v : null);

type Item = { branch_id: string; role: "pin" | "exclude" };

/**
 * Kitchen collections: rules pick kitchens, pins force kitchens in at the
 * front in their order, excludes take them out. Customers only ever see the
 * kitchens that deliver to them, so the preview takes a point.
 */
export default function MerchCollectionsPage() {
  const qc = useQueryClient();
  const key = ["admin", "merch", "collections"];
  const { data, error } = useQuery({
    queryKey: key,
    queryFn: getMerchCollections,
  });
  const kitchens = useQuery({
    queryKey: ["admin", "meals", "kitchens", "", false],
    queryFn: () => getMealKitchens(),
  });
  const [editing, setEditing] = useState<string | null>(null);
  const [form, setForm] = useState<MerchCollectionInput>(EMPTY);
  const [cuisines, setCuisines] = useState("");
  const [dietary, setDietary] = useState("");
  const [maxPrice, setMaxPrice] = useState("");
  const [items, setItems] = useState<Item[]>([]);
  const [pick, setPick] = useState("");
  const [pt, setPt] = useState({ lat: "25.13", lng: "55.23" });
  const [problem, setProblem] = useState<string | null>(null);
  const rows = data?.items ?? [];
  const names = new Map(
    (kitchens.data?.items ?? []).map((k) => [
      k.branch_id,
      `${k.vendor_name} · ${k.branch_name}`,
    ]),
  );

  const open = (c: MerchCollection | null) => {
    setProblem(null);
    const src = c ?? { ...EMPTY, items: [] as MerchCollection["items"] };
    setEditing(c ? c.id : "new");
    setForm({
      slug: src.slug,
      title: src.title,
      title_ar: src.title_ar,
      subtitle: src.subtitle,
      subtitle_ar: src.subtitle_ar,
      rules: src.rules,
      sort: src.sort,
      max_items: src.max_items,
      starts_at: src.starts_at,
      ends_at: src.ends_at,
      is_active: src.is_active,
    });
    setCuisines((src.rules.cuisines ?? []).join(", "));
    setDietary((src.rules.dietary ?? []).join(", "));
    setMaxPrice(
      src.rules.max_price_minor ? String(src.rules.max_price_minor / 100) : "",
    );
    setItems(
      (src.items ?? []).map((i) => ({ branch_id: i.branch_id, role: i.role })),
    );
  };

  const current = rows.find((c) => c.id === editing) ?? null;
  useEffect(() => {
    if (editing && editing !== "new" && !current && data) setEditing(null);
  }, [editing, current, data]);

  const save = useMutation({
    mutationFn: () => {
      const price = Number(maxPrice);
      const rules: MerchCollectionInput["rules"] = {
        ...form.rules,
        cuisines: list(cuisines),
        dietary: list(dietary),
      };
      if (maxPrice && price > 0)
        rules.max_price_minor = Math.round(price * 100);
      else delete rules.max_price_minor;
      return saveMerchCollection(editing === "new" ? null : editing, {
        ...form,
        rules,
      });
    },
    onSuccess: async (c) => {
      setProblem(null);
      setEditing(c.id);
      await qc.invalidateQueries({ queryKey: key });
    },
    onError: (e: Error) => setProblem(e.message),
  });
  const saveItems = useMutation({
    mutationFn: () => setMerchCollectionItems(editing!, items),
    onSuccess: async () => {
      setProblem(null);
      await qc.invalidateQueries({ queryKey: key });
    },
    onError: (e: Error) => setProblem(e.message),
  });
  const remove = useMutation({
    mutationFn: (id: string) => deleteMerchCollection(id),
    onSuccess: async () => {
      setEditing(null);
      await qc.invalidateQueries({ queryKey: key });
    },
    onError: (e: Error) => setProblem(e.message),
  });
  const point = { lat: Number(pt.lat), lng: Number(pt.lng) };
  const preview = useQuery({
    queryKey: [
      "admin",
      "merch",
      "preview",
      editing,
      current?.updated_at,
      pt.lat,
      pt.lng,
    ],
    queryFn: () => previewMerchCollection(editing!, point),
    enabled:
      !!editing &&
      editing !== "new" &&
      Number.isFinite(point.lat) &&
      Number.isFinite(point.lng),
  });

  const move = (i: number, d: -1 | 1) => {
    const next = [...items];
    const j = i + d;
    const a = next[i];
    const b = next[j];
    if (!a || !b) return;
    next[i] = b;
    next[j] = a;
    setItems(next);
  };
  const add = (role: Item["role"]) => {
    if (!pick || items.some((i) => i.branch_id === pick)) return;
    setItems([...items, { branch_id: pick, role }]);
    setPick("");
  };
  const text = (
    k: "title" | "title_ar" | "subtitle" | "subtitle_ar" | "slug",
    label: string,
    dir?: "rtl",
  ) => (
    <label className="field">
      <span>{label}</span>
      <input
        dir={dir}
        value={form[k] ?? ""}
        onChange={(e) =>
          setForm({
            ...form,
            [k]:
              k === "title" || k === "slug"
                ? e.target.value
                : opt(e.target.value),
          })
        }
      />
    </label>
  );

  if (error)
    return (
      <p className="danger">
        Collections could not be loaded. Needs commerce.read.
      </p>
    );

  return (
    <div className="detail">
      <div className="detail-head">
        <div>
          <h2>Kitchen collections</h2>
          <p className="console-lede">
            Groups of kitchens to place on the home page, each with its own
            page.
          </p>
        </div>
        <div className="spacer" />
        <button
          type="button"
          className="btn primary"
          onClick={() => open(null)}
        >
          New collection
        </button>
      </div>

      <Panel title="Collections" note={plural(rows.length, "collection")}>
        {rows.length === 0 ? (
          <p className="muted">No collections yet.</p>
        ) : (
          <table className="money-table">
            <thead>
              <tr>
                <th>Title</th>
                <th>Page</th>
                <th>State</th>
                <th className="num">Pinned</th>
                <th>Sort</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((c) => (
                <tr key={c.id}>
                  <td>
                    <button
                      type="button"
                      className="btn link"
                      onClick={() => open(c)}
                    >
                      {c.title}
                    </button>
                  </td>
                  <td className="faint mono">/collections/{c.slug}</td>
                  <td>{windowState(c)}</td>
                  <td className="num">
                    {(c.items ?? []).filter((i) => i.role === "pin").length}
                  </td>
                  <td className="faint">{SORTS[c.sort]}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Panel>

      {editing && (
        <Panel
          title={
            editing === "new"
              ? "New collection"
              : `Edit “${current?.title ?? ""}”`
          }
        >
          <form
            className="merch-form"
            onSubmit={(e) => {
              e.preventDefault();
              save.mutate();
            }}
          >
            {text("title", "Title (English)")}
            {text("title_ar", "Title (Arabic)", "rtl")}
            {text("subtitle", "Subtitle (English)")}
            {text("subtitle_ar", "Subtitle (Arabic)", "rtl")}
            {text("slug", "Page address, e.g. healthy-lunch")}
            <label className="field">
              <span>Sort</span>
              <select
                value={form.sort}
                onChange={(e) =>
                  setForm({
                    ...form,
                    sort: e.target.value as MerchCollectionInput["sort"],
                  })
                }
              >
                {Object.entries(SORTS).map(([v, l]) => (
                  <option key={v} value={v}>
                    {l}
                  </option>
                ))}
              </select>
            </label>
            <label className="field">
              <span>Cuisines, any of (comma separated)</span>
              <input
                value={cuisines}
                onChange={(e) => setCuisines(e.target.value)}
                placeholder="indian, arabic"
              />
            </label>
            <label className="field">
              <span>Dietary tags, any of</span>
              <input
                value={dietary}
                onChange={(e) => setDietary(e.target.value)}
                placeholder="vegetarian, keto"
              />
            </label>
            <label className="field">
              <span>Highest plan price (major units)</span>
              <input
                inputMode="decimal"
                value={maxPrice}
                onChange={(e) => setMaxPrice(e.target.value)}
              />
            </label>
            <label className="field">
              <span>Most kitchens shown</span>
              <input
                type="number"
                min={1}
                max={48}
                value={form.max_items ?? ""}
                onChange={(e) =>
                  setForm({
                    ...form,
                    max_items: e.target.value ? Number(e.target.value) : null,
                  })
                }
              />
            </label>
            <fieldset className="field merch-slots">
              <span>Serves any of these meals</span>
              {SLOTS.map((s) => (
                <label key={s} className="merch-check">
                  <input
                    type="checkbox"
                    checked={(form.rules.slots ?? []).includes(s)}
                    onChange={(e) => {
                      const cur = form.rules.slots ?? [];
                      const slots = e.target.checked
                        ? [...cur, s]
                        : cur.filter((x) => x !== s);
                      setForm({ ...form, rules: { ...form.rules, slots } });
                    }}
                  />
                  {s}
                </label>
              ))}
            </fieldset>
            <label className="field merch-check">
              <input
                type="checkbox"
                checked={!!form.rules.rotation_only}
                onChange={(e) =>
                  setForm({
                    ...form,
                    rules: { ...form.rules, rotation_only: e.target.checked },
                  })
                }
              />
              <span>Only kitchens in the 4-kitchen rotation</span>
            </label>
            <label className="field">
              <span>Starts</span>
              <input
                type="datetime-local"
                value={toLocalInput(form.starts_at)}
                onChange={(e) =>
                  setForm({
                    ...form,
                    starts_at: fromLocalInput(e.target.value),
                  })
                }
              />
            </label>
            <label className="field">
              <span>Ends</span>
              <input
                type="datetime-local"
                value={toLocalInput(form.ends_at)}
                onChange={(e) =>
                  setForm({ ...form, ends_at: fromLocalInput(e.target.value) })
                }
              />
            </label>
            <label className="field merch-check">
              <input
                type="checkbox"
                checked={form.is_active}
                onChange={(e) =>
                  setForm({ ...form, is_active: e.target.checked })
                }
              />
              <span>Active</span>
            </label>
            {problem && <p className="danger">{problem}</p>}
            <div className="merch-actions">
              <button
                type="submit"
                className="btn primary"
                disabled={save.isPending}
              >
                {save.isPending ? "Saving…" : "Save collection"}
              </button>
              {editing !== "new" && (
                <button
                  type="button"
                  className="btn danger"
                  onClick={() =>
                    window.confirm("Delete this collection?") &&
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
        <>
          <Panel
            title="Pinned and excluded kitchens"
            note="Pins show first, in this order"
          >
            <div className="merch-picker">
              <select
                value={pick}
                onChange={(e) => setPick(e.target.value)}
                aria-label="Kitchen"
              >
                <option value="">Choose a kitchen…</option>
                {(kitchens.data?.items ?? []).map((k) => (
                  <option key={k.branch_id} value={k.branch_id}>
                    {k.vendor_name} · {k.branch_name}
                  </option>
                ))}
              </select>
              <button type="button" className="btn" onClick={() => add("pin")}>
                Pin
              </button>
              <button
                type="button"
                className="btn"
                onClick={() => add("exclude")}
              >
                Exclude
              </button>
            </div>
            {items.length === 0 ? (
              <p className="muted">No pins or excludes: the rules decide.</p>
            ) : (
              <ol className="merch-list">
                {items.map((it, i) => (
                  <li key={it.branch_id}>
                    <span className={it.role === "pin" ? "" : "faint"}>
                      {it.role === "pin" ? "Pinned" : "Excluded"} ·{" "}
                      {names.get(it.branch_id) ?? it.branch_id}
                    </span>
                    <span className="merch-row-actions">
                      <button
                        type="button"
                        className="btn"
                        onClick={() => move(i, -1)}
                        disabled={i === 0}
                        aria-label="Move up"
                      >
                        ↑
                      </button>
                      <button
                        type="button"
                        className="btn"
                        onClick={() => move(i, 1)}
                        disabled={i === items.length - 1}
                        aria-label="Move down"
                      >
                        ↓
                      </button>
                      <button
                        type="button"
                        className="btn danger"
                        onClick={() =>
                          setItems(
                            items.filter((x) => x.branch_id !== it.branch_id),
                          )
                        }
                      >
                        Remove
                      </button>
                    </span>
                  </li>
                ))}
              </ol>
            )}
            <div className="merch-actions">
              <button
                type="button"
                className="btn primary"
                onClick={() => saveItems.mutate()}
                disabled={saveItems.isPending}
              >
                {saveItems.isPending ? "Saving…" : "Save pins"}
              </button>
            </div>
          </Panel>

          <Panel title="Preview" note="What a customer at this point sees">
            <div className="merch-picker">
              <label className="field">
                <span>Lat</span>
                <input
                  value={pt.lat}
                  onChange={(e) => setPt({ ...pt, lat: e.target.value })}
                />
              </label>
              <label className="field">
                <span>Lng</span>
                <input
                  value={pt.lng}
                  onChange={(e) => setPt({ ...pt, lng: e.target.value })}
                />
              </label>
            </div>
            {preview.isError ? (
              <p className="danger">
                Preview failed: {(preview.error as Error).message}
              </p>
            ) : !preview.data ? (
              <p className="muted">Loading…</p>
            ) : preview.data.items.length === 0 ? (
              <p className="muted">
                No kitchen here matches. The rail is hidden for customers at
                this point.
              </p>
            ) : (
              <ol className="merch-list">
                {preview.data.items.map((k) => (
                  <li key={k.branch_id}>
                    {k.vendor_name} · {k.name}
                    <span className="faint">{k.cuisines.join(", ")}</span>
                  </li>
                ))}
              </ol>
            )}
          </Panel>
        </>
      )}
    </div>
  );
}
