"use client";

import { useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { usePathname } from "next/navigation";
import {
  useEffect,
  useId,
  useRef,
  useState,
  type ReactNode,
} from "react";

import { ApiError } from "@marketplace/api-client";

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
    blocked:
      "Location is blocked in your browser settings. Allow it for this site, or choose an area below.",
    savedLoading: "Loading your addresses…",
    savedFailed: "Couldn't load your addresses.",
    savedRetry: "Try again",
    savedSignin: "Sign in again to see your addresses",
  },
  ar: {
    change: "تغيير",
    title: "أين نوصل؟",
    mine: "استخدم موقعي الحالي",
    saved: "عناوينك",
    areas: "أو اختر منطقة",
    search: "ابحث عن منطقة",
    none: "لا توجد منطقة مطابقة. جرّب كتابة أخرى.",
    blocked:
      "الموقع محظور في إعدادات المتصفح. اسمح به لهذا الموقع أو اختر منطقة أدناه.",
    savedLoading: "جارٍ تحميل عناوينك…",
    savedFailed: "تعذر تحميل عناوينك.",
    savedRetry: "حاول مرة أخرى",
    savedSignin: "سجّل الدخول مجددًا لرؤية عناوينك",
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
  label,
  className,
  blocked = false,
}: {
  locale: Locale;
  current: Place | null;
  onChoose: (p: Place) => void;
  onLocate: () => void;
  /** Trigger text; defaults to "Change". */
  label?: ReactNode;
  className?: string;
  /** The browser blocks location, so "Use my current location" can't work. */
  blocked?: boolean;
}) {
  const t = COPY[locale];
  const [open, setOpen] = useState(false);
  const panelId = useId();
  const rootRef = useRef<HTMLDivElement>(null);
  const toggleRef = useRef<HTMLButtonElement>(null);
  const pathname = usePathname();
  const [q, setQ] = useState("");
  const signedIn = useHasSession();
  const addresses = useQuery({
    queryKey: ["me", "addresses"],
    queryFn: listAddresses,
    enabled: open && signedIn,
    retry: false,
  });
  const saved = addresses.data?.items ?? [];
  const savedExpired =
    addresses.error instanceof ApiError && addresses.error.status === 401;
  const needle = q.trim().toLowerCase();
  const areas = AREAS.filter(
    (a) =>
      !needle || a.en.toLowerCase().includes(needle) || a.ar.includes(q.trim()),
  );
  const done = () => {
    setOpen(false);
    setQ("");
  };
  // Escape or a press outside closes the panel and hands focus back.
  useEffect(() => {
    if (!open) return;
    const close = () => {
      setOpen(false);
      setQ("");
      toggleRef.current?.focus();
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") close();
    };
    const onDown = (e: PointerEvent) => {
      if (!rootRef.current?.contains(e.target as Node)) close();
    };
    document.addEventListener("keydown", onKey);
    document.addEventListener("pointerdown", onDown);
    return () => {
      document.removeEventListener("keydown", onKey);
      document.removeEventListener("pointerdown", onDown);
    };
  }, [open]);
  const on = (kind: Place["kind"], id: string) =>
    current?.kind === kind && current.id === id;

  return (
    <div
      ref={rootRef}
      className={className ? `ms-loc ${className}` : "ms-loc"}
    >
      <button
        ref={toggleRef}
        type="button"
        className="ms-loc-toggle"
        aria-expanded={open}
        aria-controls={panelId}
        onClick={() => setOpen(!open)}
      >
        {label ?? t.change}
      </button>
      {open && (
        <div id={panelId} className="ms-loc-panel">
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
          {blocked && <p className="ms-sub">{t.blocked}</p>}
          {addresses.isLoading && (
            <>
              <h4>{t.saved}</h4>
              <p className="ms-sub" role="status">
                {t.savedLoading}
              </p>
            </>
          )}
          {addresses.isError && (
            <>
              <h4>{t.saved}</h4>
              {savedExpired ? (
                <p className="ms-sub">
                  <Link
                    href={`/${locale}/signin?next=${encodeURIComponent(pathname ?? `/${locale}`)}`}
                  >
                    {t.savedSignin}
                  </Link>
                </p>
              ) : (
                <p className="ms-sub ms-bad" role="alert">
                  {t.savedFailed}{" "}
                  <button
                    type="button"
                    className="ms-link"
                    onClick={() => void addresses.refetch()}
                  >
                    {t.savedRetry}
                  </button>
                </p>
              )}
            </>
          )}
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
