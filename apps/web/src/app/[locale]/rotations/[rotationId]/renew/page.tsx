"use client";

import { ApiError, formatMoney } from "@marketplace/api-client";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { use, useEffect, useState } from "react";

import { LoadError } from "@/components/load-error";
import { CustomerShell, asLocale, type Locale } from "@/components/meals/shell";
import { tagLabel } from "@/components/meals/kitchen-card";
import {
  getRenewalProposal,
  payRotation,
  renewRotation,
  setMealPreferences,
  type Adventure,
  type ProposalKitchen,
  type RenewalProposal,
} from "@/lib/customer-api";
import { rotationProblem } from "@/lib/rotation-problem";
import { useBusyGuard } from "@/lib/use-busy-guard";
import { useIdempotencyKey } from "@/lib/use-idempotency-key";

const COPY = {
  en: {
    back: "Rotation",
    title: "Renew with fresh kitchens",
    lede: "Your next 4 weeks. We kept what you love and found new kitchens that deliver to you on the same days and meals.",
    starts: (d: string) => `Starts ${d}`,
    week: (n: number) => `Kitchen ${n}`,
    orderNote:
      "Which kitchen cooks which week is set at checkout, by each kitchen's load. The kitchen that cooks your last week never starts the next rotation.",
    tooFew:
      "Not enough kitchens deliver to you right now for a full rotation. Try the rotation builder, or come back later.",
    builder: "Build a rotation",
    perWeek: "/ week",
    perMeal: (m: string) => `${m} per meal`,
    swap: "Swap",
    keep: "Keep",
    cancelSwap: "Done",
    pickFor: (n: number) => `Pick kitchen ${n}`,
    inRotation: "In your current rotation",
    notAgain: "Not again",
    reasons: {
      loved: "You loved it",
      top_rated: "Your top rated",
      kept: "Kept from this rotation",
      new_kitchen: "New kitchen",
      new_cuisine: (c: string) => `New: ${c} home cooking`,
      familiar_cuisine: (c: string) => `More ${c}, a new kitchen`,
    },
    adventure: "How adventurous?",
    adventures: {
      familiar: "Familiar",
      balanced: "Balanced",
      surprise: "Surprise me",
    } as Record<Adventure, string>,
    adventureNote: {
      familiar: "New kitchens in cuisines you already eat.",
      balanced: "A mix of cuisines you know and new ones.",
      surprise: "Cuisines you haven't had in the last two rotations.",
    } as Record<Adventure, string>,
    total: "Total for 28 days",
    was: (m: string) => `Last rotation: ${m}`,
    pay: (m: string) => `Confirm and pay ${m}`,
    paying: "Processing…",
    renewFailed: "Could not renew. Try again.",
    adventureFailed: "Could not change the setting. Try again.",
    loading: "Finding kitchens…",
    notFound: "Rotation not found.",
    failed: "Could not load the proposal.",
    gone: "This rotation has already been renewed.",
    viewNext: "View next rotation",
    sameFour: "Same four kitchens",
    fewer: "Not enough kitchens deliver here for four different ones; some weeks repeat a kitchen from this rotation.",
  },
  ar: {
    back: "التناوب",
    title: "جدّد بمطابخ جديدة",
    lede: "أسابيعك الأربعة التالية. أبقينا ما تحبه ووجدنا مطابخ جديدة توصّل إليك في نفس الأيام والوجبات.",
    starts: (d: string) => `يبدأ ${d}`,
    week: (n: number) => `المطبخ ${n}`,
    orderNote:
      "يتحدد أي مطبخ يطبخ أي أسبوع عند الدفع بحسب حمل كل مطبخ. المطبخ الذي يطبخ أسبوعك الأخير لا يبدأ التناوب التالي أبدًا.",
    tooFew:
      "لا توجد مطابخ كافية توصّل إليك الآن لتناوب كامل. جرّب منشئ التناوب أو عُد لاحقًا.",
    builder: "أنشئ تناوبًا",
    perWeek: "/ أسبوع",
    perMeal: (m: string) => `${m} للوجبة`,
    swap: "بدّل",
    keep: "أبقِ",
    cancelSwap: "تم",
    pickFor: (n: number) => `اختر المطبخ ${n}`,
    inRotation: "في تناوبك الحالي",
    notAgain: "ليس مرة أخرى",
    reasons: {
      loved: "أحببته",
      top_rated: "أعلى تقييم منك",
      kept: "من تناوبك الحالي",
      new_kitchen: "مطبخ جديد",
      new_cuisine: (c: string) => `جديد: طبخ ${c} بيتي`,
      familiar_cuisine: (c: string) => `مزيد من ${c}، مطبخ جديد`,
    },
    adventure: "كم تحب المغامرة؟",
    adventures: {
      familiar: "مألوف",
      balanced: "متوازن",
      surprise: "فاجئني",
    } as Record<Adventure, string>,
    adventureNote: {
      familiar: "مطابخ جديدة من مطابخ تأكلها أصلًا.",
      balanced: "مزيج من مطابخ تعرفها وأخرى جديدة.",
      surprise: "مطابخ لم تتذوقها في آخر تناوبين.",
    } as Record<Adventure, string>,
    total: "المجموع لـ28 يومًا",
    was: (m: string) => `التناوب السابق: ${m}`,
    pay: (m: string) => `أكّد وادفع ${m}`,
    paying: "جارٍ المعالجة…",
    renewFailed: "تعذر التجديد. حاول مرة أخرى.",
    adventureFailed: "تعذر تغيير الإعداد. حاول مرة أخرى.",
    loading: "نبحث عن المطابخ…",
    notFound: "التناوب غير موجود.",
    failed: "تعذر تحميل الاقتراح.",
    gone: "تم تجديد هذا التناوب من قبل.",
    viewNext: "عرض التناوب التالي",
    sameFour: "نفس المطابخ الأربعة",
    fewer: "لا توجد مطابخ كافية هنا لأربعة مطابخ مختلفة؛ تتكرر بعض الأسابيع بمطبخ من هذا التناوب.",
  },
};
type Copy = (typeof COPY)["en"];

