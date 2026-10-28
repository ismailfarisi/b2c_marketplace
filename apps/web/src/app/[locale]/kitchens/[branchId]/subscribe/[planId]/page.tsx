"use client";

import { ApiError, formatMoney } from "@marketplace/api-client";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useRouter } from "next/navigation";
import { use, useEffect, useMemo, useState, type FormEvent } from "react";

import { useCustomerPoint } from "@/components/meals/location";
import { CustomerShell, asLocale, type Locale } from "@/components/meals/shell";
import {
  createAddress,
  describeAddress,
  listAddresses,
  payOrder,
  subscribe,
  type Address,
  type AddressInput,
} from "@/lib/customer-api";
import { getMealKitchen } from "@/lib/meals-api";

const COPY = {
  en: {
    back: "Back to kitchen",
    title: "Subscribe",
    plan: "Plan",
    size: "Size",
    days: (n: number) => `Delivery days (choose ${n})`,
    slots: (n: number) => `Meals (choose ${n})`,
    window: "Delivery time",
    start: "First delivery on or after",
    address: "Deliver to",
    noAddress: "Add an address to continue.",
    addAddress: "Add a new address",
    label: "Label (Home, Office…)",
    building: "Building / villa",
    flat: "Flat",
    street: "Street",
    area: "Area",
    city: "City",
    pin: "Location",
    useMine: "Use my current location",
    pinNote: (lat: number, lng: number) =>
      `Pinned at ${lat.toFixed(4)}, ${lng.toFixed(4)}`,
    save: "Save address",
    total: "Total for the first cycle",
    cycle: (d: number) =>
      `${d}-day cycle, renews automatically. Pause, skip or cancel any time before the cutoff.`,
    pay: "Subscribe and pay",
    paying: "Processing…",
    loading: "Loading…",
    notFound: "This plan is not available.",
    failed: "Something went wrong. Try again.",
    outside:
      "This kitchen does not deliver to that address. Choose another one.",
    weekdays: ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"],
    slot: {
      breakfast: "Breakfast",
      lunch: "Lunch",
      dinner: "Dinner",
      snack: "Snack",
      suhoor: "Suhoor",
      iftar: "Iftar",
    } as Record<string, string>,
  },
  ar: {
    back: "العودة إلى المطبخ",
    title: "الاشتراك",
    plan: "الباقة",
    size: "الحجم",
    days: (n: number) => `أيام التوصيل (اختر ${n})`,
    slots: (n: number) => `الوجبات (اختر ${n})`,
    window: "وقت التوصيل",
    start: "أول توصيل في هذا اليوم أو بعده",
    address: "التوصيل إلى",
    noAddress: "أضف عنوانًا للمتابعة.",
    addAddress: "إضافة عنوان جديد",
    label: "الاسم (المنزل، المكتب…)",
    building: "المبنى / الفيلا",
    flat: "الشقة",
    street: "الشارع",
    area: "المنطقة",
    city: "المدينة",
    pin: "الموقع",
    useMine: "استخدم موقعي الحالي",
    pinNote: (lat: number, lng: number) =>
      `الموقع ${lat.toFixed(4)}، ${lng.toFixed(4)}`,
    save: "حفظ العنوان",
    total: "الإجمالي للدورة الأولى",
    cycle: (d: number) =>
      `دورة ${d} يومًا تتجدد تلقائيًا. يمكنك الإيقاف أو التخطي أو الإلغاء قبل الموعد النهائي.`,
    pay: "اشترك وادفع",
    paying: "جارٍ المعالجة…",
    loading: "جارٍ التحميل…",
    notFound: "هذه الباقة غير متاحة.",
    failed: "حدث خطأ. حاول مرة أخرى.",
    outside: "هذا المطبخ لا يوصل إلى هذا العنوان. اختر عنوانًا آخر.",
    weekdays: [
      "الأحد",
      "الاثنين",
      "الثلاثاء",
      "الأربعاء",
      "الخميس",
      "الجمعة",
      "السبت",
    ],
    slot: {
      breakfast: "فطور",
      lunch: "غداء",
      dinner: "عشاء",
      snack: "وجبة خفيفة",
      suhoor: "سحور",
      iftar: "إفطار",
    } as Record<string, string>,
  },
};
type Copy = (typeof COPY)["en"];

