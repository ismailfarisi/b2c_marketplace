"use client";

import { formatMoney } from "@marketplace/api-client";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";

import { Panel } from "@/components/admin/detail";
import {
  getReferralProgram,
  getReferrals,
  saveReferralProgram,
  voidReferral,
  type AdminReferral,
  type ReferralProgram,
} from "@/lib/admin-api";
import { parseMajor } from "@/lib/vendor-api";

const STATUSES = ["", "pending", "rewarded", "rejected", "void"] as const;

function major(minor: number) {
  return (minor / 100).toFixed(2);
}

function ProgramForm({ program }: { program: ReferralProgram }) {
  const qc = useQueryClient();
  const [active, setActive] = useState(program.is_active);
  const [friend, setFriend] = useState(major(program.friend_amount_minor));
  const [reward, setReward] = useState(major(program.reward_amount_minor));
  const [cap, setCap] = useState(String(program.monthly_cap));
  const [days, setDays] = useState(String(program.reward_valid_days));
  const [error, setError] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);

  const save = useMutation({
    mutationFn: () => {
      const f = parseMajor(friend, program.currency);
      const r = parseMajor(reward, program.currency);
      if (f == null || r == null) throw new Error("Enter amounts like 50 or 50.00.");
      return saveReferralProgram({
        is_active: active,
        friend_amount_minor: f,
        reward_amount_minor: r,
        currency: program.currency,
        monthly_cap: Number(cap),
        reward_valid_days: Number(days),
      });
    },
    onSuccess: (p) => {
      setError(null);
      setSaved(true);
      qc.setQueryData(["admin", "referrals", "program"], p);
    },
    onError: (e: unknown) => {
      setSaved(false);
      const prob = (e as { problem?: { errors?: { message: string }[]; detail?: string } }).problem;
      setError(prob?.errors?.[0]?.message ?? prob?.detail ?? (e as Error).message);
    },
  });

  return (
    <Panel
      title="Programme"
      note="Both discounts come out of platform commission. Kitchens are paid in full."
    >
      <form
        className="merch-form"
        onSubmit={(e) => {
          e.preventDefault();
          save.mutate();
        }}
      >
        <label className="field merch-check">
          <input type="checkbox" checked={active} onChange={(e) => setActive(e.target.checked)} />
          <span>Invites are running</span>
        </label>
        <label className="field">
          <span>Friend&apos;s discount on first plan ({program.currency})</span>
          <input inputMode="decimal" value={friend} onChange={(e) => setFriend(e.target.value)} />
        </label>
        <label className="field">
          <span>Referrer&apos;s voucher ({program.currency})</span>
          <input inputMode="decimal" value={reward} onChange={(e) => setReward(e.target.value)} />
        </label>
        <label className="field">
          <span>Vouchers per referrer per month</span>
          <input type="number" min={1} max={100} value={cap} onChange={(e) => setCap(e.target.value)} />
        </label>
        <label className="field">
          <span>Voucher valid for (days)</span>
          <input type="number" min={7} max={365} value={days} onChange={(e) => setDays(e.target.value)} />
        </label>
        <div className="merch-actions">
          <button className="btn primary" type="submit" disabled={save.isPending}>
            Save programme
          </button>
          {saved && !save.isPending && <span className="muted">Saved.</span>}
          {error && <span className="danger">{error}</span>}
        </div>
      </form>
    </Panel>
  );
}

