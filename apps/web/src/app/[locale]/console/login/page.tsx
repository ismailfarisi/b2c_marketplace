"use client";

import { ApiError } from "@marketplace/api-client";
import { useQueryClient } from "@tanstack/react-query";
import { useRouter, useSearchParams } from "next/navigation";
import { Suspense, use, useEffect, useRef, useState, type FormEvent } from "react";

import { listMyBranches } from "@/lib/vendor-api";
import { logout, requestOtp, verifyOtp } from "@/lib/auth-api";
import { BRANCHES_KEY } from "@/components/vendor/portal";

/**
 * Vendor sign-in. The same phone-and-code flow as the staff console (see
 * admin/login), landing on the vendor portal and checking kitchen membership
 * instead of staff access.
 */

/** Whether to explain where the code actually goes. See `OTP_DEV_ECHO`. */
const IS_DEV = (process.env.NEXT_PUBLIC_ENV ?? "dev") !== "production";

type Step =
  | { phase: "phone" }
  | { phase: "code"; challengeId: string; phone: string; resendAt: number };

/**
 * `useSearchParams` is read below, which opts this route out of static
 * prerendering unless the read sits behind a Suspense boundary — without one
 * `next build` fails the whole route rather than degrading. The boundary is
 * here rather than around the whole page so the card and its environment badge
 * render immediately either way.
 */
export default function VendorLogin({
  params,
}: {
  params: Promise<{ locale: string }>;
}) {
  const { locale } = use(params);

  return (
    <Suspense fallback={<Frame>Loading…</Frame>}>
      <LoginForm locale={locale} />
    </Suspense>
  );
}

