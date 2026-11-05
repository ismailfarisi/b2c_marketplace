"use client";

import { ApiError, formatMoney } from "@marketplace/api-client";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { use, useState, type FormEvent } from "react";

import { LoadError } from "@/components/load-error";
import { CustomerShell, asLocale, signInHref, type Locale } from "@/components/meals/shell";
import { companyFields, companyMessage } from "@/lib/company-problem";
import {
  addCompanyMember,
  addCompanyOffice,
  getCompany,
  getCompanyStatement,
  listMyCompanies,
  registerCompany,
  removeCompanyMember,
  removeCompanyOffice,
  setCompanyPolicy,
  type CompanyDashboard,
  type CompanyMember,
  type CompanyStatement,
} from "@/lib/customer-api";

import { COPY, WEEKDAYS, type Copy } from "./copy";
import { OfficeFields, emptyOffice, officeInput, type OfficeDraft } from "./office-fields";

function Loading({ t }: { t: Copy }) {
  return (
    <div className="ms-panel" role="status" aria-busy="true">
      <p className="ms-sub">{t.loading}</p>
    </div>
  );
}

export default function BusinessPage({ params }: { params: Promise<{ locale: string }> }) {
  const locale = asLocale(use(params).locale);
  const t = COPY[locale];
  const mine = useQuery({ queryKey: ["me", "companies"], queryFn: listMyCompanies, retry: false });
  const items = mine.data?.items ?? [];
  const signedOut = mine.error instanceof ApiError && mine.error.status === 401;
  return (
    <CustomerShell locale={locale}>
      <section className="ms-biz">
        <h1>{t.title}</h1>
        <p className="ms-sub">{t.intro}</p>
        {signedOut ? (
          <p>
            <Link className="ms-cta" href={signInHref(locale, `/${locale}/business`)}>
              {t.signin}
            </Link>
          </p>
        ) : mine.isPending ? (
          <Loading t={t} />
        ) : mine.isError ? (
          <LoadError error={mine.error} onRetry={() => mine.refetch()} locale={locale} />
        ) : items.length === 0 ? (
          <Register locale={locale} t={t} />
        ) : (
          items.map((c) =>
            c.role === "admin" ? (
              <Manage key={c.company_id} id={c.company_id} locale={locale} t={t} />
            ) : (
              <div key={c.company_id} className="ms-panel">
                <h2 dir="auto">{c.name}</h2>
                <p>{c.status === "active" ? t.employee : c.status === "pending" ? t.pending : t[c.status]}</p>
                <Link className="ms-cta" href={`/${locale}`}>
                  {t.browse}
                </Link>
              </div>
            ),
          )
        )}
      </section>
    </CustomerShell>
  );
}

function Register({ locale, t }: { locale: Locale; t: Copy }) {
  const qc = useQueryClient();
  const [f, setF] = useState({ name: "", legal_name: "", trn: "", billing_email: "", billing_phone: "" });
  const [office, setOffice] = useState<OfficeDraft>(emptyOffice);
  const [error, setError] = useState<string | null>(null);
  const [fields, setFields] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const field = (k: keyof typeof f, label: string, type = "text", required = false) => (
    <label className="ms-field">
      <span>{label}</span>
      <input
        type={type}
        dir={type === "email" || type === "tel" ? "ltr" : "auto"}
        value={f[k]}
        required={required}
        inputMode={k === "trn" ? "numeric" : undefined}
        aria-invalid={fields[k] ? true : undefined}
        onChange={(e) => setF({ ...f, [k]: e.target.value })}
      />
      {fields[k] && <small className="ms-bad">{fields[k]}</small>}
    </label>
  );
  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    if (busy) return;
    setBusy(true);
    setError(null);
    setFields({});
    try {
      await registerCompany({ ...f, office: officeInput(office) });
      await qc.invalidateQueries({ queryKey: ["me", "companies"] });
    } catch (err) {
      const byField = companyFields(err, locale);
      setFields(byField);
      setError(Object.keys(byField).length ? null : companyMessage(err, locale));
    } finally {
      setBusy(false);
    }
  }
  return (
    <form className="ms-panel ms-biz-form" onSubmit={onSubmit} lang={locale} noValidate>
      <h2>{t.register}</h2>
      <div className="ms-row">{field("name", t.name, "text", true)}{field("legal_name", t.legal, "text", true)}</div>
      <div className="ms-row">{field("trn", t.trn)}{field("billing_email", t.email, "email", true)}</div>
      {field("billing_phone", t.phone, "tel")}
      <OfficeFields value={office} onChange={setOffice} errors={fields} prefix="office." locale={locale} t={t} />
      {error && <p className="ms-bad" role="alert">{error}</p>}
      <button className="ms-cta" type="submit" disabled={busy}>{busy ? t.sending : t.submit}</button>
    </form>
  );
}