const ADVENTURES: Adventure[] = ["familiar", "balanced", "surprise"];

export default function RenewRotationPage({
  params,
}: {
  params: Promise<{ locale: string; rotationId: string }>;
}) {
  const p = use(params);
  const locale = asLocale(p.locale);
  const t = COPY[locale];
  const q = useQuery({
    queryKey: ["me", "rotation", p.rotationId, "renewal-proposal"],
    queryFn: () => getRenewalProposal(p.rotationId),
  });
  return (
    <CustomerShell
      locale={locale}
      back={{ href: `/${locale}/rotations/${p.rotationId}`, label: t.back }}
    >
      <h1>{t.title}</h1>
      {q.isPending ? (
        <p className="ms-note">{t.loading}</p>
      ) : q.isError ? (
        q.error instanceof ApiError && q.error.status === 404 ? (
          <p className="ms-empty">{t.notFound}</p>
        ) : q.error instanceof ApiError && q.error.status === 409 ? (
          <p className="ms-empty">
            {t.gone}{" "}
            <Link href={`/${locale}/rotations/${p.rotationId}`}>→ {t.back}</Link>
          </p>
        ) : (
          <LoadError error={q.error} onRetry={() => void q.refetch()} locale={locale} />
        )
      ) : (
        <Proposal
          rotationId={p.rotationId}
          proposal={q.data}
          locale={locale}
          t={t}
          onReload={() => q.refetch()}
        />
      )}
    </CustomerShell>
  );
}

