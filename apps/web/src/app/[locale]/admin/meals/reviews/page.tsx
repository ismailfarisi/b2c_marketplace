"use client";

import Link from "next/link";
import { useParams } from "next/navigation";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

import { Panel, plural } from "@/components/admin/detail";
import { getMealReviews, setMealReviewHidden, type AdminReview } from "@/lib/admin-api";

/** Every kitchen review. Hiding one removes it from the kitchen page and its rating. */
export default function MealReviewsPage() {
  const [status, setStatus] = useState("");
  const { data, error } = useQuery({
    queryKey: ["admin", "meal-reviews", status],
    queryFn: () => getMealReviews(status),
  });
  if (error) return <p className="danger">Reviews could not be loaded. Needs commerce.read.</p>;
  const rows = data?.items ?? [];
  return (
    <div className="detail">
      <div className="detail-head">
        <div>
          <h2>Kitchen reviews</h2>
          <p className="console-lede">Only customers with a completed delivery can review. Hidden reviews leave the rating.</p>
        </div>
        <div className="spacer" />
        <select className="field" value={status} onChange={(e) => setStatus(e.target.value)}>
          <option value="">All</option>
          <option value="published">Published</option>
          <option value="hidden">Hidden</option>
        </select>
      </div>
      <Panel title="Reviews" note={plural(rows.length, "review")}>
        {rows.length === 0 ? (
          <p className="muted">No reviews.</p>
        ) : (
          <table className="money-table">
            <thead>
              <tr>
                <th>When</th>
                <th>Kitchen</th>
                <th>Rating</th>
                <th>Comment</th>
                <th>State</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {rows.map((r) => (
                <Row key={r.id} r={r} />
              ))}
            </tbody>
          </table>
        )}
      </Panel>
    </div>
  );
}

function Row({ r }: { r: AdminReview }) {
  const qc = useQueryClient();
  const { locale } = useParams<{ locale: string }>();
  const act = useMutation({
    mutationFn: (v: { hidden: boolean; reason: string }) => setMealReviewHidden(r.id, v.hidden, v.reason),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["admin", "meal-reviews"] }),
  });
  return (
    <tr>
      <td className="faint">{new Date(r.created_at).toLocaleString()}</td>
      <td>
        <Link href={`/${locale}/admin/meals/kitchens/${r.branch_id}`}>{r.kitchen_name}</Link>
      </td>
      <td>{"★".repeat(r.rating)}</td>
      <td dir="auto">
        {r.comment || <span className="faint">—</span>}
        {r.reply && <div className="faint">Reply: {r.reply}</div>}
      </td>
      <td>
        {r.status}
        {r.hidden_reason ? <div className="faint">{r.hidden_reason}</div> : null}
      </td>
      <td>
        {r.status === "published" ? (
          <button
            className="btn danger"
            type="button"
            disabled={act.isPending}
            onClick={() => {
              const reason = window.prompt("Why hide this review?");
              if (reason?.trim()) act.mutate({ hidden: true, reason });
            }}
          >
            Hide
          </button>
        ) : (
          <button className="btn" type="button" disabled={act.isPending} onClick={() => act.mutate({ hidden: false, reason: "" })}>
            Restore
          </button>
        )}
      </td>
    </tr>
  );
}
