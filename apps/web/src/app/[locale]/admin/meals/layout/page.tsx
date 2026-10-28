"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";

import { Panel } from "@/components/admin/detail";
import { fromLocalInput, toLocalInput } from "@/components/admin/merch";
import { Tag } from "@/components/admin/table";
import {
  getMerchBanners,
  getMerchCollections,
  getMerchLayout,
  previewMerchLayout,
  publishMerchLayout,
  restoreMerchVersion,
  saveMerchDraft,
  type MerchSection,
  type MerchVersion,
} from "@/lib/admin-api";
import type { MealsHome, SectionType } from "@/lib/meals-api";

const TYPES: Record<SectionType, { label: string; help: string }> = {
  banner_carousel: {
    label: "Banner carousel",
    help: "Banners you pick, in their window",
  },
  kitchen_rail: {
    label: "Kitchen rail",
    help: "One collection, swiped sideways",
  },
  cuisine_strip: {
    label: "Cuisine strip",
    help: "Cuisines of kitchens delivering here",
  },
  rotation_promo: {
    label: "Rotation promo",
    help: "The 4-kitchen rotation card",
  },
  all_kitchens: {
    label: "All kitchens",
    help: "Search, meal filters and the full list",
  },
};

const newId = () =>
  typeof crypto !== "undefined" && "randomUUID" in crypto
    ? crypto.randomUUID()
    : String(Date.now());

function versionState(v: MerchVersion, liveId: string | null) {
  if (v.id === liveId) return <Tag tone="ok">live</Tag>;
  if (v.publish_at && Date.parse(v.publish_at) > Date.now())
    return <Tag tone="warn">scheduled</Tag>;
  return <Tag>past</Tag>;
}

/**
 * The storefront home, as an ordered list of sections. Edits go to a draft;
 * publishing makes it live now or at a set time, and any older version can be
 * brought back into the draft. With nothing published the default home shows.
 */
