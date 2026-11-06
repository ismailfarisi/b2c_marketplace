"use client";

import { formatMoney } from "@marketplace/api-client";
import { useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { useState } from "react";

import type { Locale } from "@/components/meals/shell";
import type { CancellationReasonCode, CheaperPlans } from "@/lib/customer-api";
import { problemMessage } from "@/lib/problem";

// Boredom-aware cancellation. One question (why?), one honest save option that
// fits the answer, and the cancel button right there under it the whole time.
// No "are you sure" dialogs, no hidden buttons: a customer who wants out gets
// out in one tap.

export const CANCEL_REASONS: CancellationReasonCode[] = [
  "bored_of_food",
  "too_expensive",
  "food_quality",
  "delivery_problems",
  "moving_away",
  "other",
];

const COPY = {
  en: {
    why: "Why are you leaving?",
    reasons: {
      bored_of_food: "Bored of the food",
      too_expensive: "Too expensive",
      food_quality: "Food quality",
      delivery_problems: "Delivery problems",
      moving_away: "Moving / away",
      other: "Something else",
    } as Record<CancellationReasonCode, string>,
    note: "Anything else? (optional)",
    beforeYouGo: "Before you go",
    boredRotation: "Change next week's kitchen instead — your rotation can swap a week.",
    boredSingle: "Same kitchen every week gets old. Try someone new for a week, or rotate four kitchens.",
    guestWeek: "Try a guest kitchen next week",
    rotationBuilder: "Switch to a 4-kitchen rotation",
    changeWeek: "Change next week's kitchen",
    expensive: "Kitchens near you with a lower price per meal:",
    expensiveNone: "No cheaper kitchen covers your address right now.",
    perMeal: (m: string) => `${m} / meal`,
    pauseInstead: "Pause instead — your meals wait for you and the credit stays yours.",
    pause: "Pause my meals",
    loading: "Looking around…",
    retry: "Retry",
    cancelSubscription: "Cancel subscription",
    cancelRotation: "Cancel rotation",
    cancelling: "Cancelling…",
    pickFirst: "Pick a reason to continue.",
  },
  ar: {
    why: "لماذا ترغب في الإلغاء؟",
    reasons: {
      bored_of_food: "مللت من الأكل",
      too_expensive: "السعر مرتفع",
      food_quality: "جودة الطعام",
      delivery_problems: "مشاكل في التوصيل",
      moving_away: "انتقال / مسافر",
      other: "سبب آخر",
    } as Record<CancellationReasonCode, string>,
    note: "هل من شيء آخر؟ (اختياري)",
    beforeYouGo: "قبل أن تذهب",
    boredRotation: "غيّر مطبخ الأسبوع القادم بدلًا من ذلك — يمكن لتناوبك تبديل أسبوع.",
    boredSingle: "المطبخ نفسه كل أسبوع يصبح مملًا. جرّب مطبخًا جديدًا لأسبوع، أو تناوب بين أربعة مطابخ.",
    guestWeek: "جرّب مطبخًا ضيفًا الأسبوع القادم",
    rotationBuilder: "انتقل إلى تناوب ٤ مطابخ",
    changeWeek: "غيّر مطبخ الأسبوع القادم",
    expensive: "مطابخ قريبة منك بسعر أقل للوجبة:",
    expensiveNone: "لا يوجد مطبخ أرخص يغطي عنوانك حاليًا.",
    perMeal: (m: string) => `${m} / وجبة`,
    pauseInstead: "أوقف مؤقتًا بدلًا من ذلك — وجباتك تنتظرك ورصيدك يبقى لك.",
    pause: "إيقاف وجباتي مؤقتًا",
    loading: "نبحث حولك…",
    retry: "أعد المحاولة",
    cancelSubscription: "إلغاء الاشتراك",
    cancelRotation: "إلغاء التناوب",
    cancelling: "جارٍ الإلغاء…",
    pickFirst: "اختر سببًا للمتابعة.",
  },
};

export interface CancelSaves {
  /** Opens or scrolls to the existing pause controls. */
  pause?: (() => void) | undefined;
  /** Rotation detail, where another week's kitchen can be changed. */
  rotationHref?: string | undefined;
  /** The 4-kitchen rotation builder. */
  rotationBuilderHref?: string | undefined;
  /** Scrolls to the guest-week offer when one is on the table. */
  guestWeek?: (() => void) | undefined;
  /** Cheaper plans around the customer, for "too expensive". */
  cheaper?: (() => Promise<CheaperPlans>) | undefined;
  kitchenHref: (branchId: string) => string;
}

export function CancelFlow({
  kind,
  locale,
  pending,
  error,
  onCancel,
  saves,
}: {
  kind: "subscription" | "rotation";
  locale: Locale;
  pending: boolean;
  error: string | null;
  onCancel: (code: CancellationReasonCode, note: string) => void;
  saves: CancelSaves;
}) {
  const t = COPY[locale];
  const [code, setCode] = useState<CancellationReasonCode | null>(null);
  const [note, setNote] = useState("");
  const title = kind === "rotation" ? t.cancelRotation : t.cancelSubscription;

  return (
    <details className="ms-danger">
      <summary>{title}</summary>
      <p className="ms-sub">{t.why}</p>
      <div className="ms-chips ms-reasons" role="radiogroup" aria-label={t.why}>
        {CANCEL_REASONS.map((c) => (
          <button
            key={c}
            type="button"
            role="radio"
            aria-checked={code === c}
            className={code === c ? "on" : undefined}
            onClick={() => setCode(c)}
          >
            {t.reasons[c]}
          </button>
        ))}
      </div>
      {code && (
        <label className="ms-field">
          <span>{t.note}</span>
          <textarea
            value={note}
            onChange={(e) => setNote(e.target.value)}
            rows={2}
            maxLength={500}
            dir="auto"
          />
        </label>
      )}
      {code && <SaveOption code={code} kind={kind} locale={locale} saves={saves} />}
      {error && (
        <p className="ms-bad" role="alert">
          {error}
        </p>
      )}
      <button
        type="button"
        className="ms-cta ms-cta-danger"
        disabled={pending || !code}
        title={code ? undefined : t.pickFirst}
        onClick={() => code && onCancel(code, note.trim())}
      >
        {pending ? t.cancelling : title}
      </button>
    </details>
  );
}

function SaveOption({
  code,
  kind,
  locale,
  saves,
}: {
  code: CancellationReasonCode;
  kind: "subscription" | "rotation";
  locale: Locale;
  saves: CancelSaves;
}) {
  const t = COPY[locale];
  const pause = saves.pause ? (
    <>
      <p>{t.pauseInstead}</p>
      <button type="button" className="ms-cta ms-cta-quiet" onClick={saves.pause}>
        {t.pause}
      </button>
    </>
  ) : null;

  let body: React.ReactNode = pause;
  if (code === "bored_of_food") {
    body =
      kind === "rotation" ? (
        <>
          <p>{t.boredRotation}</p>
          {saves.rotationHref && (
            <Link className="ms-cta ms-cta-quiet" href={saves.rotationHref}>
              {t.changeWeek}
            </Link>
          )}
        </>
      ) : (
        <>
          <p>{t.boredSingle}</p>
          <div className="ms-actions">
            {saves.guestWeek && (
              <button type="button" className="ms-cta ms-cta-quiet" onClick={saves.guestWeek}>
                {t.guestWeek}
              </button>
            )}
            {saves.rotationBuilderHref && (
              <Link className="ms-cta ms-cta-quiet" href={saves.rotationBuilderHref}>
                {t.rotationBuilder}
              </Link>
            )}
          </div>
        </>
      );
  } else if (code === "too_expensive") {
    body = (
      <>
        {saves.cheaper && <Cheaper locale={locale} load={saves.cheaper} kitchenHref={saves.kitchenHref} />}
        {pause}
      </>
    );
  }
  if (!body) return null;
  return (
    <div className="ms-callout ms-save">
      <strong>{t.beforeYouGo}</strong>
      {body}
    </div>
  );
}

function Cheaper({
  locale,
  load,
  kitchenHref,
}: {
  locale: Locale;
  load: () => Promise<CheaperPlans>;
  kitchenHref: (branchId: string) => string;
}) {
  const t = COPY[locale];
  const q = useQuery({ queryKey: ["me", "cheaper-plans", load], queryFn: load });
  if (q.isLoading) return <p className="ms-sub">{t.loading}</p>;
  if (q.error) {
    return (
      <p className="ms-bad" role="alert">
        {problemMessage(q.error, locale)}{" "}
        <button type="button" className="ms-link" onClick={() => void q.refetch()}>
          {t.retry}
        </button>
      </p>
    );
  }
  const items = q.data?.items ?? [];
  if (items.length === 0) return <p>{t.expensiveNone}</p>;
  return (
    <>
      <p>{t.expensive}</p>
      <ul className="ms-list ms-cheaper">
        {items.map((p) => (
          <li key={p.plan_id}>
            <Link href={kitchenHref(p.branch_id)} dir="auto">
              {(locale === "ar" && p.kitchen_name_ar) || p.kitchen_name}
            </Link>{" "}
            <span className="ms-sub">
              {t.perMeal(formatMoney({ amount: p.per_meal_minor, currency: p.currency }, locale))}
            </span>
          </li>
        ))}
      </ul>
    </>
  );
}
