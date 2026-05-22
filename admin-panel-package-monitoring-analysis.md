# Admin Panel Paket ve Event Monitoring Analizi (Guncel Kapsam)

## Faz 1 Deliverables (Tek Sayfa Ozet)

- Kapsam: Sadece **Package/Add-On Edit** (Monitoring Faz 2'ye alindi)
- Tahmini sure: **2 is gunu**
- Backend deliverables:
  - `PATCH /api/admin/products/{productUid}` (tek endpoint, package ve add-on update)
  - Gerekirse admin listeleme icin `GET /api/admin/products` (veya MVP'de mevcut `GET /api/products`)
- Editlenecek alanlar:
  - Package Name
  - Package Price
  - Package Description
  - Number of Guests
  - Number of Pictures/Videos
  - Activation Period (default korunur, degistirilebilir)
  - Storage Period (default korunur, degistirilebilir)
  - Voice message included (Yes/No)
  - Add-On Package Name
  - Add-On Package Price
- Karar (sistemi bozmadan):
  - Yukaridaki 10 alanin tamami editlenebilir olacak.
  - Degisiklikler yalnizca urun tanim seviyesinde uygulanacak; mevcut event/purchase kayitlari geriye donuk kirilmayacak.
  - Mevcut default akis korunacak; sadece admin override yetkisi eklenecek.
  - Kritik kural: `product_id` (teknik urun kimligi) degismeyecek; sadece ekranda gorunen isim/metin guncellenecek.
- Frontend deliverables:
  - Package/Add-On edit ekrani (sade ama sik)
  - Save/Cancel, temel validation, success/error state
- Faz 2 notu:
  - Event list + monitoring detay endpoint/UI kapsam disi (sonraki faz)
  - Storage expiration hesap kurali (Faz 2): `storage_expiration = active_until + storage_days`
  - Faz 1'de veri temizleme/retention mekanizmasi degistirilmeyecek

## Kesinlesmis Kapsam

### Faz 1 Scope Freeze (Sadece Edit)

Bu fazda sadece urun guncelleme yapilacak.  
**Monitoring/event listeleme kapsam disi** ve sonraki faza alindi.

### 1) Admin tarafinda editlenecek alanlar

Admin, **Package** ve **Add-On** urunlerde asagidaki alanlari guncelleyebilecek:

- Package Name
- Package Price
- Package Description
- Number of Guests
- Number of Pictures/Videos
- Activation Period (admin belirleyecek, default 2 hafta ama degistirilebilir)
- Storage Period (admin belirleyecek, degistirilebilir)
- Voice message included (Yes/No)
- Add-On Package Name
- Add-On Package Price

Not: Bu alanlarin mevcut varsayilan degerleri korunur; admin panelden sonradan degistirilebilir hale gelir. Uygulama geriye donuk uyumlu olacak sekilde yapilacaktir (breaking degisiklik yok). 

### 2) Admin tarafinda event monitoring akisi (Faz 2 - Sonraya Alindi)

Bu bolum Faz 2'de ele alinacak, Faz 1 teslim kapsaminda yoktur.

1. Event listesi (sonraki faz)
2. Event detay monitoring (sonraki faz)

---

## Mevcut Yapiya Gore Teknik Durum

- `products` tablosu uzerinden katalog verisi okunuyor (`id`, `price`, `options`, `is_add_on`, `granted_features`, `is_enabled`).
- Su an `products` icin admin update endpointi yok.
- Event tarafinda `activation_date` ve `active_until` var.
- Mevcut DB kuralinda `active_until` sabit olarak `activation_date + 2 hafta` mantigina bagli.
- Toplam boyut (`size_mb`) icin endpoint var, ancak monitoring verisi tek endpointte toplanmis degil.

Sonuc: Bu kapsam sadece UI degisikligi degil; backend tarafinda yeni endpointler ve sure kuralini dinamiklestirme gerekiyor.

---

## Yapilacak Isler

## Backend

### A) Paket / Add-On Yonetimi

- `GET /api/admin/products`
  - Package ve Add-On urunleri admin icin listeler.
- `PATCH /api/admin/products/{productUid}`
  - Yukaridaki tum alanlari gunceller.
  - Validation:
    - `price >= 0`
    - `guest/media/activation/storage` sayisal deger
    - `voice` alanini `granted_features` ile eslestirme

### B) Event List + Monitoring (Faz 2 - Sonraya Alindi)

- Faz 1 kapsaminda uygulanmayacak.
- Faz 2'de eklenecek endpointler:
  - `GET /api/admin/events`
  - `GET /api/admin/events/{eventUid}/monitoring`

### C) Activation/Storage sure modeli

Kritik nokta:

- Artik sureler admin tarafindan degisecek.
- Bu nedenle mevcut sabit `+2 hafta` mantigi dinamik sure modeline cekilmeli.
- Default deger korunur (`2 hafta`), ama admin degistirebilir.

Not: Bu degisiklik backend tarih kuralini etkiledigi icin eforu yukselten ana kalem budur.

---

## Frontend (Admin Panel)

- Product management ekrani:
  - Package/Add-On liste
  - Tum alanlar icin edit formu
  - Save, validation, hata state
- Event monitoring ekranlari Faz 2'ye alindi.
- Tasarim:
  - Sade ama sik (MVP odakli)

---

## Yapi Degisikligi Olur mu?

Kisa cevap:

- Major mimari kirilim beklenmez.
- Ancak bu kapsamda **backendde kural seviyesinde degisiklik** var:
  - Activation ve storage sureleri dinamiklestirilecek.

Yani "sadece endpoint ekleme" degil; tarih/sure mantigi da guncellenecek.

---

## Tahmini Sure (Guncel)

Bu dokumandaki **Faz 1 (sadece edit)** kapsamina gore:

- Backend + Frontend birlikte: **1.5-2.5 is gunu**
- Guvenli iletisim/taahhut: **2 is gunu**

Yoneticiye iletilecek net cumle:

**"Bu fazda sadece package/add-on edit kapsamindayiz; 2 is gununde teslim edilir. Monitoring kapsamı sonraki fazda ele alinacaktir."**

---

## Onerilen Uygulama Sirasi

1. API contract ve field isimlerini kilitle
2. Product admin list + update endpointlerini tamamla
3. Activation/storage sure kurallarini dinamik hale getir
4. Admin panelde package/add-on edit ekranlarini bagla
5. Uc uca test ve son duzeltmeler

---

## 2 Gunluk Hizlandirilmis Uygulama Plani (6 Saat + 6 Saat, Faz 1 Edit-Only)

Bu plan, **sadece edit kapsamı** icin agresif ama uygulanabilir bir plandir.
Temel kosul: kapsam freeze (yeni madde eklenmemesi).

### Gun 1 (6 saat) - Backend agirlikli, UI baglantisina hazir API

#### Saat 0:00 - 0:45
- Kapsam ve field contract kilitleme
- Tek payload standardi:
  - product update payload
  - event list response
  - event monitoring response 

#### Saat 0:45 - 2:15
- `GET /api/admin/products`
- `PATCH /api/admin/products/{productUid}`
  - name, price, guest/media, activation/storage, voice, add-on alanlari
  - validation + yetki kontrolu

#### Saat 2:15 - 3:15
- Activation/storage sure modelini backendde dinamiklestirme
- Default degerlerin korunmasi (ornek: activation 14 gun)

#### Saat 3:15 - 4:15
#### Saat 3:15 - 4:15
- Product update is kurallarinin tamamlanmasi
  - activation/storage period guncelleme
  - voice feature map dogrulamasi

#### Saat 4:15 - 5:15
- Backend smoke testleri
  - package update
  - add-on name/price update

#### Saat 5:15 - 6:00
- Smoke test + response kontrolu
- Frontend baglantisi icin ornek response dokumu

### Gun 2 (6 saat) - Frontend baglama, uc uca test, teslim

#### Saat 0:00 - 1:30
- Product edit UI (Package + Add-On)
- Tum alanlar icin form ve save akisi

#### Saat 1:30 - 2:30
#### Saat 1:30 - 2:30
- Add-on edit UI
  - isim/fiyat guncelleme

#### Saat 2:30 - 3:30
- Form UX polish
  - sade ama sik duzen
  - success/error state

#### Saat 3:30 - 4:30
- API entegrasyonu ve hata state'leri
- Tarih/sayi formatlama (MB, expiration)

#### Saat 4:30 - 5:30
- Uc uca senaryo testleri:
  - package edit
  - add-on edit
  - activation/storage sure degisimi

#### Saat 5:30 - 6:00
- Son bugfix turu
- Teslim checklist:
  - endpoint listesi
  - ekran listesi
  - bilinen limitler/notlar

### 2 Gun Icinde Basari Icin Kurallar

- Scope freeze: yeni istek alinmaz
- UI MVP kalir: monitoring ekranlari sonraki faza
- Validation "yeterli" seviyede tutulur, asiri hardening ertelenir
- Kararsiz kalan noktalar ayni gun icinde kapanir

---

## Son Netlesmeler (Frontend + Backend)

### 1) Mevcut STORAGE EXPIRATION hesabi (Faz 2 notu)

- Frontendde Event detail ekraninda su an:
  - `STORAGE EXPIRATION = event.active_until + 14 gun`
- Bu hesap sabit oldugu icin yeni gereksinimi karsilamiyor.
- Yapilacak degisiklik:
  - Bu sabit `+14 gun` mantigi kaldirilacak.
  - Storage expiration, paket bazli `storage_days` degerine gore hesaplanacak.

### 2) Event suresi default 14 gun kaynagi

- Default 14 gun mantigi frontendden degil, backend/DB tarafindan geliyor.
- Mevcut kural:
  - `active_until = activation_date + 2 hafta`
- Bu nedenle event suresini dinamik yapmak icin backend kurali guncellenmelidir.

### 3) Paket options verisi

- Paket options icinde `storage_days` hali hazirda mevcut: 
  - ornek: `30`, `90`, `180`
- Bu veri mevcutta retention/expiration icin tutarli sekilde source of truth olarak kullanilmiyor.
- Yapilacak:
  - Admin bu degeri `PATCH /api/admin/products/{productUid}` endpointi ile duzenleyecek.
  - Faz 1'de sadece editlenecek.
  - Faz 2'de monitoring endpointi bu guncel degeri okuyup hesaplanan expiration sonucunu gosterecek.
  - Hesap kurali: `storage_expiration = active_until + storage_days`

---

## Backend Prompt (Uygulama Talimati)

Asagidaki kapsam icin backend implementasyonu yap:

1. Admin urun yonetimi
- `GET /api/admin/products`
- `PATCH /api/admin/products/{productUid}`
- Asagidaki alanlar update edilebilir olmali:
  - package_name
  - package_price
  - guest_count
  - media_count
  - activation_period_days
  - storage_period_days
  - voice_included (granted_features map)
  - add_on_name
  - add_on_price

2. Admin event listeleme ve monitoring
- `GET /api/admin/events`
  - event name
  - owner/buyer info
  - created_at (varsa status)
- `GET /api/admin/events/{eventUid}/monitoring`
  - package_type
  - total_guest_number
  - photo_number
  - video_number
  - text_message_number
  - total_size_mb
  - activation_expiration_date
  - storage_expiration_date

3. Is kurallari
- Mevcut varsayilanlar korunacak.
- Activation/storage period admin tarafindan degistirilebilir olacak.
- Sistem expiration tarihlerini period degerlerinden hesaplayacak.
- Frontenddeki sabit `storage = active_until + 14 gun` varsayimina bagli kalinmayacak.

4. Guvenlik ve kalite
- Sadece admin/super admin yazma endpointlerine erisebilsin.
- Temel validation zorunlu:
  - sayisal alanlar integer ve mantikli aralikta
  - price negatif olamaz
- API response formati tutarli olsun.
- Smoke test senaryolari gecsin.

5. Sinirlar
- Kapsam disi yeni ozellik ekleme.
- Gereksiz refactor yapma.
- Kod degisikliklerini mevcut mimariye uygun ve minimum etkili yap.

---

## Frontend Prompt (Uygulama Talimati)

Asagidaki kapsam icin admin panel implementasyonu yap:

1. Product management ekrani
- Package ve Add-On urunlerini listele.
- Tum alanlar editable olsun:
  - Package Name
  - Package Price
  - Number of Guests
  - Number of Pictures/Videos
  - Activation Period (days)
  - Storage Period (days)
  - Voice message included (Yes/No)
  - Add-On Name
  - Add-On Price
- Save/Cancel akisi ve temel form validation olsun.

2. Event monitoring akisi
- Event listesi sayfasi:
  - event name
  - owner/buyer
  - olusturulma tarihi
- Event detay sayfasi:
  - Package Type
  - Total Guest Number
  - Photo Number
  - Video Number
  - Text Message Number
  - Total Size (MB)
  - Activation Expiration Date
  - Storage Expiration Date

3. Is kurallari
- Varsayilan degerler ekranda gorunur ama admin tarafindan degistirilebilir.
- Expiration tarihleri backend response'undan alinacak.
- Frontendde hardcoded `+14 gun` storage hesabi kullanilmayacak.

4. UX beklentisi
- Sade ama sik arayuz.
- Hata durumlari ve loading state'leri duzgun gosterilsin.
- Mobil/desktop temel uyumluluk korunsun.

5. Sinirlar
- Kapsam disi ek ozellik (gelismis filtre/export) ekleme.
- Once calisan MVP, sonra ufak polish.

---

## Faz 1 API Contract (Frontend Entegrasyon Icin)

Bu bolum Faz 1 (sadece edit) icin kullanilacak endpoint sozlesmesini tanimlar.

### 1) Admin urun listeleme

- Endpoint: `GET /api/admin/products`
- Yetki: Super admin
- Amaç: Admin panelde package/add-on satirlarini doldurmak

Ornek response (tek urun):

```json
{
  "uid": "9a9d5c9b-2f2d-4a3a-8d48-0f88f6f2b111",
  "id": "core_package_basic",
  "price": "799.00",
  "options": {
    "guest_count": 20,
    "media_count": 500,
    "activation_days": 14,
    "storage_days": 30
  },
  "priority": "100",
  "fullfillment_type": "digital",
  "granted_features": [1, 3],
  "is_add_on": false,
  "is_enabled": true
}
```

### 2) Admin urun guncelleme

- Endpoint: `PATCH /api/admin/products/{productUID}`
- Yetki: Super admin
- Amaç: Package ve Add-On alanlarini guncellemek

Onerilen standart request alanlari:

- `name`
- `price`
- `guest_count`
- `media_count`
- `activation_period_days`
- `storage_period_days`
- `voice_included`

Not:

- Add-On guncellemesi de ayni endpointten yapilir.
- Add-On icin pratikte sadece `name` ve `price` gondermek yeterlidir.
- Geriye donuk uyumluluk icin alias alanlar da kabul edilir (`package_name`, `add_on_name`, `package_price`, `add_on_price`, `activation_days`, `storage_days`).

Ornek request (package):

```json
{
  "name": "core_package_plus",
  "price": 1199,
  "guest_count": 100,
  "media_count": -1,
  "activation_period_days": 21,
  "storage_period_days": 90,
  "voice_included": true
}
```

Ornek request (add-on):

```json
{
  "name": "extra_storage_90d",
  "price": 199
}
```

Ornek response:

```json
{
  "uid": "9a9d5c9b-2f2d-4a3a-8d48-0f88f6f2b111",
  "id": "core_package_plus",
  "price": "1199",
  "options": {
    "guest_count": 100,
    "media_count": -1,
    "activation_days": 21,
    "storage_days": 90
  },
  "priority": "100",
  "fullfillment_type": "digital",
  "granted_features": [1, 3],
  "is_add_on": false,
  "is_enabled": true
}
```

### 3) Validation kurallari (Faz 1)

- `price >= 0`
- `guest_count >= -1`
- `media_count >= -1`
- `activation_period_days > 0`
- `storage_period_days > 0`
- `voice_included` boolean olmalidir

---

## Kritik Ek Madde: Orders Sayfasinda Gecmis Siparis Fiyati Drift Sorunu

### Problem tanimi (su anki durum)

Orders ekraninda satir ve toplam tutar, canli `products.price` degerinden hesaplaniyor.  
Bu nedenle bir urunun fiyati siparis sonrasinda degistiginde, gecmis siparisler de yeni fiyatla gorunuyor.

- Teknik kok neden:
  - `cart_items` tablosunda urunun satin alma anindaki birim fiyatini saklayan alan yok.
  - `GET /api/admin/orders` sorgusu toplami `SUM(pr.price * ci.quantity)` ile canli urun fiyatindan uretiyor.
  - `GET /api/admin/orders/{purchaseUID}` item fiyatini `pr.price` uzerinden donuyor.

### Is etkisi

- Finansal raporlama tutarsizlasir (gecmis satis tutarlari degisir).
- Refund/chargeback sureclerinde anlasmazlik riski artar.
- Operasyon ve destek ekipleri ayni sipariste farkli donemlerde farkli tutar gorebilir.

### Sistemi bozmadan cozum (onerilen)

En dusuk riskli yaklasim: **fiyat snapshot** modeline gecmek.

1. `cart_items` tablosuna geriye donuk uyumlu alanlar eklenir:
   - `unit_price_snapshot DECIMAL(10,2) NULL`
   - (opsiyonel) `currency_snapshot TEXT NULL`
2. Checkout/odeme akisinda satin alma tutari kesinlestigi anda bu alan doldurulur.
3. Orders sorgulari geriye donuk uyumla su sekilde okunur:
   - satir fiyat: `COALESCE(ci.unit_price_snapshot, pr.price)`
   - toplam: `SUM(COALESCE(ci.unit_price_snapshot, pr.price) * ci.quantity)`
4. Eski kayitlar icin migration/backfill yapilir:
   - Snapshot'i olmayan kayitlara bir kez mevcut fiyat yazilir (teknik borc kapanir).
   - Not: cok eski siparislerde gercek tarihsel fiyat geri getirilemeyebilir; bu bilinen limit olarak dokumante edilir.

### Neden bu yaklasim?

- Mevcut API'leri bozmadan ilerler (sadece sorgu ve yazim noktasi guncellenir).
- Frontend degisikligi minimum kalir; backend response ayni yapida kalabilir.
- Kademeli rollout yapilabilir (once DB + write path, sonra read path).

### Uygulama sirasi (onerilen mini plan)

1. DB migration: snapshot kolonlarini ekle.
2. Odeme tamamlaninca `cart_items` satirlarina snapshot fiyat yaz.
3. Admin orders query'lerinde `COALESCE(snapshot, live price)` kullan.
4. Backfill script calistir ve bilinen limitleri not et.
5. Smoke test:
   - Siparis olustur -> urun fiyati degis -> eski siparis tutari sabit kalsin.

---

## Kritik Ek Madde: Landing/Packages Kart Icerikleri Hardcoded (Frontend)

### Problem tanimi (su anki durum)

Paket kartlarindaki ozet maddeler (ornek: guest/media, storage, activation, voice) frontendde sabit metinle yazili.  
Admin panelde urun degerleri guncellense bile landing/packages ekraninda eski sabit metin gorulebilir.

### Is etkisi

- Fiyat ve paket ozellikleri arasinda tutarsizlik riski olusur.
- Adminin yaptigi update aninda pazarlama ekranina yansimaz.
- Destek tarafinda "sitede baska, panelde baska" problemi dogar.

### Onerilen cozum (minimum kirilim)

1. Frontend paket kartlari hardcode metin yerine API verisinden render edilir.
2. Kaynak veri:
   - `GET /api/products` (veya admin panelde `GET /api/admin/products`)
   - `price`, `options.guest_count`, `options.media_count`, `options.storage_days`, `options.activation_days`, `granted_features`
3. UI formatlama kurallari:
   - `-1` gibi sinirsiz degerler "Unlimited" olarak gosterilir.
   - `storage_days` gun/ay formatina cevrilir (30 -> 1 Month, 90 -> 3 Months vb.).
   - Voice ozelligi `granted_features` icinden derive edilir.
4. Geriye donuk guvenlik:
   - Eksik alan varsa fallback metin gosterilir (sayfa bos kalmaz).

### Neden bu yaklasim?

- Yeni endpoint zorunlu degil; mevcut product payload ile cozulebilir.
- Admin update -> storefront gorunumu zinciri tutarli hale gelir.
- Faz 1'deki "edit" isinin gercek is degerini tamamlar.

### Efor etkisi (guncel)

- Sadece Faz 1 edit: **2 is gunu**
- Faz 1 edit + orders fiyat snapshot: **2.5-3 is gunu**
- Faz 1 edit + orders fiyat snapshot + hardcoded paket kartlarini dinamiklestirme: **3.5-4.5 is gunu**
- Guvenli taahhut (bu uc kalem birlikte): **4 is gunu**

---

## Yeni Ek Kapsam Etki Analizi (AI-Only Uygulama)

Bu bolum, asagidaki yeni isteklerin sadece AI destekli gelistirme ile uygulanmasi varsayimi ile hesaplanmistir.
AI hizlandirici etki saglar; ancak entegrasyon, test, rollback guvenligi ve kabul testleri nedeniyle belirli bir taban sure yine gereklidir.

### Yeni istek listesi

1. Order sayfasinda Promo Code alani (client kod girer, indirim uygulanir)
2. Admin panel:
   - promo code create/delete + discount rate
   - admin ekleme/cikarma
   - promo code bazli satis goruntuleme
   - order account ekraninda: total guest, total size (MB), storage expiration
<!--
3. QR Card Templates ve Welcome Board (IPTAL - bu faz sure hesabina dahil DEGIL):
   - 20 QR card + 20 welcome board tasarimi sisteme ekleme
   - Sadece mevcut text alanlarinin konumunu manuel tasima (drag-drop, x/y)
   - Renk/font/yeni alan ekleme kapsam disi
-->
4. Yeni guest interface:
   - advertorial banner alani
   - is kurali: premium paket alindiysa otomatik aktif, diger paketlerde sadece banner add-on alinmissa aktif

### AI-only sure etkisi (ek kapsam)

- Promo code checkout + backend is kurali + odeme akisina entegrasyon: **2-3.5 is gunu**
- Admin promo code CRUD + discount rate yönetimi: **0.75-1.5 is gunu**
- Admin ekleme/cikarma (rol/izin guvenligi ile): **1-2 is gunu**
- Promo code satis raporu + order account metrikleri: **1.5-3 is gunu**
<!-- - 20+20 template ekleme + text alanlarini manuel konumlama: **1.5-3 is gunu** (IPTAL) -->
- Yeni guest interface + advertorial banner (premium/add-on erisim kurali ile): **1.25-2.5 is gunu**

Toplam ek etki (AI-only, template maddesi HARIC): **6.5-12.5 is gunu**

### Toplam takvim (guncel, tum kapsam)

- Mevcut Faz 1 cekirdek (edit + orders snapshot + kart sayilari backend-driven): **3.5-4.5 is gunu**
- Yeni ek kapsam (AI-only, template maddesi haric): **6.5-12.5 is gunu**
- Genel toplam: **10-17 is gunu**
- Guvenli taahhut: **13-15 is gunu**

### Uygulama sirasi (onerilen)

1. Promo code altyapisi (DB + checkout + odeme callback uyumu)
2. Admin promo code yönetimi ve satis gorunurulugu
3. Order account metrik endpoint/ekranlari
4. Guest interface banner alani + premium/add-on erisim kurali
<!-- 5. Template import (20 QR + 20 Welcome Board) - IPTAL, sure hesabina dahil degil -->

### Risk notlari

- Admin ekleme/cikarma ve promo discount dogrulamasi guvenlik-kritik alandir; test kapsamindan kisilmamali.
- Promo code raporu icin net metrik sozlugu (gross/net/discount) proje basinda kilitlenmelidir.
<!-- - Template kapsaminda alan sinirlari/mobil onizleme/kaydet-yukle tutarliligi netlestirilmelidir. (IPTAL) -->

---

## Konsolide Kapsam (Tum Isler) + Frontend/Backend Sure Ayrimi

Bu bolum, tum islerin tek kapsamda netlestirilmis halidir.  
Evet, **urun fiyati degistiginde eski siparislerin degismemesi** gereksinimi dahildir (checkout/payment aninda fiyat snapshot alinacak).

### Kapsama dahil isler (tam liste)

1. Admin Product Edit (10 alanin tamami, sistemi bozmadan):
   - Package Name, Package Price, Package Description, Number of Guests, Number of Pictures/Videos
   - Activation Period, Storage Period, Voice included
   - Add-On Package Name, Add-On Package Price
2. Period/default kurallari:
   - Defaultlar korunur, degerler backendden okunur, admin override edilir.
   - Event lifecycle defaultu korunur (baslangic +1 hafta, aktif sure +14 gun).
   - Storage time (`storage_period_days`) kapsam dahildir; admin girdisine gore guncellenir ve hesaplamalar backend kaynakli tutulur.
3. Orders fiyat dogrulugu:
   - `cart_items` price snapshot modeli
   - `COALESCE(snapshot, live price)` ile geriye donuk uyumluluk
4. Promo code:
   - Order sayfasinda kod girisi ve indirim uygulama
   - Adminde promo create/delete + discount rate
   - Promo code bazli satis goruntuleme
5. Admin yonetimi:
   - Admin ekleme/cikarma (rol ve yetki guvenligi ile)
6. Order account gorunumleri:
   - Total Guest Number, Total Size (MB), Storage Expiration Date
7. Yeni guest interface:
   - Advertorial banner alani
   - Is kurali: premium paket alindiysa otomatik aktif, diger paketlerde yalnizca banner add-on ile aktif
8. Landing package kartlari:
   - Metin yapisi korunur, sayisal degerler backendden render edilir (MVP: dynamic numbers)
   - Description metni backendden guncellenebilir olacak sekilde render edilir.
<!--
9. Tasarim/template (IPTAL - bu faz sure hesabina dahil DEGIL):
   - 20 QR card + 20 welcome board
   - Sadece predefined text alanlari manuel tasinabilir (x/y)
   - Renk/font/yeni text field ekleme yok
-->

### Backend is paketleri ve sure

- Product admin API + validation + backward compatibility: **1.75-2.75 is gunu**
- Orders snapshot migration + write/read path + backfill: **1.5-2.5 is gunu**
- Promo engine + checkout/payment entegrasyonu: **2-3.5 is gunu**
- Promo admin CRUD + satis rapor endpointleri: **1.5-2.5 is gunu**
- Admin ekleme/cikarma + role security hardening: **1-2 is gunu**
- Order account metrik endpointleri (guest/size/storage expiration): **1-1.5 is gunu**
- Banner backend baglantilari (guest interface): **0.5-1 is gunu**

Backend toplam (template maddesi haric): **8.75-15 is gunu**

### Frontend is paketleri ve sure

- Admin panel product edit UI (10 alan): **1.25-1.75 is gunu**
- Landing/package kartlarinda backend-driven sayisal render: **0.5-1 is gunu**
- Promo code order UI + indirim gosterimi: **1-1.5 is gunu**
- Promo/admin ekranlari + satis gorunurulugu: **1.5-2.5 is gunu**
- Admin yonetimi UI (add/remove): **0.75-1.5 is gunu**
- Order account metrik gorunumu: **0.75-1 is gunu**
- Yeni guest interface + advertorial banner UI: **0.75-1.5 is gunu**

Frontend toplam (template maddesi haric): **6.75-11 is gunu**

### Entegrasyon, regression ve teslim

- Uc uca testler, smoke/regression, bugfix buffer: **2-3 is gunu**

### Genel toplam (AI-only, tek akis)

- Backend + Frontend + Entegrasyon: **17.5-29 is gunu**
- Scope kontrolu ve MVP sinirlari ile beklenen gercek bant: **12-18 is gunu**
- Guvenli taahhut: **15-17 is gunu**

Not: Surelerin tek akis (aynı kisi/AI tarafindan sirali yurutum) varsayimiyla verildigi unutulmamalidir. Paralel ekiplenmede toplam sure kisalir.

---

## 27 Nisan Ilk Plan (Adim Adim, Guvenli Ilerleme)

Bu bolum, bugunden baslayarak sadece admin panel edit kapsamini (snapshot dahil) guvenli sekilde uygulamak icin olusturuldu.
Hedef: calisan sistemi bozmadan, her adimi kontrol ederek ilerlemek.

### Kapsam (kilit)

- Package alanlari edit:
  - Package Name
  - Package Price
  - Package Description
  - Number of Guests
  - Number of Pictures/Videos
  - Activation Period
  - Storage Period
  - Voice message included (Yes/No)
- Add-On alanlari edit:
  - Package Name
  - Package Price
- Zorunlu teknik kural:
  - Satin alim aninda fiyat snapshot alinacak.
  - Gecmis siparis fiyatlari urun fiyati degisse bile korunacak.
  - `product_id` immutable kalacak; isim guncellemesi display alanina uygulanacak.

### Dil destegi icin secilen teknik karar

- Yeni dil eklenmeyecegi icin `products` tablosuna dogrudan display kolonlari eklenecek:
  - `display_name_en`
  - `display_name_uk`
  - `display_description_en`
  - `display_description_uk`
- `product_id` teknik kimlik olarak degismeyecek (immutable).
- Okuma/fallback kurali:
  1. Ilgili dilde display kolonu doluysa onu goster.
  2. Bossa mevcut S3 dil dosyasindan (`products.{product_id}.name/description`) oku.
  3. O dil bulunamazsa `en` fallback devam eder.

### Isleyis kurali

- Her adim kucuk ve izole degisiklik ile yapilir.
- Her adim sonunda:
  - ne degisti
  - hangi testler gecti
  - hangi alanlarin etkilenmedigi
  raporlanir.
- Sonraki adima gecis icin kullanici onayi gerekir.

### Adimlar

1. Adim 1 (Read-only analiz, kod degisikligi yok)
   - Mevcut urun okuma/yazma akislarini ve snapshot entegrasyon noktasini netle.
   - Degisecek dosyalar ve risk listesi cikar.

2. Adim 2 (Backend product edit)
   - `GET /api/admin/products`
   - `PATCH /api/admin/products/{productUid}`
   - 10 alan update + validation + yetki kontrolu.
   - EN/UK display alanlari icin kolon update destegi.

3. Adim 3 (Backend snapshot korumasi)
   - `cart_items` icin `unit_price_snapshot` alanini ekle.
   - Satin alim kesinlestiginde snapshot yaz.
   - Orders sorgularinda `COALESCE(snapshot, live_price)` kullan.

4. Adim 4 (Frontend admin edit)
   - 10 alanin tamami icin edit formu.
   - Save/Cancel + success/error state.

5. Adim 5 (Frontend package karti yansimasi)
   - SS'deki kart alanlari backend verisinden render edilir.
   - Hardcoded degerler kaldirilir, fallback korunur.
   - Dil secimine gore EN/UK display alanlari kullanilir; bossa S3 dil dosyasina dusulur.

6. Adim 6 (Final kontrol)
   - Uc uca smoke/regression.
   - Teslim notu ve kalan riskler.

### Kontrol checklist

- Product update sonrasi kartta yeni degerler gorunur.
- Product Name degisse bile `product_id` sabit kalir.
- Dil secimine gore display alanlari dogru render edilir (EN/UK).
- Display alan bosken S3 fallback dogru calisir.
- Add-on name/price update gorunur.
- Yeni sipariste snapshot dolar.
- Urun fiyati degistirilse bile eski order toplami degismez.
- Hata durumunda API tutarli response doner.

### Hedef sure

- Guvenli uygulama bandi: **2-3 is gunu**
- Guvenli taahhut: **3 is gunu**
