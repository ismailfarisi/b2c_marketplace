"use client";

import { ApiError } from "@marketplace/api-client";
import { useRouter, useSearchParams } from "next/navigation";
import { Suspense, use, useState, type FormEvent } from "react";

import {
  CustomerShell,
  asLocale,
  safeNext,
  type Locale,
} from "@/components/meals/shell";
import { requestOtp, verifyOtp } from "@/lib/auth-api";

const IS_DEV = (process.env.NEXT_PUBLIC_ENV ?? "dev") !== "production";

const COPY = {
  en: {
    title: "Sign in",
    intro: "We'll text you a six-digit code. No password.",
    phone: "Mobile number",
    send: "Send code",
    sending: "Sending…",
    sentTo: (p: string) => `Code sent to ${p}.`,
    code: "Six-digit code",
    verify: "Continue",
    checking: "Checking…",
    change: "Change number",
    badCode: "That code is not right, or it has expired. Request a new one.",
    tooMany: "Too many attempts. Wait a little, then try again.",
    badPhone:
      "Check the number and try again. Use the international format, e.g. +971501234567.",
    failed: "Something went wrong. Try again.",
    dev: "Development: no SMS is sent; the code is in the API log.",
  },
  ar: {
    title: "تسجيل الدخول",
    intro: "سنرسل لك رمزًا من ستة أرقام. بدون كلمة مرور.",
    phone: "رقم الجوال",
    send: "أرسل الرمز",
    sending: "جارٍ الإرسال…",
    sentTo: (p: string) => `أرسلنا الرمز إلى ${p}.`,
    code: "الرمز المكون من ستة أرقام",
    verify: "متابعة",
    checking: "جارٍ التحقق…",
    change: "تغيير الرقم",
    badCode: "الرمز غير صحيح أو انتهت صلاحيته. اطلب رمزًا جديدًا.",
    tooMany: "محاولات كثيرة. انتظر قليلًا ثم حاول مرة أخرى.",
    badPhone:
      "تحقق من الرقم وحاول مرة أخرى بالصيغة الدولية، مثل ‎+971501234567.",
    failed: "حدث خطأ. حاول مرة أخرى.",
    dev: "بيئة التطوير: لا تُرسل رسائل، الرمز في سجل الخادم.",
  },
};

export default function SignInPage({
  params,
}: {
  params: Promise<{ locale: string }>;
}) {
  const locale = asLocale(use(params).locale);
  return (
    <CustomerShell locale={locale}>
      <Suspense fallback={null}>
        <Form locale={locale} />
      </Suspense>
    </CustomerShell>
  );
}

function Form({ locale }: { locale: Locale }) {
  const t = COPY[locale];
  const router = useRouter();
  const next = safeNext(locale, useSearchParams().get("next"));
  const [phone, setPhone] = useState("");
  const [code, setCode] = useState("");
  const [challenge, setChallenge] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function send(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const c = await requestOtp(phone.trim(), locale);
      setChallenge(c.challenge_id);
      setCode("");
    } catch (err) {
      setError(
        err instanceof ApiError && (err.status === 429 || err.status === 409)
          ? t.tooMany
          : err instanceof ApiError && err.status === 422
            ? t.badPhone
            : t.failed,
      );
    } finally {
      setBusy(false);
    }
  }

  async function verify(e: FormEvent) {
    e.preventDefault();
    if (!challenge) return;
    setBusy(true);
    setError(null);
    try {
      await verifyOtp(challenge, code.trim());
      router.replace(next);
    } catch (err) {
      setError(
        err instanceof ApiError && err.status === 401
          ? t.badCode
          : err instanceof ApiError && err.status === 429
            ? t.tooMany
            : t.failed,
      );
      setBusy(false);
    }
  }

  return (
    <section className="ms-auth">
      <h1>{t.title}</h1>
      {challenge ? (
        <form onSubmit={verify} className="ms-form">
          <p className="ms-sub">{t.sentTo(phone.trim())}</p>
          <label>
            <span>{t.code}</span>
            <input
              value={code}
              onChange={(e) =>
                setCode(e.target.value.replace(/\D/g, "").slice(0, 6))
              }
              inputMode="numeric"
              autoComplete="one-time-code"
              placeholder="000000"
              dir="ltr"
              autoFocus
              required
            />
          </label>
          {error && (
            <p className="ms-bad" role="alert">
              {error}
            </p>
          )}
          <button
            className="ms-cta"
            type="submit"
            disabled={busy || code.length < 6}
          >
            {busy ? t.checking : t.verify}
          </button>
          <button
            type="button"
            className="ms-link"
            onClick={() => setChallenge(null)}
          >
            {t.change}
          </button>
          {IS_DEV && <small className="ms-sub">{t.dev}</small>}
        </form>
      ) : (
        <form onSubmit={send} className="ms-form">
          <p className="ms-sub">{t.intro}</p>
          <label>
            <span>{t.phone}</span>
            <input
              value={phone}
              onChange={(e) => setPhone(e.target.value)}
              type="tel"
              inputMode="tel"
              autoComplete="tel"
              placeholder="+971501234567"
              dir="ltr"
              autoFocus
              required
            />
          </label>
          {error && (
            <p className="ms-bad" role="alert">
              {error}
            </p>
          )}
          <button
            className="ms-cta"
            type="submit"
            disabled={busy || phone.trim().length < 8}
          >
            {busy ? t.sending : t.send}
          </button>
        </form>
      )}
    </section>
  );
}