function VoidButton({ r }: { r: AdminReferral }) {
  const qc = useQueryClient();
  const [open, setOpen] = useState(false);
  const [reason, setReason] = useState("");
  const [error, setError] = useState<string | null>(null);
  const m = useMutation({
    mutationFn: () => voidReferral(r.id, reason),
    onSuccess: () => {
      setOpen(false);
      qc.invalidateQueries({ queryKey: ["admin", "referrals", "list"] });
    },
    onError: (e: unknown) => {
      const prob = (e as { problem?: { errors?: { message: string }[]; detail?: string } }).problem;
      setError(prob?.errors?.[0]?.message ?? prob?.detail ?? "Could not void.");
    },
  });
  if (r.status !== "pending" && r.status !== "rewarded") return null;
  if (!open)
    return (
      <button className="btn" onClick={() => setOpen(true)}>
        Void
      </button>
    );
  return (
    <span className="referral-void">
      <input
        aria-label="Reason"
        placeholder="Reason"
        value={reason}
        onChange={(e) => setReason(e.target.value)}
      />
      <button className="btn danger" disabled={m.isPending} onClick={() => m.mutate()}>
        Confirm void
      </button>
      <button className="btn" onClick={() => setOpen(false)}>
        Cancel
      </button>
      {error && <span className="danger">{error}</span>}
    </span>
  );
}

export default function ReferralsAdminPage() {
  const program = useQuery({
    queryKey: ["admin", "referrals", "program"],
    queryFn: getReferralProgram,
  });
  const [status, setStatus] = useState("");
  const [qDraft, setQDraft] = useState("");
  const [q, setQ] = useState("");
  useEffect(() => {
    const id = setTimeout(() => setQ(qDraft.trim()), 300);
    return () => clearTimeout(id);
  }, [qDraft]);
  const list = useQuery({
    queryKey: ["admin", "referrals", "list", status, q],
    queryFn: () => getReferrals(status, q),
  });
  const currency = program.data?.currency ?? "AED";
  const items = list.data?.items ?? [];

  return (
    <div className="referrals-admin">
      {program.data ? (
        <ProgramForm key={program.data.updated_at} program={program.data} />
      ) : program.isError ? (
        <p className="danger">Could not load the programme.</p>
      ) : (
        <p className="muted">Loading…</p>
      )}

      <Panel
        title="Referrals"
        note="A voucher is issued when the friend's first meal is delivered. Voiding stops an unused voucher."
      >
        <div className="merch-actions">
          <select aria-label="Status" value={status} onChange={(e) => setStatus(e.target.value)}>
            {STATUSES.map((s) => (
              <option key={s} value={s}>
                {s || "All statuses"}
              </option>
            ))}
          </select>
          <input
            aria-label="Search"
            placeholder="Phone, invite code or voucher"
            value={qDraft}
            onChange={(e) => setQDraft(e.target.value)}
          />
        </div>
        <div style={{ overflowX: "auto" }}>
          <table className="money-table">
            <thead>
              <tr>
                <th>Referrer</th>
                <th>Friend</th>
                <th>Status</th>
                <th>Voucher</th>
                <th>Invited</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {list.isPending ? (
                <tr>
                  <td colSpan={6} className="muted">Loading…</td>
                </tr>
              ) : items.length === 0 ? (
                <tr>
                  <td colSpan={6} className="muted">No referrals.</td>
                </tr>
              ) : (
                items.map((r) => (
                  <tr key={r.id}>
                    <td>
                      {r.referrer_name ?? "—"}
                      <div className="muted">{r.referrer_phone}</div>
                    </td>
                    <td>
                      {r.friend_name ?? "—"}
                      <div className="muted">{r.friend_phone}</div>
                    </td>
                    <td>
                      <strong>{r.status}</strong>
                      {r.reason && <div className="muted">{r.reason}</div>}
                    </td>
                    <td>
                      {r.voucher_code ? (
                        <>
                          <code>{r.voucher_code}</code>
                          <div className="muted">
                            {r.voucher_used ? "used" : "not used"} ·{" "}
                            {formatMoney(
                              { amount: program.data?.reward_amount_minor ?? 0, currency },
                              "en",
                            )}
                          </div>
                        </>
                      ) : (
                        "—"
                      )}
                    </td>
                    <td>{new Date(r.created_at).toLocaleDateString("en-GB")}</td>
                    <td>
                      <VoidButton r={r} />
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      </Panel>
    </div>
  );
}
