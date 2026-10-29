"use client";

import { useState } from "react";

const PALETTES = 4;

function pick(id: string) {
  let h = 0;
  for (const c of id) h = (h * 31 + c.charCodeAt(0)) >>> 0;
  return (h % PALETTES) + 1;
}

/**
 * Kitchen cover photo, or a drawn cover (tinted ground, monogram, cuisines)
 * when the kitchen has no photo or it fails to load.
 */
export function KitchenCover({
  id,
  name,
  src,
  fallback,
  caption,
  className = "",
}: {
  id: string;
  name: string;
  src: string | null;
  /** Tried when `src` is missing or fails to load, e.g. a dish photo. */
  fallback?: string | null | undefined;
  caption?: string;
  className?: string;
}) {
  const [failed, setFailed] = useState(0);
  const shown = [src, fallback].filter((u): u is string => !!u)[failed];
  if (shown) {
    return (
      <div className={`ms-cover ${className}`} aria-hidden>
        <img
          key={shown}
          src={shown}
          alt=""
          onError={() => setFailed((n) => n + 1)}
        />
      </div>
    );
  }
  return (
    <div
      className={`ms-cover ms-cover-drawn ms-cover-p${pick(id)} ${className}`}
      aria-hidden
    >
      <span className="ms-cover-mark" dir="auto">
        {Array.from(name.trim())[0] ?? "·"}
      </span>
      {caption && (
        <span className="ms-cover-cap" dir="auto">
          {caption}
        </span>
      )}
    </div>
  );
}
