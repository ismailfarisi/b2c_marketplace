"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

import { Panel, plural } from "@/components/admin/detail";
import {
  fromLocalInput,
  toLocalInput,
  windowState,
} from "@/components/admin/merch";
import {
  deleteMerchBanner,
  getMerchBanners,
  saveMerchBanner,
  type MerchBanner,
  type MerchBannerInput,
} from "@/lib/admin-api";

const EMPTY: MerchBannerInput = {
  title: "",
  title_ar: null,
  body: null,
  body_ar: null,
  cta: null,
  cta_ar: null,
  image_url: null,
  link_url: null,
  starts_at: null,
  ends_at: null,
  is_active: true,
};

const opt = (v: string) => (v.trim() ? v : null);

/** Banners for the home carousel. A banner shows only when a layout section lists it and it is in its window. */
export default function MerchBannersPage() {
  const qc = useQueryClient();
  const { data, error } = useQuery({
    queryKey: ["admin", "merch", "banners"],
    queryFn: getMerchBanners,
  });
  const [editing, setEditing] = useState<string | null>(null);
  const [form, setForm] = useState<MerchBannerInput>(EMPTY);
  const [problem, setProblem] = useState<string | null>(null);
  const rows = data?.items ?? [];

  const save = useMutation({
    mutationFn: () => saveMerchBanner(editing === "new" ? null : editing, form),
    onSuccess: async (b) => {
      setProblem(null);
      setEditing(b.id);
      await qc.invalidateQueries({ queryKey: ["admin", "merch", "banners"] });
    },
    onError: (e: Error) => setProblem(e.message),
  });
  const remove = useMutation({
    mutationFn: (id: string) => deleteMerchBanner(id),
    onSuccess: async () => {
      setEditing(null);
      await qc.invalidateQueries({ queryKey: ["admin", "merch", "banners"] });
    },
  });

  const open = (b: MerchBanner | null) => {
    setProblem(null);
    if (!b) {
      setEditing("new");
      setForm(EMPTY);
      return;
    }
    const { id: _id, updated_at: _u, ...rest } = b;
    setEditing(b.id);
    setForm(rest);
  };
  const text = (k: keyof MerchBannerInput, label: string, dir?: "rtl") => (
    <label className="field">
      <span>{label}</span>
      <input
        dir={dir}
        value={(form[k] as string | null) ?? ""}
        onChange={(e) =>
          setForm({
            ...form,
            [k]: k === "title" ? e.target.value : opt(e.target.value),
          })
        }
      />
    </label>
  );

  if (error)
    return (
      <p className="danger">
        Banners could not be loaded. Needs commerce.read.
      </p>
    );

  return (
    <div className="detail">
      <div className="detail-head">
        <div>
          <h2>Storefront banners</h2>
          <p className="console-lede">
            Add banners to a carousel section in the home layout to show them.
          </p>
        </div>
        <div className="spacer" />
        <button
          type="button"
          className="btn primary"
          onClick={() => open(null)}
        >
          New banner
        </button>
      </div>

      <Panel title="Banners" note={plural(rows.length, "banner")}>
        {rows.length === 0 ? (
          <p className="muted">No banners yet.</p>
        ) : (
          <table className="money-table">
            <thead>
              <tr>
                <th>Title</th>
                <th>Link</th>
                <th>State</th>
                <th>Window</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((b) => (
                <tr key={b.id}>
                  <td>
                    <button
                      type="button"
                      className="btn link"
                      onClick={() => open(b)}
                    >
                      {b.title}
                    </button>
                  </td>
                  <td className="faint mono">{b.link_url ?? "—"}</td>
                  <td>{windowState(b)}</td>
                  <td className="faint">
                    {b.starts_at
                      ? new Date(b.starts_at).toLocaleString()
                      : "now"}{" "}
                    →{" "}
                    {b.ends_at
                      ? new Date(b.ends_at).toLocaleString()
                      : "no end"}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Panel>

      {editing && (
        <Panel title={editing === "new" ? "New banner" : "Edit banner"}>
          <form
            className="merch-form"
            onSubmit={(e) => {
              e.preventDefault();
              save.mutate();
            }}
          >
            {text("title", "Title (English)")}
            {text("title_ar", "Title (Arabic)", "rtl")}
            {text("body", "Text (English)")}
            {text("body_ar", "Text (Arabic)", "rtl")}
            {text("cta", "Button (English)")}
            {text("cta_ar", "Button (Arabic)", "rtl")}
            {text("link_url", "Link: /path on the storefront or https URL")}
            {text("image_url", "Image URL (https)")}
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
                {save.isPending ? "Saving…" : "Save banner"}
              </button>
              {editing !== "new" && (
                <button
                  type="button"
                  className="btn danger"
                  onClick={() =>
                    window.confirm("Delete this banner?") &&
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
    </div>
  );
}
