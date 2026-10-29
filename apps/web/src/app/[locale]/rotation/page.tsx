"use client";

import { ApiError, formatMoney } from "@marketplace/api-client";
import { useQueries, useQuery } from "@tanstack/react-query";
import { useRouter } from "next/navigation";
import { use, useEffect, useMemo, useState, type FormEvent } from "react";

import { useCustomerPoint } from "@/components/meals/location";
import { ADDRESS_COPY, NewAddress } from "@/components/meals/new-address";
import {
  CustomerShell,
  asLocale,
  formatRange,
  type Locale,
} from "@/components/meals/shell";
import {
  createRotation,
  describeAddress,
  listAddresses,
  payRotation,
  previewRotation,
  type RotationInput,
  forgetInvite,
  rememberedInvite,
} from "@/lib/customer-api";
import { offerLabel } from "@/components/meals/offers";
import { getMealKitchen, getMealKitchens } from "@/lib/meals-api";
import type { Plan } from "@/lib/vendor-api";
import { tagLabel } from "@/components/meals/kitchen-card";

const COPY = {
  en: {
    back: "Kitchens",
    title: "Build a 4-kitchen rotation",
    intro:
      "Pick four kitchens. Each week of the next 28 days is cooked by one of them; we choose the order and show it before you pay. You pay each kitchen's weekly price.",
    address: "Deliver to",
    noAddress: "Add an address to see the kitchens that deliver there.",
    kitchens: (n: number) => `Kitchens (${n} of 4 chosen)`,
    none: "No kitchens in the rotation deliver to this address yet.",
    perWeek: "a week",
    days: (n: number) => `Delivery days (choose ${n})`,
    slots: (n: number) => `Meals (choose ${n})`,
    start: "Rotation starts on",
    mismatch:
      "These kitchens' plans don't cover the same number of days and meals. Swap one to continue.",
    noCommon:
      "These kitchens don't share enough delivery days or meals. Swap one to continue.",
    schedule: "Your four weeks",
    pickFour: "Choose four kitchens and your schedule to see the weekly order.",
    emptyPick: "Choose a kitchen",
    week: (n: number) => `Week ${n}`,
    total: "Total for 28 days",
    promo: "Promo code",
    apply: "Apply",
    remove: "Remove",
    discount: "Discount",
    pay: "Start rotation and pay",
    paying: "Processing…",
    loading: "Loading…",
    failed: "Something went wrong. Try again.",
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
    back: "المطابخ",
    title: "اختر تناوب ٤ مطابخ",
    intro:
      "اختر أربعة مطابخ. يطبخ كل أسبوع من الأيام الـ28 القادمة أحدها؛ نحدد الترتيب ونعرضه قبل الدفع. تدفع السعر الأسبوعي لكل مطبخ.",
    address: "التوصيل إلى",
    noAddress: "أضف عنوانًا لعرض المطابخ التي توصل إليه.",
    kitchens: (n: number) => `المطابخ (تم اختيار ${n} من ٤)`,
    none: "لا توجد مطابخ ضمن التناوب توصل إلى هذا العنوان بعد.",
    perWeek: "أسبوعيًا",
    days: (n: number) => `أيام التوصيل (اختر ${n})`,
    slots: (n: number) => `الوجبات (اختر ${n})`,
    start: "يبدأ التناوب في",
    mismatch:
      "خطط هذه المطابخ لا تغطي نفس عدد الأيام والوجبات. استبدل أحدها للمتابعة.",
    noCommon:
      "هذه المطابخ لا تشترك في أيام توصيل أو وجبات كافية. استبدل أحدها للمتابعة.",
    schedule: "أسابيعك الأربعة",
    pickFour: "اختر أربعة مطابخ وجدولك لعرض ترتيب الأسابيع.",
    emptyPick: "اختر مطبخًا",
    week: (n: number) => `الأسبوع ${n}`,
    total: "المجموع لـ28 يومًا",
    promo: "رمز الخصم",
    apply: "تطبيق",
    remove: "إزالة",
    discount: "الخصم",
    pay: "ابدأ التناوب وادفع",
    paying: "جارٍ المعالجة…",
    loading: "جارٍ التحميل…",
    failed: "حدث خطأ. حاول مرة أخرى.",
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

const rotationPlan = (plans: Plan[]) =>
  plans.find((p) => p.rotation_enabled && p.rotation_week_price_minor != null);

export default function RotationPage({
  params,
}: {
  params: Promise<{ locale: string }>;
}) {
  const locale = asLocale(use(params).locale);
  const t = COPY[locale];
  return (
    <CustomerShell locale={locale} back={{ href: `/${locale}`, label: t.back }}>
      <Builder locale={locale} t={t} />
    </CustomerShell>
  );
}

function Builder({ locale, t }: { locale: Locale; t: Copy }) {
  const router = useRouter();
  const { point, precise } = useCustomerPoint();
  const addresses = useQuery({
    queryKey: ["me", "addresses"],
    queryFn: listAddresses,
  });
  const [addressId, setAddressId] = useState("");
  const [picked, setPicked] = useState<string[]>([]);
  const [days, setDays] = useState<number[]>([]);
  const [slots, setSlots] = useState<string[]>([]);
  const [start, setStart] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [codeDraft, setCodeDraft] = useState("");
  const [code, setCode] = useState("");
  useEffect(() => {
    const invite = rememberedInvite();
    if (invite) {
      setCode(invite);
      setCodeDraft(invite);
    }
  }, []);

  useEffect(() => {
    const list = addresses.data?.items ?? [];
    if (!addressId && list.length > 0) {
      setAddressId((list.find((a) => a.is_default) ?? list[0])!.id);
    }
  }, [addresses.data, addressId]);

  // Kitchens are listed for the address being delivered to, so every one shown
  // can actually be chosen.
  const address = addresses.data?.items.find((a) => a.id === addressId);
  const where = address?.point ?? null;

  const kitchens = useQuery({
    queryKey: ["meal-kitchens", "rotation", where?.lat, where?.lng],
    queryFn: () => getMealKitchens({ point: where, rotation: true }),
    enabled: where != null,
  });

  useEffect(() => {
    const ids = new Set((kitchens.data?.items ?? []).map((k) => k.branch_id));
    setPicked((cur) => cur.filter((id) => ids.has(id)));
  }, [kitchens.data]);

  const details = useQueries({
    queries: picked.map((id) => ({
      queryKey: ["meal-kitchen", id, "rotation"],
      queryFn: () => getMealKitchen(id),
    })),
  });
  const plans = details
    .map((d) => (d.data ? rotationPlan(d.data.plan_list) : undefined))
    .filter((p): p is Plan => p != null);
  const allLoaded = plans.length === picked.length;

  const shape = useMemo(() => {
    if (plans.length === 0) return null;
    const first = plans[0]!;
    const same = plans.every(
      (p) =>
        p.days_per_week === first.days_per_week &&
        p.meals_per_day === first.meals_per_day,
    );
    const weekdays = [0, 1, 2, 3, 4, 5, 6].filter((d) =>
      plans.every((p) => p.delivery_weekdays.includes(d)),
    );
    const mealSlots = first.meal_slots.filter((s) =>
      plans.every((p) => p.meal_slots.includes(s)),
    );
    return {
      same,
      days: first.days_per_week,
      meals: first.meals_per_day,
      weekdays,
      mealSlots,
      enough:
        weekdays.length >= first.days_per_week &&
        mealSlots.length >= first.meals_per_day,
      cutoff: Math.max(...plans.map((p) => p.cutoff_hours)),
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [plans.map((p) => p.id).join()]);

  // Keep the chosen days and meals inside what all chosen kitchens cook.
  useEffect(() => {
    if (!shape || !shape.same || !shape.enough) return;
    setDays((cur) => {
      const kept = cur.filter((d) => shape.weekdays.includes(d));
      return kept.length === shape.days
        ? kept
        : shape.weekdays.slice(0, shape.days);
    });
    setSlots((cur) => {
      const kept = cur.filter((s) => (shape.mealSlots as string[]).includes(s));
      return kept.length === shape.meals
        ? kept
        : shape.mealSlots.slice(0, shape.meals);
    });
    setStart((cur) => {
      const min = earliestStart(shape.cutoff);
      return cur && cur >= min ? cur : min;
    });
  }, [shape]);

  const input: RotationInput | null =
    picked.length === 4 &&
    allLoaded &&
    shape?.same &&
    shape.enough &&
    days.length === shape.days &&
    slots.length === shape.meals &&
    addressId &&
    start
      ? {
          plan_ids: plans.map((p) => p.id),
          address_id: addressId,
          start_date: start,
          delivery_weekdays: [...days].sort(),
          meal_slots: slots,
          ...(code ? { promo_code: code } : {}),
        }
      : null;

  const preview = useQuery({
    queryKey: ["rotation-preview", input],
    queryFn: () => previewRotation(input!),
    enabled: input != null,
    retry: false,
  });

  if (addresses.isPending) return <p className="ms-note">{t.loading}</p>;
  if (addresses.isError) return <p className="ms-empty">{t.failed}</p>;

  const currency = plans[0]?.currency ?? kitchens.data?.items[0]?.currency;
  const money = (n: number) =>
    formatMoney({ amount: n, currency: currency ?? "AED" }, locale);
  const problem = (err: unknown) =>
    err instanceof ApiError && err.problem?.errors?.[0]?.message
      ? err.problem.errors[0].message
      : err instanceof ApiError && err.problem?.detail
        ? err.problem.detail
        : t.failed;

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    if (!input || !preview.data) return;
    setBusy(true);
    setError(null);
    try {
      const rot = await createRotation(input);
      // An unpaid rotation stays on its page with a pay button; it is not lost.
      try {
        const intent = await payRotation(rot.id);
        if (intent.redirect_url) {
          window.location.href = intent.redirect_url;
          return;
        }
      } catch {
        /* surfaced on the rotation page */
      }
      router.replace(`/${locale}/rotations/${rot.id}`);
    } catch (err) {
      setError(problem(err));
      setBusy(false);
    }
  }

  const list = kitchens.data?.items ?? [];

  return (
    <form className="ms-checkout" onSubmit={onSubmit}>
      <div className="ms-checkout-main">
        <h1>{t.title}</h1>
        <p className="ms-sub">{t.intro}</p>

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
            t={ADDRESS_COPY[locale]}
            initial={point && precise ? point : null}
            onSaved={(a) => setAddressId(a.id)}
          />
        </fieldset>

        {where && (
          <fieldset className="ms-field">
            <legend>{t.kitchens(picked.length)}</legend>
            {kitchens.isPending ? (
              <p className="ms-note">{t.loading}</p>
            ) : kitchens.isError ? (
              <p className="ms-bad">{t.failed}</p>
            ) : list.length === 0 ? (
              <p className="ms-sub">{t.none}</p>
            ) : (
              <div className="ms-addresses">
                {list.map((k) => {
                  const on = picked.includes(k.branch_id);
                  return (
                    <label
                      key={k.branch_id}
                      className={`ms-address${on ? " on" : ""}`}
                    >
                      <input
                        type="checkbox"
                        checked={on}
                        disabled={!on && picked.length >= 4}
                        onChange={() =>
                          setPicked((cur) => toggle(cur, k.branch_id, 4))
                        }
                      />
                      <span>
                        <strong dir="auto">
                          {(locale === "ar" && k.name_ar) || k.name}
                        </strong>
                        <br />
                        <span className="ms-sub" dir="auto">
                          {k.cuisines
                            .map((c) => tagLabel(c, locale))
                            .join(", ")}
                          {k.rotation_week_price_minor != null &&
                            ` · ${formatMoney({ amount: k.rotation_week_price_minor, currency: k.currency }, locale)} ${t.perWeek}`}
                        </span>
                      </span>
                    </label>
                  );
                })}
              </div>
            )}
          </fieldset>
        )}

        {shape && allLoaded && !shape.same && (
          <p className="ms-bad" role="alert">
            {t.mismatch}
          </p>
        )}
        {shape && allLoaded && shape.same && !shape.enough && (
          <p className="ms-bad" role="alert">
            {t.noCommon}
          </p>
        )}

        {shape && shape.same && shape.enough && (
          <>
            <fieldset className="ms-field">
              <legend>{t.days(shape.days)}</legend>
              <div className="ms-chips">
                {shape.weekdays.map((d) => (
                  <button
                    key={d}
                    type="button"
                    className={days.includes(d) ? "on" : ""}
                    aria-pressed={days.includes(d)}
                    disabled={shape.weekdays.length === shape.days}
                    onClick={() =>
                      setDays((cur) => toggle(cur, d, shape.days).sort())
                    }
                  >
                    {t.weekdays[d]}
                  </button>
                ))}
              </div>
            </fieldset>
            {shape.mealSlots.length > shape.meals && (
              <fieldset className="ms-field">
                <legend>{t.slots(shape.meals)}</legend>
                <div className="ms-chips">
                  {shape.mealSlots.map((s) => (
                    <button
                      key={s}
                      type="button"
                      className={slots.includes(s) ? "on" : ""}
                      aria-pressed={slots.includes(s)}
                      onClick={() =>
                        setSlots((cur) => toggle(cur, s, shape.meals))
                      }
                    >
                      {t.slot[s] ?? s}
                    </button>
                  ))}
                </div>
              </fieldset>
            )}
            <label className="ms-field">
              <span>{t.start}</span>
              <input
                type="date"
                value={start}
                min={earliestStart(shape.cutoff)}
                onChange={(e) => setStart(e.target.value)}
                required
              />
            </label>
          </>
        )}
      </div>

      <aside className="ms-summary">
        <h2>{t.schedule}</h2>
        {!input ? (
          <>
            <ol className="ms-picks">
              {[0, 1, 2, 3].map((i) => {
                const id = picked[i];
                const k = kitchens.data?.items.find((x) => x.branch_id === id);
                return (
                  <li key={i} className={id ? "on" : undefined}>
                    <b>{i + 1}</b>
                    <span dir="auto">
                      {k
                        ? (locale === "ar" && k.name_ar) || k.name
                        : t.emptyPick}
                    </span>
                  </li>
                );
              })}
            </ol>
            <p className="ms-sub">{t.pickFour}</p>
          </>
        ) : preview.isPending ? (
          <p className="ms-note">{t.loading}</p>
        ) : preview.isError ? (
          <p className="ms-bad" role="alert">
            {problem(preview.error)}
          </p>
        ) : (
          <>
            <ol className="ms-weeks">
              {preview.data.weeks.map((w) => (
                <li key={w.week}>
                  <span>
                    <strong>{t.week(w.week)}</strong>{" "}
                    <span className="ms-sub">
                      {formatRange(locale, w.starts_on, w.ends_on)}
                    </span>
                    <br />
                    <span dir="auto">
                      {(locale === "ar" && w.kitchen_name_ar) || w.kitchen_name}
                    </span>
                  </span>
                  <span>{money(w.price_minor)}</span>
                </li>
              ))}
            </ol>
            {preview.data.discount_minor > 0 && (
              <dl className="ms-lines">
                <dt dir="auto">
                  {t.discount}
                  {preview.data.promo &&
                    ` · ${offerLabel(preview.data.promo, locale)}`}
                </dt>
                <dd className="ms-ok">−{money(preview.data.discount_minor)}</dd>
              </dl>
            )}
            <p className="ms-sub">{t.total}</p>
            <p className="ms-plan-price">
              <strong>{money(preview.data.amount_minor)}</strong>
            </p>
          </>
        )}
        {input && (
          <div className="ms-promo">
            {code ? (
              <p>
                <span>{code}</span>
                <button
                  type="button"
                  className="ms-link"
                  onClick={() => {
                    setCode("");
                    forgetInvite();
                    setCodeDraft("");
                  }}
                >
                  {t.remove}
                </button>
              </p>
            ) : (
              <label className="ms-field">
                <span>{t.promo}</span>
                <span className="ms-promo-row">
                  <input
                    value={codeDraft}
                    autoCapitalize="characters"
                    onChange={(e) => setCodeDraft(e.target.value)}
                  />
                  <button
                    type="button"
                    disabled={!codeDraft.trim()}
                    onClick={() => setCode(codeDraft.trim().toUpperCase())}
                  >
                    {t.apply}
                  </button>
                </span>
              </label>
            )}
          </div>
        )}
        {error && (
          <p className="ms-bad" role="alert">
            {error}
          </p>
        )}
        <button
          className="ms-cta"
          type="submit"
          disabled={!input || !preview.data || busy}
        >
          {busy ? t.paying : t.pay}
        </button>
      </aside>
    </form>
  );
}
