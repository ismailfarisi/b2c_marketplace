"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { formatMoney } from "@marketplace/api-client";
import { use, useEffect, useRef, useState } from "react";

import {
  advanceStop,
  completeStop,
  declareRemittance,
  dispatchStop,
  endShift,
  failStop,
  getHeldCash,
  getManifest,
  reportLocation,
  startShift,
  type ManifestStop,
} from "@/lib/api";

/**
 * The driver view.
 *
 * PLAN.md §5 P4: *"the driver app with background location is the single most
 * expensive item in this plan. Consider launching with vendors doing their own
 * delivery and a simple web-based driver view, and defer the native driver app
 * until order volume justifies it."*
 *
 * This is that view, and it is worth being precise about what taking that advice
 * actually costs, because "a web view instead" hides one real limitation: a
 * browser tab does not report location in the background. `navigator.geolocation`
 * stops when the screen locks or the tab is backgrounded, so live tracking works
 * while the driver is looking at their phone and not otherwise. That is enough
 * for arrival geofencing and proof-of-delivery — the things that matter at the
 * door — and not enough for a customer-facing "your driver is 4 minutes away"
 * map. Building that map is what would justify the native app.
 */

const PING_INTERVAL_MS = 20_000;

export default function DriverView({
  params,
}: {
  params: Promise<{ locale: string }>;
}) {
  const { locale } = use(params);
  const isAr = locale === "ar";
  const loc = isAr ? "ar" : "en";
  const queryClient = useQueryClient();
  const position = useLivePosition();

  const manifest = useQuery({
    queryKey: ["manifest"],
    queryFn: () => getManifest(),
    refetchInterval: 30_000,
  });

  const cash = useQuery({ queryKey: ["driver-cash"], queryFn: getHeldCash });

  const refresh = () => {
    queryClient.invalidateQueries({ queryKey: ["manifest"] });
    queryClient.invalidateQueries({ queryKey: ["driver-cash"] });
  };

  const shiftOn = useMutation({ mutationFn: () => startShift(), onSuccess: refresh });
  const shiftOff = useMutation({ mutationFn: endShift, onSuccess: refresh });
  const remit = useMutation({ mutationFn: declareRemittance, onSuccess: refresh });

  // Location reporting runs only while a stop is live. A driver sitting at home
  // with the tab open is not on a delivery, and pinging their position anyway is
  // surveillance the product does not need and cannot justify.
  const activeRoute = manifest.data?.stops.find((s) =>
    ["dispatched", "en_route", "arrived"].includes(s.status),
  )?.route_id;

  useEffect(() => {
    if (!position || !activeRoute) return;
    const timer = setInterval(() => {
      void reportLocation({
        lat: position.lat,
        lng: position.lng,
        accuracy_m: position.accuracy,
        route_id: activeRoute,
      }).catch(() => {
        // A dropped ping is not worth surfacing. The next one is 20 seconds
        // away and the driver can do nothing about it.
      });
    }, PING_INTERVAL_MS);
    return () => clearInterval(timer);
  }, [position, activeRoute]);

  const stops = manifest.data?.stops ?? [];

  return (
    <main className="page driver">
      <header className="row spread">
        <h1>{isAr ? "مساري" : "My route"}</h1>
        <div className="row">
          <button onClick={() => shiftOn.mutate()} disabled={shiftOn.isPending}>
            {isAr ? "بدء المناوبة" : "Start shift"}
          </button>
          <button onClick={() => shiftOff.mutate()} disabled={shiftOff.isPending}>
            {isAr ? "إنهاء المناوبة" : "End shift"}
          </button>
        </div>
      </header>

      {shiftOff.data && <p className="warn">{shiftOff.data.note}</p>}

      {!position && (
        <p className="warn">
          {isAr
            ? "الموقع غير مفعّل — لن تتمكّن من تسجيل الوصول."
            : "Location is off — you will not be able to mark arrival."}
        </p>
      )}

      {cash.data && cash.data.total_minor > 0 && (
        <section className="card">
          <div className="row spread">
            <span>
              {isAr ? "نقد بحوزتك" : "Cash on you"}:{" "}
              <strong>
                {formatMoney(
                  { amount: cash.data.total_minor, currency: cash.data.currency },
                  loc,
                )}
              </strong>{" "}
              <span className="muted">({cash.data.collections})</span>
            </span>
            <button onClick={() => remit.mutate()} disabled={remit.isPending}>
              {isAr ? "تسليم النقد" : "Hand in cash"}
            </button>
          </div>
          {remit.data && <p className="muted">{remit.data.next_step}</p>}
        </section>
      )}

      {stops.length === 0 && (
        <p className="muted">{isAr ? "لا توجد محطات اليوم." : "No stops today."}</p>
      )}

      <ol className="stack">
        {stops.map((stop) => (
          <Stop key={stop.fulfillment_id} stop={stop} isAr={isAr} loc={loc} position={position} onDone={refresh} />
        ))}
      </ol>
    </main>
  );
}

