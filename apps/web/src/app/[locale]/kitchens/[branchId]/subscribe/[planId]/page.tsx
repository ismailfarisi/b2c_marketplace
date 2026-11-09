"use client";

import { ApiError, formatMoney } from "@marketplace/api-client";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useRouter } from "next/navigation";
import { use, useEffect, useMemo, useState, type FormEvent } from "react";

import { LoadError } from "@/components/load-error";
import {
  checkoutProblem,
  isPromoProblem,
} from "@/components/meals/checkout-errors";
import { useCustomerPoint } from "@/components/meals/location";
import { CustomerShell, asLocale, type Locale } from "@/components/meals/shell";
import { NewAddress } from "@/components/meals/new-address";
import {
  MealAddressPicker,
  blockedMessage,
  checkoutAddressProblems,
  useCoverage,
  useMealAddresses,
} from "@/components/meals/meal-addresses";
import { OFFER_COPY, offerLabel, offerName } from "@/components/meals/offers";
import {
  describeAddress,
  listAddresses,
  listMyCompanies,
  payOrder,
  previewLunch,
  previewSubscription,
  startLunch,
  subscribe,
  type LunchInput,
  type SubscribeInput,
  forgetInvite,
  rememberedInvite,
} from "@/lib/customer-api";
import { earliestFromTooSoon, earliestStart } from "@/lib/earliest-start";
import { getMealKitchen } from "@/lib/meals-api";
import { useIdempotencyKey } from "@/lib/use-idempotency-key";

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
    payer: "Who pays",
    payMe: "I pay",
    payCompany: (name: string) => `Paid by ${name}`,
    office: "Deliver to your office",
    companyNote: (cap: string) =>
      `Your company covers its lunch days, up to ${cap} a meal. You pay nothing.`,
    companyPending: "Your company's lunch account is waiting for approval.",
    companyFailed: "Couldn't load your company lunch benefit. You can retry, or pay yourself.",
    retry: "Try again",
    startLunch: "Start office lunch",
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
    promo: "Promo code",
    apply: "Apply",
    remove: "Remove",
    subtotal: "Meals",
    discount: "Discount",
    delivery: "Delivery",
    once: (from: string, to: string) => `${from} – ${to}, does not renew.`,
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
    payer: "من يدفع",
    payMe: "أنا أدفع",
    payCompany: (name: string) => `على حساب ${name}`,
    office: "التوصيل إلى مكتبك",
    companyNote: (cap: string) =>
      `تغطي شركتك أيام الغداء حتى ${cap} للوجبة. لا تدفع شيئًا.`,
    companyPending: "حساب الغداء لشركتك بانتظار الموافقة.",
    companyFailed: "تعذر تحميل ميزة غداء شركتك. يمكنك إعادة المحاولة أو الدفع بنفسك.",
    retry: "حاول مرة أخرى",
    startLunch: "ابدأ غداء المكتب",
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
    promo: "رمز الخصم",
    apply: "تطبيق",
    remove: "إزالة",
    subtotal: "الوجبات",
    discount: "الخصم",
    delivery: "التوصيل",
    once: (from: string, to: string) => `${from} – ${to}، لا يتجدد.`,
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
  const { point, exact, place, placeReady } = useCustomerPoint();
  const kitchen = useQuery({
    queryKey: ["meal-kitchen", branchId, "checkout"],
    queryFn: () => getMealKitchen(branchId),
  });
  const addresses = useQuery({
    queryKey: ["me", "addresses"],
    queryFn: listAddresses,
  });

  // Office lunch: an employee whose company covers this can bill it there.
  const companies = useQuery({
    queryKey: ["me", "companies"],
    queryFn: listMyCompanies,
  });
  const [companyId, setCompanyId] = useState<string>("");
  const [officeId, setOfficeId] = useState<string>("");
  const company = (companies.data?.items ?? []).find(
    (c) => c.company_id === companyId,
  );
  const lunchKey = useIdempotencyKey();

  const plan = kitchen.data?.plan_list.find((pl) => pl.id === planId);
  const tiers = plan?.tiers.filter((x) => x.is_active) ?? [];

  const [tierId, setTierId] = useState<string | null>(null);
  const [days, setDays] = useState<number[]>([]);
  const [slots, setSlots] = useState<string[]>([]);
  const [windowId, setWindowId] = useState<string>("");
  const [start, setStart] = useState("");
  const where = useMealAddresses(slots);
  const addressId = where.base;
  const setAddressId = where.setBase;
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [submitAddress, setSubmitAddress] = useState<Record<string, string> | null>(null);
  // A public offer arrives as ?promo=<id> from the kitchen page; a code is typed.
  const [promoId, setPromoId] = useState<string | null>(null);
  const [codeDraft, setCodeDraft] = useState("");
  const [code, setCode] = useState("");
  useEffect(() => {
    const params = new URLSearchParams(window.location.search);
    setPromoId(params.get("promo"));
    const invite = rememberedInvite();
    if (!params.get("promo") && invite) {
      setCode(invite);
      setCodeDraft(invite);
    }
  }, []);

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
    // Wait for the stored place: cached addresses can arrive before it.
    if (!placeReady) return;
    if (!addressId && list.length > 0) {
      // The address picked on home leads, then the default.
      const chosen =
        place?.kind === "address"
          ? list.find((a) => a.id === place.id)
          : undefined;
      setAddressId((chosen ?? list.find((a) => a.is_default) ?? list[0])!.id);
    }
  }, [addresses.data, addressId, place, placeReady]);

  const price = useMemo(() => {
    if (!plan) return 0;
    return tiers.find((x) => x.id === tierId)?.price_minor ?? plan.price_minor;
  }, [plan, tiers, tierId]);

  const input: SubscribeInput | null =
    plan &&
    days.length === plan.days_per_week &&
    slots.length === plan.meals_per_day &&
    windowId !== "" &&
    (addressId !== "" || companyId !== "") &&
    start !== ""
      ? {
          plan_id: plan.id,
          ...(tierId ? { tier_id: tierId } : {}),
          address_id: addressId,
          start_date: start,
          schedule: days.flatMap((weekday) =>
            slots.map((meal_slot) => {
              const own = where.of(meal_slot);
              return {
                weekday,
                meal_slot,
                window_id: windowId,
                ...(own && own !== addressId ? { address_id: own } : {}),
              };
            }),
          ),
          ...(code
            ? { promo_code: code }
            : promoId
              ? { promo_id: promoId }
              : {}),
        }
      : null;
  const lunch: LunchInput | null =
    company && input && officeId
      ? {
          office_id: officeId,
          plan_id: input.plan_id,
          ...(input.tier_id ? { tier_id: input.tier_id } : {}),
          start_date: input.start_date,
          schedule: input.schedule,
        }
      : null;
  // One key per draft: pressing Pay again after a timeout or failure is the
  // same request to the server, so it cannot open a second subscription.
  const idem = useIdempotencyKey();
  const draftKey = JSON.stringify(input);
  const resetKey = idem.reset;
  useEffect(() => resetKey(), [draftKey, resetKey]);
  useEffect(() => setSubmitAddress(null), [draftKey]);
  // A changed draft is a different request: a new key, so it isn't refused
  // as a replay with another body.
  const lunchDraft = JSON.stringify([companyId, lunch]);
  const resetLunchKey = lunchKey.reset;
  useEffect(() => resetLunchKey(), [lunchDraft, resetLunchKey]);
  const preview = useQuery({
    queryKey: ["subscription-preview", company ? lunch : input, companyId],
    queryFn: () =>
      company
        ? previewLunch(company.company_id, lunch!).then((r) => r.preview)
        : previewSubscription(input!),
    enabled: company ? lunch != null : input != null,
    retry: false,
  });
  // Which saved addresses this kitchen delivers to; an uncovered one is
  // marked and blocks checkout before the server has to refuse it.
  const coverage = useCoverage(
    [branchId],
    (addresses.data?.items ?? []).map((a) => a.id),
  );
  const kitchenLabel =
    (locale === "ar" && kitchen.data?.name_ar) || kitchen.data?.name || "";
  const addressLabel = (id: string) =>
    addresses.data?.items.find((a) => a.id === id)?.label ?? "";
  const uncovered = company
    ? null
    : (where.split && slots.length >= 2 ? slots : [null]).find(
        (slot) =>
          coverage.delivers(branchId, slot ? where.of(slot) : addressId) === false,
      );
  const blocked =
    uncovered !== undefined && uncovered !== null
      ? blockedMessage(locale, kitchenLabel, uncovered, addressLabel(where.of(uncovered)))
      : uncovered === null
        ? blockedMessage(locale, kitchenLabel, null, addressLabel(addressId))
        : null;
  const addressProblems = (err: unknown) =>
    checkoutAddressProblems(
      err,
      locale,
      (i) => input?.schedule[i]?.meal_slot,
      (slot) => addressLabel(slot ? where.of(slot) : addressId) || null,
      kitchenLabel,
    );
  const previewAddress = preview.isError ? addressProblems(preview.error) : null;
  const previewProblem = preview.isError
    ? previewAddress
      ? Object.values(previewAddress)[0]!
      : checkoutProblem(preview.error, locale)
    : null;
  // A quote refused as too_soon names the server's first allowed date; the
  // checkout moves there instead of staying stuck on a day past its cutoff.
  const serverEarliest = earliestFromTooSoon(preview.error);
  useEffect(() => {
    if (serverEarliest) setStart((cur) => (cur >= serverEarliest ? cur : serverEarliest));
  }, [serverEarliest]);
  const promoRejected = isPromoProblem(preview.error);

  if (kitchen.isPending || addresses.isPending)
    return <p className="ms-note">{t.loading}</p>;
  if (
    kitchen.isError &&
    !(kitchen.error instanceof ApiError && kitchen.error.status === 404)
  ) {
    return (
      <LoadError
        error={kitchen.error}
        locale={locale}
        onRetry={() => void kitchen.refetch()}
      />
    );
  }
  if (kitchen.isError || !plan) return <p className="ms-empty">{t.notFound}</p>;
  if (addresses.isError) {
    return (
      <LoadError
        error={addresses.error}
        locale={locale}
        onRetry={() => void addresses.refetch()}
      />
    );
  }
  const k = kitchen.data;
  const money = (n: number) =>
    formatMoney({ amount: n, currency: plan.currency }, locale);
  const ready =
    (company ? lunch != null : input != null) && !preview.isError && !blocked;

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    if (!plan || !ready || !input) return;
    setBusy(true);
    setError(null);
    try {
      if (company && lunch) {
        const started = await startLunch(company.company_id, lunch, {
          headers: lunchKey.headers(),
        });
        lunchKey.reset();
        router.replace(`/${locale}/subscriptions/${started.id}`);
        return;
      }
      const sub = await subscribe(input!, { headers: idem.headers() });
      // A failed payment still leaves a subscription awaiting payment, which
      // its page offers to pay again; it is not lost. The page is told how
      // the payment went so it can say so.
      let outcome = "payment=failed";
      try {
        const intent = await payOrder(sub.order_id);
        if (intent.redirect_url) {
          window.location.href = intent.redirect_url;
          return;
        }
        if (intent.status === "authorized") outcome = "paid=1";
        else if (intent.status === "requires_action")
          outcome = "payment=pending";
      } catch {
        /* outcome stays failed */
      }
      idem.reset();
      router.replace(`/${locale}/subscriptions/${sub.id}?${outcome}`);
    } catch (err) {
      const byMeal = addressProblems(err);
      setSubmitAddress(byMeal);
      setError(byMeal ? Object.values(byMeal)[0]! : checkoutProblem(err, locale));
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
                  {windowLabel(w)}
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
              dir="ltr"
              onChange={(e) => setStart(e.target.value)}
              required
            />
          </label>
        </div>

        {companies.isError && (
          <p className="ms-note" role="alert">
            {t.companyFailed}{" "}
            <button
              type="button"
              className="ms-link"
              disabled={companies.isFetching}
              onClick={() => companies.refetch()}
            >
              {t.retry}
            </button>
          </p>
        )}
        {(companies.data?.items ?? []).length > 0 && (
          <fieldset className="ms-field">
            <legend>{t.payer}</legend>
            <div className="ms-chips">
              <button
                type="button"
                className={companyId === "" ? "on" : ""}
                aria-pressed={companyId === ""}
                onClick={() => setCompanyId("")}
              >
                {t.payMe}
              </button>
              {(companies.data?.items ?? []).map((c) => (
                <button
                  key={c.company_id}
                  type="button"
                  className={companyId === c.company_id ? "on" : ""}
                  aria-pressed={companyId === c.company_id}
                  onClick={() => {
                    setCompanyId(c.company_id);
                    setOfficeId(c.offices[0]?.id ?? "");
                    setCode("");
                    setPromoId(null);
                  }}
                >
                  <span dir="auto">{t.payCompany(c.name)}</span>
                </button>
              ))}
            </div>
            {company && (
              <p className="ms-sub">
                {company.status === "active"
                  ? t.companyNote(
                      formatMoney(
                        {
                          amount: company.policy.meal_cap_minor,
                          currency: company.currency,
                        },
                        locale,
                      ),
                    )
                  : t.companyPending}
              </p>
            )}
          </fieldset>
        )}

        {company ? (
          <fieldset className="ms-field">
            <legend>{t.office}</legend>
            <div className="ms-addresses">
              {company.offices.map((o) => (
                <label
                  key={o.id}
                  className={`ms-address${o.id === officeId ? " on" : ""}`}
                >
                  <input
                    type="radio"
                    name="office"
                    value={o.id}
                    checked={o.id === officeId}
                    onChange={() => setOfficeId(o.id)}
                  />
                  <span>
                    <strong dir="auto">{o.label}</strong>
                    <br />
                    <span className="ms-sub" dir="auto">
                      {[o.building, o.area, o.city].filter(Boolean).join(", ")}
                    </span>
                  </span>
                </label>
              ))}
            </div>
          </fieldset>
        ) : (
          <MealAddressPicker
            locale={locale}
            legend={t.address}
            addresses={addresses.data?.items ?? []}
            m={where}
            covers={(id) => coverage.delivers(branchId, id)}
            problems={submitAddress ?? previewAddress ?? undefined}
            coverageError={coverage.isError}
            onRetryCoverage={() => void coverage.refetch()}
            noAddress={t.noAddress}
            footer={
              <NewAddress
                t={t}
                initial={point && exact ? point : null}
                onSaved={(a) => setAddressId(a.id)}
              />
            }
          />
        )}
      </div>

      <aside className="ms-summary">
        {preview.data?.promo && (
          <p className="ms-offer on">
            <strong dir="auto">{offerLabel(preview.data.promo, locale)}</strong>
            <span dir="auto">{offerName(preview.data.promo, locale)}</span>
          </p>
        )}
        {preview.data ? (
          <dl className="ms-lines">
            <dt>{t.subtotal}</dt>
            <dd>{money(preview.data.subtotal_minor)}</dd>
            {preview.data.discount_minor > 0 && (
              <>
                <dt>{t.discount}</dt>
                <dd className="ms-ok">−{money(preview.data.discount_minor)}</dd>
              </>
            )}
            <dt>{t.delivery}</dt>
            <dd>{money(preview.data.delivery_minor)}</dd>
          </dl>
        ) : null}
        <p className="ms-sub">{t.total}</p>
        <p className="ms-plan-price">
          <strong>{money(preview.data?.total_minor ?? price)}</strong>
        </p>
        <p className="ms-sub">
          {preview.data?.promo?.trial_days
            ? OFFER_COPY[locale].trialNote(preview.data.promo.trial_days)
            : preview.data && !preview.data.renews
              ? t.once(preview.data.starts_on, preview.data.ends_on)
              : t.cycle(plan.cycle_days)}
        </p>
        {!company && (
          <div className="ms-promo">
            {code || promoId ? (
              <p>
                <span dir="auto">
                  {code ||
                    (preview.data?.promo
                      ? offerName(preview.data.promo, locale)
                      : "")}
                </span>
                <button
                  type="button"
                  className="ms-link"
                  onClick={() => {
                    setCode("");
                    forgetInvite();
                    setCodeDraft("");
                    setPromoId(null);
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
            {promoRejected && previewProblem && (
              <p className="ms-bad" role="alert">
                {previewProblem}
              </p>
            )}
          </div>
        )}
        {preview.isError && !promoRejected && previewProblem && (
          <p className="ms-bad" role="alert">
            {previewProblem}
          </p>
        )}
        {blocked && !company && (
          <p className="ms-bad" role="alert">
            {blocked}
          </p>
        )}
        {error && (
          <p className="ms-bad" role="alert">
            {error}
          </p>
        )}
        <button className="ms-cta" type="submit" disabled={!ready || busy}>
          {busy ? t.paying : company ? t.startLunch : t.pay}
        </button>
      </aside>
    </form>
  );
}

const sameRange = (a: string, b: string) =>
  a.replace(/[\s\u2013\u2014-]/g, "") === b.replace(/[\s\u2013\u2014-]/g, "");

// <option> text cannot hold <bdi>, so the range is wrapped in Unicode
// left-to-right isolates (LRI…PDI): in Arabic it would otherwise read 14:00–12:00.
function windowLabel(w: { label: string; starts_at: string; ends_at: string }) {
  const plain = `${w.starts_at}–${w.ends_at}`;
  const range = `\u2066${plain}\u2069`;
  return w.label && !sameRange(w.label, plain)
    ? `${w.label} (${range})`
    : range;
}
