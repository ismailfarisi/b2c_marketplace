"use client";

import { ApiError } from "@marketplace/api-client";
import { useMutation, useQuery } from "@tanstack/react-query";
import { useEffect, useMemo, useState, type ReactNode } from "react";

import { LoadError } from "@/components/load-error";
import { ADDRESS_COPY, NewAddress } from "@/components/meals/new-address";
import { formatDay } from "@/components/meals/shell";
import {
  addressCoverage,
  describeAddress,
  listAddresses,
  type Address,
  type DeliveryAddress,
  type SlotAddress,
} from "@/lib/customer-api";
import { networkMessage, problemMessage } from "@/lib/problem";
import { useBusyGuard } from "@/lib/use-busy-guard";
import { useIdempotencyKey } from "@/lib/use-idempotency-key";

type Locale = "en" | "ar";

/** Meals in the order of the day, as the API orders them. */
export const SLOT_ORDER = ["suhoor", "breakfast", "lunch", "snack", "iftar", "dinner"];
export const orderSlots = (slots: string[]) =>
  [...slots].sort((a, b) => SLOT_ORDER.indexOf(a) - SLOT_ORDER.indexOf(b));

const SLOT: Record<Locale, Record<string, string>> = {
  en: {
    breakfast: "Breakfast",
    lunch: "Lunch",
    dinner: "Dinner",
    snack: "Snack",
    suhoor: "Suhoor",
    iftar: "Iftar",
  },
  ar: {
    breakfast: "الفطور",
    lunch: "الغداء",
    dinner: "العشاء",
    snack: "الوجبة الخفيفة",
    suhoor: "السحور",
    iftar: "الإفطار",
  },
};
export const slotName = (slot: string, locale: Locale) => SLOT[locale][slot] ?? slot;
const slotLower = (slot: string, locale: Locale) =>
  locale === "en" ? slotName(slot, locale).toLowerCase() : slotName(slot, locale);

const COPY = {
  en: {
    oneAddress: "All meals to one address",
    perMeal: "Different address per meal",
    slotTo: (slot: string) => `${slotName(slot, "en")} to`,
    notHere: "Doesn't deliver here",
    notHereOption: "doesn't deliver here",
    notTo: (label: string) => `Doesn't deliver to ${label}`,
    blocked: (kitchen: string, slot: string | null, label: string) =>
      slot
        ? `${kitchen} doesn't deliver ${slotLower(slot, "en")} to ${label}. Choose another address for ${slotLower(slot, "en")}.`
        : `${kitchen} doesn't deliver to ${label}. Choose another address.`,
    checkFailed: "Couldn't check which addresses this kitchen delivers to.",
    retry: "Try again",
    thisKitchen: "This kitchen",
    deliverTo: "Delivery address",
    allMeals: "All meals",
    change: "Change address",
    cancel: "Cancel",
    save: "Save address",
    saving: "Saving…",
    sameAll: "Same address for all meals",
    pick: "Choose an address",
    noChange: "Choose a different address to save.",
    company: (c: string | null) =>
      c
        ? `${c} manages the office address for these lunches. Ask your company's lunch admin to change it.`
        : "Your company manages the office address for these lunches.",
    moved: (date: string | null, slot: string, label: string) =>
      date
        ? `From ${date}, ${slotLower(slot, "en")} goes to ${label}.`
        : `${slotName(slot, "en")} now goes to ${label}.`,
    prepared: "Meals already being prepared still go to the old address.",
    week: (n: number) => ` (week ${n})`,
    notFound: "That address no longer exists. Choose another one.",
    companyLunch: "Your company manages this office lunch's address.",
    notChangeable: "This plan has ended, so its address can't change.",
    rotationWeek: "This week is part of a rotation. Change the address on the rotation.",
    unknownSlot: "One of those meals isn't part of this plan.",
    failed: "Couldn't change the address. Try again.",
    noAddresses: "Add an address to choose it here.",
  },
  ar: {
    oneAddress: "كل الوجبات إلى عنوان واحد",
    perMeal: "عنوان مختلف لكل وجبة",
    slotTo: (slot: string) => `${slotName(slot, "ar")} إلى`,
    notHere: "لا يوصل إلى هنا",
    notHereOption: "لا يوصل إلى هنا",
    notTo: (label: string) => `لا يوصل إلى ${label}`,
    blocked: (kitchen: string, slot: string | null, label: string) =>
      slot
        ? `${kitchen} لا يوصل ${slotLower(slot, "ar")} إلى ${label}. اختر عنوانًا آخر لـ${slotLower(slot, "ar")}.`
        : `${kitchen} لا يوصل إلى ${label}. اختر عنوانًا آخر.`,
    checkFailed: "تعذر التحقق من العناوين التي يوصل إليها هذا المطبخ.",
    retry: "حاول مرة أخرى",
    thisKitchen: "هذا المطبخ",
    deliverTo: "عنوان التوصيل",
    allMeals: "كل الوجبات",
    change: "تغيير العنوان",
    cancel: "إلغاء",
    save: "حفظ العنوان",
    saving: "جارٍ الحفظ…",
    sameAll: "العنوان نفسه لكل الوجبات",
    pick: "اختر عنوانًا",
    noChange: "اختر عنوانًا مختلفًا للحفظ.",
    company: (c: string | null) =>
      c
        ? `تدير ${c} عنوان المكتب لوجبات الغداء هذه. اطلب من مسؤول الغداء في شركتك تغييره.`
        : "تدير شركتك عنوان المكتب لوجبات الغداء هذه.",
    moved: (date: string | null, slot: string, label: string) =>
      date
        ? `اعتبارًا من ${date}، يصل ${slotLower(slot, "ar")} إلى ${label}.`
        : `يصل ${slotLower(slot, "ar")} الآن إلى ${label}.`,
    prepared: "الوجبات التي بدأ تحضيرها ستصل إلى العنوان القديم.",
    week: (n: number) => ` (الأسبوع ${n})`,
    notFound: "هذا العنوان لم يعد موجودًا. اختر عنوانًا آخر.",
    companyLunch: "تدير شركتك عنوان غداء المكتب هذا.",
    notChangeable: "انتهت هذه الباقة، لذا لا يمكن تغيير عنوانها.",
    rotationWeek: "هذا الأسبوع جزء من تناوب. غيّر العنوان من صفحة التناوب.",
    unknownSlot: "إحدى هذه الوجبات ليست ضمن هذه الباقة.",
    failed: "تعذر تغيير العنوان. حاول مرة أخرى.",
    noAddresses: "أضف عنوانًا لتختاره هنا.",
  },
};

