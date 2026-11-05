-- Arabic is written with optional diacritics and several interchangeable
-- letter forms, so "منسف", "مَنْسَف" and "منســف" are the same dish to a
-- reader but not to ILIKE. search_fold() reduces a string to the form the
-- storefront search compares on, applied to both the query and the columns:
--   * strips tashkeel (U+064B..U+065F), the superscript alef (U+0670), the
--     Quranic annotation marks (U+06D6..U+06ED) and tatweel (U+0640);
--   * unifies alef forms (أ إ آ ٱ -> ا), alef maqsura with yaa (ى -> ي) and
--     taa marbuta with haa (ة -> ه);
--   * lower-cases, so Latin text still matches case-insensitively.
-- IMMUTABLE so it can back the expression indexes below.
CREATE FUNCTION search_fold(t text) RETURNS text
    LANGUAGE sql IMMUTABLE PARALLEL SAFE STRICT
    AS $$
        SELECT lower(translate(
            regexp_replace(t, '[\u064B-\u065F\u0670\u06D6-\u06ED\u0640]', '', 'g'),
            'أإآٱىة', 'اااايه'))
    $$;

CREATE INDEX vendor_branches_name_ar_fold_trgm ON vendor_branches
    USING gin (search_fold(name_ar) gin_trgm_ops);
CREATE INDEX vendors_trade_name_ar_fold_trgm ON vendors
    USING gin (search_fold(trade_name_ar) gin_trgm_ops);
CREATE INDEX menu_items_name_ar_fold_trgm ON menu_items
    USING gin (search_fold(name_ar) gin_trgm_ops);
