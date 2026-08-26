-- 2.16b — 2.16-qr-card-qty.sql'in geri alinmasi + Welcome Board form alanlarinin
--         QR Card ile hizalanmasi (etiketler).
--
-- Adet kurali 8'lik bloktan 4'luk bloga cevrildi ve veriden koda tasindi:
-- src/client/cart.ts -> getQtyRule artik is_add_on olan (ve SINGLE_QUANTITY_ADDON_IDS'te
-- olmayan) her urun icin sabit 4/4 doner, options.min_qty / qty_step'i hic okumaz.
-- Backend bu iki alani zaten hicbir yerde okumuyor (options'i opaque JSONB olarak
-- client'a geciriyor), yani silmek davranis degistirmez -- sadece admin panelinde
-- "degistirsem ne olur" diye bakan kisiyi yaniltan olu veriyi kaldirir.
--
-- Etiket tarafinda frontend'de displayConfigFieldLabel() adli bir temizleyici var:
-- sondaki '?' isaretini ve Ingilizce "What is your " on ekini kirpiyor. Musteri dogru
-- etiketi goruyor ama DB'deki ham deger kirli kaldigi icin admin paneli soru isaretli
-- hallerini gosteriyordu ve UK tarafinda ("Яка дата вашої події?") on ek regex'i
-- Ingilizce'ye ozel oldugu icin QR Card'daki "Дата події" ile hic eslesmiyordu.
-- Bu script kaynagi temizler; regex de calismaya devam eder, sadece artik no-op olur.
--
-- Calistirmadan once mevcut hali gor:
--   SELECT id, options->>'min_qty', options->>'qty_step' FROM products WHERE id = 'printedBanner';
--   SELECT f->>'key', f->>'label', f->>'label_uk' FROM products p,
--          jsonb_array_elements(p.options->'config_fields') f WHERE p.id = 'welcome_board';
--
-- Yedek: ukr-membox/deploy/backups/products-options-2026-08-26-1530-pre.json

BEGIN;

-- 1) printedBanner: 2.16'da eklenen olu min_qty / qty_step alanlarini kaldir.
UPDATE products
SET options = options - 'min_qty' - 'qty_step'
WHERE id = 'printedBanner';

-- 2) welcome_board: config_fields etiketlerini temizle.
--    a) Butun alanlarda label ve label_uk sonundaki '?' kirpilir.
--    b) event_date'in iki etiketi de QR Card'daki karsiligiyla ('Event Date' / 'Дата події')
--       birebir ayni yapilir; "What is your ..." ifadesi UK tarafinda karsiliksizdi.
--    Alanlar yerinde donusturuluyor (yeniden yazilmiyor) ki type/maxLength gibi
--    dokunmadigimiz anahtarlar aynen kalsin; jsonb_agg ... ORDER BY ord dizi sirasini korur.
UPDATE products
SET options = jsonb_set(
      options,
      '{config_fields}',
      (
        SELECT jsonb_agg(
                 CASE WHEN field->>'key' = 'event_date' THEN
                        field
                          || jsonb_build_object('label', 'Event Date')
                          || jsonb_build_object('label_uk', 'Дата події')
                      ELSE
                        field
                          || jsonb_build_object('label',    rtrim(field->>'label',    '?'))
                          || jsonb_build_object('label_uk', rtrim(field->>'label_uk', '?'))
                 END
                 ORDER BY ord
               )
        FROM jsonb_array_elements(options->'config_fields') WITH ORDINALITY AS t(field, ord)
      )
    )
WHERE id = 'welcome_board';

-- Dogrulama 1: her iki sutun da f donmeli.
SELECT id,
       options ? 'min_qty'  AS has_min_qty,
       options ? 'qty_step' AS has_qty_step
FROM products
WHERE id IN ('printedBanner', 'welcome_board')
ORDER BY id;

-- Dogrulama 2: hicbir etiket '?' ile bitmemeli, iki urunun event_date etiketi ayni olmali,
--              alan sirasi bozulmamis olmali.
SELECT p.id, f.ord, f.field->>'key' AS key,
       f.field->>'label' AS label_en, f.field->>'label_uk' AS label_uk
FROM products p,
     jsonb_array_elements(p.options->'config_fields') WITH ORDINALITY AS f(field, ord)
WHERE p.id IN ('printedBanner', 'welcome_board')
ORDER BY p.id, f.ord;

-- Dogrulama 3: 0 satir donmeli.
SELECT p.id, f->>'key' AS key, f->>'label' AS label_en, f->>'label_uk' AS label_uk
FROM products p, jsonb_array_elements(p.options->'config_fields') AS f
WHERE f->>'label' LIKE '%?' OR f->>'label_uk' LIKE '%?';

COMMIT;

-- Geri alma:
--   UPDATE products SET options = options || '{"min_qty": 8, "qty_step": 8}'::jsonb
--   WHERE id = 'printedBanner';
--
--   UPDATE products SET options = jsonb_set(options, '{config_fields}', '[
--     {"key":"name_text",     "type":"textarea","label":"Event Title?",             "label_uk":"Назва події?",           "maxLength":75},
--     {"key":"event_date",    "type":"textarea","label":"What is your event date?", "label_uk":"Яка дата вашої події?",  "maxLength":50},
--     {"key":"secondary_text","type":"textarea","label":"Welcome Message?",         "label_uk":"Вітальне повідомлення?", "maxLength":100},
--     {"key":"footer_text",   "type":"textarea","label":"Footer Text?",             "label_uk":"Текст унизу?",           "maxLength":100}
--   ]'::jsonb) WHERE id = 'welcome_board';
