"use client";

import { ApiError } from "@marketplace/api-client";
import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";

import {
  createAddress,
  type Address,
  type AddressInput,
} from "@/lib/customer-api";

export interface AddressCopy {
  addAddress: string;
  label: string;
  building: string;
  flat: string;
  street: string;
  area: string;
  city: string;
  pin: string;
  useMine: string;
  pinNote: (lat: number, lng: number) => string;
  save: string;
  failed: string;
}

export const ADDRESS_COPY: Record<"en" | "ar", AddressCopy> = {
  en: {
    addAddress: "Add a new address",
    label: "Label (Home, Office…)",
    building: "Building / villa",
    flat: "Flat",
    street: "Street",
    area: "Area",
    city: "City",
    pin: "Location",
    useMine: "Use my current location",
    pinNote: (lat, lng) => `Pinned at ${lat.toFixed(4)}, ${lng.toFixed(4)}`,
    save: "Save address",
    failed: "Something went wrong. Try again.",
  },
  ar: {
    addAddress: "إضافة عنوان جديد",
    label: "الاسم (المنزل، المكتب…)",
    building: "المبنى / الفيلا",
    flat: "الشقة",
    street: "الشارع",
    area: "المنطقة",
    city: "المدينة",
    pin: "الموقع",
    useMine: "استخدم موقعي الحالي",
    pinNote: (lat, lng) => `الموقع ${lat.toFixed(4)}، ${lng.toFixed(4)}`,
    save: "حفظ العنوان",
    failed: "حدث خطأ. حاول مرة أخرى.",
  },
};

function pinText(p: { lat: number; lng: number }) {
  return { lat: String(p.lat), lng: String(p.lng) };
}

function parsePin(
  c: { lat: string; lng: string } | null,
): { lat: number; lng: number } | null {
  if (!c || !c.lat.trim() || !c.lng.trim()) return null;
  const lat = Number(c.lat);
  const lng = Number(c.lng);
  if (!Number.isFinite(lat) || !Number.isFinite(lng)) return null;
  if (Math.abs(lat) > 90 || Math.abs(lng) > 180) return null;
  return { lat, lng };
}

export function NewAddress({
  t,
  initial,
  onSaved,
}: {
  t: AddressCopy;
  initial: { lat: number; lng: number } | null;
  onSaved: (a: Address) => void;
}) {
  const qc = useQueryClient();
  const [open, setOpen] = useState(false);
  const [f, setF] = useState({
    label: "",
    building: "",
    flat: "",
    street: "",
    area: "",
    city: "Dubai",
  });
  const [coords, setCoords] = useState<{ lat: string; lng: string } | null>(
    null,
  );
  const pin = parsePin(coords);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!coords && initial) setCoords(pinText(initial));
  }, [initial, coords]);

  if (!open) {
    return (
      <button type="button" className="ms-link" onClick={() => setOpen(true)}>
        + {t.addAddress}
      </button>
    );
  }

  const set = (k: keyof typeof f) => (e: React.ChangeEvent<HTMLInputElement>) =>
    setF((cur) => ({ ...cur, [k]: e.target.value }));

  async function save() {
    if (!pin) return;
    setBusy(true);
    setError(null);
    const input: AddressInput = {
      label: f.label.trim(),
      city: f.city.trim(),
      country: "AE",
      point: pin,
    };
    for (const k of ["building", "flat", "street", "area"] as const) {
      const v = f[k].trim();
      if (v) input[k] = v;
    }
    try {
      const a = await createAddress(input);
      await qc.invalidateQueries({ queryKey: ["me", "addresses"] });
      onSaved(a);
      setOpen(false);
    } catch (err) {
      setError(
        err instanceof ApiError && err.problem?.errors?.[0]?.message
          ? err.problem.errors[0].message
          : t.failed,
      );
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="ms-newaddr">
      <h3>{t.addAddress}</h3>
      <div className="ms-row">
        <label className="ms-field">
          <span>{t.label}</span>
          <input
            value={f.label}
            onChange={set("label")}
            maxLength={40}
            dir="auto"
          />
        </label>
        <label className="ms-field">
          <span>{t.city}</span>
          <input value={f.city} onChange={set("city")} dir="auto" />
        </label>
      </div>
      <div className="ms-row">
        <label className="ms-field">
          <span>{t.building}</span>
          <input value={f.building} onChange={set("building")} dir="auto" />
        </label>
        <label className="ms-field">
          <span>{t.flat}</span>
          <input value={f.flat} onChange={set("flat")} dir="auto" />
        </label>
      </div>
      <div className="ms-row">
        <label className="ms-field">
          <span>{t.street}</span>
          <input value={f.street} onChange={set("street")} dir="auto" />
        </label>
        <label className="ms-field">
          <span>{t.area}</span>
          <input value={f.area} onChange={set("area")} dir="auto" />
        </label>
      </div>
      <div className="ms-field">
        <span>{t.pin}</span>
        <div className="ms-row">
          <input
            aria-label="Latitude"
            inputMode="decimal"
            dir="ltr"
            value={coords?.lat ?? ""}
            onChange={(e) =>
              setCoords({ lat: e.target.value, lng: coords?.lng ?? "" })
            }
          />
          <input
            aria-label="Longitude"
            inputMode="decimal"
            dir="ltr"
            value={coords?.lng ?? ""}
            onChange={(e) =>
              setCoords({ lat: coords?.lat ?? "", lng: e.target.value })
            }
          />
        </div>
        <button
          type="button"
          className="ms-link"
          onClick={() =>
            navigator.geolocation?.getCurrentPosition(
              (pos) =>
                setCoords(
                  pinText({
                    lat: pos.coords.latitude,
                    lng: pos.coords.longitude,
                  }),
                ),
              () => undefined,
              { enableHighAccuracy: true, timeout: 8000 },
            )
          }
        >
          {t.useMine}
        </button>
        {pin && <small className="ms-sub">{t.pinNote(pin.lat, pin.lng)}</small>}
      </div>
      {error && (
        <p className="ms-bad" role="alert">
          {error}
        </p>
      )}
      <button
        type="button"
        className="ms-cta ms-cta-quiet"
        disabled={busy || !f.label.trim() || !f.city.trim() || !pin}
        onClick={save}
      >
        {t.save}
      </button>
    </div>
  );
}