export default function MerchLayoutPage() {
  const qc = useQueryClient();
  const key = ["admin", "merch", "layout"];
  const state = useQuery({ queryKey: key, queryFn: getMerchLayout });
  const collections = useQuery({
    queryKey: ["admin", "merch", "collections"],
    queryFn: getMerchCollections,
  });
  const banners = useQuery({
    queryKey: ["admin", "merch", "banners"],
    queryFn: getMerchBanners,
  });
  const [sections, setSections] = useState<MerchSection[] | null>(null);
  const [dirty, setDirty] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  const [publishAt, setPublishAt] = useState("");
  const [note, setNote] = useState("");
  const [pt, setPt] = useState({ lat: "25.13", lng: "55.23" });
  const [preview, setPreview] = useState<MealsHome | null>(null);

  const live =
    state.data?.versions.find((v) => v.id === state.data?.live_id) ?? null;
  useEffect(() => {
    if (state.data && sections === null) {
      setSections(state.data.draft?.sections ?? live?.sections ?? []);
    }
  }, [state.data, live, sections]);

  const edit = (next: MerchSection[]) => {
    setSections(next);
    setDirty(true);
    setPreview(null);
  };
  const patch = (i: number, p: Partial<MerchSection>) =>
    edit(
      (sections ?? []).map((s, j) =>
        j === i
          ? { ...s, ...p, settings: { ...s.settings, ...p.settings } }
          : s,
      ),
    );
  const move = (i: number, d: -1 | 1) => {
    const next = [...(sections ?? [])];
    const a = next[i];
    const b = next[i + d];
    if (!a || !b) return;
    next[i] = b;
    next[i + d] = a;
    edit(next);
  };

  const onErr = (e: Error) => setProblem(e.message);
  const saveDraft = useMutation({
    mutationFn: () => saveMerchDraft(sections ?? []),
    onSuccess: async (v) => {
      setProblem(null);
      setDirty(false);
      setSections(v.sections);
      await qc.invalidateQueries({ queryKey: key });
    },
    onError: onErr,
  });
  const publish = useMutation({
    mutationFn: async () => {
      if (dirty) await saveMerchDraft(sections ?? []);
      return publishMerchLayout(fromLocalInput(publishAt), note.trim());
    },
    onSuccess: async () => {
      setProblem(null);
      setDirty(false);
      setNote("");
      setPublishAt("");
      setSections(null);
      await qc.invalidateQueries({ queryKey: key });
    },
    onError: onErr,
  });
  const restore = useMutation({
    mutationFn: (id: string) => restoreMerchVersion(id),
    onSuccess: async (v) => {
      setProblem(null);
      setDirty(false);
      setSections(v.sections);
      await qc.invalidateQueries({ queryKey: key });
    },
    onError: onErr,
  });
  const runPreview = useMutation({
    mutationFn: () =>
      previewMerchLayout(sections ?? [], {
        lat: Number(pt.lat),
        lng: Number(pt.lng),
      }),
    onSuccess: (h) => {
      setProblem(null);
      setPreview(h);
    },
    onError: onErr,
  });

  if (state.error)
    return (
      <p className="danger">
        The layout could not be loaded. Needs commerce.read.
      </p>
    );
  if (!state.data || sections === null)
    return <p className="muted">Loading…</p>;
  const colls = collections.data?.items ?? [];
  const bans = banners.data?.items ?? [];
  const draft = state.data.draft;

  return (
    <div className="detail">
      <div className="detail-head">
        <div>
          <h2>Storefront home layout</h2>
          <p className="console-lede">
            {live
              ? `Version ${live.version_number} is live.`
              : "Nothing published: customers see the default home."}{" "}
            {draft
              ? `Draft saved ${new Date(draft.updated_at).toLocaleString()}.`
              : "No draft yet."}
            {dirty && " Unsaved changes."}
          </p>
        </div>
      </div>

      <Panel title="Sections" note="Top to bottom, as customers see them">
        {sections.length === 0 && (
          <p className="muted">No sections. Add one below.</p>
        )}
        <ol className="merch-sections">
          {sections.map((s, i) => (
            <li key={s.id} className={s.enabled ? "" : "merch-off"}>
              <div className="merch-sec-head">
                <strong>{TYPES[s.type].label}</strong>
                <span className="faint">{TYPES[s.type].help}</span>
                <span className="merch-row-actions">
                  <label className="merch-check">
                    <input
                      type="checkbox"
                      checked={s.enabled}
                      onChange={(e) => patch(i, { enabled: e.target.checked })}
                    />
                    Shown
                  </label>
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
                    disabled={i === sections.length - 1}
                    aria-label="Move down"
                  >
                    ↓
                  </button>
                  <button
                    type="button"
                    className="btn danger"
                    onClick={() => edit(sections.filter((_, j) => j !== i))}
                  >
                    Remove
                  </button>
                </span>
              </div>
              {(s.type === "kitchen_rail" ||
                s.type === "cuisine_strip" ||
                s.type === "banner_carousel") && (
                <div className="merch-form">
                  {s.type !== "banner_carousel" && (
                    <>
                      <label className="field">
                        <span>Heading (English, optional)</span>
                        <input
                          value={s.settings.title ?? ""}
                          onChange={(e) =>
                            patch(i, { settings: { title: e.target.value } })
                          }
                        />
                      </label>
                      <label className="field">
                        <span>Heading (Arabic, optional)</span>
                        <input
                          dir="rtl"
                          value={s.settings.title_ar ?? ""}
                          onChange={(e) =>
                            patch(i, { settings: { title_ar: e.target.value } })
                          }
                        />
                      </label>
                    </>
                  )}
                  {s.type === "kitchen_rail" && (
                    <label className="field">
                      <span>Collection</span>
                      <select
                        value={s.settings.collection_id ?? ""}
                        onChange={(e) =>
                          patch(i, {
                            settings: { collection_id: e.target.value },
                          })
                        }
                      >
                        <option value="">Choose…</option>
                        {colls.map((c) => (
                          <option key={c.id} value={c.id}>
                            {c.title} (/{c.slug})
                          </option>
                        ))}
                      </select>
                    </label>
                  )}
                  {s.type === "banner_carousel" && (
                    <fieldset className="field merch-slots">
                      <span>Banners, in this order</span>
                      {bans.length === 0 && (
                        <span className="muted">Create banners first.</span>
                      )}
                      {bans.map((b) => {
                        const ids = s.settings.banner_ids ?? [];
                        return (
                          <label key={b.id} className="merch-check">
                            <input
                              type="checkbox"
                              checked={ids.includes(b.id)}
                              onChange={(e) =>
                                patch(i, {
                                  settings: {
                                    banner_ids: e.target.checked
                                      ? [...ids, b.id]
                                      : ids.filter((x) => x !== b.id),
                                  },
                                })
                              }
                            />
                            {b.title}
                          </label>
                        );
                      })}
                    </fieldset>
                  )}
                </div>
              )}
            </li>
          ))}
        </ol>
        <div className="merch-picker">
          {(Object.keys(TYPES) as SectionType[]).map((t) => (
            <button
              key={t}
              type="button"
              className="btn"
              onClick={() =>
                edit([
                  ...sections,
                  { id: newId(), type: t, enabled: true, settings: {} },
                ])
              }
            >
              + {TYPES[t].label}
            </button>
          ))}
        </div>
        {problem && <p className="danger">{problem}</p>}
        <div className="merch-actions">
          <button
            type="button"
            className="btn primary"
            onClick={() => saveDraft.mutate()}
            disabled={saveDraft.isPending}
          >
            {saveDraft.isPending ? "Saving…" : "Save draft"}
          </button>
        </div>
      </Panel>

      <Panel
        title="Preview"
        note="Resolved for a customer at this point; nothing is saved"
      >
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
          <button
            type="button"
            className="btn"
            onClick={() => runPreview.mutate()}
            disabled={runPreview.isPending}
          >
            Preview
          </button>
        </div>
        {preview?.sections && (
          <ol className="merch-list">
            {preview.sections.length === 0 && (
              <li className="muted">Every section is empty here.</li>
            )}
            {preview.sections.map((s) => (
              <li key={s.id}>
                <strong>{TYPES[s.type].label}</strong>
                <span className="faint">
                  {s.collection ? `${s.collection.title}: ` : ""}
                  {s.kitchens?.map((k) => k.name).join(", ") ??
                    s.banners?.map((b) => b.title).join(", ") ??
                    s.cuisines
                      ?.map((c) => `${c.cuisine} (${c.kitchens})`)
                      .join(", ") ??
                    ""}
                </span>
              </li>
            ))}
          </ol>
        )}
        {preview &&
          sections.some((s) => s.enabled) &&
          (preview.sections?.length ?? 0) <
            sections.filter((s) => s.enabled).length && (
            <p className="muted">
              Sections with nothing to show here are hidden.
            </p>
          )}
      </Panel>

      <Panel title="Publish">
        <div className="merch-form">
          <label className="field">
            <span>Go live at (empty = now)</span>
            <input
              type="datetime-local"
              value={publishAt}
              onChange={(e) => setPublishAt(e.target.value)}
            />
          </label>
          <label className="field">
            <span>Note for the history</span>
            <input
              value={note}
              onChange={(e) => setNote(e.target.value)}
              placeholder="Ramadan home"
            />
          </label>
        </div>
        <div className="merch-actions">
          <button
            type="button"
            className="btn primary"
            disabled={publish.isPending || (!draft && !dirty)}
            onClick={() =>
              window.confirm(
                publishAt
                  ? "Schedule this layout?"
                  : "Publish this layout to all customers now?",
              ) && publish.mutate()
            }
          >
            {publishAt ? "Schedule" : "Publish now"}
          </button>
        </div>
      </Panel>

      <Panel title="History">
        {state.data.versions.length === 0 ? (
          <p className="muted">Nothing published yet.</p>
        ) : (
          <table className="money-table">
            <thead>
              <tr>
                <th>Version</th>
                <th>State</th>
                <th>Live from</th>
                <th className="num">Sections</th>
                <th>Note</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {state.data.versions.map((v) => (
                <tr key={v.id}>
                  <td>v{v.version_number}</td>
                  <td>{versionState(v, state.data.live_id)}</td>
                  <td className="faint">
                    {v.publish_at
                      ? new Date(v.publish_at).toLocaleString()
                      : "—"}
                  </td>
                  <td className="num">{v.sections.length}</td>
                  <td className="faint">{v.note ?? ""}</td>
                  <td>
                    <button
                      type="button"
                      className="btn"
                      onClick={() =>
                        (!dirty ||
                          window.confirm("Replace your unsaved changes?")) &&
                        restore.mutate(v.id)
                      }
                    >
                      Copy to draft
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
        {state.data.versions.some(
          (v) =>
            v.publish_at &&
            toLocalInput(v.publish_at) > toLocalInput(new Date().toISOString()),
        ) && (
          <p className="muted">
            A scheduled version goes live at its time without anyone pressing
            anything.
          </p>
        )}
      </Panel>
    </div>
  );
}
