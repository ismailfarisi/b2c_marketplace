"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";

import { Facts, Panel } from "@/components/admin/detail";
import { label } from "@/components/kitchen/copy";
import { ConsoleError, errorText, fieldErrors, useBranch } from "@/components/kitchen/portal";
import { getProfile, updateProfile, type Profile, type ProfileInput } from "@/lib/vendor-api";

const list = (s: string) =>
  s
    .split(",")
    .map((x) => x.trim())
    .filter(Boolean);
const orNull = (s: string) => (s.trim() === "" ? null : s.trim());

/**
 * The kitchen's storefront card: what customers read before subscribing.
 * Address, service area and delivery windows are set during onboarding by the
 * marketplace, because they decide who the kitchen can deliver to.
 */
export default function ProfilePage() {
  const branch = useBranch();
  const { t, locale } = branch;
  const c = t.profile;
  const qc = useQueryClient();
  const key = ["vendor", "profile", branch.branch_id];
  const { data, error, refetch } = useQuery({ queryKey: key, queryFn: () => getProfile(branch.branch_id) });

  // The form follows the server only while the user has not typed into it. A
  // refetch on window focus or an invalidation elsewhere must not overwrite
  // unsaved edits; a successful save clears the flag so the saved row shows.
  const [form, setForm] = useState<Record<string, string>>({});
  const dirty = useRef(false);
  useEffect(() => {
    if (data && !dirty.current) setForm(toForm(data));
  }, [data]);

  const save = useMutation({
    mutationFn: (body: ProfileInput) => updateProfile(branch.branch_id, body),
    onSuccess: (p) => {
      dirty.current = false;
      setForm(toForm(p));
      qc.setQueryData(key, p);
      qc.invalidateQueries({ queryKey: ["vendor", "overview", branch.branch_id] });
      qc.invalidateQueries({ queryKey: ["vendor", "branches"] });
    },
  });
  const errs = fieldErrors(save.error, locale);

  if (error) return <ConsoleError error={error} onRetry={() => void refetch()} locale={locale} />;
  if (!data) return <div className="skeleton" style={{ inlineSize: "40%" }} />;

  const set = (k: string) => (e: { target: { value: string } }) => {
    dirty.current = true;
    // "Saved." describes the last save; a new edit is not saved yet.
    if (!save.isPending) save.reset();
    setForm({ ...form, [k]: e.target.value });
  };
  const v = (k: string) => form[k] ?? "";
  const ro = !branch.editable || save.isPending;

  return (
    <>
      <Panel title={c.storefront}>
        <form
          className="vp-form"
          onSubmit={(e) => {
            e.preventDefault();
            save.mutate({
              name: v("name"),
              name_ar: orNull(v("name_ar")),
              description: orNull(v("description")),
              description_ar: orNull(v("description_ar")),
              cuisines: list(v("cuisines")),
              dietary_tags: list(v("dietary_tags")),
              cover_image_url: orNull(v("cover_image_url")),
              phone: orNull(v("phone")),
            });
          }}
        >
          <label>
            {c.name}
            <input className="field" dir="auto" value={v("name")} onChange={set("name")} disabled={ro} required />
            {errs.name ? <span className="err">{errs.name}</span> : null}
          </label>
          <label>
            {c.nameAr}
            <input className="field" dir="rtl" value={v("name_ar")} onChange={set("name_ar")} disabled={ro} />
          </label>
          <label className="wide">
            {c.description}
            <textarea className="field" dir="auto" rows={3} value={v("description")} onChange={set("description")} disabled={ro} />
            {errs.description ? <span className="err">{errs.description}</span> : null}
          </label>
          <label className="wide">
            {c.descriptionAr}
            <textarea
              className="field"
              dir="rtl"
              rows={3}
              value={v("description_ar")}
              onChange={set("description_ar")}
              disabled={ro}
            />
          </label>
          <label>
            {c.cuisines} <span className="hint">{c.cuisinesHint}</span>
            <input className="field" dir="auto" value={v("cuisines")} onChange={set("cuisines")} disabled={ro} />
            {errs.cuisines ? <span className="err">{errs.cuisines}</span> : null}
          </label>
          <label>
            {c.dietary} <span className="hint">{c.dietaryHint}</span>
            <input className="field" dir="auto" value={v("dietary_tags")} onChange={set("dietary_tags")} disabled={ro} />
            {errs.dietary_tags ? <span className="err">{errs.dietary_tags}</span> : null}
          </label>
          <label>
            {c.cover}
            <input className="field" dir="ltr" value={v("cover_image_url")} onChange={set("cover_image_url")} disabled={ro} placeholder="https://" />
            {errs.cover_image_url ? <span className="err">{errs.cover_image_url}</span> : null}
          </label>
          <label>
            {c.phone}
            <input className="field" dir="ltr" value={v("phone")} onChange={set("phone")} disabled={ro} />
          </label>
          {branch.editable ? (
            <div className="vp-actions wide">
              <button className="btn primary" type="submit" disabled={save.isPending}>
                {save.isPending ? t.saving : c.save}
              </button>
              {save.isSuccess ? <span className="muted">{t.saved}</span> : null}
              {save.error && Object.keys(errs).length === 0 ? (
                <span className="danger">{errorText(save.error, locale)}</span>
              ) : null}
            </div>
          ) : null}
        </form>
      </Panel>

      <Panel title={c.marketplace} note={c.marketplaceNote}>
        <Facts
          rows={[
            [c.vendor, <span dir="auto">{data.vendor_name}</span>],
            [
              c.vendorStatus,
              <span className={`tag ${data.vendor_status === "active" ? "ok" : "warn"}`}>
                {label(t.statuses, data.vendor_status)}
              </span>,
            ],
            [c.address, <span dir="auto">{[data.address_line, data.city, data.emirate].filter(Boolean).join(", ") || "—"}</span>],
            [c.timezone, <span dir="ltr">{data.timezone}</span>],
            [c.currency, data.currency],
            [c.area, data.has_service_area ? c.areaSet : <span className="danger">{c.areaUnset}</span>],
            [
              c.windows,
              data.delivery_windows.length === 0 ? (
                c.noWindows
              ) : (
                <span dir="auto">
                  {data.delivery_windows.map((w) => `${w.label} (${w.starts_at}–${w.ends_at})`).join(", ")}
                </span>
              ),
            ],
          ]}
        />
      </Panel>
    </>
  );
}

function toForm(p: Profile): Record<string, string> {
  return {
    name: p.name,
    name_ar: p.name_ar ?? "",
    description: p.description ?? "",
    description_ar: p.description_ar ?? "",
    cuisines: p.cuisines.join(", "),
    dietary_tags: p.dietary_tags.join(", "),
    cover_image_url: p.cover_image_url ?? "",
    phone: p.phone ?? "",
  };
}