function Manage({ id, locale, t }: { id: string; locale: Locale; t: Copy }) {
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ["company", id], queryFn: () => getCompany(id) });
  const reload = () => qc.invalidateQueries({ queryKey: ["company", id] });
  if (q.isPending) return <Loading t={t} />;
  if (q.isError) return <LoadError error={q.error} onRetry={() => q.refetch()} locale={locale} />;
  const d = q.data;
  const money = (n: number) => formatMoney({ amount: n, currency: d.company.currency }, locale);
  const c = d.company;
  return (
    <div className="ms-biz-grid">
      <div className="ms-panel">
        <h2 dir="auto">{c.name}</h2>
        <p className="ms-sub" dir="auto">{c.legal_name}{c.trn ? ` · TRN ${c.trn}` : ""}</p>
        {c.status !== "active" && <p className="ms-offer on">{t[c.status]}</p>}
        {c.status === "active" && (
          <dl className="ms-lines">
            <dt>{t.available}</dt><dd><strong>{money(d.credit.available_minor)}</strong> / {money(d.credit.limit_minor)}</dd>
            <dt>{t.unbilled}</dt><dd>{money(d.credit.unbilled_minor)}</dd>
            <dt>{t.outstanding}</dt><dd>{money(d.credit.outstanding_minor)}</dd>
            <dt>{t.month}</dt><dd>{money(d.credit.month_spend_minor)}</dd>
          </dl>
        )}
      </div>
      <Policy d={d} locale={locale} t={t} onSaved={reload} />
      <People d={d} locale={locale} t={t} onChange={reload} />
      <Offices d={d} locale={locale} t={t} onChange={reload} />
      <div className="ms-panel">
        <h2>{t.lunches}</h2>
        {(d.lunches ?? []).length === 0 ? <p className="ms-sub">{t.none}</p> : (
          <ul className="ms-list">
            {(d.lunches ?? []).map((l) => (
              <li key={l.subscription_id}>
                <strong dir="auto">{l.employee || l.phone}</strong> · <span dir="auto">{l.kitchen}</span>
                <span className="ms-sub"> · <span dir="auto">{l.plan}</span> · {t.status[l.status] ?? l.status}</span>
                {l.renewal_unpaid && <p className="ms-bad">{t.renewalUnpaid}</p>}
              </li>
            ))}
          </ul>
        )}
      </div>
      <div className="ms-panel">
        <h2>{t.statements}</h2>
        {(d.statements ?? []).length === 0 ? <p className="ms-sub">{t.none}</p> : (
          <ul className="ms-list">
            {(d.statements ?? []).map((s) => (
              <StatementRow key={s.id} companyId={c.id} s={s} money={money} locale={locale} t={t} />
            ))}
          </ul>
        )}
      </div>
    </div>
  );
}

function StatementRow({
  companyId, s, money, locale, t,
}: {
  companyId: string; s: CompanyStatement; money: (n: number) => string; locale: Locale; t: Copy;
}) {
  const [open, setOpen] = useState(false);
  const detail = useQuery({
    queryKey: ["company", companyId, "statement", s.id],
    queryFn: () => getCompanyStatement(companyId, s.id),
    enabled: open,
  });
  return (
    <li>
      <strong dir="ltr">{s.statement_no}</strong> · {money(s.total_minor)}
      <span className="ms-sub"> · {s.status === "paid" ? t.paid : `${t.issued} · ${t.due} ${s.due_on}`}</span>{" "}
      <button type="button" className="ms-link" aria-expanded={open} onClick={() => setOpen(!open)}>
        {open ? t.hide : t.view}
      </button>
      {open && (
        <div className="ms-panel">
          {s.status === "paid" ? (
            <p className="ms-sub">{t.paidRef(s.paid_reference)}</p>
          ) : (
            <p>{t.transfer(s.statement_no, s.due_on)}</p>
          )}
          {detail.isPending ? (
            <p className="ms-sub" role="status">{t.loading}</p>
          ) : detail.isError ? (
            <LoadError error={detail.error} onRetry={() => detail.refetch()} locale={locale} />
          ) : (
            <>
              <h3>{t.lines}</h3>
              <ul className="ms-list">
                {(detail.data.lines ?? []).map((l) => (
                  <li key={l.id}>
                    <span dir="ltr">{l.subscription_no}</span> · <span dir="auto">{l.employee}</span> ·{" "}
                    <span dir="auto">{l.kitchen}</span>
                    {l.kind === "credit" ? ` · ${t.credit_line}` : ""} · <strong>{money(l.amount_minor)}</strong>
                  </li>
                ))}
              </ul>
            </>
          )}
        </div>
      )}
    </li>
  );
}

