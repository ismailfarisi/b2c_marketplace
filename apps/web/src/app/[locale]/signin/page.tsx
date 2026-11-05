"use client";

import { ApiError } from "@marketplace/api-client";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { Suspense, use, useEffect, useState, type FormEvent } from "react";

import {
  CustomerShell,
  asLocale,
  safeNext,
  useHasSession,
  type Locale,
} from "@/components/meals/shell";
import { PhoneInput } from "@/components/phone-input";
import { requestOtp, verifyOtp } from "@/lib/auth-api";
import {
  DEFAULT_PHONE_COUNTRY,
  displayPhone,
  ltr,
  phoneCountry,
  phoneProblem,
} from "@/lib/phone";
import { networkMessage } from "@/lib/problem";

const IS_DEV = (process.env.NEXT_PUBLIC_ENV ?? "dev") !== "production";

const COPY = {
  en: {
    title: "Sign in",
    intro: "We'll text you a six-digit code. No password.",
    agree: ["By continuing you agree to our ", "Terms", " and ", "Privacy policy", "."],
    phone: "Mobile number",
    country: "Country code",
    phoneHint: (country: string, example: string) =>
      `Pick the country, then type the number as you would locally, e.g. ${example} in ${country}.`,
    send: "Send code",
    sending: "Sending…",
    sentTo: (p: string) => `Code sent to ${p}.`,
    code: "Six-digit code",
    verify: "Continue",
    checking: "Checking…",
    change: "Change number",
    resend: "Resend code",
    resendIn: (s: number) => `Resend code in ${s}s`,
    resent: "New code sent. Earlier codes no longer work.",
    wrongCode: "That code is not right. Check the message and try again.",
    staleCode: "That code can no longer be used. Send a new one.",
    blocked:
      "This account can't sign in right now. Contact support for help.",
    tooMany: "Too many attempts. Wait a little, then try again.",
    staleSuperseded: "A newer code was sent. Use the code from the latest message.",
    resendFailed:
      "Couldn't send a new code. Your previous code still works until it expires.",
    resendTooMany:
      "Too many codes requested. Wait a while; your previous code still works until it expires.",
    badPhone:
      "Check the number and try again, e.g. 0501234567 or +971501234567.",
    badLength: (country: string, example: string) =>
      `That isn't a ${country} mobile number. It should look like ${example}. Check the country code.`,
    needCountry: "Choose the country code for this number, or start it with +.",
    offline: "No connection. Check your network and try again.",
    failed: "Something went wrong. Try again.",
    dev: "Development: no SMS is sent; the code is in the API log.",
  },
  ar: {
    title: "تسجيل الدخول",
    intro: "سنرسل لك رمزًا من ستة أرقام. بدون كلمة مرور.",
    agree: ["بالمتابعة فإنك توافق على ", "الشروط", " و", "سياسة الخصوصية", "."],
    phone: "رقم الجوال",
    country: "رمز الدولة",
    phoneHint: (country: string, example: string) =>
      `اختر الدولة ثم اكتب الرقم كما يُكتب محليًا، مثل ‎${example} في ${country}.`,
    send: "أرسل الرمز",
    sending: "جارٍ الإرسال…",
    sentTo: (p: string) => `أرسلنا الرمز إلى ${ltr(p)}.`,
    code: "الرمز المكون من ستة أرقام",
    verify: "متابعة",
    checking: "جارٍ التحقق…",
    change: "تغيير الرقم",
    resend: "أعد إرسال الرمز",
    resendIn: (s: number) => `إعادة الإرسال بعد ${s} ث`,
    resent: "أرسلنا رمزًا جديدًا. الرموز السابقة لم تعد صالحة.",
    wrongCode: "الرمز غير صحيح. تحقق من الرسالة وحاول مرة أخرى.",
    staleCode: "لم يعد بالإمكان استخدام هذا الرمز. اطلب رمزًا جديدًا.",
    blocked: "لا يمكن لهذا الحساب تسجيل الدخول حاليًا. تواصل مع الدعم للمساعدة.",
    tooMany: "محاولات كثيرة. انتظر قليلًا ثم حاول مرة أخرى.",
    staleSuperseded: "أرسلنا رمزًا أحدث. استخدم الرمز من آخر رسالة.",
    resendFailed:
      "تعذر إرسال رمز جديد. رمزك السابق ما زال صالحًا حتى تنتهي مدته.",
    resendTooMany:
      "طلبت رموزًا كثيرة. انتظر قليلًا؛ رمزك السابق ما زال صالحًا حتى تنتهي مدته.",
    badPhone:
      "تحقق من الرقم وحاول مرة أخرى، مثل ‎0501234567 أو ‎+971501234567.",
    badLength: (country: string, example: string) =>
      `هذا ليس رقم جوال في ${country}. يجب أن يكون مثل ‎${example}. تحقق من رمز الدولة.`,
    needCountry: "اختر رمز الدولة لهذا الرقم، أو ابدأه بعلامة +.",
    offline: "لا يوجد اتصال. تحقق من الشبكة وحاول مرة أخرى.",
    failed: "حدث خطأ. حاول مرة أخرى.",
    dev: "بيئة التطوير: لا تُرسل رسائل، الرمز في سجل الخادم.",
  },
};

