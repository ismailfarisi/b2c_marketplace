"use client";

import { useQuery } from "@tanstack/react-query";
import { useState } from "react";

import { AREAS, CITY_LABEL } from "@/components/meals/areas";
import type { Place } from "@/components/meals/location";
import { useHasSession } from "@/components/meals/shell";
import { describeAddress, listAddresses } from "@/lib/customer-api";

type Locale = "en" | "ar";

const COPY = {
  en: {
    change: "Change",
    title: "Where should we deliver?",
    mine: "Use my current location",
    saved: "Your addresses",
    areas: "Or choose an area",
    search: "Search areas",
    none: "No area matches. Try another spelling.",
  },
  ar: {
    change: "تغيير",
    title: "أين نوصل؟",
    mine: "استخدم موقعي الحالي",
    saved: "عناوينك",
    areas: "أو اختر منطقة",
    search: "ابحث عن منطقة",
    none: "لا توجد منطقة مطابقة. جرّب كتابة أخرى.",
  },
};

export const placeLabel = (p: Place, locale: Locale) =>
  (locale === "ar" && p.label_ar) || p.label;

/** "Change" control that opens saved addresses, current location and areas. */
export function LocationPicker({
  locale,
  current,
  onChoose,
  onLocate,
}: {
  locale: Locale;
  current: Place | null;
  onChoose: (p: Place) => void;
  onLocate: () => void;
}) {
  const t = COPY[locale];
  const [open, setOpen] = useState(false);
  const [q, setQ] = useState("");
  const signedIn = useHasSession();
  const addresses = useQuery({
    queryKey: ["me", "addresses"],
    queryFn: listAddresses,
    enabled: open && signedIn,
    retry: false,
  });
  const saved = addresses.data?.items ?? [];
  const needle = q.trim().toLowerCase();
  const areas = AREAS.filter(
    (a) =>
      !needle || a.en.toLowerCase().includes(needle) || a.ar.includes(q.trim()),
  );
  const done = () => {
    setOpen(false);
    setQ("");
  };
  const on = (kind: Place["kind"], id: string) =>
    current?.kind === kind && current.id === id;

  return (
    <div className="ms-loc">
      <button
        type="button"
        className="ms-loc-toggle"
        aria-expanded={open}
        aria-controls="ms-loc-panel"
        onClick={() => setOpen(!open)}
      >
        {t.change}
      </button>
      {open && (
        <div id="ms-loc-panel" className="ms-loc-panel">
          <h3>{t.title}</h3>
          <button
            type="button"
            className="ms-loc-mine"
            onClick={() => {
              done();
              onLocate();
            }}
          >
            {t.mine}
          </button>
          {saved.length > 0 && (
            <>
              <h4>{t.saved}</h4>
              <ul className="ms-loc-list">
                {saved.map((a) => (
                  <li key={a.id}>
                    <button
                      type="button"
                      aria-pressed={on("address", a.id)}
                      onClick={() => {
                        done();
                        onChoose({
                          kind: "address",
                          id: a.id,
                          label: a.area || a.label,
                          point: a.point,
                        });
                      }}
                    >
                      <strong dir="auto">{a.label}</strong>
                      <span className="ms-sub" dir="auto">
                        {describeAddress(a)}
                      </span>
                    </button>
                  </li>
                ))}
              </ul>
            </>
          )}
          <h4>{t.areas}</h4>
          <input
            type="search"
            value={q}
            onChange={(e) => setQ(e.target.value)}
            placeholder={t.search}
            aria-label={t.search}
          />
          {areas.length === 0 ? (
            <p className="ms-sub">{t.none}</p>
          ) : (
            <ul className="ms-loc-list ms-loc-areas">
              {areas.map((a) => (
                <li key={a.id}>
                  <button
                    type="button"
                    aria-pressed={on("area", a.id)}
                    onClick={() => {
                      done();
                      onChoose({
                        kind: "area",
                        id: a.id,
                        label: a.en,
                        label_ar: a.ar,
                        point: { lat: a.lat, lng: a.lng },
                      });
                    }}
                  >
                    <strong>{locale === "ar" ? a.ar : a.en}</strong>
                    <span className="ms-sub">{CITY_LABEL[a.city][locale]}</span>
                  </button>
                </li>
              ))}
            </ul>
          )}
        </div>
      )}
    </div>
  );
}
