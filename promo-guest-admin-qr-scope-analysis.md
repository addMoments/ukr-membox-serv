# Promo Code, Admin Roller, QR/Welcome, Guest Banner — Konsolide Kapsam ve Teknik Analiz

Bu dokuman **musteri maddeleri (1–4)**, **yeni rol/promo kararlari**, **collaborator davet kapsami duzeltmesi** ve [admin-panel-package-monitoring-analysis.md](./admin-panel-package-monitoring-analysis.md) icindeki **urun/snapshot/landing** kararlarinin tek yerde toplanmis halidir.

---

## Geriye Donuk Uyumluluk — Aktif Sistem Bozulmayacak

**Ilke:** Bugun prod’da calisan odeme, siparis, event, guest okuma, mevcut admin endpointleri ve snapshot davranisi **kirilmadan** ilerlenir.

| Yaklasim | Aciklama |
|----------|-----------|
| Opsiyonel veri | Yeni kolonlar **NULL** / yeni tablolar mevcut satirlari etkilemez; eski siparisler `promo_code_uid = NULL`. |
| Varsayilan davranis | Reklam/banner ozelligi yoksa veya ayar yapilmamisssa guest ve API **onceki gibi** (banner alani render edilmez veya bos). |
| API uyumu | Mevcut response alanlari mumkun oldugunca **korunur**; yeni alanlar eklenir. Davranis degisikligi gerekiyorsa **major** degil **genisleme** (yeni endpoint veya `v2` ancak zorunluysa). |
| Ozellik kilidi | Banner grid ve icerik yalnizca **add-on veya premium feature** aktif etkinliklerde sunulur; diger etkinliklerde kod yolu etkisiz. |
| Test | Her deploy oncesi: mevcut checkout, callback, orders list/detail (super admin), guest public sayfalari **regression smoke**. |

Bu madde **tum kapsam** (promo, roller, QR, banner, collaborator) icin gecerlidir; ozellikle odeme ve siparis akislari icin breaking change kabul edilmez.

**Collaborator notu:** Bugunu yanlis genis yetki vermis kullanicilar icin duzeltme **bilerek erisim daraltir** (secilmeyen etkinliklere erisim kaldirilir); bu istenen davranistir, regresyon degil.

---

## Musteri Maddeleri — Kapsam Kontrol Listesi (Hepsi Bu Dokumda)