function Stop({
  stop,
  isAr,
  loc,
  position,
  onDone,
}: {
  stop: ManifestStop;
  isAr: boolean;
  loc: "en" | "ar";
  position: { lat: number; lng: number; accuracy: number } | null;
  onDone: () => void;
}) {
  const [otp, setOtp] = useState("");
  const [collected, setCollected] = useState<string>("");
  const [error, setError] = useState<string | null>(null);

  const at = position ? { lat: position.lat, lng: position.lng } : undefined;
  const run = <T,>(p: Promise<T>) =>
    p.then(onDone).catch((e: unknown) => setError(errorMessage(e, isAr)));

  const terminal = stop.status === "completed" || stop.status === "failed";

  return (
    <li className={`card stop ${terminal ? "done" : ""}`}>
      <div className="row spread">
        <strong>
          {stop.sequence}. {stop.address.line}
        </strong>
        <span className="pill">{stop.status}</span>
      </div>

      <p className="muted">
        {[stop.address.area, stop.address.city].filter(Boolean).join(" · ")}
        {stop.address.landmark && ` · ${stop.address.landmark}`}
      </p>
      {stop.address.notes && <p className="notes">{stop.address.notes}</p>}

      <p>
        {stop.window}
        {stop.eta && ` · ${new Date(stop.eta).toLocaleTimeString(loc)}`}
        {" · "}
        <a href={`tel:${stop.recipient_phone}`}>{stop.recipient_name || stop.recipient_phone}</a>
      </p>

      {stop.cod_minor > 0 && (
        <p className="cash">
          {isAr ? "اقبض" : "Collect"}{" "}
          <strong>{formatMoney({ amount: stop.cod_minor, currency: stop.currency }, loc)}</strong>
        </p>
      )}

      {stop.attempt_count > 0 && !terminal && (
        <p className="warn">
          {isAr
            ? `محاولة رقم ${stop.attempt_count + 1}`
            : `Attempt ${stop.attempt_count + 1}`}
        </p>
      )}

      {error && <p className="bad">{error}</p>}

      {!terminal && (
        <div className="actions">
          {stop.status === "scheduled" && (
            <button onClick={() => run(dispatchStop(stop.fulfillment_id))}>
              {isAr ? "استلمت" : "Picked up"}
            </button>
          )}
          {stop.status === "dispatched" && (
            <button onClick={() => run(advanceStop(stop.fulfillment_id, "en_route", at))}>
              {isAr ? "في الطريق" : "On my way"}
            </button>
          )}
          {stop.status === "en_route" && (
            <button onClick={() => run(advanceStop(stop.fulfillment_id, "arrived", at))}>
              {isAr ? "وصلت" : "Arrived"}
            </button>
          )}

          {stop.status === "arrived" && (
            <div className="stack">
              {stop.requires_otp && (
                <input
                  inputMode="numeric"
                  maxLength={4}
                  placeholder={isAr ? "رمز التسليم" : "Delivery code"}
                  value={otp}
                  onChange={(e) => setOtp(e.target.value)}
                />
              )}
              {stop.cod_minor > 0 && (
                <input
                  inputMode="decimal"
                  placeholder={isAr ? "المبلغ المستلم" : "Amount collected"}
                  value={collected}
                  onChange={(e) => setCollected(e.target.value)}
                />
              )}
              <button
                onClick={() =>
                  run(
                    completeStop(stop.fulfillment_id, {
                      proof: stop.requires_otp ? "otp" : "recipient_name",
                      otp: otp || undefined,
                      recipient_name: stop.recipient_name,
                      // Minor units, parsed from the major-unit field the driver
                      // typed. The one conversion in this client, and it lives
                      // next to the input rather than in a shared helper so it
                      // cannot be reused for a price.
                      collected_minor:
                        stop.cod_minor > 0
                          ? Math.round(Number(collected || "0") * 100)
                          : undefined,
                      ...at,
                    }),
                  )
                }
              >
                {isAr ? "تم التسليم" : "Delivered"}
              </button>
              <button
                className="ghost"
                onClick={() =>
                  run(failStop(stop.fulfillment_id, { outcome: "customer_absent", ...at }))
                }
              >
                {isAr ? "العميل غير موجود" : "Customer absent"}
              </button>
            </div>
          )}
        </div>
      )}
    </li>
  );
}

/**
 * Watches position while the tab is in the foreground.
 *
 * `watchPosition` rather than repeated `getCurrentPosition`: the browser
 * coalesces updates from a single watch and it is materially cheaper on battery
 * than waking the GPS on a timer.
 */
function useLivePosition() {
  const [position, setPosition] = useState<{ lat: number; lng: number; accuracy: number } | null>(
    null,
  );
  const watchId = useRef<number | null>(null);

  useEffect(() => {
    if (typeof navigator === "undefined" || !navigator.geolocation) return;

    watchId.current = navigator.geolocation.watchPosition(
      (p) =>
        setPosition({
          lat: p.coords.latitude,
          lng: p.coords.longitude,
          accuracy: p.coords.accuracy,
        }),
      () => setPosition(null),
      { enableHighAccuracy: true, maximumAge: 10_000, timeout: 15_000 },
    );

    return () => {
      if (watchId.current !== null) navigator.geolocation.clearWatch(watchId.current);
    };
  }, []);

  return position;
}

function errorMessage(e: unknown, isAr: boolean): string {
  if (e && typeof e === "object" && "problem" in e) {
    const problem = (e as { problem: { detail?: string; title?: string } | null }).problem;
    if (problem?.detail) return problem.detail;
    if (problem?.title) return problem.title;
  }
  return isAr ? "تعذّر تنفيذ الإجراء." : "That did not work.";
}
