"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

import { Panel } from "@/components/admin/detail";
import { errorText, useBranch } from "@/components/kitchen/portal";
import { listBranchReviews, replyToReview, type BranchReview } from "@/lib/vendor-api";

/** Ratings from subscribers. Owners and managers can post one public reply per review. */
export default function ReviewsPage() {
  const branch = useBranch();
  const { data, error, isPending } = useQuery({
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
      title="Reviews"
      note={avg ? `${avg} / 5 from ${published.length} published` : "Only customers who have had a delivery can rate you."}
    >
      {error ? (
        <div className="state error">{errorText(error)}</div>
      ) : isPending ? (
        <div className="state">Loading…</div>
      ) : items.length === 0 ? (
        <div className="state">No reviews yet.</div>
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
  const qc = useQueryClient();
  const [reply, setReply] = useState(r.reply ?? "");
  const save = useMutation({
    mutationFn: () => replyToReview(branchId, r.id, reply),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["vendor", "reviews", branchId] }),
  });
  return (
    <li>
      <div>
        <strong>{"★".repeat(r.rating)}</strong>{" "}
        <span className="muted">{new Date(r.created_at).toLocaleDateString()}</span>
        {r.status === "hidden" && (
          <span className="danger"> · hidden by staff{r.hidden_reason ? `: ${r.hidden_reason}` : ""}</span>
        )}
      </div>
      {r.comment && <p dir="auto">{r.comment}</p>}
      <form
        className="row"
        onSubmit={(e) => {
          e.preventDefault();
          save.mutate();
        }}
      >
        <input
          className="field"
          dir="auto"
          placeholder="Public reply (optional)"
          maxLength={1000}
          value={reply}
          onChange={(e) => setReply(e.target.value)}
        />
        <button className="btn" type="submit" disabled={save.isPending}>
          {save.isPending ? "Saving…" : r.reply ? "Update reply" : "Reply"}
        </button>
      </form>
      {save.error && <div className="state error">{errorText(save.error)}</div>}
    </li>
  );
}
