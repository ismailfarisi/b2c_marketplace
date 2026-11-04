"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { useEffect, useState } from "react";

import "./account.css";

import { LoadError } from "@/components/load-error";
import {
  getProfile,
  listNotificationPreferences,
  setDefaultAddress,
  setNotificationPreference,
  updateProfile,
  type NotificationPreference,
} from "@/lib/account-api";
import {
  deleteAddress,
  describeAddress,
  listAddresses,
} from "@/lib/customer-api";
import { problemFields, problemMessage } from "@/lib/problem";

type Locale = "en" | "ar";

const COPY = {
  en: {
    profile: "Your details",
    name: "Name",
    email: "Email",
    phone: "Signed in as",
    noName: "Add your name",
    noEmail: "No email",
    edit: "Edit",
    save: "Save",
    saving: "Saving…",
    cancel: "Cancel",
    phoneNote: "To change your phone number, contact support.",
    support: "Help",
    addresses: "Saved addresses",
    noAddresses: "No saved addresses yet. You can add one when you subscribe.",
    isDefault: "Default",
    makeDefault: "Make default",
    remove: "Remove",
    confirmRemove: "Remove this address?",
    yesRemove: "Yes, remove",
    keep: "Keep",
    notifications: "Notifications",
    marketingNote:
      "Offers and news. Delivery codes, sign-in codes and order updates are always sent.",
    channel: {
      push: "Marketing messages: app notifications",
      in_app: "Marketing messages: in the app",
      email: "Marketing messages: email",
      whatsapp: "Marketing messages: WhatsApp",
      sms: "Marketing messages: SMS",
    } as Record<string, string>,
  },
  ar: {
    profile: "بياناتك",
    name: "الاسم",
    email: "البريد الإلكتروني",
    phone: "مسجّل الدخول برقم",
    noName: "أضف اسمك",
    noEmail: "لا يوجد بريد إلكتروني",
    edit: "تعديل",
    save: "حفظ",
    saving: "جارٍ الحفظ…",
    cancel: "إلغاء",
    phoneNote: "لتغيير رقم هاتفك، تواصل مع الدعم.",
    support: "المساعدة",
    addresses: "العناوين المحفوظة",
    noAddresses: "لا توجد عناوين محفوظة بعد. يمكنك إضافة عنوان عند الاشتراك.",
    isDefault: "الافتراضي",
    makeDefault: "اجعله الافتراضي",
    remove: "إزالة",
    confirmRemove: "هل تريد إزالة هذا العنوان؟",
    yesRemove: "نعم، أزله",
    keep: "إبقاء",
    notifications: "الإشعارات",
    marketingNote:
      "العروض والأخبار. نرسل دائمًا رموز التوصيل ورموز الدخول وتحديثات الطلبات.",
    channel: {
      push: "الرسائل التسويقية: إشعارات التطبيق",
      in_app: "الرسائل التسويقية: داخل التطبيق",
      email: "الرسائل التسويقية: البريد الإلكتروني",
      whatsapp: "الرسائل التسويقية: واتساب",
      sms: "الرسائل التسويقية: الرسائل النصية",
    } as Record<string, string>,
  },
};

/** Profile, saved addresses and marketing switches, above Delete account. */
export function AccountSections({ locale }: { locale: Locale }) {
  return (
    <div className="ms-account">
      <ProfileSection locale={locale} />
      <AddressesSection locale={locale} />
      <NotificationsSection locale={locale} />
    </div>
  );
}

