"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

import { Panel } from "@/components/admin/detail";
import { ConsoleError, errorText, useBranch } from "@/components/kitchen/portal";
import { useIdempotencyKey } from "@/lib/use-idempotency-key";
import { listBranchReviews, replyToReview, type BranchReview } from "@/lib/vendor-api";

/** Ratings from subscribers. Owners and managers can post one public reply per review. */
export default function ReviewsPage() {
  const branch = useBranch();
  const { t, locale } = branch;
  const c = t.reviews;
  const { data, error, isPending, refetch } = useQuery({
    queryKey: ["vendor", "reviews", branch.branch_id],
    queryFn: () => listBranchReviews(branch.branch_id),
  });
  const items = data?.items ?? [];
  const published = items.filter((r) => r.status === "published");
  const avg =
    published.length > 0
      ? (published.reduce((n, r) => n + r.rating, 0) / published.length).toFixed(1)
      : null;

  return (
    <Panel
      title={c.title}
      note={avg ? c.average(avg, published.length) : c.onlyCustomers}
    >
      {error ? (
        <ConsoleError error={error} onRetry={() => void refetch()} locale={locale} />
      ) : isPending ? (
        <div className="state">{t.loading}</div>
      ) : items.length === 0 ? (
        <div className="state">{c.empty}</div>
      ) : (
        <ul className="merch-list">
          {items.map((r) => (
            <ReviewRow key={r.id} r={r} branchId={branch.branch_id} />
          ))}
        </ul>
      )}
    </Panel>
  );
}

function ReviewRow({ r, branchId }: { r: BranchReview; branchId: string }) {
  const { t, locale, editable } = useBranch();
  const c = t.reviews;
  const qc = useQueryClient();
  const [reply, setReply] = useState(r.reply ?? "");
  // Same key for repeated presses of the same reply; a new text is a new request.
  const idem = useIdempotencyKey();
  const save = useMutation({
    mutationFn: (text: string) => replyToReview(branchId, r.id, text, { headers: idem.headers() }),
    onSuccess: () => {
      idem.reset();
      qc.invalidateQueries({ queryKey: ["vendor", "reviews", branchId] });
    },
    onError: () => idem.reset(),
  });
  return (
    <li>
      <div>
        <strong>{"★".repeat(r.rating)}</strong>{" "}
        <span className="muted">{new Date(r.created_at).toLocaleDateString(locale === "ar" ? "ar" : "en")}</span>
        {r.status === "hidden" && <span className="danger">{c.hidden(r.hidden_reason)}</span>}
      </div>
      {r.comment && <p dir="auto">{r.comment}</p>}
      <form
        className="row"
        onSubmit={(e) => {
          e.preventDefault();
          if (save.isPending) return;
          save.mutate(reply);
        }}
      >
        <input
          className="field"
          dir="auto"
          placeholder={c.placeholder}
          maxLength={1000}
          value={reply}
          disabled={!editable}
          onChange={(e) => {
            idem.reset();
            setReply(e.target.value);
          }}
        />
        {editable ? (
          <button className="btn" type="submit" disabled={save.isPending}>
            {save.isPending ? t.saving : r.reply ? c.update : c.reply}
          </button>
        ) : null}
      </form>
      {save.error && <div className="state error">{errorText(save.error, locale)}</div>}
    </li>
  );
}
