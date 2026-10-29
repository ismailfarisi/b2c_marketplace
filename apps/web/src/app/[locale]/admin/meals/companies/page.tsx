"use client";

import { ApiError, formatMoney } from "@marketplace/api-client";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

import { Panel } from "@/components/admin/detail";
import {
  getAdminCompany,
  getAdminCompanyStatement,
  getCompanies,
  issueCompanyStatement,
  markCompanyStatementPaid,
  reviewCompany,
} from "@/lib/admin-api";

const aed = (n: number, currency = "AED") => formatMoney({ amount: n, currency }, "en");
const today = () => new Date().toISOString().slice(0, 10);
const WD = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];
const problem = (err: unknown) =>
  err instanceof ApiError
    ? (err.problem?.errors?.[0]?.message ?? err.problem?.detail ?? "That could not be done.")
    : "That could not be done.";

/** Office lunch accounts: approve companies, set credit, bill and record payment. */
export default function CompaniesPage() {
  const [status, setStatus] = useState("");
  const [q, setQ] = useState("");
  const [open, setOpen] = useState<string | null>(null);
  const list = useQuery({
    queryKey: ["admin", "companies", status, q],
    queryFn: () => getCompanies(status, q),
  });
  const rows = list.data?.items ?? [];
  return (
    <div className="company-admin">
      <div className="merch-actions">
        <select value={status} onChange={(e) => setStatus(e.target.value)} aria-label="Status">
          <option value="">All statuses</option>
          <option value="pending">Pending approval</option>
          <option value="active">Active</option>
          <option value="suspended">Suspended</option>
          <option value="rejected">Rejected</option>
        </select>
        <input placeholder="Name or TRN" value={q} onChange={(e) => setQ(e.target.value)} />
      </div>
      {list.error && <p className="danger">{problem(list.error)}</p>}
      <table className="money-table">
        <thead>
          <tr>
            <th>Company</th><th>Status</th><th>Employees</th><th>Lunches</th>
            <th>Credit used / limit</th><th>Overdue</th>
          </tr>
        </thead>
        <tbody>
          {rows.length === 0 && (
            <tr><td colSpan={6} className="muted">No companies.</td></tr>
          )}
          {rows.map((c) => (
            <tr key={c.id} className={open === c.id ? "selected" : ""} onClick={() => setOpen(c.id)} style={{ cursor: "pointer" }}>
              <td><strong>{c.name}</strong><br /><span className="muted">{c.legal_name}</span></td>
              <td><span className="mono">{c.status}</span></td>
              <td>{c.members}</td>
              <td>{c.lunches}</td>
              <td>{aed(c.credit.unbilled_minor + c.credit.outstanding_minor, c.currency)} / {aed(c.credit_limit_minor, c.currency)}</td>
              <td>{c.credit.overdue_statements || "—"}</td>
            </tr>
          ))}
        </tbody>
      </table>
      {open && <Detail id={open} />}
    </div>
  );
}

