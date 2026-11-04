"use client";

// Last resort for a crash in the root [locale] layout itself, which
// [locale]/error.tsx cannot catch. It replaces the whole document, so it
// carries its own <html> and reads the locale from the URL.
export default function GlobalError({
  error,
  reset,
}: {
  error: Error & { digest?: string };
  reset: () => void;
}) {
  const ar =
    typeof window !== "undefined" &&
    /^\/ar(\/|$)/.test(window.location.pathname);
  const t = ar
    ? {
        title: "حدث خطأ ما",
        body: "تعذر فتح الصفحة. حاول مرة أخرى.",
        retry: "حاول مرة أخرى",
        home: "العودة إلى المطابخ",
        ref: "المرجع",
      }
    : {
        title: "Something went wrong",
        body: "This page couldn't open. Try again.",
        retry: "Try again",
        home: "Back to kitchens",
        ref: "Reference",
      };
  return (
    <html lang={ar ? "ar" : "en"} dir={ar ? "rtl" : "ltr"}>
      <body
        style={{
          margin: 0,
          minHeight: "100vh",
          display: "grid",
          placeItems: "center",
          background: "#fff8f0",
          color: "#1e1410",
          fontFamily: "system-ui, sans-serif",
        }}
      >
        <main role="alert" style={{ textAlign: "center", padding: "2rem" }}>
          <h1>{t.title}</h1>
          <p>{t.body}</p>
          {error.digest ? (
            <p style={{ color: "#76675b" }}>
              {t.ref}: <span dir="ltr">{error.digest}</span>
            </p>
          ) : null}
          <p style={{ display: "flex", gap: "1rem", justifyContent: "center" }}>
            <button type="button" onClick={reset}>
              {t.retry}
            </button>
            {/* A plain link: the router may be what crashed. */}
            <a href={ar ? "/ar" : "/en"}>{t.home}</a>
          </p>
        </main>
      </body>
    </html>
  );
}
