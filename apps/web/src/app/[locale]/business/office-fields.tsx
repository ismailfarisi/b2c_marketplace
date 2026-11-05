"use client";

import { useState } from "react";

import { LocationPicker, placeLabel } from "@/components/meals/location-picker";
import type { Place } from "@/components/meals/location";
import type { Locale } from "@/components/meals/shell";

import type { Copy } from "./copy";

export interface OfficeDraft {
  label: string;
  building: string;
  area: string;
  city: string;
  lat: string;
  lng: string;
}

export const emptyOffice: OfficeDraft = { label: "", building: "", area: "", city: "Dubai", lat: "", lng: "" };

export const officeInput = (o: OfficeDraft) => ({
  label: o.label,
  building: o.building,
  area: o.area,
  city: o.city,
  lat: o.lat === "" ? 0 : Number(o.lat),
  lng: o.lng === "" ? 0 : Number(o.lng),
});

/**
 * What the server would reject, checked before sending so the form can use
 * noValidate and say it in the reader's language. Keys carry the server's
 * field prefix, so the same map feeds OfficeFields' `errors`.
 */
export function officeErrors(o: OfficeDraft, prefix: string, t: Copy): Record<string, string> {
  const out: Record<string, string> = {};
  if (!o.label.trim()) out[`${prefix}label`] = t.officeRequired;
  if (!o.city.trim()) out[`${prefix}city`] = t.cityRequired;
  const lat = Number(o.lat);
  const lng = Number(o.lng);
  if (
    o.lat.trim() === "" ||
    o.lng.trim() === "" ||
    !Number.isFinite(lat) ||
    !Number.isFinite(lng) ||
    Math.abs(lat) > 90 ||
    Math.abs(lng) > 180 ||
    (lat === 0 && lng === 0)
  )
    out[`${prefix}location`] = t.locationRequired;
  return out;
}

/** Office name, address and location; location from an area, the device, or typed. */
export function OfficeFields({
  value,
  onChange,
  errors,
  prefix,
  locale,
  t,
}: {
  value: OfficeDraft;
  onChange: (o: OfficeDraft) => void;
  errors: Record<string, string>;
  /** Server field prefix, "office." on registration. */
  prefix: string;
  locale: Locale;
  t: Copy;
}) {
  const [place, setPlace] = useState<Place | null>(null);
  const [locateFailed, setLocateFailed] = useState(false);
  const err = (k: string) => errors[`${prefix}${k}`];
  const input = (k: keyof OfficeDraft, label: string, required = false) => (
    <label className="ms-field">
      <span>{label}</span>
      <input
        dir="auto"
        value={value[k]}
        required={required}
        aria-invalid={err(k) ? true : undefined}
        onChange={(e) => onChange({ ...value, [k]: e.target.value })}
      />
      {err(k) && <small className="ms-bad">{err(k)}</small>}
    </label>
  );
  const coord = (k: "lat" | "lng", label: string, max: number) => (
    <label className="ms-field">
      <span>{label}</span>
      <input
        type="number"
        dir="ltr"
        step="any"
        min={-max}
        max={max}
        value={value[k]}
        aria-invalid={err("location") ? true : undefined}
        onChange={(e) => onChange({ ...value, [k]: e.target.value })}
      />
    </label>
  );
  function locate() {
    setLocateFailed(false);
    if (!("geolocation" in navigator)) {
      setLocateFailed(true);
      return;
    }
    navigator.geolocation.getCurrentPosition(
      (pos) => {
        setPlace(null);
        onChange({ ...value, lat: pos.coords.latitude.toFixed(6), lng: pos.coords.longitude.toFixed(6) });
      },
      () => setLocateFailed(true),
      { timeout: 10000 },
    );
  }
  const located = value.lat !== "" && value.lng !== "";
  return (
    <>
      <div className="ms-row">{input("label", t.office, true)}{input("building", t.building)}</div>
      <div className="ms-row">{input("area", t.area)}{input("city", t.city, true)}</div>
      <fieldset className="ms-field">
        <legend>{t.location}</legend>
        <LocationPicker
          locale={locale}
          current={place}
          label={located ? `${t.located}${place ? ` · ${placeLabel(place, locale)}` : ""}` : t.pickLocation}
          onLocate={locate}
          onChoose={(p) => {
            setPlace(p);
            setLocateFailed(false);
            onChange({
              ...value,
              area: value.area || placeLabel(p, locale),
              lat: String(p.point.lat),
              lng: String(p.point.lng),
            });
          }}
        />
        <div className="ms-row">{coord("lat", t.lat, 90)}{coord("lng", t.lng, 180)}</div>
        {locateFailed && <small className="ms-bad">{t.locateFailed}</small>}
        {err("location") && <small className="ms-bad" role="alert">{err("location")}</small>}
      </fieldset>
    </>
  );
}