function reasonLabel(k: ProposalKitchen, locale: Locale, t: Copy) {
  const cuisine = k.reason_cuisine ? tagLabel(k.reason_cuisine, locale) : "";
  switch (k.reason) {
    case "loved":
      return t.reasons.loved;
    case "top_rated":
      return t.reasons.top_rated;
    case "kept":
      return t.reasons.kept;
    case "new_cuisine":
      return cuisine ? t.reasons.new_cuisine(cuisine) : t.reasons.new_kitchen;
    case "familiar_cuisine":
      return cuisine ? t.reasons.familiar_cuisine(cuisine) : t.reasons.new_kitchen;
    case "new_kitchen":
      return t.reasons.new_kitchen;
    default:
      return k.in_current ? t.reasons.kept : t.reasons.new_kitchen;
  }
}

function Proposal({
  rotationId,
  proposal,
  locale,
  t,
  onReload,
}: {
  rotationId: string;
  proposal: RenewalProposal;
  locale: Locale;
  t: Copy;
  onReload: () => Promise<unknown>;
}) {
  const router = useRouter();
  const qc = useQueryClient();
  const money = (n: number) =>
    formatMoney({ amount: n, currency: proposal.currency }, locale);
  // The four slots as plan ids; the proposal is the starting point.
  const [picks, setPicks] = useState<string[]>(() =>
    proposal.kitchens.map((k) => k.plan_id),
  );
  const [swapping, setSwapping] = useState<number | null>(null);
  useEffect(() => {
    setPicks(proposal.kitchens.map((k) => k.plan_id));
    setSwapping(null);
  }, [proposal]);

  const byPlan = new Map<string, ProposalKitchen>();
  for (const k of proposal.eligible) byPlan.set(k.plan_id, k);
  for (const k of proposal.kitchens) byPlan.set(k.plan_id, k);
  const chosen = picks
    .map((id) => byPlan.get(id))
    .filter((k): k is ProposalKitchen => k != null);
  const total = chosen.reduce((sum, k) => sum + k.week_price_minor, 0);
  const complete = chosen.length === 4;
  const repeats = new Set(chosen.map((k) => k.branch_id)).size < chosen.length;

  const renewKey = useIdempotencyKey();
  const tap = useBusyGuard();
  const renew = useMutation({
    mutationFn: async () => {
      const next = await renewRotation(rotationId, {
        headers: renewKey.headers(),
        planIds: picks,
      });
      try {
        await payRotation(next.id);
      } catch {
        // The new rotation shows its own pay button.
      }
      return next;
    },
    onSuccess: async (next) => {
      renewKey.reset();
      await qc.invalidateQueries({ queryKey: ["me"] });
      router.push(`/${locale}/rotations/${next.id}`);
    },
    onSettled: tap.release,
  });
  const adventure = useMutation({
    mutationFn: (a: Adventure) => setMealPreferences({ adventure: a }),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ["me", "meal-preferences"] });
      await onReload();
    },
    onSettled: tap.release,
  });

  return (
    <div className="ms-renew-page">
      <p className="ms-sub" dir="auto">
        {t.lede} · {t.starts(formatDate(locale, proposal.start_date))}
      </p>

      <fieldset className="ms-adventure">
        <legend>{t.adventure}</legend>
        <div className="ms-chips" role="radiogroup" aria-label={t.adventure}>
          {ADVENTURES.map((a) => (
            <button
              key={a}
              type="button"
              role="radio"
              aria-checked={proposal.adventure === a}
              className={proposal.adventure === a ? "on" : undefined}
              disabled={adventure.isPending || renew.isPending}
              onClick={tap.guard(() => adventure.mutate(a))}
            >
              {t.adventures[a]}
            </button>
          ))}
        </div>
        <p className="ms-sub">{t.adventureNote[proposal.adventure]}</p>
        {adventure.isError && (
          <p className="ms-bad" role="alert">
            {rotationProblem(adventure.error, locale, t.adventureFailed)}
          </p>
        )}
      </fieldset>

      {proposal.kitchens.length < 4 && (
        <section className="ms-renew-total" role="status">
          <p className="ms-note">{t.tooFew}</p>
          <Link className="ms-cta ms-cta-quiet" href={`/${locale}/rotation`}>
            {t.builder}
          </Link>
        </section>
      )}

      <ol className="ms-rot-weeks ms-renew-weeks">
        {chosen.map((k, i) => (
          <li key={`${i}-${k.plan_id}`} className="ms-renew-slot">
            <span className="ms-week-no">{t.week(i + 1)}</span>
            <span className="ms-renew-kitchen">
              <strong dir="auto">
                {(locale === "ar" && k.kitchen_name_ar) || k.kitchen_name}
              </strong>
              <small className="ms-sub">
                {reasonLabel(k, locale, t)}
                {k.rating != null && ` · ★ ${k.rating.toFixed(1)}`}
              </small>
            </span>
            <span className="ms-renew-price">
              <strong>{money(k.week_price_minor)}</strong>
              <small className="ms-sub">
                {t.perWeek}
                {k.meal_price_minor > 0 && ` · ${t.perMeal(money(k.meal_price_minor))}`}
              </small>
            </span>
            <button
              type="button"
              className="ms-cta ms-cta-quiet"
              aria-expanded={swapping === i}
              disabled={renew.isPending}
              onClick={() => setSwapping(swapping === i ? null : i)}
            >
              {swapping === i ? t.cancelSwap : t.swap}
            </button>
          </li>
        ))}
      </ol>
      {repeats && <p className="ms-note">{t.fewer}</p>}
      <p className="ms-sub">{t.orderNote}</p>

      {swapping != null && (
        <section className="ms-renew-pick" aria-label={t.pickFor(swapping + 1)}>
          <h2>{t.pickFor(swapping + 1)}</h2>
          <ul className="ms-renew-eligible">
            {proposal.eligible.map((k) => {
              const taken = picks.includes(k.plan_id);
              return (
                <li key={k.plan_id}>
                  <button
                    type="button"
                    className={`ms-option${picks[swapping] === k.plan_id ? " on" : ""}`}
                    disabled={taken && picks[swapping] !== k.plan_id}
                    onClick={() => {
                      setPicks((cur) => cur.map((id, i) => (i === swapping ? k.plan_id : id)));
                      setSwapping(null);
                    }}
                  >
                    <span dir="auto">
                      <strong>{(locale === "ar" && k.kitchen_name_ar) || k.kitchen_name}</strong>
                      <small className="ms-sub">
                        {k.cuisines.map((c) => tagLabel(c, locale)).join(" · ")}
                        {k.rating != null && ` · ★ ${k.rating.toFixed(1)} (${k.rating_count})`}
                        {k.in_current && ` · ${t.inRotation}`}
                        {k.favourite && " · ♥"}
                        {k.avoided && ` · ⊘ ${t.notAgain}`}
                      </small>
                    </span>
                    <span>
                      {money(k.week_price_minor)} {t.perWeek}
                    </span>
                  </button>
                </li>
              );
            })}
          </ul>
        </section>
      )}

      <section className="ms-renew-total">
        <p>
          <span>{t.total}</span>
          <strong>{money(total)}</strong>
        </p>
        <p className="ms-sub">{t.was(money(proposal.previous_amount_minor))}</p>
        <button
          type="button"
          className="ms-cta"
          disabled={!complete || renew.isPending || adventure.isPending}
          onClick={tap.guard(() => renew.mutate())}
        >
          {renew.isPending ? t.paying : t.pay(money(total))}
        </button>
        {renew.isError && (
          <p className="ms-bad" role="alert">
            {rotationProblem(renew.error, locale, t.renewFailed)}
          </p>
        )}
      </section>
    </div>
  );
}

function formatDate(locale: Locale, iso: string) {
  const [y, m, d] = iso.split("-").map(Number);
  if (!y || !m || !d) return iso;
  return new Intl.DateTimeFormat(locale === "ar" ? "ar-AE" : "en-AE", {
    day: "numeric",
    month: "short",
  }).format(new Date(y, m - 1, d));
}
