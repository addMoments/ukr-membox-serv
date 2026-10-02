// Package uploadreceipt, misafir yuklemesini dosyasi S3'e gercekten ulasinca galeriye alir.
//
// Satir presign aninda received_at NULL yazilir (routes/upload.go GuestUpload). Iki yol onu
// sonuclandirir:
//   - Tarayici PUT bitince /api/guest/upload/{event}/confirm'u cagirir (routes GuestUploadConfirm).
//   - Dakikalik tarama, son 24 saatte presign edilip hala dogrulanmamis satirlara bakar: eski
//     arayuzu acik kalmis misafirler /confirm'u hic cagirmaz, yeni arayuzun /confirm'u da ag
//     koptugu icin ulasmayabilir.
//
// Karar ikisinde de ayni (Resolve): dosya var ve bos degilse satir gorunur olur; 0 baytsa satir
// ve bos nesne silinir; hic yoksa satir yalnizca tarayici "bu anahtarla isim bitti" dediginde
// silinir, aksi halde yukleme suruyor olabilir diye bekler. Bir gun sonra tarama da birakir;
// satir gizli kalir ve UploadCountsSQL geregi hicbir kotaya girmez.
// Ayni albumde ayni dosya zaten gorunurse yeni satir ve nesnesi silinir (AM-07): misafirler ayni
// fotograflari ertesi gun yeniden secip yukluyordu, galeride kopyalar birikiyor ve host birini
// silince fotograf "silinmedi" gibi gorunuyordu.
package uploadreceipt

import (
	"fmt"
	dbscripts "membox-serv/src/db_scripts"
	s3wrap "membox-serv/src/s3-wrap"
	"time"
)

// Status, bir yuklemenin S3'e gore son durumu; /confirm cevabinda da bu metin doner.
type Status string

const (
	// Received: dosya S3'te ve bos degil; satir artik galeride.
	Received Status = "received"
	// Pending: dosya henuz yok ama gelebilir; satir gizli bekliyor.
	Pending Status = "pending"
	// Missing: dosya yok ve gelmeyecek; satir silindi.
	Missing Status = "missing"
	// Empty: PUT bos govdeyle bitmis (iOS okunamayan dosya); satir ve bos nesne silindi.
	Empty Status = "empty"
	// Duplicate: ayni dosya bu albumde zaten var; yeni satir ve nesne silindi. Misafir icin basari.
	Duplicate Status = "duplicate"
)

const (
	sweepEvery = time.Minute
	// sweepMinAge: /confirm'a once sans ver; tarama tarayiciyla ayni anda ayni satira bakmasin.
	sweepMinAge = 30 * time.Second
	// sweepMaxAge: presign URL'i 1 dakika gecerli, PUT o surede baslamak zorunda; en yavas
	// baglantida bile buyuk bir video bir gunde biter.
	sweepMaxAge = 24 * time.Hour
	sweepBatch  = 500
)

// Resolve, dogrulanmamis bir yuklemeyi S3'teki haline gore sonuclandirir.
// clientDone: tarayici bu anahtara yazma denemesini bitirdi ve basarisiz gordu. Dosya yoksa
// artik hic gelmeyecek demektir ve satir silinir, misafirin kotasi hemen bosalir.
func Resolve(u dbscripts.PendingUpload, clientDone bool) (Status, error) {
	if u.Received {
		return Received, nil
	}

	size, etag, exists, err := s3wrap.Public_s3.Head(u.Value)
	if err != nil {
		return Pending, err
	}

	switch {
	case exists && size > 0:
		dup, dupErr := isDuplicate(u, size, etag)
		if dupErr != nil {
			// Kontrol edilemedi: yuklemeyi kaybetmektense olasi bir kopyayi gostermek iyidir.
			fmt.Printf("[upload_receipt] duplicate check %s: %v\n", u.Value, dupErr)
		}
		if dup {
			if err = dbscripts.Drop_pending_upload(u.UID); err != nil {
				return Pending, err
			}
			if rmErr := s3wrap.Public_s3.Rm(u.Value); rmErr != nil {
				fmt.Printf("[upload_receipt] duplicate object not removed %s: %v\n", u.Value, rmErr)
			}
			return Duplicate, nil
		}
		if err = dbscripts.Mark_upload_received(u.UID, size); err != nil {
			return Pending, err
		}
		return Received, nil

	case exists:
		if err = dbscripts.Drop_pending_upload(u.UID); err != nil {
			return Pending, err
		}
		if rmErr := s3wrap.Public_s3.Rm(u.Value); rmErr != nil {
			fmt.Printf("[upload_receipt] empty object not removed %s: %v\n", u.Value, rmErr)
		}
		return Empty, nil

	case clientDone:
		if err = dbscripts.Drop_pending_upload(u.UID); err != nil {
			return Pending, err
		}
		return Missing, nil
	}

	return Pending, nil
}

// isDuplicate, ayni albumde ayni boyutta gorunur bir dosyanin ETag'i (icerigin MD5'i) bununkiyle
// ayni mi diye bakar. ETag yoksa karar verilmez.
func isDuplicate(u dbscripts.PendingUpload, size int64, etag string) (bool, error) {
	if etag == "" {
		return false, nil
	}
	candidates, err := dbscripts.Duplicate_candidates(u, size)
	if err != nil {
		return false, err
	}
	for _, c := range candidates {
		_, otherETag, exists, headErr := s3wrap.Public_s3.Head(c.Value)
		if headErr != nil {
			err = headErr
			continue
		}
		if exists && otherETag == etag {
			return true, nil
		}
	}
	return false, err
}

// Init, main.go'dan cagirilir; taramayi dakikada bir calistirir.
func Init() {
	fmt.Printf("[upload_receipt] initialised; tick=%s window=%s\n", sweepEvery, sweepMaxAge)

	go func() {
		ticker := time.NewTicker(sweepEvery)
		defer ticker.Stop()
		for range ticker.C {
			if err := RunOnce(); err != nil {
				fmt.Printf("[upload_receipt] run error: %v\n", err)
			}
		}
	}()
}

// RunOnce, bekleyen satirlari bir kez tarar. Yalnizca bir sey degistiyse log yazar.
func RunOnce() error {
	list, err := dbscripts.Pending_uploads(sweepMinAge, sweepMaxAge, sweepBatch)
	if err != nil {
		return err
	}

	counts := map[Status]int{}
	for _, u := range list {
		status, err := Resolve(u, false)
		if err != nil {
			fmt.Printf("[upload_receipt] %s %s: %v\n", u.UID, u.Value, err)
		}
		counts[status]++
	}

	if counts[Received] > 0 || counts[Empty] > 0 || counts[Duplicate] > 0 {
		fmt.Printf("[upload_receipt] sweep pending=%d received=%d empty=%d duplicate=%d still_pending=%d\n",
			len(list), counts[Received], counts[Empty], counts[Duplicate], counts[Pending])
	}
	return nil
}
