"use client";

import { ApiError, formatMoney } from "@marketplace/api-client";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { use, useState, type FormEvent } from "react";

import { CustomerShell, asLocale, signInHref, type Locale } from "@/components/meals/shell";
import {
  addCompanyMember,
  getCompany,
  listMyCompanies,
  registerCompany,
  removeCompanyMember,
  setCompanyPolicy,
  type CompanyDashboard,
} from "@/lib/customer-api";

const WEEKDAYS = {
  en: ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"],
  ar: ["الأحد", "الإثنين", "الثلاثاء", "الأربعاء", "الخميس", "الجمعة", "السبت"],
};

const COPY = {
  en: {
    title: "Office lunches",
    intro:
      "Your company pays for staff lunches from home-style kitchens, delivered to the office. One monthly statement, paid by bank transfer.",
    signin: "Sign in to set up or manage your company's lunches.",
    register: "Set up a company account",
    name: "Company name",
    legal: "Legal name (as on the trade licence)",
    trn: "TRN (15 digits, optional)",
    email: "Billing email",
    phone: "Billing phone",
    office: "Office name",
    building: "Building",
    area: "Area",
    city: "City",
    lat: "Latitude",
    lng: "Longitude",
    submit: "Send for approval",
    pending: "Waiting for approval. We'll set your credit limit and switch it on.",
    suspended: "This account is suspended. New lunches are paused.",
    rejected: "This application was not approved.",
    employee: "Your company covers lunches. Pick a kitchen and choose \"Paid by\" your company at checkout.",
    browse: "Browse kitchens",
    credit: "Credit",
    available: "Available",
    unbilled: "Not yet billed",
    outstanding: "Awaiting payment",
    month: "Spent this month",
    policy: "Lunch policy",
    days: "Covered days",
    cap: "Cap per meal (AED)",
    save: "Save policy",
    saved: "Saved.",
    people: "Employees",
    invite: "Add employee",
    mobile: "Mobile number",
    empName: "Name",
    admin: "Lunch admin",
    add: "Add",
    remove: "Remove",
    invited: "Invited",
    lunches: "Active lunches",
    none: "Nothing yet.",
    statements: "Statements",
    due: "Due",
    paid: "Paid",
    issued: "Awaiting payment",
    failed: "Something went wrong. Try again.",
  },
  ar: {
    title: "غداء المكاتب",
    intro:
      "تدفع شركتك غداء الموظفين من مطابخ منزلية، ويصل إلى المكتب. كشف حساب شهري واحد يُدفع بتحويل بنكي.",
    signin: "سجّل الدخول لإعداد غداء شركتك أو إدارته.",
    register: "إنشاء حساب شركة",
    name: "اسم الشركة",
    legal: "الاسم القانوني (كما في الرخصة التجارية)",
    trn: "الرقم الضريبي (15 رقمًا، اختياري)",
    email: "بريد الفواتير",
    phone: "هاتف الفواتير",
    office: "اسم المكتب",
    building: "المبنى",
    area: "المنطقة",
    city: "المدينة",
    lat: "خط العرض",
    lng: "خط الطول",
    submit: "إرسال للموافقة",
    pending: "بانتظار الموافقة. سنحدد حد الائتمان ونفعّل الحساب.",
    suspended: "هذا الحساب موقوف. الطلبات الجديدة متوقفة.",
    rejected: "لم تتم الموافقة على هذا الطلب.",
    employee: "تغطي شركتك الغداء. اختر مطبخًا ثم اختر الدفع على حساب شركتك عند الدفع.",
    browse: "تصفح المطابخ",
    credit: "الائتمان",
    available: "المتاح",
    unbilled: "لم يُفوتر بعد",
    outstanding: "بانتظار الدفع",
    month: "مصروف هذا الشهر",
    policy: "سياسة الغداء",
    days: "الأيام المشمولة",
    cap: "الحد لكل وجبة (درهم)",
    save: "حفظ السياسة",
    saved: "تم الحفظ.",
    people: "الموظفون",
    invite: "إضافة موظف",
    mobile: "رقم الجوال",
    empName: "الاسم",
    admin: "مسؤول الغداء",
    add: "إضافة",
    remove: "إزالة",
    invited: "مدعو",
    lunches: "الوجبات النشطة",
    none: "لا شيء بعد.",
    statements: "كشوف الحساب",
    due: "الاستحقاق",
    paid: "مدفوع",
    issued: "بانتظار الدفع",
    failed: "حدث خطأ. حاول مرة أخرى.",
  },
};
type Copy = (typeof COPY)["en"];

