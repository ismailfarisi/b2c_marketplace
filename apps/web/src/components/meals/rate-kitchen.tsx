"use client";

import { ApiError } from "@marketplace/api-client";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";

import { getMyReview, writeReview } from "@/lib/customer-api";

const COPY = {
  en: {
    title: "Rate this kitchen",
    after: "You can rate this kitchen after your first delivery.",
    stars: (n: number) => `${n} star${n === 1 ? "" : "s"}`,
    comment: "What did you think? (optional)",
    save: "Post review",
    update: "Update review",
    saved: "Thanks. Your review is on the kitchen page.",
    hidden: "This review was hidden by our team.",
    failed: "Could not save your review. Try again.",
  },
  ar: {
    title: "قيّم هذا المطبخ",
    after: "يمكنك تقييم المطبخ بعد أول توصيلة.",
    stars: (n: number) => `${n} نجوم`,
    comment: "ما رأيك؟ (اختياري)",
    save: "نشر التقييم",
    update: "تحديث التقييم",
    saved: "شكرًا. تقييمك منشور في صفحة المطبخ.",
    hidden: "أخفى فريقنا هذا التقييم.",
    failed: "تعذّر حفظ التقييم. حاول مرة أخرى.",
  },
};

/** One review per subscription, from a customer who has had a delivery. */
export function RateKitchen({
  subscriptionId,
  locale,
}: {
  subscriptionId: string;
  locale: "en" | "ar";
}) {
  const t = COPY[locale];
  const qc = useQueryClient();
  const q = useQuery({
    queryKey: ["me", "review", subscriptionId],
    queryFn: () => getMyReview(subscriptionId),
  });
  const [rating, setRating] = useState(0);
  const [comment, setComment] = useState("");
  const [done, setDone] = useState(false);
  useEffect(() => {
    if (q.data?.review) {
      setRating(q.data.review.rating);
      setComment(q.data.review.comment);
    }
  }, [q.data?.review]);
  const save = useMutation({
    mutationFn: () => writeReview(subscriptionId, { rating, comment }),
    onSuccess: async () => {
      setDone(true);
      await qc.invalidateQueries({
        queryKey: ["me", "review", subscriptionId],
      });
    },
  });
  if (!q.data) return null;
  if (!q.data.can_review)
    return (
      <section>
        <h2>{t.title}</h2>
        <p className="ms-sub">{t.after}</p>
      </section>
    );
  const err =
    save.error instanceof ApiError && save.error.problem?.errors?.[0]?.message
      ? save.error.problem.errors[0].message
      : save.error
        ? t.failed
        : null;
  return (
    <section>
      <h2>{t.title}</h2>
      {q.data.review?.status === "hidden" && (
        <p className="ms-bad">{t.hidden}</p>
      )}
      <form
        className="ms-form"
        onSubmit={(e) => {
          e.preventDefault();
          if (rating > 0) save.mutate();
        }}
      >
        <div className="ms-stars" role="radiogroup" aria-label={t.title}>
          {[1, 2, 3, 4, 5].map((n) => (
            <button
              key={n}
              type="button"
              role="radio"
              aria-checked={rating === n}
              aria-label={t.stars(n)}
              className={n <= rating ? "on" : ""}
              onClick={() => setRating(n)}
            >
              ★
            </button>
          ))}
        </div>
        <label className="ms-field">
          <span>{t.comment}</span>
          <textarea
            dir="auto"
            rows={3}
            maxLength={1000}
            value={comment}
            onChange={(e) => setComment(e.target.value)}
          />
        </label>
        {err && (
          <p className="ms-bad" role="alert">
            {err}
          </p>
        )}
        {done && !save.isPending && <p className="ms-ok">{t.saved}</p>}
        <button
          className="ms-cta"
          type="submit"
          disabled={rating === 0 || save.isPending}
        >
          {q.data.review ? t.update : t.save}
        </button>
      </form>
    </section>
  );
}