type Copy = (typeof COPY)["en"];

/** Codes after which this challenge is dead and only a new code helps. */
const STALE_CODES = new Set([
  "otp_expired",
  "otp_already_used",
  "otp_too_many_attempts",
  "otp_unknown_challenge",
  "otp_superseded",
]);

const isRateLimited = (err: ApiError) =>
  err.status === 429 || err.problem?.code === "otp_rate_limited";

function requestFailure(err: unknown, t: Copy, locale: Locale, country: string): string {
  if (!(err instanceof ApiError)) return networkMessage(err, locale) ?? t.offline;
  if (isRateLimited(err)) return t.tooMany;
  if (err.status === 422) {
    const c = phoneCountry(country);
    switch (phoneProblem(err.problem)) {
      case "wrong_length":
        return t.badLength(c.name[locale], c.example);
      case "country_required":
        return t.needCountry;
      default:
        return t.badPhone;
    }
  }
  return t.failed;
}

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

type Challenge = { id: string; resendAt: number };

function Form({ locale }: { locale: Locale }) {
  const t = COPY[locale];
  const router = useRouter();
  const next = safeNext(locale, useSearchParams().get("next"));
  const signedIn = useHasSession();
  const [phone, setPhone] = useState("");
  const [country, setCountry] = useState<string>(DEFAULT_PHONE_COUNTRY);
  const [code, setCode] = useState("");
  const [challenge, setChallenge] = useState<Challenge | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [now, setNow] = useState(() => Date.now());

  useEffect(() => {
    if (signedIn) router.replace(next);
  }, [signedIn, next, router]);

  const waiting = challenge ? challenge.resendAt > now : false;
  useEffect(() => {
    if (!waiting) return;
    const id = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(id);
  }, [waiting]);

  async function request(): Promise<Challenge> {
    const c = await requestOtp(phone.trim(), country, locale);
    const at = Date.now();
    setNow(at);
    return { id: c.challenge_id, resendAt: at + c.resend_after_seconds * 1000 };
  }

  async function send(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    setNotice(null);
    try {
      setChallenge(await request());
      setCode("");
    } catch (err) {
      setError(requestFailure(err, t, locale, country));
    } finally {
      setBusy(false);
    }
  }

  // A successful resend retires the earlier codes on the server; a failed one
  // sends nothing and retires nothing, so the code already sent is still good
  // until it expires, and saying so stops a retype into nothing.
  async function resend() {
    setBusy(true);
    setError(null);
    setNotice(null);
    try {
      setChallenge(await request());
      setCode("");
      setNotice(t.resent);
    } catch (err) {
      setError(
        err instanceof ApiError && isRateLimited(err)
          ? t.resendTooMany
          : t.resendFailed,
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
    setNotice(null);
    try {
      await verifyOtp(challenge.id, code.trim(), "Web");
      router.replace(next);
    } catch (err) {
      if (!(err instanceof ApiError)) {
        setError(networkMessage(err, locale) ?? t.offline);
      } else if (err.status === 401 && STALE_CODES.has(err.problem?.code ?? "")) {
        setChallenge(null);
        setCode("");
        setError(
          err.problem?.code === "otp_superseded" ? t.staleSuperseded : t.staleCode,
        );
      } else if (err.status === 401) {
        setError(t.wrongCode);
      } else if (err.status === 403) {
        setError(t.blocked);
      } else if (isRateLimited(err)) {
        setError(t.tooMany);
      } else {
        setError(t.failed);
      }
      setBusy(false);
    }
  }

  const alert = error && (
    <p className="ms-bad" role="alert">
      {error}
    </p>
  );

  return (
    <section className="ms-auth">
      <h1>{t.title}</h1>
      {challenge ? (
        <form onSubmit={verify} className="ms-form">
          <p className="ms-sub">
            {t.sentTo(displayPhone(phone, country))}
            {notice && (
              <>
                {" "}
                <span role="status">{notice}</span>
              </>
            )}
          </p>
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
          {alert}
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
            disabled={busy || waiting}
            onClick={resend}
          >
            {waiting && challenge
              ? t.resendIn(Math.ceil((challenge.resendAt - now) / 1000))
              : t.resend}
          </button>
          <button
            type="button"
            className="ms-link"
            disabled={busy}
            onClick={() => {
              setChallenge(null);
              setError(null);
              setNotice(null);
            }}
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
            <PhoneInput
              className="ms-phone"
              country={country}
              onCountry={setCountry}
              phone={phone}
              onPhone={setPhone}
              locale={locale}
              countryLabel={t.country}
            />
            <small className="ms-sub">
              {t.phoneHint(
                phoneCountry(country).name[locale],
                phoneCountry(country).example,
              )}
            </small>
          </label>
          {alert}
          <button
            className="ms-cta"
            type="submit"
            disabled={busy || phone.replace(/\D/g, "").length < 8}
          >
            {busy ? t.sending : t.send}
          </button>
          <small className="ms-sub">
            {t.agree[0]}
            <Link href={`/${locale}/terms`}>{t.agree[1]}</Link>
            {t.agree[2]}
            <Link href={`/${locale}/privacy`}>{t.agree[3]}</Link>
            {t.agree[4]}
          </small>
        </form>
      )}
    </section>
  );
}
