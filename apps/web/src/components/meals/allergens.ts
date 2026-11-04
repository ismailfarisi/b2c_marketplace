/**
 * The API's closed allergen list (kitchen.Allergens: the fourteen EU/GSO
 * labelling names) in English and Arabic. Unknown codes fall back to the code.
 */
const ALLERGENS: Record<string, { en: string; ar: string }> = {
  gluten: { en: "Gluten", ar: "غلوتين" },
  crustaceans: { en: "Crustaceans", ar: "قشريات" },
  eggs: { en: "Eggs", ar: "بيض" },
  fish: { en: "Fish", ar: "سمك" },
  peanuts: { en: "Peanuts", ar: "فول سوداني" },
  soy: { en: "Soy", ar: "صويا" },
  milk: { en: "Milk", ar: "حليب" },
  tree_nuts: { en: "Tree nuts", ar: "مكسرات" },
  celery: { en: "Celery", ar: "كرفس" },
  mustard: { en: "Mustard", ar: "خردل" },
  sesame: { en: "Sesame", ar: "سمسم" },
  sulphites: { en: "Sulphites", ar: "كبريتيت" },
  lupin: { en: "Lupin", ar: "ترمس" },
  molluscs: { en: "Molluscs", ar: "رخويات" },
};

export function allergenLabel(code: string, locale: "en" | "ar"): string {
  return ALLERGENS[code]?.[locale] ?? code.replace(/_/g, " ");
}

export function allergenLine(codes: string[], locale: "en" | "ar"): string {
  return codes
    .map((c) => allergenLabel(c, locale))
    .join(locale === "ar" ? "، " : ", ");
}