const problem = (err: unknown, t: Copy) =>
  err instanceof ApiError
    ? (err.problem?.errors?.[0]?.message ?? err.problem?.detail ?? t.failed)
    : t.failed;

export default function BusinessPage({ params }: { params: Promise<{ locale: string }> }) {
  const locale = asLocale(use(params).locale);
  const t = COPY[locale];
  const mine = useQuery({ queryKey: ["me", "companies"], queryFn: listMyCompanies, retry: false });
  const items = mine.data?.items ?? [];
  return (
    <CustomerShell locale={locale}>
      <section className="ms-biz">
        <h1>{t.title}</h1>
        <p className="ms-sub">{t.intro}</p>
        {mine.error instanceof ApiError && mine.error.status === 401 ? (
          <p>
            <Link className="ms-cta" href={signInHref(locale, `/${locale}/business`)}>
              {t.signin}
            </Link>
          </p>
        ) : mine.isPending ? null : items.length === 0 ? (
          <Register locale={locale} t={t} />
        ) : (
          items.map((c) =>
            c.role === "admin" ? (
              <Manage key={c.company_id} id={c.company_id} locale={locale} t={t} />
            ) : (
              <div key={c.company_id} className="ms-panel">
                <h2 dir="auto">{c.name}</h2>
                <p>{c.status === "active" ? t.employee : t.pending}</p>
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
  const [f, setF] = useState({
    name: "", legal_name: "", trn: "", billing_email: "", billing_phone: "",
    label: "", building: "", area: "", city: "Dubai", lat: "", lng: "",
  });
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const field = (k: keyof typeof f, label: string, type = "text", required = false) => (
    <label className="ms-field">
      <span>{label}</span>
      <input
        type={type}
        dir="auto"
        value={f[k]}
        required={required}
        inputMode={k === "lat" || k === "lng" ? "decimal" : undefined}
        onChange={(e) => setF({ ...f, [k]: e.target.value })}
      />
    </label>
  );
  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await registerCompany({
        name: f.name, legal_name: f.legal_name, trn: f.trn,
        billing_email: f.billing_email, billing_phone: f.billing_phone,
        office: {
          label: f.label, building: f.building, area: f.area, city: f.city,
          lat: Number(f.lat), lng: Number(f.lng),
        },
      });
      await qc.invalidateQueries({ queryKey: ["me", "companies"] });
    } catch (err) {
      setError(problem(err, t));
    } finally {
      setBusy(false);
    }
  }
  return (
    <form className="ms-panel ms-biz-form" onSubmit={onSubmit} lang={locale}>
      <h2>{t.register}</h2>
      <div className="ms-row">{field("name", t.name, "text", true)}{field("legal_name", t.legal, "text", true)}</div>
      <div className="ms-row">{field("trn", t.trn)}{field("billing_email", t.email, "email", true)}</div>
      {field("billing_phone", t.phone, "tel")}
      <div className="ms-row">{field("label", t.office, "text", true)}{field("building", t.building)}</div>
      <div className="ms-row">{field("area", t.area)}{field("city", t.city, "text", true)}</div>
      <div className="ms-row">{field("lat", t.lat, "text", true)}{field("lng", t.lng, "text", true)}</div>
      {error && <p className="ms-bad" role="alert">{error}</p>}
      <button className="ms-cta" type="submit" disabled={busy}>{t.submit}</button>
    </form>
  );
}

function Manage({ id, locale, t }: { id: string; locale: Locale; t: Copy }) {
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ["company", id], queryFn: () => getCompany(id) });
  const reload = () => qc.invalidateQueries({ queryKey: ["company", id] });
  if (!q.data) return q.isError ? <p className="ms-bad">{t.failed}</p> : null;
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
      <People d={d} t={t} onChange={reload} />
      <div className="ms-panel">
        <h2>{t.lunches}</h2>
        {(d.lunches ?? []).length === 0 ? <p className="ms-sub">{t.none}</p> : (
          <ul className="ms-list">
            {(d.lunches ?? []).map((l) => (
              <li key={l.subscription_id}>
                <strong dir="auto">{l.employee || l.phone}</strong> · <span dir="auto">{l.kitchen}</span>
                <span className="ms-sub"> · {l.plan} · {l.status}</span>
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
              <li key={s.id}>
                <strong>{s.statement_no}</strong> · {money(s.total_minor)}
                <span className="ms-sub"> · {s.status === "paid" ? t.paid : `${t.issued} · ${t.due} ${s.due_on}`}</span>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  );
}

function Policy({ d, locale, t, onSaved }: { d: CompanyDashboard; locale: Locale; t: Copy; onSaved: () => void }) {
  const [days, setDays] = useState<number[]>(d.company.policy.weekdays);
  const [cap, setCap] = useState(String(d.company.policy.meal_cap_minor / 100));
  const [msg, setMsg] = useState<string | null>(null);
  async function save() {
    setMsg(null);
    try {
      await setCompanyPolicy(d.company.id, {
        weekdays: [...days].sort(),
        slots: d.company.policy.slots,
        meal_cap_minor: Math.round(Number(cap) * 100),
      });
      setMsg(t.saved);
      onSaved();
    } catch (err) {
      setMsg(problem(err, t));
    }
  }
  return (
    <div className="ms-panel">
      <h2>{t.policy}</h2>
      <fieldset className="ms-field">
        <legend>{t.days}</legend>
        <div className="ms-chips">
          {WEEKDAYS[locale].map((w, i) => (
            <button key={w} type="button" className={days.includes(i) ? "on" : ""} aria-pressed={days.includes(i)}
              onClick={() => setDays(days.includes(i) ? days.filter((x) => x !== i) : [...days, i])}>
              {w}
            </button>
          ))}
        </div>
      </fieldset>
      <label className="ms-field">
        <span>{t.cap}</span>
        <input inputMode="decimal" value={cap} onChange={(e) => setCap(e.target.value)} />
      </label>
      <button type="button" className="ms-cta" onClick={save}>{t.save}</button>
      {msg && <p className="ms-sub" role="status">{msg}</p>}
    </div>
  );
}

function People({ d, t, onChange }: { d: CompanyDashboard; t: Copy; onChange: () => void }) {
  const [phone, setPhone] = useState("");
  const [name, setName] = useState("");
  const [admin, setAdmin] = useState(false);
  const [error, setError] = useState<string | null>(null);
  async function add(e: FormEvent) {
    e.preventDefault();
    setError(null);
    try {
      await addCompanyMember(d.company.id, { phone, name, role: admin ? "admin" : "employee" });
      setPhone("");
      setName("");
      setAdmin(false);
      onChange();
    } catch (err) {
      setError(problem(err, t));
    }
  }
  async function remove(memberId: string) {
    setError(null);
    try {
      await removeCompanyMember(d.company.id, memberId);
      onChange();
    } catch (err) {
      setError(problem(err, t));
    }
  }
  return (
    <div className="ms-panel">
      <h2>{t.people}</h2>
      <ul className="ms-list">
        {(d.members ?? []).map((m) => (
          <li key={m.id}>
            <strong dir="auto">{m.name || m.phone}</strong>
            <span className="ms-sub" dir="ltr"> {m.name ? m.phone : ""}</span>
            <span className="ms-sub"> · {m.role === "admin" ? t.admin : m.joined ? `${m.lunches} ${t.lunches.toLowerCase()}` : t.invited}</span>{" "}
            <button type="button" className="ms-link" onClick={() => remove(m.id)}>{t.remove}</button>
          </li>
        ))}
      </ul>
      <form onSubmit={add}>
        <h3>{t.invite}</h3>
        <div className="ms-row">
          <label className="ms-field"><span>{t.mobile}</span><input type="tel" dir="ltr" required value={phone} placeholder="+9715…" onChange={(e) => setPhone(e.target.value)} /></label>
          <label className="ms-field"><span>{t.empName}</span><input dir="auto" value={name} onChange={(e) => setName(e.target.value)} /></label>
        </div>
        <label><input type="checkbox" checked={admin} onChange={(e) => setAdmin(e.target.checked)} /> {t.admin}</label>
        {error && <p className="ms-bad" role="alert">{error}</p>}
        <button className="ms-cta" type="submit">{t.add}</button>
      </form>
    </div>
  );
}