function Detail({ id }: { id: string }) {
  const qc = useQueryClient();
  const d = useQuery({ queryKey: ["admin", "company", id], queryFn: () => getAdminCompany(id) });
  const [limit, setLimit] = useState("");
  const [terms, setTerms] = useState("");
  const [note, setNote] = useState("");
  const [periodEnd, setPeriodEnd] = useState(today());
  const [ref, setRef] = useState<Record<string, string>>({});
  const [openSt, setOpenSt] = useState<string | null>(null);
  const [msg, setMsg] = useState<string | null>(null);
  const st = useQuery({
    queryKey: ["admin", "company", id, "statement", openSt],
    queryFn: () => getAdminCompanyStatement(id, openSt!),
    enabled: openSt != null,
  });
  if (!d.data) return d.error ? <p className="danger">{problem(d.error)}</p> : null;
  const c = d.data.company;
  const money = (n: number) => aed(n, c.currency);
  const refresh = () => {
    qc.invalidateQueries({ queryKey: ["admin", "company", id] });
    qc.invalidateQueries({ queryKey: ["admin", "companies"] });
  };
  async function act(fn: () => Promise<unknown>, done: string) {
    setMsg(null);
    try {
      await fn();
      setMsg(done);
      refresh();
    } catch (err) {
      setMsg(problem(err));
    }
  }
  const review = (status?: "active" | "rejected" | "suspended") =>
    act(
      () =>
        reviewCompany(id, {
          ...(status ? { status } : {}),
          ...(limit !== "" ? { credit_limit_minor: Math.round(Number(limit) * 100) } : {}),
          ...(terms !== "" ? { payment_terms_days: Number(terms) } : {}),
          ...(note ? { note } : {}),
        }),
      status ? `Company ${status}.` : "Terms saved.",
    );
  return (
    <div className="company-admin">
      <Panel title={c.name} note={<span className="mono">{c.status}</span>}>
        <dl className="detail">
          <dt>Legal name</dt><dd>{c.legal_name}</dd>
          <dt>TRN</dt><dd>{c.trn || "—"}</dd>
          <dt>Billing</dt><dd>{c.billing_email} {c.billing_phone}</dd>
          <dt>Policy</dt><dd>{c.policy.weekdays.map((w) => WD[w]).join(", ")} · {c.policy.slots.join(", ")} · up to {money(c.policy.meal_cap_minor)} a meal</dd>
          <dt>Credit</dt><dd>{money(d.data.credit.available_minor)} available of {money(c.credit_limit_minor)} · unbilled {money(d.data.credit.unbilled_minor)} · awaiting payment {money(d.data.credit.outstanding_minor)}</dd>
          <dt>Terms</dt><dd>{c.payment_terms_days} days</dd>
          <dt>Offices</dt><dd>{d.data.offices.map((o) => `${o.label} (${o.city})`).join("; ")}</dd>
          {c.review_note && (<><dt>Review note</dt><dd>{c.review_note}</dd></>)}
        </dl>
      </Panel>
      <Panel title="Approval and credit">
        <div className="merch-actions">
          <label>Credit limit (AED) <input inputMode="decimal" value={limit} placeholder={String(c.credit_limit_minor / 100)} onChange={(e) => setLimit(e.target.value)} /></label>
          <label>Payment terms (days) <input inputMode="numeric" value={terms} placeholder={String(c.payment_terms_days)} onChange={(e) => setTerms(e.target.value)} /></label>
          <label>Note <input value={note} onChange={(e) => setNote(e.target.value)} /></label>
        </div>
        <div className="merch-actions">
          {c.status !== "active" && <button className="btn primary" onClick={() => review("active")}>Approve</button>}
          {c.status === "pending" && <button className="btn" onClick={() => review("rejected")}>Reject</button>}
          {c.status === "active" && <button className="btn" onClick={() => review("suspended")}>Suspend</button>}
          <button className="btn" onClick={() => review()}>Save terms</button>
        </div>
        {msg && <p className="muted" role="status">{msg}</p>}
      </Panel>
      <Panel title="Statements" note={`${(d.data.unbilled ?? []).length} unbilled lines`}>
        <div className="merch-actions">
          <label>Bill everything up to <input type="date" value={periodEnd} onChange={(e) => setPeriodEnd(e.target.value)} /></label>
          <button className="btn primary" onClick={() => act(() => issueCompanyStatement(id, periodEnd), "Statement issued.")}>Issue statement</button>
        </div>
        <table className="money-table">
          <thead><tr><th>No.</th><th>Period end</th><th>Total</th><th>Due</th><th>Status</th><th /></tr></thead>
          <tbody>
            {(d.data.statements ?? []).length === 0 && <tr><td colSpan={6} className="muted">No statements yet.</td></tr>}
            {(d.data.statements ?? []).map((s) => (
              <tr key={s.id}>
                <td><button className="btn link" onClick={() => setOpenSt(s.id)}>{s.statement_no}</button></td>
                <td>{s.period_end}</td>
                <td>{money(s.total_minor)}</td>
                <td>{s.due_on}</td>
                <td>{s.status === "paid" ? `paid · ${s.paid_reference}` : s.status}</td>
                <td>
                  {s.status === "issued" && (
                    <span className="merch-actions">
                      <input placeholder="Transfer reference" value={ref[s.id] ?? ""} onChange={(e) => setRef({ ...ref, [s.id]: e.target.value })} />
                      <button className="btn" onClick={() => act(() => markCompanyStatementPaid(id, s.id, ref[s.id] ?? ""), "Marked paid.")}>Mark paid</button>
                    </span>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        {st.data && (
          <table className="money-table">
            <thead><tr><th colSpan={4}>{st.data.statement_no} lines</th></tr></thead>
            <tbody>
              {(st.data.lines ?? []).map((l) => (
                <tr key={l.id}><td>{l.subscription_no}</td><td>{l.employee}</td><td>{l.kitchen}</td><td>{l.kind === "credit" ? "Credit " : ""}{money(l.amount_minor)}</td></tr>
              ))}
            </tbody>
          </table>
        )}
      </Panel>
      <Panel title="Employees and lunches" note={`${(d.data.members ?? []).length} employees · ${(d.data.lunches ?? []).length} active lunches`}>
        <table className="money-table">
          <thead><tr><th>Employee</th><th>Kitchen</th><th>Plan</th><th>Status</th><th>Starts</th></tr></thead>
          <tbody>
            {(d.data.lunches ?? []).map((l) => (
              <tr key={l.subscription_id}><td>{l.employee || l.phone}</td><td>{l.kitchen}</td><td>{l.plan}</td><td>{l.status}</td><td>{l.start_date}</td></tr>
            ))}
          </tbody>
        </table>
      </Panel>
    </div>
  );
}
