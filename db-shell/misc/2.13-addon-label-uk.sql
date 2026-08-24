-- 2.13 — QR Card (printedBanner) ve Welcome Board (welcome_board) form etiketleri Ukraynaca.
--
-- Ingilizce metinler frontend'de degil bu iki urunun kaydinda duruyor:
--   options.config_fields[].label -> "Event Title", "Welcome Message", "Event Date" ...
--   options.designs[].label       -> "Design 1" .. "Design 32"
-- Bu alanlar admin panelden duzenlenebildigi icin anahtar bazli i18n ile cozulemez;
-- ceviri de veriyle birlikte gelmeli. Frontend her ikisini de localizedLabel() uzerinden
-- okuyor (src/utils/product_i18n.ts): dil uk ise label_uk, yoksa label. Alan yoksa
-- davranis degismiyor, yani bu script calismadan once hicbir sey bozulmuyor.
--
-- Welcome Board da dahil, cunku ayni kusur onda da var ve iki kart checkout'ta yan yana
-- render ediliyor; yalnizca birini cevirmek sayfayi karisik birakirdi.
--
-- Calistirmadan once mevcut hali gor:
--   SELECT id, jsonb_pretty(options->'config_fields') FROM products
--   WHERE id IN ('printedBanner','welcome_board');

BEGIN;

-- 1) config_fields: her alanin key'ine gore label_uk eklenir, diger alanlar korunur.
WITH tr(product_id, field_key, label_uk) AS (
    VALUES
        ('printedBanner', 'name_text',       'Назва події'),
        ('printedBanner', 'welcome_message', 'Вітальне повідомлення'),
        ('printedBanner', 'event_date',      'Дата події'),
        ('welcome_board', 'name_text',       'Назва події?'),
        ('welcome_board', 'event_date',      'Яка дата вашої події?'),
        ('welcome_board', 'secondary_text',  'Вітальне повідомлення?'),
        ('welcome_board', 'footer_text',     'Текст унизу?')
)
UPDATE products p
SET options = jsonb_set(p.options, '{config_fields}', (
        SELECT jsonb_agg(
                   CASE WHEN t.label_uk IS NOT NULL
                        THEN e.f || jsonb_build_object('label_uk', t.label_uk)
                        ELSE e.f
                   END
                   ORDER BY e.ord
               )
        FROM jsonb_array_elements(p.options->'config_fields') WITH ORDINALITY AS e(f, ord)
        LEFT JOIN tr t ON t.product_id = p.id AND t.field_key = e.f->>'key'
    ))
WHERE p.id IN ('printedBanner', 'welcome_board')
  AND jsonb_array_length(COALESCE(p.options->'config_fields', '[]'::jsonb)) > 0;

-- 2) designs: "Design N" -> "Дизайн N". Kalibi tutmayan bir etiket olursa aynen kalir,
--    label_uk = label olur ve gorunum degismez.
UPDATE products p
SET options = jsonb_set(p.options, '{designs}', (
        SELECT jsonb_agg(
                   e.d || jsonb_build_object('label_uk', replace(e.d->>'label', 'Design ', 'Дизайн '))
                   ORDER BY e.ord
               )
        FROM jsonb_array_elements(p.options->'designs') WITH ORDINALITY AS e(d, ord)
    ))
WHERE p.id IN ('printedBanner', 'welcome_board')
  AND jsonb_array_length(COALESCE(p.options->'designs', '[]'::jsonb)) > 0;

-- Dogrulama: iki urun de hem config_fields hem designs tarafinda label_uk tasimali.
SELECT id,
       (SELECT count(*) FROM jsonb_array_elements(options->'config_fields') x WHERE x ? 'label_uk') AS fields_uk,
       jsonb_array_length(options->'config_fields') AS fields_total,
       (SELECT count(*) FROM jsonb_array_elements(options->'designs') x WHERE x ? 'label_uk') AS designs_uk,
       jsonb_array_length(options->'designs') AS designs_total
FROM products WHERE id IN ('printedBanner', 'welcome_board') ORDER BY id;

COMMIT;

-- Geri alma:
--   UPDATE products p SET options = jsonb_set(p.options, '{config_fields}',
--       (SELECT jsonb_agg(e.f - 'label_uk' ORDER BY e.ord)
--        FROM jsonb_array_elements(p.options->'config_fields') WITH ORDINALITY AS e(f, ord)))
--   WHERE p.id IN ('printedBanner','welcome_board');
--   UPDATE products p SET options = jsonb_set(p.options, '{designs}',
--       (SELECT jsonb_agg(e.d - 'label_uk' ORDER BY e.ord)
--        FROM jsonb_array_elements(p.options->'designs') WITH ORDINALITY AS e(d, ord)))
--   WHERE p.id IN ('printedBanner','welcome_board');
