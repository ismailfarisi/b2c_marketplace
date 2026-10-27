"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";

import { Facts, Panel } from "@/components/admin/detail";
import { errorText, fieldErrors, useBranch } from "@/components/vendor/portal";
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
  const qc = useQueryClient();
  const key = ["vendor", "profile", branch.branch_id];
  const { data, error } = useQuery({ queryKey: key, queryFn: () => getProfile(branch.branch_id) });

  const [form, setForm] = useState<Record<string, string>>({});
  useEffect(() => {
    if (data) setForm(toForm(data));
  }, [data]);

  const save = useMutation({
    mutationFn: (body: ProfileInput) => updateProfile(branch.branch_id, body),
    onSuccess: (p) => {
      qc.setQueryData(key, p);
      qc.invalidateQueries({ queryKey: ["vendor", "overview", branch.branch_id] });
      qc.invalidateQueries({ queryKey: ["vendor", "branches"] });
    },
  });
  const errs = fieldErrors(save.error);

  if (error) return <div className="state error">{errorText(error)}</div>;
  if (!data) return <div className="skeleton" style={{ inlineSize: "40%" }} />;

  const set = (k: string) => (e: { target: { value: string } }) => setForm({ ...form, [k]: e.target.value });
  const v = (k: string) => form[k] ?? "";
  const ro = !branch.editable || save.isPending;

  return (
    <>
      <Panel title="Storefront">
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
            Kitchen name
            <input className="field" value={v("name")} onChange={set("name")} disabled={ro} required />
            {errs.name ? <span className="err">{errs.name}</span> : null}
          </label>
          <label>
            Name in Arabic
            <input className="field" dir="rtl" value={v("name_ar")} onChange={set("name_ar")} disabled={ro} />
          </label>
          <label className="wide">
            Description
            <textarea className="field" rows={3} value={v("description")} onChange={set("description")} disabled={ro} />
            {errs.description ? <span className="err">{errs.description}</span> : null}
          </label>
          <label className="wide">
            Description in Arabic
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
            Cuisines <span className="hint">comma-separated, e.g. indian, arabic</span>
            <input className="field" value={v("cuisines")} onChange={set("cuisines")} disabled={ro} />
            {errs.cuisines ? <span className="err">{errs.cuisines}</span> : null}
          </label>
          <label>
            Dietary tags <span className="hint">e.g. halal, keto, vegetarian</span>
            <input className="field" value={v("dietary_tags")} onChange={set("dietary_tags")} disabled={ro} />
            {errs.dietary_tags ? <span className="err">{errs.dietary_tags}</span> : null}
          </label>
          <label>
            Cover image URL
            <input className="field" value={v("cover_image_url")} onChange={set("cover_image_url")} disabled={ro} placeholder="https://" />
            {errs.cover_image_url ? <span className="err">{errs.cover_image_url}</span> : null}
          </label>
          <label>
            Customer phone
            <input className="field" value={v("phone")} onChange={set("phone")} disabled={ro} />
          </label>
          {branch.editable ? (
            <div className="vp-actions wide">
              <button className="btn primary" type="submit" disabled={save.isPending}>
                {save.isPending ? "Saving…" : "Save profile"}
              </button>
              {save.isSuccess ? <span className="muted">Saved.</span> : null}
              {save.error && Object.keys(errs).length === 0 ? (
                <span className="danger">{errorText(save.error)}</span>
              ) : null}
            </div>
          ) : null}
        </form>
      </Panel>

      <Panel title="Set by the marketplace" note="Contact support to change these">
        <Facts
          rows={[
            ["Vendor", <span dir="auto">{data.vendor_name}</span>],
            ["Vendor status", <span className={`tag ${data.vendor_status === "active" ? "ok" : "warn"}`}>{data.vendor_status}</span>],
            ["Address", [data.address_line, data.city, data.emirate].filter(Boolean).join(", ") || "—"],
            ["Timezone", data.timezone],
            ["Currency", data.currency],
            ["Delivery area", data.has_service_area ? "Set" : <span className="danger">Not set — customers cannot find you yet</span>],
            [
              "Delivery windows",
              data.delivery_windows.length === 0
                ? "None"
                : data.delivery_windows.map((w) => `${w.label} (${w.starts_at}–${w.ends_at})`).join(", "),
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