function Policy({ d, locale, t, onSaved }: { d: CompanyDashboard; locale: Locale; t: Copy; onSaved: () => void }) {
  const [days, setDays] = useState<number[]>(d.company.policy.weekdays);
  const [cap, setCap] = useState(String(d.company.policy.meal_cap_minor / 100));
  const [msg, setMsg] = useState<string | null>(null);
  const [fields, setFields] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  async function save() {
    if (busy) return;
    setMsg(null);
    const capValue = Number(cap);
    const local: Record<string, string> = {};
    if (cap.trim() === "" || !Number.isFinite(capValue) || capValue < 1) local.meal_cap_minor = t.capInvalid;
    if (days.length === 0) local.weekdays = t.daysRequired;
    setFields(local);
    if (Object.keys(local).length) return;
    setBusy(true);
    try {
      await setCompanyPolicy(d.company.id, {
        weekdays: [...days].sort(),
        slots: d.company.policy.slots,
        meal_cap_minor: Math.round(capValue * 100),
      });
      setMsg(t.saved);
      onSaved();
    } catch (err) {
      const byField = companyFields(err, locale);
      setFields(byField);
      setMsg(Object.keys(byField).length ? null : companyMessage(err, locale));
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="ms-panel">
      <h2>{t.policy}</h2>
      <fieldset className="ms-field" aria-invalid={fields.weekdays ? true : undefined}>
        <legend>{t.days}</legend>
        <div className="ms-chips">
          {WEEKDAYS[locale].map((w, i) => (
            <button key={w} type="button" className={days.includes(i) ? "on" : ""} aria-pressed={days.includes(i)}
              onClick={() => setDays(days.includes(i) ? days.filter((x) => x !== i) : [...days, i])}>
              {w}
            </button>
          ))}
        </div>
        {fields.weekdays && <small className="ms-bad">{fields.weekdays}</small>}
      </fieldset>
      <label className="ms-field">
        <span>{t.cap}</span>
        <input type="number" dir="ltr" min={1} step={0.5} inputMode="decimal" value={cap}
          aria-invalid={fields.meal_cap_minor ? true : undefined}
          onChange={(e) => setCap(e.target.value)} />
        {fields.meal_cap_minor && <small className="ms-bad">{fields.meal_cap_minor}</small>}
      </label>
      <button type="button" className="ms-cta" onClick={save} disabled={busy}>{busy ? t.saving : t.save}</button>
      {msg && <p className="ms-sub" role="status">{msg}</p>}
    </div>
  );
}

function People({ d, locale, t, onChange }: { d: CompanyDashboard; locale: Locale; t: Copy; onChange: () => void }) {
  const [phone, setPhone] = useState("");
  const [name, setName] = useState("");
  const [admin, setAdmin] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [fields, setFields] = useState<Record<string, string>>({});
  const [adding, setAdding] = useState(false);
  const [removing, setRemoving] = useState<string | null>(null);
  async function add(e: FormEvent) {
    e.preventDefault();
    if (adding) return;
    setAdding(true);
    setError(null);
    setFields({});
    try {
      await addCompanyMember(d.company.id, { phone, name, role: admin ? "admin" : "employee" });
      setPhone("");
      setName("");
      setAdmin(false);
      onChange();
    } catch (err) {
      const byField = companyFields(err, locale);
      setFields(byField);
      setError(Object.keys(byField).length ? null : companyMessage(err, locale));
    } finally {
      setAdding(false);
    }
  }
  async function remove(m: CompanyMember) {
    if (removing) return;
    if (!window.confirm(t.removeConfirm(m.name || m.phone, m.lunches))) return;
    setRemoving(m.id);
    setError(null);
    try {
      await removeCompanyMember(d.company.id, m.id);
      onChange();
    } catch (err) {
      setError(companyMessage(err, locale));
    } finally {
      setRemoving(null);
    }
  }
  return (
    <div className="ms-panel">
      <h2>{t.people}</h2>
      <ul className="ms-list">
        {(d.members ?? []).map((m) => (
          <li key={m.id} aria-busy={removing === m.id ? true : undefined}>
            <strong dir="auto">{m.name || m.phone}</strong>
            <span className="ms-sub" dir="ltr"> {m.name ? m.phone : ""}</span>
            <span className="ms-sub"> · {m.role === "admin" ? t.admin : m.joined ? t.activeCount(m.lunches) : t.invited}</span>{" "}
            <button type="button" className="ms-link" disabled={removing !== null} onClick={() => remove(m)}>
              {removing === m.id ? t.removing : t.remove}
            </button>
          </li>
        ))}
      </ul>
      <form onSubmit={add}>
        <h3>{t.invite}</h3>
        <div className="ms-row">
          <label className="ms-field">
            <span>{t.mobile}</span>
            <input type="tel" dir="ltr" required value={phone} placeholder="+971 5…"
              aria-invalid={fields.phone ? true : undefined} aria-describedby={`phone-hint-${d.company.id}`}
              onChange={(e) => setPhone(e.target.value)} />
            <small className="ms-sub" id={`phone-hint-${d.company.id}`}>{t.mobileHint}</small>
            {fields.phone && <small className="ms-bad">{fields.phone}</small>}
          </label>
          <label className="ms-field">
            <span>{t.empName}</span>
            <input dir="auto" value={name} aria-invalid={fields.name ? true : undefined} onChange={(e) => setName(e.target.value)} />
            {fields.name && <small className="ms-bad">{fields.name}</small>}
          </label>
        </div>
        <label><input type="checkbox" checked={admin} onChange={(e) => setAdmin(e.target.checked)} /> {t.admin}</label>
        {error && <p className="ms-bad" role="alert">{error}</p>}
        <button className="ms-cta" type="submit" disabled={adding}>{adding ? t.adding : t.add}</button>
      </form>
    </div>
  );
}

function Offices({ d, locale, t, onChange }: { d: CompanyDashboard; locale: Locale; t: Copy; onChange: () => void }) {
  const [draft, setDraft] = useState<OfficeDraft>(emptyOffice);
  const [fields, setFields] = useState<Record<string, string>>({});
  const [error, setError] = useState<string | null>(null);
  const [adding, setAdding] = useState(false);
  const [removing, setRemoving] = useState<string | null>(null);
  async function add(e: FormEvent) {
    e.preventDefault();
    if (adding) return;
    setAdding(true);
    setError(null);
    setFields({});
    try {
      await addCompanyOffice(d.company.id, officeInput(draft));
      setDraft(emptyOffice);
      onChange();
    } catch (err) {
      const byField = companyFields(err, locale);
      setFields(byField);
      setError(Object.keys(byField).length ? null : companyMessage(err, locale));
    } finally {
      setAdding(false);
    }
  }
  async function remove(officeId: string) {
    if (removing) return;
    setRemoving(officeId);
    setError(null);
    try {
      await removeCompanyOffice(d.company.id, officeId);
      onChange();
    } catch (err) {
      setError(companyMessage(err, locale));
    } finally {
      setRemoving(null);
    }
  }
  return (
    <div className="ms-panel">
      <h2>{t.offices}</h2>
      <ul className="ms-list">
        {d.offices.map((o) => (
          <li key={o.id} aria-busy={removing === o.id ? true : undefined}>
            <strong dir="auto">{o.label}</strong>
            <span className="ms-sub" dir="auto"> · {[o.building, o.area, o.city].filter(Boolean).join(", ")}</span>{" "}
            {d.offices.length > 1 && (
              <button type="button" className="ms-link" disabled={removing !== null} onClick={() => remove(o.id)}>
                {removing === o.id ? t.removing : t.remove}
              </button>
            )}
          </li>
        ))}
      </ul>
      <form onSubmit={add}>
        <h3>{t.addOffice}</h3>
        <OfficeFields value={draft} onChange={setDraft} errors={fields} prefix="office." locale={locale} t={t} />
        {error && <p className="ms-bad" role="alert">{error}</p>}
        <button className="ms-cta" type="submit" disabled={adding}>{adding ? t.adding : t.add}</button>
      </form>
    </div>
  );
}