| # | Madde | Dahil mi? | Dokumanda bolum |
|---|--------|-----------|-----------------|
| 1 | Order sayfasinda **Promo Code** alani (musteri kod girer) | Evet | [1) Order Sayfasi — Promo](#1-order-sayfasi--promo-code-alani) |
| 2a | Admin: promo **create/delete**; indirim **yuzde (`percent`)** ve/veya **sabit tutar (`fixed`)**; **limit** ve **tarih araligi** opsiyonel | Evet | [2) Admin Panel](#2-admin-panel) |
| 2b | Admin **ekleme / cikarma**; **iki rol**: super admin (mevcut yetki), normal admin (kisitli) | Evet | [2.1 Admin rolleri](#21-admin-rolleri-super-admin--normal-admin) |
| 2c | **Promo bazli satis** gorunumu | Evet | [2.2 Yetkiye gore](#22-yetkiye-gore-ozet) |
| 2d | **Order Account**: Total Guest, Total Size (MB), Storage Expiration; event silinmeden once upload analitik snapshot'i | Evet | [2.3 Order account](#23-order-account-metrikleri) |
| 3 | **32 QR Card** + **32 Welcome Board**, tasarimlar disaridan; checkout'ta secim + bilgi alma, gorsel uzerinde canli text/QR render yok | Evet | [3) QR ve Welcome Board](#3-qr-card-templates-ve-welcome-boards) |
| 4 | Guest arayuzunde **reklam alani**; **add-on** / **premium**; ayarlarda **layout** (tekli, 1x1, 2x1, 1x2, 2x2); hucrelere **gorsel** + istege bagli **link**; guest sayfasinda gosterim | Evet | [4) Reklam alani](#4-yeni-guest-arayuz--reklam-alani-layout--icerik) |
| 5 | **Collaborator daveti:** Davet edilen kisi yalnizca **secilen etkinlige** katilmali; davet maili/onayi sonrasi diger etkinliklere otomatik erisim **olmamali** (bug fix) | Evet | [5) Collaborator](#5-etkinlik-collaborator-davet-ve-yetki-tek-etkinlik) |
| — | **Tum isler — Backend / Frontend ayri listeler** | Evet | [Tum Isler — Backend ve Frontend](#tum-isler--backend-ve-frontend-yapilacaklar) |

---

## Ozet Kararlar (Guncel)

| Konu | Karar |
|------|--------|
| Promo veri modeli | Ayri **`promo_codes`** tablosu |
| Siparis baglantisi | **`purchases.promo_code_uid`** (kullanilan kod kaydinin UUID'si); rapor icin **snapshot** alanlari onerilir |
| Promo indirim tipi | **`percent`**: yuzde (ornegin %10); **`fixed`**: sabit tutar (ornegin 50 UAH) — admin promoyu olustururken secer; ayni model `discount_type` + `discount_value` |
| Promo limit / tarih | **Opsiyonel** toplam kullanim limiti; **opsiyonel** `valid_from` / `valid_until`; asim veya tarih disi = gecersiz |
| Promo kapsami (simdi) | Kod **yalnizca premium paket** ile kullanilabilir (sepette ilgili core paket satiri olmali) |
| Promo kapsami (gelecek) | **Urun bazli esneklik**: asagida [genisletilebilir model](#promonun-sadece-premiumda-gecerli-olmasi--gelecege-acik-tasarim) |
| Promo yonetimi | **Sadece super admin** (promo CRUD, raporlar) |
| Admin rolleri | **Super admin**: mevcut panel kapsamindaki her sey (fiyat dahil). **Normal admin**: yalnizca siparis operasyonu; **liste ve detayda fiyat/tutar gorunmez**; satira tiklayinca acilan **detay paneli** diger alanlari gorebilir |
| Reklam alani | **Add-on** urun; **premium** pakette **ucretsiz** (`granted_features`). Satin alma sonrasi **event ayarlari**nda layout secimi: **tekli**, **1x1**, **2x1**, **1x2**, **2x2**. Her slotta **gorsel**; **link opsiyonel** (linksiz = sadece gorsel). Sunum **guest** sayfasinda. |
| QR + Welcome | Tasarimlar `products.options.designs` icinde DB'de durur; checkout'ta secilen tasarim + input bilgileri `cart_items.buyer_config` olarak saklanir; yeni akista girilen bilgiler gorsel uzerine basilmadan event add-on detayinda gosterilir |
| Collaborator daveti | Davet + mail + kabul akisi **tek `event_uid`** ile sinirli; yetki kontrolleri (`Is_events_admin`, liste endpointleri) yalnizca bagli etkinlik icin **true** olmali; davet token’inda hedef etkinlik zorunlu |

---

## Onceki Analiz Dosyasindan Tasinan Kararlar (Ozet)

Asagidakiler [admin-panel-package-monitoring-analysis.md](./admin-panel-package-monitoring-analysis.md) ile uyumlu **cekirdek** kararlardir; bu proje kapsaminda birlikte ele alinmalidir.

### Urun / paket yonetimi (Faz 1)

- **`GET /api/admin/products`**, **`PATCH /api/admin/products/{productUid}`** ile Package + Add-On guncelleme.
- Editlenebilir alanlar: isim, fiyat, aciklama, guest/media sayilari, activation/storage gunleri, voice (`granted_features`), add-on isim/fiyat.
- **`product_id` immutable**; gorunen isim/metin guncellenir; mevcut satin alimlar kirilmaz.

### Activation / storage suresi

- Sabit `+2 hafta` yerine **admin `options` uzerinden** dinamik sure; default korunur.
- **Storage expiration** (monitoring/order account ile uyum): `storage_expiration = active_until + storage_days` (`storage_days` urun `options` kaynakli).

### Orders — fiyat drift

- **`cart_items.unit_price_snapshot`** satin alim aninda doldurulur.
- Okuma: **`COALESCE(ci.unit_price_snapshot, pr.price)`** (liste toplam ve satir fiyati).
- Promo sonrasi **net odeme** icin `purchases` uzerinde ek alanlar onerilir (indirim/net tutar); aksi halde raporlama gross ile karisir.

### Landing / paket kartlari

- Kart ozetleri **API’den** (`GET /api/products` veya admin products) sayisal olarak; hardcode metinler kaldirilir; `-1` = Unlimited vb.

### Dil / display (Nisan plan notu)

- Ihtiyaca gore `products` uzerinde **display_name_en/uk**, **display_description_en/uk**; bos ise S3 dil dosyasi fallback.

### Event listesi / monitoring (Faz 2 notu)

- `GET /api/admin/events`, `GET /api/admin/events/{eventUid}/monitoring` bazi maddelerde sonraki faz; **Order Account metrikleri** bu dokumada musteri maddesi olarak tanimli.

---

## 1) Order Sayfasi — Promo Code Alani

### Is gereksinimi

Checkout/order akisinda **Promo Code** input; gecerli kodda indirim uygulanir. Indirim **yuzde** veya **sabit tutar** olabilir (super admin promoyu tanimlarken secer); backend `discount_type` + `discount_value` ile tutar.

### `promo_codes` tablosu — alanlar

| Alan | Aciklama |
|------|----------|
| `uid` (PK) | UUID |
| `code` | Benzersiz, normalize (trim, tutarli buyuk/kucuk harf) |
| `discount_type` | `percent` = yuzde indirim; `fixed` = sabit tutar indirimi |
| `discount_value` | Yuzde icin oran (ornegin `10` = %10); sabit icin para tutari (para birimi policy ile) |
| `usage_limit_total` | **NULL** = sinirsiz toplam kullanim |
| `usage_count` | Atomik sayac |
| `per_user_limit` | **NULL** = kullanici basina limit yok |
| `valid_from` / `valid_until` | **NULL** = sinir yok; doluysa aralik disi = gecersiz |
| `is_active` | Manuel kapatma |
| `deleted_at` | Soft delete (gecmis FK korunur) |
| Esneklik | Asagida [urun uygunlugu](#promonun-sadece-premiumda-gecerli-olmasi--gelecege-acik-tasarim) |

**Gecerlilik:** `is_active` ve tarih/limit kurallari tek fonksiyonda; odeme oncesi ve kod uygulamada ayni kontrol. Cron sart degil.

### Promonun sadece premiumda gecerli olmasi — gelecege acik tasarim

**Simdi:** Promo yalnizca **premium core paket** satin alinirken kullanilabilir.

**Onerilen model (genisletilebilir):**

1. **`promo_code_eligible_products`** (coktan coga): `promo_code_uid`, `product_uid`  
   - MVP: premium paketin `products.uid` satiri(lari) bu tabloya eklenir (veya seed).  
   - Gelecek: Ayni promo veya baska promolar icin birden fazla `product_uid`; admin UI coklu secim.

2. **Alternatif (daha hafif MVP):** `promo_codes.eligible_product_uids UUID[]`  
   - MVP dizide sadece premium UID; ileride dizi genisler.

**Checkout dogrulamasi:** Sepette **en az bir** `cart_items` satiri, promonun uygun urun listesinde olmali (tipik: ana paket satiri). İleride “sadece add-on’a indirim” gibi kurallar icin kural genisletilebilir.

### `purchases` tablosu

```text
purchases.promo_code_uid UUID NULL REFERENCES promo_codes(uid) ON DELETE SET NULL
```

- Silinen promo: FK SET NULL; raporda kod metni icin `promo_code_text_snapshot` (veya benzeri) onerilir.

### Fiyat snapshot ile iliski

- `unit_price_snapshot` **liste birim fiyatini** korur.
- Indirim sonrasi **tahsil edilen tutar** ve rapor uyumu icin `purchases` (veya ayri tablo) uzerinde **discount_amount**, **net_paid** vb. tutulmali.

---

## 2) Admin Panel

### 2.1 Admin rolleri: Super admin / Normal admin

| | Super admin | Normal admin |
|---|-------------|--------------|
| Kapsam | Su an panelde olan **tum** islevler: urun edit, siparisler **fiyat ve toplam ile**, promo CRUD, promo raporu, (gelecekteki) diger admin sayfalari | **Yalnizca siparis odagi**: siparis listesi ve detay |
| Fiyat | Gorur | **Gormez** — API response ve UI’da **tutar, birim fiyat, indirim, toplam** alanlari **cikarilir veya maskelenir** |
| Liste | Mevcut orders listesi | Ayni liste yapisinda **finansal kolonlar yok** |
| Detay | Tam detay | Satira tiklaninca acilan **detay paneli**: alici, durum, kargo, notlar, event baglantisi, **Order Account metrikleri** (asagida) — **para alanlari yok** |
| Promo tanimlama | Evet | Hayir |
| Promo satis raporu | Evet | Hayir (veya sadece super admin endpoint) |
| Admin ekleme/cikarma | Super admin (tercihen sadece super) | Hayir |

**Uygulama notu:** Bugun super admin **`env.AdminEmails` + `IsSuperAdmin`** ile. Iki rol icin:

- **Secenek A:** `users` (veya `admin_users`) tablosunda `role = super_admin | admin` + JWT claim.
- **Secenek B:** Super admin env’de kalir; normal adminler DB’de; middleware once env super, sonra DB rol kontrolu.

Normal admin icin **ayri handler veya ortak handler + response filter** ile fiyat sizmasini engelle (sadece UI gizlemek yetmez).

### 2.2 Yetkiye gore ozet

- **Promo CRUD + limit/tarih alanlari:** yalnizca **super admin** paneli.
- **Promo indirim:** Admin kod olustururken **`discount_type`** ile **yuzde (`percent`)** veya **sabit tutar (`fixed`)** secer; musteri order sayfasinda kod **hangi tipte yazildiysa** o kuralla uygulanir.
- **Promo bazli satis:** super admin (gruplama `purchases.promo_code_uid` uzerinden; metrik sozlugu gross/net/discount kilitlenmeli).

### 2.3 Order Account metrikleri

Musteri maddesi (super ve normal admin detayinda, **fiyat olmadan** normal admin icin de):

- **Total Guest Number** — tanim: paket limiti mi, gercek katilan sayisi mi (netlestirme acik).
- **Total Size (MB)** — limit mi, kullanilan alan mi (mevcut `calc-size` vb. ile hizala).
- **Storage Expiration Date** — `active_until + storage_days` (urun `options.storage_days`).

### 2.3.1 Event silme sonrasi upload analitik snapshot'i

Yeni musteri istegi: Event silindiginde bugunku soft delete davranisi korunur, ancak yuk bindiren upload medyalari artik hem veritabanindan hem de S3'ten temizlenir. Analitik veriler kaybolmamali. Bu nedenle medya silme isleminden **once** upload bazli son durum snapshot'i kalici olarak saklanir.

Kapsam bilerek **upload verileri** ile sinirlidir:

- Dahil: `uploads` tablosundaki `photo`, `video`, `voice`, `text` sayilari; `participants` guest sayisi; `uploads.client_uid` bazli contributor sayisi; ilk/son upload zamani; upload medyalarinin toplam MB bilgisi.
- Dahil degil: event banner (`events.image`), reklam/advertorial gorselleri (`advertorial_config`), QR logo assetleri, welcome/QR template assetleri. Bunlar upload analitigi sayilmaz.
- Silinecek agir icerik: `uploads.upload_type IN ('photo', 'video', 'voice')` kayitlari ve bu kayitlarin `uploads.value` path'lerindeki S3 objeleri.
- Kalabilecek hafif icerik: `text` upload'lar. Bunlar medya yukunu olusturmaz; istenirse guestbook/yorum analitigi icin DB'de kalabilir.

Uygulanan tablo:

```sql
CREATE TABLE event_upload_snapshots (
    uid UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_uid UUID NOT NULL REFERENCES events(uid) UNIQUE,
    captured_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    reason TEXT NOT NULL CHECK (reason IN ('manual_delete', 'storage_expired')),

    guest_count INT NOT NULL DEFAULT 0,
    contributor_count INT NOT NULL DEFAULT 0,
    upload_count_total INT NOT NULL DEFAULT 0,
    photo_count INT NOT NULL DEFAULT 0,
    video_count INT NOT NULL DEFAULT 0,
    voice_count INT NOT NULL DEFAULT 0,
    text_count INT NOT NULL DEFAULT 0,

    total_upload_size_mb DECIMAL(12, 2),
    first_upload_at TIMESTAMPTZ,
    last_upload_at TIMESTAMPTZ,

    media_paths JSONB NOT NULL DEFAULT '[]'::jsonb,
    purge_started_at TIMESTAMPTZ,
    purge_finished_at TIMESTAMPTZ,
    purge_error TEXT
);
```

`media_paths` alani retry icin kritik: DB'den upload satirlari silindikten sonra S3 silme yarida kalirsa hangi objelerin tekrar denenecegi kaybolmaz. Uygulamada hedef davranis "bir event icin tek final snapshot" oldugu icin `event_uid UNIQUE` secildi; manuel delete ve storage cron ayni event icin ikinci snapshot uretmez.

Snapshot sorgu mantigi:

```sql
SELECT
    COUNT(*) FILTER (WHERE upload_type = 'photo') AS photo_count,
    COUNT(*) FILTER (WHERE upload_type = 'video') AS video_count,
    COUNT(*) FILTER (WHERE upload_type = 'voice') AS voice_count,
    COUNT(*) FILTER (WHERE upload_type = 'text')  AS text_count,
    COUNT(*) AS upload_count_total,
    COUNT(DISTINCT client_uid) FILTER (WHERE trashed_at IS NULL) AS contributor_count,
    MIN(created_at) AS first_upload_at,
    MAX(created_at) AS last_upload_at
FROM uploads
WHERE event_uid = $1;
```

Guest sayisi:

```sql
SELECT COUNT(*)
FROM participants
WHERE event_uid = $1;
```

MB hesabi icin en dogru yol `events/{packedEventUid}` prefix'ini komple hesaplamak degil, `uploads.value` icindeki `photo/video/voice` path'lerini tek tek S3 `HeadObject`/size helper ile toplamak. Boylece QR, export zip veya baska event klasoru dosyalari upload toplam MB'ye karismaz.

Uygulanan ortak servis:

```go
PurgeUploadsAndSoftDeleteEvent(eventUID string, actorUserUID string, reason string) (*UploadSnapshot, error)
```

Bu servis iki yerden kullanilir:

- Manuel silme: `DELETE /event/{eventPackedUid}` -> `reason = "manual_delete"`; aktif edildi ve test edildi.
- Storage cron: `storage_until <= NOW()` eventleri -> `reason = "storage_expired"`; 24 saatte bir calisan cron yoluna baglandi.

Cron notu: `storage_cron.Init()` servis acilisinda bir kez `RunOnce(false)` calistirir, sonra `time.NewTicker(24 * time.Hour)` ile 24 saatte bir tekrar kontrol eder. Ayrica `POST /api/admin/run-storage-check` manuel fail-safe olarak durur. Cron medya purge flag'i aktif edildi; kontrol aninda `deleted_at IS NULL AND storage_until <= NOW()` event sayisi `0` oldugu icin anlik toplu etki beklenmedi.

Onerilen islem sirasi:

1. Event admin/cron uygunlugunu kontrol et; event zaten `deleted_at IS NOT NULL` ise snapshot/purge durumuna gore idempotent don.
2. `uploads` ve `participants` uzerinden snapshot metriklerini hesapla.
3. `photo/video/voice` upload path'lerini `media_paths` olarak snapshot'a yaz.
4. S3 boyutlarini toplayip `total_upload_size_mb` alanina yaz.
5. Event'i soft delete yap (`events.deleted_at = NOW()`).
6. `uploads` icinden `photo/video/voice` kayitlarini hard delete et.
7. `media_paths` listesindeki S3 objelerini `s3wrap.Public_s3.Rm(path)` ile sil.
8. Basariliyse `purge_finished_at` doldur; hata varsa `purge_error` yaz ve retry edilebilir durumda birak.

DB transaction sadece DB adimlarini garanti eder; S3 transaction'a dahil degildir. Bu yuzden snapshot/path kaydi silmeden once olusmali ve S3 hatalari kaybolmamalidir.

Uygulama ve test durumu:

- Yeni paket: `src/event_cleanup`.
- Snapshot fonksiyonu: `CreateEventUploadSnapshot(eventUID, reason)`.
- Ortak purge fonksiyonu: `PurgeUploadsAndSoftDeleteEvent(eventUID, actorUserUID, reason)`.
- Retry helper: `RetryFailedEventUploadPurges()`; henuz route/cron'a bagli degil, yalniz altyapi olarak hazir.
- S3 tekil boyut helper'i: `s3wrap.Calc_object_size(path)`; upload MB hesabi prefix yerine `uploads.value` path'leri uzerinden yapilir.
- Frontend sozlesmesi degismedi: `DELETE /event/{eventPackedUid}` response'u `success` ve `already_closed` alanlarini ayni sekilde dondurur.
- Order Account frontend sozlesmesi de degismedi: aktif eventlerde canli metrikler, purge edilmis soft-delete eventlerde `event_upload_snapshots` fallback'i ile ayni `order_account` alanlari doner.
- Manuel delete smoke test sonucu: snapshot olustu, `photo/video/voice` upload kayitlari DB'den silindi, `text` ve `participants` kaldi, S3 event klasorunde upload medyalari silindi; upload kapsami disinda oldugu icin `qr.png` kaldı.

### 2.4 Admin ekleme / cikarma

- Super admin: diger adminleri ekler/siler.
- Son super admin silinemez; audit log onerilir.
- Normal admin: siparis gorunumu disinda route’lara **403**.

---

## 3) QR Card Templates ve Welcome Boards

- **32 QR Card** + **32 Welcome Board**; tum tasarim asset'leri musteri tarafindan saglanir ve S3/CDN'de barindirilir.
- Tasarimlar frontend manifest yerine **DB'de `products.options.designs`** icinde tutulur. Her add-on urunun kendi `designs` listesi olur (`welcome_board`, `qr_card`).
- Checkout/order sayfasinda kullanici:
  - add-on'u secer,
  - `products.options.designs` listesinden bir tasarim secer,
  - `products.options.config_fields` alanlarina metin/tarih vb. bilgileri girer.
- Yeni karar: girilen bilgiler **artik gorsel uzerinde canli degismeyecek**. Yani `overlays` ile preview canvas/SVG/text bind etme hedef degil.
- Girilen bilgiler yine de kaybolmaz; satin alma isteginde `buyer_configs` ile backend'e gider ve ilgili `cart_items.buyer_config` icinde saklanir.
- Event sahibi daha sonra event sayfasinda satin aldigi add-on'a tikladiginda, secilen tasarim gorseli ve girilen bilgiler detay olarak gosterilir.
- `overlays` mevcut eski veriyle uyumluluk icin bir sure kalabilir ama yeni 32+32 importta bos birakilmali veya FE tarafinda yok sayilmalidir.

### `products.options` hedef semasi

Welcome Board ve QR Card add-on'lari icin `products.options` su modelde tutulur:

```json
{
  "image": "https://memboxpub-qo1gff2e.s3.eu-north-1.amazonaws.com/addon_banner/welcome_board.png",
  "designs": [
    {
      "id": "1",
      "label": "Design 1",
      "image": "https://memboxpub-qo1gff2e.s3.eu-north-1.amazonaws.com/templates/welcome_board/1.jpg"
    }
  ],
  "config_fields": [
    { "key": "name_text", "type": "textarea", "label": "Event Title?", "maxLength": 75 },
    { "key": "event_date", "type": "textarea", "label": "What is your event date?", "maxLength": 50 },
    { "key": "secondary_text", "type": "textarea", "label": "Welcome Message?", "maxLength": 100 },
    { "key": "footer_text", "type": "textarea", "label": "Footer Text?", "maxLength": 100 }
  ],
  "render_mode": "static_config_only"
}
```

QR Card icin ayni pattern kullanilir; sadece asset path'i ve gerekirse `config_fields` farkli olur. Ornek path:

- `templates/welcome_board/1.jpg` ... `templates/welcome_board/32.jpg`
- `templates/qr_card/1.jpg` ... `templates/qr_card/32.jpg`

### `buyer_config` hedef semasi

Checkout'tan gelen payload urun id'si bazinda `buyer_configs` map'ine yazilir. Ilgili add-on satiri icin beklenen JSON:

```json
{
  "design_id": "1",
  "name_text": "Зловив крутий момент?",
  "event_date": "December 17, 2026",
  "secondary_text": "Ділися фото тут",
  "footer_text": "Скануй та завантажуй фото"
}
```

Bu veri `cart_items.buyer_config` icinde saklanir. Event sayfasinda `GET /api/event/{eventPackedUID}/order` response'u zaten add-on satirlarinda `buyer_config` ve `product.options` dondurdugu icin FE, secilen `design_id` ile `product.options.designs` listesinden gorseli bulup detay ekraninda gosterebilir.

---

## 4) Yeni Guest Arayuz — Reklam Alani (Layout + Icerik)

### Is gereksinimi (ozet)

- Reklam alani **add-on** ile opsiyonel; **premium** pakette ek ucret olmadan ayni hak.
- Etkinlik sahibi, hak aktif olduktan sonra **ayarlar (settings)** ekraninda once **layout** secer:
  - **Tekli** — tek gorsel alani (tam genislik veya tek hucre; UI’da tek upload).
  - **1x1** — tek hucre grid (1 satir x 1 sutun).
  - **2x1** — iki hucre yan yana (2 sutun x 1 satir).
  - **1x2** — iki hucre alt alta (1 sutun x 2 satir).
  - **2x2** — dort hucre (2x2).
- Layout secildikten sonra ilgili hucrelere **gorsel** yuklenir; her hucre icin **link** alani **opsiyonel** (doluysa tiklaninca URL; bos ise sadece gorsel, tiklanabilir degil veya no-op — UX karari FE’de netlenir).
- **Guest** tarafinda secilen layout’a gore grid render edilir; hucrede gorsel yoksa bos/kompakt placeholder veya hic gosterme (tercih FE).

### Layout → hucre sayisi (referans)

| Layout | Hucre sayisi |
|--------|----------------|
| Tekli | 1 |
| 1x1 | 1 |
| 2x1 | 2 |
| 1x2 | 2 |
| 2x2 | 4 |

Teknik olarak **tekli** ile **1x1** ayni hucre sayisi; fark CSS/marketing (tekli = daha buyuk tek banner hissi) ise FE’de `layout` enum ile ayristirilir.

### Ozellik acma / kapama

- Yeni `granted_features` id (veya esdegeri): etkinlikte bu hak yoksa settings’te reklam bolumu **gorunmez** veya salt-okunur uyari; guest’te alan **hic acilmaz** — mevcut sayfalar bozulmaz.

**Backend / Frontend gorev ozeti:** Asagidaki [Tum Isler — Backend ve Frontend](#tum-isler--backend-ve-frontend-yapilacaklar) bolumunde **Reklam alani** altinda listelenmistir.

---

## 5) Etkinlik Collaborator — Davet ve Yetki (Tek Etkinlik)

### Mevcut sorun

- Etkinlige **collaborator** eklenince davet maili gidiyor; kabul sonrasi davet edilen kisi, **o kullaniciya (davet eden / hesap)** ait **tum etkinliklere** erisebiliyormus gibi davraniyor veya mail akisi tum etkinlikleri kapsiyor.
- **Istenen:** Davet ve yetki **yalnizca davet cikarilan etkinlik** (`event_uid`) ile sinirli olmali; baska etkinliklerde collaborator/admin hakki **olmamali**.

### Hedef davranis

1. Davet olusturulurken backend **hedef `event_uid`** kaydeder (token, DB satiri veya her ikisi).
2. Gonderilen maildeki link/token **bu etkinligi** tasir; kabul endpoint’i yalnizca bu kayit icin rol baglar.
3. `Is_events_admin` / event listesi / private API’ler collaborator icin **yalnizca yetkili olunan etkinliklerde** true doner.
4. Regresyon: Davet eden event owner’in diger etkinlikleri collaborator’a **gorunmez** ve erisilemez.

### Teknik iz (uygulama tarafinda netlestirilecek)

- DB: Collaborator iliskisi **`event_uid` + `collaborator_user_uid`** (veya pending email) satiri olarak tutulmali; kullanici bazli “tum eventler” genislemesi **olmayan** sorgu.
- Token: JWT veya imzali payload icinde **`event_uid`** zorunlu; kabulde baska event’e genelleme yapilmamali.
- Mevcut yanlis genis kayitlar icin: migration veya tek seferlik temizlik / audit (hangi satirlar tum event yetkisi vermis) — operasyon karari.

**Backend / Frontend gorev ozeti:** [Tum Isler — Backend ve Frontend](#tum-isler--backend-ve-frontend-yapilacaklar) altinda **Collaborator (tek etkinlik)** basliklari.

---

## Tum Isler — Backend ve Frontend Yapilacaklar

Asagida bu dokumandaki **tum kapsam** (promo, admin rolleri, siparis/promo raporu, order account, onceki analizdeki urun/landing, QR-Welcome, reklam alani, **collaborator tek-etkinlik duzeltmesi**) **backend** ve **frontend** olarak ayrilmistir. Mevcut **snapshot** (`cart_items.unit_price_snapshot`) ve odeme callback davranisi korunur; yeni isler **ek** endpoint/kolon ile gelir.

### Backend yapilacaklar

#### Promo code (order + odeme)

- `promo_codes` tablosu + MVP uygunluk (`promo_code_eligible_products` veya `eligible_product_uids`), seed/admin ile premium urun baglantisi.
- `purchases.promo_code_uid` (+ rapor icin onerilen `promo_code_text_snapshot`, `discount_amount`, `net_paid` vb.).
- Kod dogrulama servisi: tarih araligi, `usage_limit_total`, `per_user_limit`, `is_active`, premium sepet kontrolu.
- Checkout / cart hesap endpointlerinde indirim uygulama; odeme provider’a **net tutar** gitmesi.
- Odeme callback’te idempotent yazim (`usage_count`, promo FK); yarismali limit icin transaction / `UPDATE … RETURNING`.
- (Opsiyonel) Rate limit — kod deneme spam’i.

#### Admin rolleri ve siparis API

- Normal admin icin kimlik: DB rol + JWT claim; super admin icin mevcut env veya birlestirilmis model.
- Middleware: route bazli `super_admin` vs `admin` (siparis-only).
- `GET /api/admin/orders`, `GET /api/admin/orders/{uid}` (ve ilgili PATCH’ler): normal admin icin **fiyat/tutar/indirim satirlari filtrelenmis** response (sadece UI degil).
- Son super admin silinemez; admin CRUD endpointleri (super admin only).

#### Collaborator daveti (tek etkinlik — bug fix)

- Davet olusturma: istekte **`event_uid`** kaynagi dogrulanir; collaborator kaydi **event bazli** tabloya yazilir (mevcut sema genis kullaniciya bagliyorsa refactor).
- Mail linki / token: **tek etkinlik** tasiyan imza veya nonce; kabul (`accept`) endpoint’i yalnizca bu cift icin rol ekler.
- `Is_events_admin` ve tum event-collaborator sorgulari: **yalnizca ilgili `event_uid`** icin true; davet edenin diger eventleri dahil edilmez.
- Event listesi / dashboard: collaborator’a **yalnizca yetkili olunan** etkinlikler listelenir (yanlis genis liste varsa duzeltilir).
- Idempotency: ayni davet iki kez kabul edilmez; iptal/revoke akisi kontrol edilir.
- Veri duzeltme: prod’da yanlis genis yetki verilmis satirlar icin migration veya script (operasyon onayi ile).

#### Promo yonetimi ve rapor (super admin)

- `GET/POST/PATCH/DELETE` (veya soft delete) promo API’leri.
- Promo bazli satis ozeti endpoint’i (`purchases.promo_code_uid` gruplama; gross/net/discount sozlugu sabitlenmeli).

#### Order Account metrikleri

- Siparis veya event baglaminda aggregate endpoint: Total Guest, Total Size (MB), Storage Expiration (`active_until + storage_days`); mevcut `calc-size` / event verisi ile entegrasyon.
- Normal admin detayinda da kullanilacak sekilde **fiyatsiz** payload’a uyum.
- Event silme/storage expiration oncesi upload snapshot'i: guest, contributor, photo/video/voice/text sayilari, ilk/son upload zamani ve upload medya MB toplami kalici saklanir.
- Event silme/storage expiration sonrasi `photo/video/voice` upload kayitlari DB'den hard delete edilir; ayni path'ler S3'ten silinir. `text` upload'lar medya yuku olmadigi icin kalabilir.
- Cron ve manuel delete ayni ortak servisi kullanir. Manual delete aktif ve test edildi; cron storage expiration icin ayni servise baglandi ve 24 saatte bir calisir.
- Order detail `order_account` payload'i aktif eventte canli hesaplanir; event soft-delete/purge edildiyse ayni alanlar snapshot fallback'inden doldurulur, FE ekstra kaynaga gitmez.

#### Urun / paket ve sure modeli (onceki analiz ile hizali)

- `GET /api/admin/products`, `PATCH /api/admin/products/{productUid}` (yoksa tamamlama).
- Activation / storage gunleri event olusturmada dinamik kullanim (sabit +2 hafta kirilimi giderme — analizdeki karar).
- Isteniyorsa `products` display kolonlari (`display_name_en/uk`, …) + okuma fallback.

#### QR Card ve Welcome Board

- Statik tasarim dosyalari S3/CDN'e yuklenir: `templates/welcome_board/{id}.jpg`, `templates/qr_card/{id}.jpg`.
- `products.options.designs` iki add-on icin DB'de guncellenir: 32 Welcome Board + 32 QR Card kaydi.
- Yeni importta `overlays` kullanilmaz; eski options icinde varsa geriye donuk uyumluluk icin silinmeden FE tarafinda yok sayilabilir.
- `products.options.config_fields` form alanlarini tanimlamaya devam eder; bunlar gorsel uzerinde render icin degil, `buyer_config` toplamak icindir.
- `cart_items.buyer_config` kolonu canli DB'de yoksa migration eklenir (`JSONB NOT NULL DEFAULT '{}'` onerilir); mevcut kod bu alani insert/select/update ediyor.
- Checkout `POST /api/purchase` akisi `buyer_configs` map'ini kullanmaya devam eder; backend ek validation ile `design_id` ilgili `products.options.designs` icinde var mi, `config_fields.maxLength` asiliyor mu kontrol edebilir.
- Event add-on detayinda `GET /api/event/{eventPackedUID}/order` kullanilir; response zaten `buyer_config` + `product.options` dondurur. Eksikse sadece bu endpoint genisletilir, ayri template API gerekmez.
- Admin product update endpoint'i mevcut haliyle `options.designs` / `options.config_fields` guncellemiyor; 32+32 ilk import icin SQL seed/migration daha guvenli, panelden duzenleme istenirse `PATCH /api/admin/products/{productUid}` options allowlist'i genisletilir.

#### Reklam alani (layout + icerik)

- `events.advertorial_config JSONB` veya `event_advertorial_settings` + `event_advertorial_cells` tablolari.
- Layout enum validation; hucre indeksi; URL validation; goruntu MIME/boyut limiti.
- Upload: mevcut S3 upload ile uyum veya presigned URL.
- `GET /api/event/:packedUid/advertorial-public` (veya esdegeri): feature hakki kontrolu; yoksa bos/404.
- Event admin icin `GET/PATCH` (veya `PUT`) ayar endpoint’i; yetki mevcut event admin kurallari ile.

---

### Frontend yapilacaklar

#### Promo code (musteri order sayfasi)

- Promo Code input, Uygula, hata/basarili mesajlari.
- Indirim onizlemesi (ara toplam / net); mobil uyum.

#### Admin — roller ve siparisler

- Giris sonrasi rol bilgisine gore menu/route kisitlari.
- Normal admin: siparis listesi **fiyatsiz kolonlar**; satir tikinda detay drawer — operasyonel alanlar + Order Account metrikleri, **para alani yok**.
- Super admin: mevcut siparis UI (fiyatli) korunur veya genisletilir.

#### Collaborator (tek etkinlik)

- Davet gonderme UI: secili **etkinlik baglami** API’ye acikca gider (`event_uid`); kullanicinin diger etkinlikleri daveti etkilemez.
- Kabul sonrasi yonlendirme / mesaj: hangi etkinlige katildigi net gosterilir.
- Collaborator olarak girildiginde etkinlik listesi / sidebar **yalnizca yetkili olunan** etkinlikleri gosterir (backend ile uyumlu).

#### Admin — promo (super admin)

- Promo liste / olusturma / duzenleme / silme veya pasifleştirme; **indirim tipi (yuzde / sabit tutar)** + deger alani; limit ve tarih araligi alanlari.
- Promo bazli satis raporu ekrani (tablo, istenirse tarih filtresi).

#### Admin — kullanici yonetimi (super admin)

- Admin ekleme / cikarma UI.
- Super admin yeni normal admin hesabini olusturur: `email`, `name`, `password`, `confirm_password`, `role=order_admin`.
- Bu admin mevcut kullaniciya rol verme degil; `users` + `credentials` + `panel_admins` kayitlari backend tarafinda olusur.
- Role su an `order_admin` olarak sabitlenir; env super adminler bu ekrandan silinemez.

#### Order Account

- Siparis detayinda uc metrik widget’i (super + normal admin); normal admin’de finans satiri yok.

#### Urun / paket edit ve landing (onceki analiz)

- Admin product edit formu (10 alan); validation ve hata state.
- Landing / paket kartlari: API’den sayisal alanlar + display metinleri; `-1` Unlimited vb.

#### QR Card ve Welcome Board

- Checkout add-on formu `products.options.designs` listesinden tasarim dropdown/grid gosterir.
- `products.options.config_fields` alanlarindan dinamik form uretir; kullanici girdilerini `buyer_configs[product_id]` icine JSON olarak koyar.
- Gorsel preview sadece secilen tasarimin statik image'ini gosterir; `overlays` ile text/QR bind edilmez.
- Satin alma sonrasi event sayfasinda "My order / Add-ons" bolumunde satin alinan Welcome Board / QR Card satirlari tiklanabilir olur.
- Add-on detayinda secilen `design_id` ile `product.options.designs` icinden gorsel bulunur; altinda/yaninda `buyer_config` alanlari okunabilir formatta gosterilir.
- Eski kayitlarda `buyer_config` bos olabilir; FE bos state gostermeli ve gerekiyorsa mevcut `PATCH /api/event/{eventPackedUID}/order/items/{cartItemUID}` ile kullanicidan sonradan bilgi tamamlatmali.

#### Reklam alani

- Event settings: layout secici (tekli, 1x1, 2x1, 1x2, 2x2); dinamik hucre formlari.
- Hucre basina goruntu yukleme, onizleme, kaldirma; opsiyonel link URL.
- Guest sayfa: grid layout; responsive; harici link `rel="noopener noreferrer"`; hak yoksa hic render etmeme.
- Yukleniyor / bos hucre / hata durumlari.

---

## Oncelik Sirasi — Nasil Ilerlenmeli?

**Ilkeler**

1. **Once guvenlik ve izolasyon:** Collaborator bug’i ve admin rol ayrimi, yanlis erisim ve fiyat sizintisini onler; promo/reklam bunun ustune biner.
2. **Para akisi tek zincir:** Promo, checkout ve callback **ardisik** ve **testli** tamamlanmali; yarim birakilan promo odemeyi kirilir.
3. **Geriye donuk:** Her faz sonunda mevcut checkout, siparis listesi ve guest **smoke** ([Geriye Donuk Uyumluluk](#geriye-donuk-uyumluluk--aktif-sistem-bozulmayacak)).
4. **Tek gelistirici:** Paralel hat yok; sirayi **A → B → C → D → E → F → G** olarak **tek tek** kapat. Bir fazda hem BE hem FE’yi bitirip smoke al; yarida buyuk baglam degistirme (promo + odeme zinciri yarim kalmasin).

**Ozet faz sirasi**

| Faz | Konu | Neden bu sirada? |
|-----|------|------------------|
| **A** | Collaborator tek etkinlik | Yetki bug’i; diger event API testleri bundan sonra anlamli |
| **B** | Roller + orders (fiyatsiz normal admin) + admin ekle/cikar | Panel temeli; promo raporu ve order account UI ayni kimlik modeline bagli |
| **C** | Promo DB + checkout + callback + promo UI + satis raporu | Odeme ile bitmeli; rapor `purchases.promo_code_uid` yazimindan sonra |
| **D** | Order Account metrikleri | Siparis/event verisi + roller hazir; promo ile bagimsiz ama detayda yan yana |
| **E** | Reklam BE → FE | Feature flag + upload; odemeden bagimsiz ama efor buyuk |
| **F** | QR / Welcome add-on template importu | En az bagimlilik; checkout ve event add-on detay akisi uzerine eklenir |
| **G** | Regression + audit | Ozellikle collaborator prod temizligi karari |

**Urun PATCH / landing (onceki analiz):** Bu dokumanin ana zincirinden ayri; **tek gelistirici** icin oneri: **Faz C (promo + odeme) kapandiktan sonra** veya musteri acil istiyorsa **kisa bir ara sprint** olarak ara (promo ile **aynı anda iki buyuk baglam** acmayi minimize et).

---

## Sirali Todo Listesi (Checklist)

Asagidaki liste **onerilen uygulama sirasi**dir; tick box’lari takip icin kullanilabilir. **Tek kisi:** Her faz icin once backend + sonra frontend (veya ince dilimler halinde) tamamlayip deploy/smoke; fazlari ust uste “yarim” birakma.

### Faz A — Collaborator (tek etkinlik) — **tamamlandi**

- [x] **A1.** Mevcut davet/kabul akisini ve DB semasini analiz et (`event_uid` nerede kayboluyor?)
- [x] **A2.** Davet + kabul: token/imzada **hedef `event_uid`** zorunlu; kayit **event bazli** satir
- [x] **A3.** `Is_events_admin` ve event listesi sorgularini gozden gecir; collaborator yalniz yetkili event’lerde true
- [x] **A4.** Mail icerigi ve kabul linkinin tek etkinligi net gosterdigini dogrula
- [x] **A5.** FE: davet gonderirken secili event baglami API’ye gidiyor mu?
- [x] **A6.** Test: B’ye davet → A event’ine erisim **yok**; olumlu/negatif
- [x] **A7.** (Opsiyonel) Prod audit / migration: yanlis genis yetkileri daralt

### Faz B — Admin rolleri ve siparisler

- [x] **B1.** DB `panel_admins` rol tablosu: `super_admin` | `order_admin`; helper fonksiyonlar
- [x] **B2.** Super admin: mevcut env listesi korunur + DB `super_admin` fallback hazir
- [x] **B3.** Middleware: route bazli super vs normal admin
- [x] **B4.** `GET /api/admin/orders*`: normal admin response’ta **fiyat/tutar/indirim** yok; PATCH/waybill super-only kalir
- [x] **B5.** FE sozlesmesi net: normal admin liste + detay drawer; carrier/tracking gorur ama editleyemez
- [x] **B6.** Super admin: **admin ekle / cikar** API tamam; yeni admin hesabi `users` + `credentials` + `panel_admins` ile olusur; FE bekliyor
- [x] **B7.** Guvenlik kuralı net: normal admin fiyat endpoint/alanlarini gormez; `PATCH`/waybill/products 403; manuel smoke test FE entegrasyonunda yapilacak

### Faz C — Promo kod — **tamamlandi**

- [x] **C1.** Migration: `promo_codes` tablosu + `purchases` promo snapshot alanlari (`promo_code_uid`, `promo_code_text_snapshot`, `gross_total`, `discount_amount`, `net_total`) eklendi.
- [x] **C2.** Promo modeli net: `discount_type` alani tutulur ama MVP'de yalniz `percent`; `discount_value=10` %10 anlamina gelir. Kisi basi limit yok. `valid_from` tutulur ve admin bos birakirsa otomatik `CURRENT_TIMESTAMP` alir; `valid_until` ve `usage_limit_total` alanlari opsiyoneldir.
- [x] **C3.** Promo cron tamam: mevcut cron stilinde ayri 5 dk ticker; suresi gecen veya toplam limiti dolan kodlari `is_active=false`, `deactivated_reason=expired|usage_limit_reached` yapar. Manuel DB testleri ile `expired`, `usage_limit_reached` ve aktif kalmasi gereken promo senaryolari dogrulandi.
- [x] **C4.** Kod dogrulama servisi tamam: aktiflik, tarih, toplam limit, `discount_type=percent`, `discount_value`, sepette `premium` kontrolu.
- [x] **C5.** Checkout/apply akisi indirim onizlemesi dondurur; indirim yalniz `premium` urun satirindan hesaplanir, add-on fiyatlarina dokunulmaz.
- [x] **C6.** `POST /api/purchase` promo snapshotlarini yazar ve odeme provider'a `net_total` gonderir; purchase aninda promo tekrar validate edilir.
- [x] **C7.** Payment callback basarili ilk islemde `usage_count` artirir; duplicate callback veya failed payment usage count'u tekrar/artirmaz. Dev simulate success akisi da test odemeleri icin ayni idempotent usage artisini yapar.
- [x] **C8.** Super admin promo **CRUD** API + FE tamam: kod, yuzde oran, toplam limit, tarih araligi, aktif/pasif, soft delete desteklenir.
- [x] **C9.** Promo **satis raporu** API + FE tamam; satislar `purchases.promo_code_uid` ve snapshot toplamlarindan raporlanir.
- [x] **C10.** Uc uca test tamam: premium yoksa red, premium varsa yalniz premiumdan indirim, add-on fiyatlari degismez, cron/limit/callback idempotency dogru.

Not: Promo indirimi sadece `products.id = 'premium'` satirina uygulanir. Add-on urunler indirime girmez. Promo kod metni kullanici girisinde `trim + uppercase` normalize edilir; DB'de `UPPER(code)` unique mantigi kullanilir. `valid_from` zorunludur; admin bos birakirsa DB otomatik bugunu yazar, admin tarih secerse secilen tarih kullanilir. `valid_until IS NULL` ise bitis siniri yoktur, `usage_limit_total IS NULL` ise toplam kullanim limiti yoktur. `is_expired` kolonu tutulmaz; otomatik pasiflesme `is_active=false`, `deactivated_at`, `deactivated_reason` ile kaydedilir.

Gelecek opsiyon: Promo kapsamı ileride genisletilmek istenirse mevcut purchase snapshot yapisi buna uygundur. `promo_codes` tarafina `discount_scope` (`premium`, `all`, `selected`) ve gerekirse `eligible_product_ids TEXT[]` eklenebilir. Bu durumda `premium` yalniz premium satirini, `all` tum sepeti, `selected` ise secili urunleri discount base olarak kullanir. Mevcut `purchases.gross_total`, `discount_amount`, `net_total`, `promo_code_uid`, `promo_code_text_snapshot` alanlari eski satislari korumaya devam eder. Ileride urun bazli indirim detayi raporlanmak istenirse opsiyonel `promo_discount_breakdown JSONB` snapshot alani eklenebilir.

### Faz D — Order Account — **tamamlandi**

- [x] **D1.** Total Guest / Total Size / Storage Expiration tanimlarini kilitle (limit mi kullanim mi)
- [x] **D2.** Aggregate endpoint (event veya purchase baglami); `storage_expiration = active_until + storage_days`
- [x] **D3.** FE: siparis detayinda uc metrik (super admin + normal admin; normalda fiyat yok)
- [x] **D4.** Event silme/storage expiration oncesi upload snapshot tablosu + ortak purge servisi: upload MB, photo/video/voice/text, guest/contributor, ilk/son upload zamani saklanir; medya DB ve S3'ten temizlenir. Manual delete smoke test gecti; cron purge yolu aktif; order detail snapshot fallback'i eklendi.

### Faz E — Reklam alani — **tamamlandi**

- [x] **E1.** `granted_features` / add-on + premium eslemesi (urun tarafi)
- [x] **E2.** BE: `advertorial_config` veya tablolar + validation + upload (S3)
- [x] **E3.** BE: event admin `GET/PATCH` ayar + guest **public GET** (feature kontrolu)
- [x] **E4.** FE: settings layout + hucre gorsel/link
- [x] **E5.** FE: guest grid + responsive + XSS-safe link

Not: Reklam hakki `FeatureAdvertorial = 4` ile `granted_features` uzerinden verilir. `advertorial` add-on urunu bu feature'i grant eder; `premium` paket de ayni feature'i ucretsiz verir. Event ayari `events.advertorial_config` icinde tutulur. Layout seceneklerine `none` dahildir; `none` secilirse guest sayfasinda reklam alani render edilmez ve yer ayrilmaz.

### Faz F — QR ve Welcome Board — **tamamlandi**

- [x] **F1.** 32 Welcome Board + 32 QR Card asset'ini S3'e yukle; path ve dosya adlarini sabitle (`templates/welcome_board/{id}.jpg`, `templates/qr_card/{id}.jpg`)
- [x] **F2.** DB seed/migration: ilgili add-on `products.options.designs` listelerini 32+32 kayitla guncelle; `overlays` yeni importta kullanma
- [x] **F3.** DB kontrol: `cart_items.buyer_config` kolonu yoksa migration ekle (`JSONB NOT NULL DEFAULT '{}'`); eski satirlara default uygula
- [x] **F4.** BE: gerekirse `POST /api/purchase` icin `buyer_configs` validation ekle (`design_id` var mi, `config_fields.maxLength` asildi mi)
- [x] **F5.** BE: `GET /api/event/{eventPackedUID}/order` add-on response'unda `buyer_config` + `product.options.designs/config_fields` eksiksiz donuyor mu dogrula
- [x] **F6.** FE checkout: tasarim secimi + config form; gorsel uzerine text/QR bind etmeden statik preview
- [x] **F7.** FE event sayfasi: satin alinan add-on'a tiklayinca secilen tasarim ve girilen bilgileri detayda goster
- [x] **F8.** Uc uca test: add-on sec → form doldur → odeme → event add-on detayinda ayni bilgiler gorunur

### Faz G — Kapanis ve kalite

- [ ] **G1.** Tam regression smoke (checkout, orders iki rol, guest, collaborator)
- [ ] **G2.** Dokumantasyon: yeni endpoint ozeti, bilinen limitler

---

## Bagimliliklar ve Siralama (Kisa Referans)

1. **Collaborator tek-etkinlik** duzeltmesi (davet token + DB + `Is_events_admin`) — diger event API’leri ile capraz test  
2. Rol modeli (DB + JWT) + orders API **role-aware** response  
3. `promo_codes` + `purchases` promo snapshot alanlari; MVP'de yuzde indirim ve sadece premium satirina uygulanir  
4. Promo cron + checkout validate: premium satiri + tarih + toplam limit; kisi basi limit yok  
5. Super admin: promo UI + rapor  
6. Order Account metrik endpoint + UI (normal admin detayda fiyatsiz)  
7. Reklam: backend config + upload + public endpoint; sonra FE settings + guest grid  
8. QR/Welcome add-on template importu + `buyer_config` detay gosterimi  

---

## Risk ve Test

- **Regresyon:** Her sprint sonunda “eski etkinlik / eski siparis / promo’suz checkout” senaryolari calisir durumda kalmali ([Geriye Donuk Uyumluluk](#geriye-donuk-uyumluluk--aktif-sistem-bozulmayacak)).
- Normal admin: **fiyat sizintisi** yok — her endpoint test (list, detail, export varsa).  
- Promo: premium disi sepette kod reddi; gelecekte coklu urun uygunlugu.  
- Promo limit yarisi: transaction / `UPDATE ... RETURNING`.  
- Callback idempotency: `usage_count` cift artmamali.
- Reklam: XSS (link URL), dosya tipi/boyut, public endpoint’te **sadece ilgili event** verisi.
- QR/Welcome: `design_id` DB'deki `products.options.designs` disinda olursa reddedilmeli veya FE'de secilememeli; maxLength kontrolu FE + BE'de tutarli olmali.
- QR/Welcome: Yeni akista girilen text gorsel uzerine basilmadigi icin musteri beklentisi net yazilmali; bilgiler yalniz event add-on detayinda gosterilir.
- Collaborator: Davet sonrasi **B etkinliginde** yetki varken **A etkinligine** erisim olmadigini dogrula (olumlu/negatif test).
- Event media purge: DB hard delete ile S3 delete atomik degil; `media_paths` snapshot/job kaydi olmadan upload satirlari silinmemeli. S3 silme yarida kalirsa retry edilebilir olmali.
- Event media purge: MB hesabi prefix bazli degil, `uploads.value` path'leri bazli yapilmali; aksi halde QR/export gibi upload disi dosyalar toplam MB'ye karisabilir.
- Event media purge smoke: Manuel delete testinde snapshot + DB medya silme + S3 medya silme dogrulandi; S3'te sadece upload disi `qr.png` kalmasi beklenen davranistir.
- Cron media purge: Cron servis acilisinda bir kez, sonra 24 saatte bir calisir. Acmadan once expired open event count `0` dogrulandi; bundan sonra storage suresi dolan eventlerde ayni purge servisi calisir.
- Order Account fallback: Silinmis/purge edilmis eventlerde detail ekranindaki Total Guest / Total Size / Storage Expiration alanlari snapshot'tan beslenmeli; aktif eventlerde canli hesap kullanilmaya devam eder.

---

## Sure Bandi (Referans)

| Paket | Not |
|-------|-----|
| Rol + orders API filtresi | +1-2.5 gun (onceki “admin add” bandina ek) |
| Promo (premium-scope + CRUD + checkout) | ~4-7 gun |
| Order Account | ~1.5-2.5 gun |
| Upload snapshot + media purge | ~1-2 gun |
| Reklam (layout + hucre + upload + guest grid) | ~3-6 gun (FE+BE) |
| QR + Welcome template importu + event detay gosterimi | ~1.5-3.5 gun |
| Collaborator tek-etkinlik (BE + FE + mail/token) | ~1-3 gun (semaya bagli) |
| Entegrasyon buffer | ~2-3 gun |

---

## Acik Netlestirmeler

- “Premium” teknik karsiligi: hangi `products.uid` / `product_id`?
- Normal admin detayda **hangi alanlar** kesin (or. sadece fiziksel kargo, digital durum)?
- Total Guest / Total Size tanimlari (limit vs gercek)
- **Tekli** vs **1x1**: goruntuleme boyutu/CSS farki mi, yoksa urun olarak tek tip mi?
- Linksiz gorsele tiklaninca: hic aksiyon mu, buyutme (lightbox) mi?
- QR/Welcome: 32 asset'in final dosya adlari ve siralamasi kesin mi (`1.jpg`...`32.jpg`)?
- QR/Welcome: QR Card ve Welcome Board icin `config_fields` ayni mi, yoksa QR Card'da farkli alanlar olacak mi?
- QR/Welcome: `cart_items.buyer_config` canli DB'de mevcut mu; yoksa once migration calistirilacak mi?
- Collaborator: Prod’da yanlis genis yetki verilmis kullanicilar **otomatik daraltilsin mi**, yoksa once audit mi?

---

*Guncelleme: tek gelistirici senaryosu — paralel hat kaldirildi; linear A→G ve urun PATCH icin tek kisi notu.*