const isoDate = (d: Date) =>
  `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;

/** The first day whose cutoff has not already passed. */
function earliestStart(cutoffHours: number) {
  const d = new Date();
  d.setDate(d.getDate() + 1 + Math.floor(cutoffHours / 24));
  return isoDate(d);
}

function toggle<T>(list: T[], v: T, max: number): T[] {
  if (list.includes(v)) return list.filter((x) => x !== v);
  if (max === 1) return [v];
  return list.length >= max ? list : [...list, v];
}

export default function CheckoutPage({
  params,
}: {
  params: Promise<{ locale: string; branchId: string; planId: string }>;
}) {
  const p = use(params);
  const locale = asLocale(p.locale);
  const t = COPY[locale];
  return (
    <CustomerShell
      locale={locale}
      back={{ href: `/${locale}/kitchens/${p.branchId}`, label: t.back }}
    >
      <Checkout locale={locale} t={t} branchId={p.branchId} planId={p.planId} />
    </CustomerShell>
  );
}

function Checkout({
  locale,
  t,
  branchId,
  planId,
}: {
  locale: Locale;
  t: Copy;
  branchId: string;
  planId: string;
}) {
  const router = useRouter();
  const { point, precise } = useCustomerPoint();
  const kitchen = useQuery({
    queryKey: ["meal-kitchen", branchId, "checkout"],
    queryFn: () => getMealKitchen(branchId),
  });
  const addresses = useQuery({
    queryKey: ["me", "addresses"],
    queryFn: listAddresses,
  });

  const plan = kitchen.data?.plan_list.find((pl) => pl.id === planId);
  const tiers = plan?.tiers.filter((x) => x.is_active) ?? [];

  const [tierId, setTierId] = useState<string | null>(null);
  const [days, setDays] = useState<number[]>([]);
  const [slots, setSlots] = useState<string[]>([]);
  const [windowId, setWindowId] = useState<string>("");
  const [start, setStart] = useState("");
  const [addressId, setAddressId] = useState<string>("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Defaults once the plan arrives: the plan's own days and meals in order,
  // the first window, and the earliest start the cutoff allows.
  useEffect(() => {
    if (!plan || !kitchen.data) return;
    setDays(plan.delivery_weekdays.slice(0, plan.days_per_week));
    setSlots(plan.meal_slots.slice(0, plan.meals_per_day));
    setWindowId(kitchen.data.delivery_windows[0]?.id ?? "");
    setStart(earliestStart(plan.cutoff_hours));
    if (tiers.length > 1) setTierId(tiers[0]?.id ?? null);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [plan?.id, kitchen.data]);

  useEffect(() => {
    const list = addresses.data?.items ?? [];
    if (!addressId && list.length > 0) {
      setAddressId((list.find((a) => a.is_default) ?? list[0])!.id);
    }
  }, [addresses.data, addressId]);

  const price = useMemo(() => {
    if (!plan) return 0;
    return tiers.find((x) => x.id === tierId)?.price_minor ?? plan.price_minor;
  }, [plan, tiers, tierId]);

  if (kitchen.isPending || addresses.isPending)
    return <p className="ms-note">{t.loading}</p>;
  if (kitchen.isError || !plan) {
    return (
      <p className="ms-empty">
        {kitchen.error instanceof ApiError && kitchen.error.status !== 404
          ? t.failed
          : t.notFound}
      </p>
    );
  }
  if (addresses.isError) return <p className="ms-empty">{t.failed}</p>;
  const k = kitchen.data;
  const money = (n: number) =>
    formatMoney({ amount: n, currency: plan.currency }, locale);
  const ready =
    days.length === plan.days_per_week &&
    slots.length === plan.meals_per_day &&
    windowId !== "" &&
    addressId !== "" &&
    start !== "";

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    if (!plan || !ready) return;
    setBusy(true);
    setError(null);
    try {
      const sub = await subscribe({
        plan_id: plan.id,
        ...(tierId ? { tier_id: tierId } : {}),
        address_id: addressId,
        start_date: start,
        schedule: days.flatMap((weekday) =>
          slots.map((meal_slot) => ({
            weekday,
            meal_slot,
            window_id: windowId,
          })),
        ),
      });
      // A failed payment still leaves a subscription awaiting payment, which
      // its page offers to pay again; it is not lost.
      try {
        const intent = await payOrder(sub.order_id);
        if (intent.redirect_url) {
          window.location.href = intent.redirect_url;
          return;
        }
      } catch {
        /* surfaced on the subscription page */
      }
      router.replace(`/${locale}/subscriptions/${sub.id}`);
    } catch (err) {
      if (err instanceof ApiError && err.fieldErrors["address_id"])
        setError(t.outside);
      else if (err instanceof ApiError && err.problem?.errors?.[0]?.message)
        setError(err.problem.errors[0].message);
      else setError(t.failed);
      setBusy(false);
    }
  }

  return (
    <form className="ms-checkout" onSubmit={onSubmit}>
      <div className="ms-checkout-main">
        <h1>{t.title}</h1>
        <p className="ms-sub" dir="auto">
          {(locale === "ar" && k.name_ar) || k.name} ·{" "}
          {(locale === "ar" && plan.name_ar) || plan.name}
        </p>

        {tiers.length > 1 && (
          <fieldset className="ms-field">
            <legend>{t.size}</legend>
            <div className="ms-chips">
              {tiers.map((tier) => (
                <button
                  key={tier.id}
                  type="button"
                  className={tier.id === tierId ? "on" : ""}
                  aria-pressed={tier.id === tierId}
                  onClick={() => setTierId(tier.id)}
                >
                  {(locale === "ar" && tier.name_ar) || tier.name} ·{" "}
                  {money(tier.price_minor)}
                </button>
              ))}
            </div>
          </fieldset>
        )}

        <fieldset className="ms-field">
          <legend>{t.days(plan.days_per_week)}</legend>
          <div className="ms-chips">
            {plan.delivery_weekdays.map((d) => (
              <button
                key={d}
                type="button"
                className={days.includes(d) ? "on" : ""}
                aria-pressed={days.includes(d)}
                disabled={plan.delivery_weekdays.length === plan.days_per_week}
                onClick={() =>
                  setDays((cur) => toggle(cur, d, plan.days_per_week).sort())
                }
              >
                {t.weekdays[d]}
              </button>
            ))}
          </div>
        </fieldset>

        {plan.meal_slots.length > plan.meals_per_day && (
          <fieldset className="ms-field">
            <legend>{t.slots(plan.meals_per_day)}</legend>
            <div className="ms-chips">
              {plan.meal_slots.map((s) => (
                <button
                  key={s}
                  type="button"
                  className={slots.includes(s) ? "on" : ""}
                  aria-pressed={slots.includes(s)}
                  onClick={() =>
                    setSlots((cur) => toggle(cur, s, plan.meals_per_day))
                  }
                >
                  {t.slot[s] ?? s}
                </button>
              ))}
            </div>
          </fieldset>
        )}

        <div className="ms-row">
          <label className="ms-field">
            <span>{t.window}</span>
            <select
              value={windowId}
              onChange={(e) => setWindowId(e.target.value)}
              required
            >
              {k.delivery_windows.map((w) => (
                <option key={w.id} value={w.id}>
                  {w.label} ({w.starts_at}–{w.ends_at})
                </option>
              ))}
            </select>
          </label>
          <label className="ms-field">
            <span>{t.start}</span>
            <input
              type="date"
              value={start}
              min={earliestStart(plan.cutoff_hours)}
              onChange={(e) => setStart(e.target.value)}
              required
            />
          </label>
        </div>

        <fieldset className="ms-field">
          <legend>{t.address}</legend>
          {(addresses.data?.items ?? []).length === 0 && (
            <p className="ms-sub">{t.noAddress}</p>
          )}
          <div className="ms-addresses">
            {(addresses.data?.items ?? []).map((a) => (
              <label
                key={a.id}
                className={`ms-address${a.id === addressId ? " on" : ""}`}
              >
                <input
                  type="radio"
                  name="address"
                  value={a.id}
                  checked={a.id === addressId}
                  onChange={() => setAddressId(a.id)}
                />
                <span>
                  <strong dir="auto">{a.label}</strong>
                  <br />
                  <span className="ms-sub" dir="auto">
                    {describeAddress(a)}
                  </span>
                </span>
              </label>
            ))}
          </div>
          <NewAddress
            t={t}
            initial={point && precise ? point : null}
            onSaved={(a) => setAddressId(a.id)}
          />
        </fieldset>
      </div>

      <aside className="ms-summary">
        <p className="ms-sub">{t.total}</p>
        <p className="ms-plan-price">
          <strong>{money(price)}</strong>
        </p>
        <p className="ms-sub">{t.cycle(plan.cycle_days)}</p>
        {error && (
          <p className="ms-bad" role="alert">
            {error}
          </p>
        )}
        <button className="ms-cta" type="submit" disabled={!ready || busy}>
          {busy ? t.paying : t.pay}
        </button>
      </aside>
    </form>
  );
}

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

function NewAddress({
  t,
  initial,
  onSaved,
}: {
  t: Copy;
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