function LoginForm({ locale }: { locale: string }) {
  const router = useRouter();
  const search = useSearchParams();
  const queryClient = useQueryClient();

  const [step, setStep] = useState<Step>({ phase: "phone" });
  const [phone, setPhone] = useState("");
  const [code, setCode] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  /** Set when the code was right but the account holds no console access. */
  const [refused, setRefused] = useState(false);

  const codeRef = useRef<HTMLInputElement>(null);
  useEffect(() => {
    if (step.phase === "code") codeRef.current?.focus();
  }, [step.phase]);

  /**
   * Where to land. Only same-console paths are honoured: `next` arrives in a
   * URL, and a URL is something an attacker can send someone. Anything that is
   * not a path under this locale's console is dropped rather than sanitised.
   */
  const destination = (() => {
    const base = `/${locale}/console`;
    const next = search.get("next");
    if (next && next.startsWith(`${base}/`) && !next.startsWith("//")) {
      return next === `${base}/login` ? base : next;
    }
    return base;
  })();

  async function onRequestCode(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const challenge = await requestOtp(phone.trim(), locale);
      setCode("");
      setStep({
        phase: "code",
        challengeId: challenge.challenge_id,
        phone: phone.trim(),
        resendAt: Date.now() + challenge.resend_after_seconds * 1000,
      });
    } catch (err) {
      setError(describeRequestFailure(err));
    } finally {
      setBusy(false);
    }
  }

  async function onVerify(event: FormEvent) {
    event.preventDefault();
    if (step.phase !== "code") return;
    setBusy(true);
    setError(null);
    try {
      await verifyOtp(step.challengeId, code.trim());

      // Ask who this is before navigating, and seed the cache with the answer.
      // Sending someone to the console and letting the gate discover they have
      // no access means a screen that loads and then throws them out; asking
      // here means the refusal lands on the page they are already looking at.
      const branches = await listMyBranches();
      if (branches.items.length === 0) {
        setRefused(true);
        return;
      }
      queryClient.setQueryData(BRANCHES_KEY, branches);
      router.replace(destination);
    } catch (err) {
      setError(describeVerifyFailure(err));
    } finally {
      setBusy(false);
    }
  }

  if (refused) {
    return (
      <Frame>
        <div className="state error">
          <div className="headline">This number is not on a kitchen&apos;s team.</div>
        </div>
        <p className="muted">
          The code was right — you are signed in. Ask the kitchen&apos;s owner to
          add this number, or contact the marketplace to onboard your kitchen.
        </p>
        {/* Revokes before returning to the form. The code was right, so a real
            session exists — leaving it alive because the *console* refused them
            would strand a working refresh token in this tab's storage for
            someone who is about to hand the machine to a colleague. */}
        <button
          type="button"
          className="btn"
          disabled={busy}
          onClick={async () => {
            setBusy(true);
            try {
              await logout();
            } finally {
              setBusy(false);
              setRefused(false);
              setCode("");
              setError(null);
              setStep({ phase: "phone" });
            }
          }}
        >
          Use a different number
        </button>
      </Frame>
    );
  }

  if (step.phase === "code") {
    return (
      <Frame>
        <form onSubmit={onVerify} className="console-login-form">
          <p className="muted">
            Code sent to <span className="mono">{step.phone}</span>.
          </p>

          <label className="console-login-field">
            <span>Six-digit code</span>
            <input
              ref={codeRef}
              className="field"
              value={code}
              onChange={(e) => setCode(e.target.value.replace(/\D/g, "").slice(0, 6))}
              inputMode="numeric"
              autoComplete="one-time-code"
              placeholder="000000"
              required
            />
          </label>

          {error ? <p className="console-login-error">{error}</p> : null}

          <div className="console-login-actions">
            <button className="btn primary" type="submit" disabled={busy || code.length < 6}>
              {busy ? "Checking…" : "Sign in"}
            </button>
            <button
              type="button"
              className="btn link"
              disabled={busy}
              onClick={() => {
                setError(null);
                setStep({ phase: "phone" });
              }}
            >
              Change number
            </button>
          </div>

          {IS_DEV ? (
            <p className="console-login-note">
              Development: <code>OTP_DEV_ECHO</code> is on, so no SMS is sent —
              the code is written to the API log as{" "}
              <code>OTP issued (dev echo…)</code>.
            </p>
          ) : null}
        </form>
      </Frame>
    );
  }

  return (
    <Frame>
      <form onSubmit={onRequestCode} className="console-login-form">
        <label className="console-login-field">
          <span>Phone number</span>
          <input
            className="field"
            value={phone}
            onChange={(e) => setPhone(e.target.value)}
            type="tel"
            inputMode="tel"
            autoComplete="tel"
            placeholder="+971501234567"
            autoFocus
            required
          />
        </label>

        {error ? <p className="console-login-error">{error}</p> : null}

        <div className="console-login-actions">
          <button className="btn primary" type="submit" disabled={busy || phone.trim().length < 8}>
            {busy ? "Sending…" : "Send code"}
          </button>
        </div>

        <p className="console-login-note">
          The same account you use as a customer. Kitchen access is granted by
          the kitchen&apos;s owner and checked on the server on every call.
        </p>
      </form>
    </Frame>
  );
}

function Frame({ children }: { children: React.ReactNode }) {
  return (
    <div className="console-gate">
      <div className="console-gate-card">
        <div className="console-login-brand">
          <span>Kitchen portal</span>
          {/* Which environment this is, on the sign-in screen as well as in the
              sidebar. Signing into production believing it is staging is the
              mistake worth spending a badge on. */}
          <span className="env">{process.env.NEXT_PUBLIC_ENV ?? "dev"}</span>
        </div>
        {children}
      </div>
    </div>
  );
}

function describeRequestFailure(err: unknown): string {
  if (err instanceof ApiError) {
    const field = err.fieldErrors["phone"];
    if (field) return `That number was not accepted: ${field}`;
    if (err.status === 409 || err.status === 429) {
      return "Too many codes have been requested for this number in the last hour. Wait, then try again.";
    }
  }
  return "The code could not be sent. Check the number and try again.";
}

function describeVerifyFailure(err: unknown): string {
  if (err instanceof ApiError) {
    if (err.status === 401) {
      return "That code is not right, or it has expired. Request a new one.";
    }
    if (err.status === 429) {
      return "Too many attempts. Request a new code.";
    }
  }
  return "Could not verify that code. Try again.";
}