function ProfileSection({ locale }: { locale: Locale }) {
  const t = COPY[locale];
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ["me", "profile"], queryFn: getProfile });
  const [editing, setEditing] = useState(false);
  const [name, setName] = useState("");
  const [email, setEmail] = useState("");
  const save = useMutation({
    mutationFn: () => updateProfile({ display_name: name, email }),
    onSuccess: (p) => {
      qc.setQueryData(["me", "profile"], p);
      setEditing(false);
    },
  });
  const fields = save.error ? problemFields(save.error, locale) : {};
  const p = q.data;
  return (
    <section aria-labelledby="ms-profile-title">
      <h2 id="ms-profile-title">{t.profile}</h2>
      {q.isPending ? null : q.isError ? (
        <LoadError error={q.error} onRetry={() => q.refetch()} locale={locale} />
      ) : p && editing ? (
        <form
          onSubmit={(e) => {
            e.preventDefault();
            save.mutate();
          }}
        >
          <label className="ms-field">
            <span>{t.name}</span>
            <input
              value={name}
              onChange={(e) => setName(e.target.value)}
              maxLength={80}
              autoComplete="name"
              dir="auto"
              aria-invalid={fields.display_name ? true : undefined}
            />
            {fields.display_name && <small className="ms-bad">{fields.display_name}</small>}
          </label>
          <label className="ms-field">
            <span>{t.email}</span>
            <input
              type="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              autoComplete="email"
              dir="ltr"
              aria-invalid={fields.email ? true : undefined}
            />
            {fields.email && <small className="ms-bad">{fields.email}</small>}
          </label>
          {save.error && !fields.display_name && !fields.email && (
            <p className="ms-bad" role="alert">
              {problemMessage(save.error, locale)}
            </p>
          )}
          <div className="ms-account-actions">
            <button type="submit" className="ms-cta" disabled={save.isPending}>
              {save.isPending ? t.saving : t.save}
            </button>
            <button
              type="button"
              className="ms-cta ms-cta-quiet"
              disabled={save.isPending}
              onClick={() => {
                save.reset();
                setEditing(false);
              }}
            >
              {t.cancel}
            </button>
          </div>
        </form>
      ) : p ? (
        <div className="ms-account-row">
          <span>
            <strong dir="auto">{p.display_name || t.noName}</strong>
            <span className="ms-sub" dir="ltr">
              {p.email || t.noEmail}
            </span>
            <span className="ms-sub">
              {t.phone} <bdi dir="ltr">{p.phone}</bdi>
            </span>
          </span>
          <button
            type="button"
            className="ms-cta ms-cta-quiet"
            onClick={() => {
              setName(p.display_name ?? "");
              setEmail(p.email ?? "");
              save.reset();
              setEditing(true);
            }}
          >
            {t.edit}
          </button>
          <small className="ms-sub">
            {t.phoneNote} <Link href={`/${locale}/support`}>{t.support}</Link>
          </small>
        </div>
      ) : null}
    </section>
  );
}

function AddressesSection({ locale }: { locale: Locale }) {
  const t = COPY[locale];
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ["me", "addresses"], queryFn: listAddresses });
  const [confirming, setConfirming] = useState<string | null>(null);
  const done = () => qc.invalidateQueries({ queryKey: ["me", "addresses"] });
  const makeDefault = useMutation({ mutationFn: setDefaultAddress, onSuccess: done });
  const remove = useMutation({
    mutationFn: deleteAddress,
    onSuccess: () => {
      setConfirming(null);
      return done();
    },
  });
  const failed = makeDefault.error ?? remove.error;
  const busy = makeDefault.isPending || remove.isPending;
  const items = q.data?.items ?? [];
  return (
    <section aria-labelledby="ms-addr-title">
      <h2 id="ms-addr-title">{t.addresses}</h2>
      {q.isPending ? null : q.isError ? (
        <LoadError error={q.error} onRetry={() => q.refetch()} locale={locale} />
      ) : items.length === 0 ? (
        <p className="ms-sub">{t.noAddresses}</p>
      ) : (
        <ul className="ms-account-list">
          {items.map((a) => (
            <li key={a.id}>
              <div className="ms-account-row">
                <span>
                  <strong dir="auto">
                    {a.label}
                    {a.is_default && (
                      <>
                        {" "}
                        <span className="ms-status ms-status-active">{t.isDefault}</span>
                      </>
                    )}
                  </strong>
                  <span className="ms-sub" dir="auto">
                    {describeAddress(a)}
                  </span>
                </span>
                {confirming === a.id ? (
                  <span className="ms-account-actions" role="group" aria-label={t.confirmRemove}>
                    <span>{t.confirmRemove}</span>
                    <button
                      type="button"
                      className="ms-cta ms-cta-danger"
                      disabled={busy}
                      onClick={() => remove.mutate(a.id)}
                    >
                      {t.yesRemove}
                    </button>
                    <button
                      type="button"
                      className="ms-cta ms-cta-quiet"
                      disabled={busy}
                      onClick={() => setConfirming(null)}
                    >
                      {t.keep}
                    </button>
                  </span>
                ) : (
                  <span className="ms-account-actions">
                    {!a.is_default && (
                      <button
                        type="button"
                        className="ms-link"
                        disabled={busy}
                        onClick={() => makeDefault.mutate(a.id)}
                      >
                        {t.makeDefault}
                      </button>
                    )}
                    <button
                      type="button"
                      className="ms-link-danger"
                      disabled={busy}
                      onClick={() => {
                        remove.reset();
                        setConfirming(a.id);
                      }}
                    >
                      {t.remove}
                    </button>
                  </span>
                )}
              </div>
            </li>
          ))}
        </ul>
      )}
      {failed && (
        <p className="ms-bad" role="alert">
          {problemMessage(failed, locale)}
        </p>
      )}
    </section>
  );
}

