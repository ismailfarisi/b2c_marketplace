"use client";

import { useEffect, useState } from "react";

import { FALLBACK_POINT, type Point } from "@/lib/meals-api";

/**
 * The customer's rough location. A district, not a doorstep: no high-accuracy
 * request and a short timeout, so a refused or slow prompt falls back to Dubai
 * Marina instead of holding the page. `precise` tells the page which it got so
 * it can say so.
 */
export function useCustomerPoint() {
  const [point, setPoint] = useState<Point | null>(null);
  const [precise, setPrecise] = useState(false);

  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    if (typeof navigator === "undefined" || !navigator.geolocation) {
      setPoint(FALLBACK_POINT);
      return;
    }
    navigator.geolocation.getCurrentPosition(
      (pos) => {
        setPoint({ lat: pos.coords.latitude, lng: pos.coords.longitude });
        setPrecise(true);
      },
      () => setPoint(FALLBACK_POINT),
      { timeout: 4000, maximumAge: attempt > 0 ? 0 : 300_000 },
    );
  }, [attempt]);

  return { point, precise, retry: () => setAttempt((n) => n + 1) };
}
