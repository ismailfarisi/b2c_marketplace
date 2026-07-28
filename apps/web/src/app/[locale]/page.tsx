export default async function HomePage({
  params,
}: {
  params: Promise<{ locale: string }>;
}) {
  const { locale } = await params;
  const isAr = locale === "ar";

  return (
    <main className="page">
      <h1>{isAr ? "السوق" : "Marketplace"}</h1>
      <p>
        {isAr
          ? "الهيكل جاهز. الخلاصة تصل في المرحلة الثانية."
          : "Scaffold is up. The feed lands in Phase 2."}
      </p>
    </main>
  );
}