function NotificationsSection({ locale }: { locale: Locale }) {
  const t = COPY[locale];
  const qc = useQueryClient();
  const q = useQuery({
    queryKey: ["me", "notification-preferences"],
    queryFn: listNotificationPreferences,
  });
  const [errors, setErrors] = useState<Record<string, string>>({});
  const set = useMutation({
    mutationFn: (p: NotificationPreference) =>
      setNotificationPreference({ category: p.category, channel: p.channel, allowed: p.allowed }),
    onMutate: (p) => {
      setErrors((e) => ({ ...e, [p.channel]: "" }));
      qc.setQueryData<{ items: NotificationPreference[] }>(
        ["me", "notification-preferences"],
        (cur) =>
          cur && {
            items: cur.items.map((i) => (i.channel === p.channel ? { ...i, allowed: p.allowed } : i)),
          },
      );
    },
    onError: (err, p) => {
      // Put the switch back and say why next to it (422 not_optional included).
      qc.setQueryData<{ items: NotificationPreference[] }>(
        ["me", "notification-preferences"],
        (cur) =>
          cur && {
            items: cur.items.map((i) => (i.channel === p.channel ? { ...i, allowed: !p.allowed } : i)),
          },
      );
      setErrors((e) => ({ ...e, [p.channel]: problemMessage(err, locale) }));
    },
  });
  return (
    <section aria-labelledby="ms-notif-title">
      <h2 id="ms-notif-title">{t.notifications}</h2>
      <p className="ms-sub">{t.marketingNote}</p>
      {q.isPending ? null : q.isError ? (
        <LoadError error={q.error} onRetry={() => q.refetch()} locale={locale} />
      ) : (
        <ul className="ms-account-list">
          {(q.data?.items ?? []).map((p) => (
            <li key={`${p.category}:${p.channel}`}>
              <label className="ms-switch">
                <input
                  type="checkbox"
                  role="switch"
                  checked={p.allowed}
                  disabled={!p.optional || set.isPending}
                  onChange={(e) => set.mutate({ ...p, allowed: e.target.checked })}
                />
                <span>{t.channel[p.channel] ?? p.channel}</span>
              </label>
              {errors[p.channel] && (
                <small className="ms-bad" role="alert">
                  {errors[p.channel]}
                </small>
              )}
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

const DELETED = {
  en: { body: "Your account was deleted. Thank you for eating with us.", dismiss: "Dismiss" },
  ar: { body: "تم حذف حسابك. شكرًا لأنك كنت معنا.", dismiss: "إغلاق" },
};

/** Shown once on the page a deleted account lands on (?deleted=1). */
export function AccountDeletedNotice({ locale }: { locale: Locale }) {
  const [show, setShow] = useState(false);
  useEffect(() => {
    const params = new URLSearchParams(window.location.search);
    if (params.get("deleted") !== "1") return;
    setShow(true);
    params.delete("deleted");
    const qs = params.toString();
    window.history.replaceState(null, "", window.location.pathname + (qs ? `?${qs}` : ""));
  }, []);
  if (!show) return null;
  const t = DELETED[locale];
  return (
    <div className="ms-notice" role="status">
      <span>{t.body}</span>
      <button type="button" onClick={() => setShow(false)}>
        {t.dismiss}
      </button>
    </div>
  );
}
