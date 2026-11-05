DROP INDEX IF EXISTS menu_items_name_ar_fold_trgm;
DROP INDEX IF EXISTS vendors_trade_name_ar_fold_trgm;
DROP INDEX IF EXISTS vendor_branches_name_ar_fold_trgm;
DROP FUNCTION IF EXISTS search_fold(text);