/** Per-meal address choice for a draft (checkout, rotation builder). */
export interface MealAddressState {
  split: boolean;
  /** The plan's address; in split mode, also the first meal's. */
  base: string;
  /** Split mode: the other meals' addresses. */
  perSlot: Record<string, string>;
}

/**
 * The address each meal of a draft goes to. `base` is the request's
 * address_id; `overrides` are the meals that go somewhere else.
 */
export function useMealAddresses(slots: string[]) {
  const [state, setState] = useState<MealAddressState>({
    split: false,
    base: "",
    perSlot: {},
  });
  const key = slots.join();
  // The first meal carries the base address. When the meals change and a
  // different meal comes first, it brings its address along and the others
  // keep theirs.
  useEffect(() => {
    setState((cur) => {
      const first = slots[0];
      if (!first || !cur.perSlot[first]) return cur;
      const perSlot: Record<string, string> = {};
      for (const s of slots.slice(1)) perSlot[s] = cur.perSlot[s] ?? cur.base;
      return { ...cur, base: cur.perSlot[first]!, perSlot };
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key]);

  return useMemo(() => {
    const of = (slot: string) =>
      state.split && slot !== slots[0]
        ? (state.perSlot[slot] ?? state.base)
        : state.base;
    const overrides: SlotAddress[] = state.split
      ? slots
          .slice(1)
          .filter((s) => of(s) && of(s) !== state.base)
          .map((s) => ({ meal_slot: s, address_id: of(s) }))
      : [];
    const distinct = Array.from(
      new Set([state.base, ...overrides.map((o) => o.address_id)].filter(Boolean)),
    );
    return {
      ...state,
      slots,
      of,
      overrides,
      distinct,
      setBase: (id: string) => setState((cur) => ({ ...cur, base: id })),
      setSplit: (split: boolean) =>
        setState((cur) => ({ ...cur, split, perSlot: split ? cur.perSlot : {} })),
      setSlot: (slot: string, id: string) =>
        setState((cur) =>
          slot === slots[0]
            ? { ...cur, base: id }
            : { ...cur, perSlot: { ...cur.perSlot, [slot]: id } },
        ),
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [state, key]);
}
export type MealAddresses = ReturnType<typeof useMealAddresses>;

function chunks<T>(list: T[], n: number): T[][] {
  const out: T[][] = [];
  for (let i = 0; i < list.length; i += n) out.push(list.slice(i, i + n));
  return out;
}

/**
 * Which of the caller's addresses each kitchen delivers to, from
 * POST /me/addresses/coverage (split into calls of at most 60 kitchens and 6
 * addresses). `delivers(b, a)` is undefined while unknown.
 */
export function useCoverage(branchIds: string[], addressIds: string[]) {
  const b = Array.from(new Set(branchIds.filter(Boolean))).sort();
  const a = Array.from(new Set(addressIds.filter(Boolean))).sort();
  const q = useQuery({
    queryKey: ["me", "address-coverage", b, a],
    enabled: b.length > 0 && a.length > 0,
    staleTime: 60_000,
    queryFn: async () => {
      const calls = chunks(b, 60).flatMap((bs) =>
        chunks(a, 6).map((as) =>
          addressCoverage({ branch_ids: bs, address_ids: as }),
        ),
      );
      const out = new Map<string, boolean>();
      for (const r of await Promise.all(calls))
        for (const it of r.items) out.set(`${it.branch_id}|${it.address_id}`, it.delivers);
      return out;
    },
  });
  const delivers = (branch: string, address: string): boolean | undefined =>
    q.data?.get(`${branch}|${address}`);
  return { ...q, delivers };
}

/** "Doesn't deliver here" next to an address the kitchen does not cover. */
export function NotHere({ locale }: { locale: Locale }) {
  return <span className="ms-addr-no">{COPY[locale].notHere}</span>;
}

/**
 * Where a draft's meals go: one address for every meal, or (with 2+ meals) a
 * picker per meal. `covers(addressId)` marks addresses the kitchen(s) don't
 * deliver to; `problems` holds a server refusal per meal ("" = the base).
 */
export function MealAddressPicker({
  locale,
  legend,
  addresses,
  m,
  covers,
  problems,
  coverageError,
  onRetryCoverage,
  noAddress,
  footer,
}: {
  locale: Locale;
  legend: string;
  addresses: Address[];
  m: MealAddresses;
  covers?: ((addressId: string) => boolean | undefined) | undefined;
  problems?: Record<string, string> | undefined;
  coverageError?: boolean | undefined;
  onRetryCoverage?: (() => void) | undefined;
  noAddress: string;
  footer?: ReactNode | undefined;
}) {
  const t = COPY[locale];
  const canSplit = m.slots.length >= 2 && addresses.length > 0;
  const split = m.split && canSplit;
  const problemFor = (slot: string) =>
    problems?.[slot] ?? (slot === m.slots[0] ? problems?.[""] : undefined);
  return (
    <fieldset className="ms-field">
      <legend>{legend}</legend>
      {addresses.length === 0 && <p className="ms-sub">{noAddress}</p>}
      {canSplit && (
        <div className="ms-chips" role="group" aria-label={legend}>
          <button
            type="button"
            className={!split ? "on" : ""}
            aria-pressed={!split}
            onClick={() => m.setSplit(false)}
          >
            {t.oneAddress}
          </button>
          <button
            type="button"
            className={split ? "on" : ""}
            aria-pressed={split}
            onClick={() => m.setSplit(true)}
          >
            {t.perMeal}
          </button>
        </div>
      )}
      {!split ? (
        <>
          <div className="ms-addresses">
            {addresses.map((a) => {
              const no = covers?.(a.id) === false;
              return (
                <label
                  key={a.id}
                  className={`ms-address${a.id === m.base ? " on" : ""}${no ? " ms-address-no" : ""}`}
                >
                  <input
                    type="radio"
                    name="address"
                    value={a.id}
                    checked={a.id === m.base}
                    onChange={() => m.setBase(a.id)}
                  />
                  <span>
                    <strong dir="auto">{a.label}</strong>
                    {no && <NotHere locale={locale} />}
                    <br />
                    <span className="ms-sub" dir="auto">
                      {describeAddress(a)}
                    </span>
                  </span>
                </label>
              );
            })}
          </div>
          {problems?.[""] && (
            <p className="ms-bad" role="alert">
              {problems[""]}
            </p>
          )}
        </>
      ) : (
        <div className="ms-meal-addrs">
          {m.slots.map((slot) => {
            const id = m.of(slot);
            const chosen = addresses.find((a) => a.id === id);
            const no = covers?.(id) === false;
            const problem = problemFor(slot);
            return (
              <label key={slot} className="ms-field ms-meal-addr">
                <span>{t.slotTo(slot)}</span>
                <select
                  value={id}
                  onChange={(e) => m.setSlot(slot, e.target.value)}
                  aria-invalid={no || !!problem || undefined}
                >
                  {!chosen && <option value="">{t.pick}</option>}
                  {addresses.map((a) => (
                    <option key={a.id} value={a.id}>
                      {covers?.(a.id) === false
                        ? `${a.label} (${t.notHereOption})`
                        : a.label}
                    </option>
                  ))}
                </select>
                {chosen && (
                  <small className="ms-sub" dir="auto">
                    {describeAddress(chosen)}
                  </small>
                )}
                {no && <NotHere locale={locale} />}
                {problem && (
                  <small className="ms-bad" role="alert">
                    {problem}
                  </small>
                )}
              </label>
            );
          })}
        </div>
      )}
      {coverageError && (
        <p className="ms-note" role="alert">
          {t.checkFailed}{" "}
          {onRetryCoverage && (
            <button type="button" className="ms-link" onClick={onRetryCoverage}>
              {t.retry}
            </button>
          )}
        </p>
      )}
      {footer}
    </fieldset>
  );
}

/** "Doesn't deliver to Office" for a kitchen card, or null if it covers them all. */
export function kitchenMiss(
  locale: Locale,
  branchId: string,
  addressIds: string[],
  delivers: (b: string, a: string) => boolean | undefined,
  labelOf: (addressId: string) => string,
): string | null {
  const miss = addressIds.find((a) => delivers(branchId, a) === false);
  return miss ? COPY[locale].notTo(labelOf(miss)) : null;
}

/** The sentence that blocks checkout when a chosen address isn't covered. */
export function blockedMessage(
  locale: Locale,
  kitchen: string | null,
  slot: string | null,
  label: string,
) {
  return COPY[locale].blocked(kitchen || COPY[locale].thisKitchen, slot, label);
}

const DELIVER_RE =
  /^(.+?) does not deliver(?: ([a-z]+))? to that address(?: \(week (\d+)\))?$/;

/**
 * The server names the kitchen, meal and week in English
 * ("Green Bowl does not deliver lunch to that address (week 2)"); this says
 * the same in the reader's language. `kitchenName` maps the English kitchen
 * name to the one shown on the page.
 */
export function outsideAreaMessage(
  message: string | undefined,
  locale: Locale,
  fallbackSlot: string | null,
  label: string | null,
  kitchenName?: (name: string) => string | null | undefined,
): string {
  const t = COPY[locale];
  const m = message ? DELIVER_RE.exec(message.trim()) : null;
  const raw = m?.[1];
  const kitchen =
    !raw || raw === "this kitchen"
      ? t.thisKitchen
      : (kitchenName?.(raw) ?? raw);
  const slot = m?.[2] ?? fallbackSlot;
  const week = m?.[3] ? t.week(Number(m[3])) : "";
  const where = label ?? (locale === "en" ? "that address" : "هذا العنوان");
  const s = t.blocked(`${kitchen}${week}`, slot, where);
  return s;
}

/**
 * Checkout's refusal of an address, keyed by meal ("" = the plan's address):
 * `schedule[i].address_id` is rule i of the request, `address_id` the default.
 */
export function checkoutAddressProblems(
  err: unknown,
  locale: Locale,
  ruleSlot: (index: number) => string | undefined,
  labelOf: (slot: string | null) => string | null,
  kitchen: string,
): Record<string, string> | null {
  if (!(err instanceof ApiError)) return null;
  const out: Record<string, string> = {};
  for (const e of err.problem?.errors ?? []) {
    const rule = /^schedule\[(\d+)\]\.address_id$/.exec(e.field);
    const slot = rule ? (ruleSlot(Number(rule[1])) ?? "") : e.field === "address_id" ? "" : null;
    if (slot == null) continue;
    out[slot] =
      e.code === "outside_delivery_area"
        ? blockedMessage(locale, kitchen, slot || null, labelOf(slot || null) ?? (locale === "en" ? "that address" : "هذا العنوان"))
        : COPY[locale].notFound;
  }
  return Object.keys(out).length ? out : null;
}

/**
 * A rotation create/preview/suggestion refusal about addresses, named by
 * kitchen and meal, or null for any other refusal.
 */
export function rotationAddressProblem(
  err: unknown,
  locale: Locale,
  labelOf: (slot: string | null) => string | null,
  kitchenName?: (name: string) => string | null | undefined,
): string | null {
  if (!(err instanceof ApiError)) return null;
  const e = (err.problem?.errors ?? []).find(
    (x) =>
      (x.field === "address_id" || x.field.startsWith("slot_addresses")) &&
      ["outside_delivery_area", "not_found", "unknown_slot"].includes(x.code),
  );
  if (!e) return null;
  if (e.code === "not_found") return COPY[locale].notFound;
  if (e.code === "unknown_slot") return COPY[locale].unknownSlot;
  const slot = DELIVER_RE.exec(e.message ?? "")?.[2] ?? null;
  return outsideAreaMessage(e.message, locale, slot, labelOf(slot), kitchenName);
}

/** Why changing a plan's address failed, in the reader's language. */
export function addressChangeProblem(
  err: unknown,
  locale: Locale,
  labelOf: (slot: string | null) => string | null,
  kitchenName?: (name: string) => string | null | undefined,
): string {
  const t = COPY[locale];
  if (!(err instanceof ApiError)) return networkMessage(err, locale) ?? t.failed;
  const code = err.problem?.code;
  if (code === "address_not_found") return t.notFound;
  if (code === "company_lunch") return t.companyLunch;
  if (code === "not_changeable") return t.notChangeable;
  if (code === "rotation_week") return t.rotationWeek;
  const fe = err.problem?.errors?.find((e) => e.field === "addresses");
  if (fe?.code === "outside_delivery_area") {
    const slot = DELIVER_RE.exec(fe.message ?? "")?.[2] ?? null;
    return outsideAreaMessage(fe.message, locale, slot, labelOf(slot), kitchenName);
  }
  if (fe?.code === "unknown_slot") return t.unknownSlot;
  if (err.status >= 500) return t.failed;
  const generic = problemMessage(err, locale);
  return locale === "ar" && /[A-Za-z]{3,}/.test(generic) ? t.failed : generic;
}

/** Where each meal goes: one line when every meal shares an address. */
export function DeliveryAddressLines({
  locale,
  items,
}: {
  locale: Locale;
  items: DeliveryAddress[];
}) {
  const t = COPY[locale];
  if (items.length === 0) return null;
  const same = items.every((d) => d.address_id === items[0]!.address_id);
  const rows = same
    ? [{ key: "all", who: items.length > 1 ? t.allMeals : slotName(items[0]!.meal_slot, locale), d: items[0]! }]
    : items.map((d) => ({ key: d.meal_slot, who: slotName(d.meal_slot, locale), d }));
  return (
    <ul className="ms-deliver-to">
      {rows.map((r) => (
        <li key={r.key}>
          <span className="ms-sub">{r.who}</span>
          <span>
            <strong dir="auto">{r.d.label}</strong>
            {r.d.line && (
              <>
                {" · "}
                <span className="ms-sub" dir="auto">
                  {r.d.line}
                </span>
              </>
            )}
          </span>
        </li>
      ))}
    </ul>
  );
}

/**
 * The plan's delivery addresses and, when allowed, a "Change address" panel:
 * per meal (or for all meals) pick a saved address, save once, and read when
 * the move takes effect. The save is one request keyed per draft; the server
 * applies it all or nothing.
 */
export function DeliveryAddresses<R extends { changed: DeliveryAddress[] }>({
  locale,
  items,
  canChange,
  companyName,
  isCompany,
  kitchens,
  kitchenName,
  save,
  onSaved,
}: {
  locale: Locale;
  items: DeliveryAddress[];
  canChange: boolean;
  isCompany: boolean;
  companyName: string | null;
  /** Kitchens that will deliver from now on, for the "doesn't deliver here" hint. */
  kitchens: { branch_id: string; name: string }[];
  kitchenName?: ((name: string) => string | null | undefined) | undefined;
  save: (addresses: SlotAddress[], headers: Record<string, string>) => Promise<R>;
  onSaved: (out: R) => Promise<unknown> | void;
}) {
  const t = COPY[locale];
  const [open, setOpen] = useState(false);
  const [done, setDone] = useState<DeliveryAddress[] | null>(null);
  if (items.length === 0) return null;
  return (
    <section id="addresses" className="ms-addr-section">
      <h2>{t.deliverTo}</h2>
      <DeliveryAddressLines locale={locale} items={items} />
      {isCompany && <p className="ms-sub">{t.company(companyName)}</p>}
      {done && done.length > 0 && (
        <div className="ms-callout ms-ok-callout" role="status">
          {done.map((d) => (
            <p key={d.meal_slot} className="ms-ok">
              {t.moved(
                d.effective_from ? formatDay(locale, d.effective_from, { weekday: "short", day: "numeric", month: "short" }) : null,
                d.meal_slot,
                d.label,
              )}
            </p>
          ))}
          <p className="ms-sub">{t.prepared}</p>
        </div>
      )}
      {canChange && !isCompany && !open && (
        <button
          type="button"
          className="ms-btn"
          onClick={() => {
            setDone(null);
            setOpen(true);
          }}
        >
          {t.change}
        </button>
      )}
      {canChange && !isCompany && open && (
        <ChangePanel
          locale={locale}
          items={items}
          kitchens={kitchens}
          kitchenName={kitchenName}
          save={save}
          onCancel={() => setOpen(false)}
          onSaved={async (out) => {
            setDone(out.changed);
            setOpen(false);
            await onSaved(out);
          }}
        />
      )}
    </section>
  );
}

function ChangePanel<R extends { changed: DeliveryAddress[] }>({
  locale,
  items,
  kitchens,
  kitchenName,
  save,
  onCancel,
  onSaved,
}: {
  locale: Locale;
  items: DeliveryAddress[];
  kitchens: { branch_id: string; name: string }[];
  kitchenName?: ((name: string) => string | null | undefined) | undefined;
  save: (addresses: SlotAddress[], headers: Record<string, string>) => Promise<R>;
  onCancel: () => void;
  onSaved: (out: R) => Promise<unknown>;
}) {
  const t = COPY[locale];
  const addresses = useQuery({ queryKey: ["me", "addresses"], queryFn: listAddresses });
  const current = useMemo(
    () => Object.fromEntries(items.map((d) => [d.meal_slot, d.address_id])),
    [items],
  );
  const slots = items.map((d) => d.meal_slot);
  const sameNow = items.every((d) => d.address_id === items[0]!.address_id);
  const [split, setSplit] = useState(!sameNow);
  const [all, setAll] = useState(items[0]!.address_id);
  const [per, setPer] = useState<Record<string, string>>(current);
  const draft: SlotAddress[] = slots
    .map((s) => ({ meal_slot: s, address_id: split ? per[s]! : all }))
    .filter((x) => x.address_id && x.address_id !== current[x.meal_slot]);
  const list = addresses.data?.items ?? [];
  const labelOf = (id: string) =>
    list.find((a) => a.id === id)?.label ?? items.find((d) => d.address_id === id)?.label ?? "";
  const chosen = Array.from(new Set(draft.map((d) => d.address_id)));
  const coverage = useCoverage(
    kitchens.map((k) => k.branch_id),
    list.map((a) => a.id),
  );
  const covers = (id: string) => {
    const known = kitchens.map((k) => coverage.delivers(k.branch_id, id));
    if (known.some((v) => v === false)) return false;
    return known.every((v) => v === true) ? true : undefined;
  };
  // A chosen address a kitchen is known not to cover blocks Save with the
  // kitchen named; the server still checks everything.
  let blocked: string | null = null;
  for (const d of draft) {
    const k = kitchens.find((k) => coverage.delivers(k.branch_id, d.address_id) === false);
    if (k) {
      blocked = blockedMessage(locale, k.name, items.length > 1 ? d.meal_slot : null, labelOf(d.address_id));
      break;
    }
  }

  const key = useIdempotencyKey();
  const draftKey = JSON.stringify(draft);
  const resetKey = key.reset;
  useEffect(() => resetKey(), [draftKey, resetKey]);
  const busy = useBusyGuard();
  const [error, setError] = useState<string | null>(null);
  const m = useMutation({
    mutationFn: () => save(draft, key.headers()),
    onSuccess: async (out) => {
      key.reset();
      await onSaved(out);
    },
    onError: (err) =>
      setError(
        addressChangeProblem(
          err,
          locale,
          (slot) => {
            const id = draft.find((d) => d.meal_slot === slot)?.address_id ?? chosen[0];
            return id ? labelOf(id) : null;
          },
          kitchenName,
        ),
      ),
    onSettled: busy.release,
  });

  if (addresses.isPending) return <p className="ms-note">…</p>;
  if (addresses.isError)
    return (
      <LoadError error={addresses.error} locale={locale} onRetry={() => void addresses.refetch()} />
    );

  const option = (a: Address) =>
    covers(a.id) === false ? `${a.label} (${t.notHereOption})` : a.label;
  const describe = (id: string) => {
    const a = list.find((x) => x.id === id);
    return a ? describeAddress(a) : (items.find((d) => d.address_id === id)?.line ?? "");
  };
  // An address deleted since it was chosen still shows as the current one.
  const extra = items
    .filter((d) => !list.some((a) => a.id === d.address_id))
    .filter((d, i, arr) => arr.findIndex((x) => x.address_id === d.address_id) === i);

  return (
    <form
      className="ms-callout ms-addr-change"
      onSubmit={(e) => {
        e.preventDefault();
        if (draft.length === 0 || blocked) return;
        busy.guard(() => {
          setError(null);
          m.mutate();
        })();
      }}
    >
      {slots.length >= 2 && (
        <div className="ms-chips" role="group" aria-label={t.change}>
          <button type="button" className={!split ? "on" : ""} aria-pressed={!split} onClick={() => setSplit(false)}>
            {t.sameAll}
          </button>
          <button type="button" className={split ? "on" : ""} aria-pressed={split} onClick={() => setSplit(true)}>
            {t.perMeal}
          </button>
        </div>
      )}
      <div className="ms-meal-addrs">
        {(split ? slots : [""]).map((slot) => {
          const value = slot ? per[slot]! : all;
          const no = covers(value) === false && (slot ? value !== current[slot] : slots.some((s) => current[s] !== value));
          return (
            <label key={slot || "all"} className="ms-field ms-meal-addr">
              <span>{slot ? t.slotTo(slot) : slots.length > 1 ? t.allMeals : t.slotTo(slots[0]!)}</span>
              <select
                value={value}
                onChange={(e) =>
                  slot
                    ? setPer((cur) => ({ ...cur, [slot]: e.target.value }))
                    : setAll(e.target.value)
                }
                aria-invalid={no || undefined}
              >
                {extra.map((d) => (
                  <option key={d.address_id} value={d.address_id}>
                    {d.label}
                  </option>
                ))}
                {list.map((a) => (
                  <option key={a.id} value={a.id}>
                    {option(a)}
                  </option>
                ))}
              </select>
              <small className="ms-sub" dir="auto">
                {describe(value)}
              </small>
              {no && <NotHere locale={locale} />}
            </label>
          );
        })}
      </div>
      {list.length === 0 && <p className="ms-sub">{t.noAddresses}</p>}
      <NewAddress
        t={ADDRESS_COPY[locale]}
        initial={null}
        onSaved={(a) => (split ? setPer((cur) => ({ ...cur, [slots[0]!]: a.id })) : setAll(a.id))}
      />
      {coverage.isError && (
        <p className="ms-note" role="alert">
          {t.checkFailed}{" "}
          <button type="button" className="ms-link" onClick={() => void coverage.refetch()}>
            {t.retry}
          </button>
        </p>
      )}
      {blocked && (
        <p className="ms-bad" role="alert">
          {blocked}
        </p>
      )}
      {error && !blocked && (
        <p className="ms-bad" role="alert">
          {error}
        </p>
      )}
      <div className="ms-row-actions">
        <button
          type="submit"
          className="ms-cta"
          disabled={draft.length === 0 || !!blocked || m.isPending}
        >
          {m.isPending ? t.saving : t.save}
        </button>
        <button type="button" className="ms-link" onClick={onCancel} disabled={m.isPending}>
          {t.cancel}
        </button>
      </div>
      {draft.length === 0 && <p className="ms-sub">{t.noChange}</p>}
    </form>
  );
}
