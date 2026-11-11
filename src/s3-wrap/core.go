package s3wrap

import (
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
)

type S3_serv struct {
	Serv     *s3.S3
	Uploader *s3manager.Uploader
	Inf      S3_init_inp
	Is_init  bool
}

type S3_init_inp struct {
	Key_id     string `json:"key_id"`
	Key_secret string `json:"key_secret"`
	Bucket     string `json:"bucket"`
	Region     string `json:"region"`
	Endpoint   string `json:"endpoint"`
	// PublicURL: nesnelerin herkese acik okundugu adres (R2 icin ozel alan adi, ornek
	// "https://media.addmoments.com.ua"). Bossa bugunku S3 bicimi kullanilir:
	// https://<bucket>.s3.<region>.amazonaws.com
	PublicURL string `json:"public_url"`
	// LegacyPublicHosts: tasimadan once yazilmis URL'lerin host'lari (ornek
	// "memboxpub-qo1gff2e.s3.eu-north-1.amazonaws.com"); Decode_url ve gorsel URL dogrulamalari
	// bunlari da kabul eder. Bucket R2'deyken region "auto" oldugu icin eski S3 host'u
	// bucket+region'dan cikarilamaz, buradan gelir.
	LegacyPublicHosts []string `json:"legacy_public_hosts"`
}

// isAWS: endpoint AWS S3 mi (bos ya da amazonaws.com). S3'e ozgu davranislar (bucket+region'dan
// kurulan host, eksik nesnede 403) yalniz burada gecerli.
func (s S3_init_inp) isAWS() bool {
	return s.Endpoint == "" || strings.Contains(s.Endpoint, "amazonaws.com")
}

// s3Host: bugunku S3 sanal-host bicimi.
func (s S3_init_inp) s3Host() string {
	return s.Bucket + ".s3." + s.Region + ".amazonaws.com"
}

func (s S3_init_inp) session() *session.Session {
	return session.Must(session.NewSessionWithOptions(session.Options{
		Config: aws.Config{
			Region:           aws.String(s.Region),
			Endpoint:         aws.String(s.Endpoint),
			S3ForcePathStyle: aws.Bool(true),
			Credentials:      credentials.NewStaticCredentials(s.Key_id, s.Key_secret, ""),
		},
	}))
}

var Public_s3 S3_serv

func (sr *S3_serv) Init(inp S3_init_inp) {
	sess := inp.session()
	sr.Serv = s3.New(sess)
	sr.Uploader = s3manager.NewUploader(sess)
	sr.Inf = inp
	sr.Is_init = true
}

func (sr S3_serv) Get(path string) (io.ReadCloser, error) {
	if !sr.Is_init {
		return nil, fmt.Errorf("s3 is not init")
	}

	res, err := sr.Serv.GetObject(&s3.GetObjectInput{
		Bucket: aws.String(sr.Inf.Bucket),
		Key:    aws.String(path),
	})

	if err != nil {
		return nil, err
	}

	return res.Body, nil
}

func (sr S3_serv) Serve(path string, w http.ResponseWriter) {
	if !sr.Is_init {
		http.Error(w, "s3 is not init", 500)
		return
	}

	w.Header().Add("Content-Type", mime.TypeByExtension(filepath.Ext(path)))
	w.WriteHeader(200)

	res, err := sr.Serv.GetObject(&s3.GetObjectInput{
		Bucket: aws.String(sr.Inf.Bucket),
		Key:    aws.String(path),
	})

	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	_, err = io.Copy(w, res.Body)

	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
}

func (sr S3_serv) Store(path string, reader io.Reader) error {
	if !sr.Is_init {
		return fmt.Errorf("s3 is not init")
	}

	_, err := sr.Serv.PutObject(&s3.PutObjectInput{
		Body:        aws.ReadSeekCloser(reader),
		Bucket:      aws.String(sr.Inf.Bucket),
		Key:         aws.String(path),
		ContentType: aws.String(mime.TypeByExtension(filepath.Ext(path))),
	})

	return err
}

// S3StreamWriter wraps a pipe writer and waits for upload completion on Close.
// Implements io.WriteCloser.
type S3StreamWriter struct {
	pw   *io.PipeWriter
	done chan error
}

func (w *S3StreamWriter) Write(p []byte) (n int, err error) {
	return w.pw.Write(p)
}

func (w *S3StreamWriter) Close() error {
	w.pw.Close()
	return <-w.done
}

// StoreStream returns a writer that streams directly to S3.
// Write to it, then call Close() to finish the upload and get any error.
func (sr S3_serv) StoreStream(path string) io.WriteCloser {
	pr, pw := io.Pipe()
	done := make(chan error, 1)

	go func() {
		_, err := sr.Uploader.Upload(&s3manager.UploadInput{
			Bucket: aws.String(sr.Inf.Bucket),
			Key:    aws.String(path),
			Body:   pr,
		})
		done <- err
	}()

	return &S3StreamWriter{pw: pw, done: done}
}

// Get_presign, GET icin kisa omurlu imzali URL uretir. filename bos degilse S3 cevaba
// "Content-Disposition: attachment; filename=..." ekler: tarayici dosyayi acmak yerine o adla
// indirir (capraz kaynakta <a download> yok sayildigi icin adi ve davranisi bu baslik belirler).
func (sr S3_serv) Get_presign(path string, expr time.Duration, filename string) (download_url string, err error) {
	if !sr.Is_init {
		err = fmt.Errorf("s3 is not init")
		return
	}

	input := &s3.GetObjectInput{
		Bucket: aws.String(sr.Inf.Bucket),
		Key:    aws.String(path),
	}
	if filename != "" {
		// FormatMediaType ASCII disi adlari RFC 5987 (filename*=utf-8''...) ile kodlar.
		input.ResponseContentDisposition = aws.String(
			mime.FormatMediaType("attachment", map[string]string{"filename": filename}),
		)
	}

	req, _ := sr.Serv.GetObjectRequest(input)

	download_url, err = req.Presign(expr)
	return
}

func (sr S3_serv) Store_presign(path string, expr time.Duration) (upload_url string, err error) {
	if !sr.Is_init {
		err = fmt.Errorf("s3 is not init")
		return
	}

	req, _ := sr.Serv.PutObjectRequest(&s3.PutObjectInput{
		Bucket: aws.String(sr.Inf.Bucket),
		Key:    aws.String(path),
	})

	upload_url, err = req.Presign(expr)

	return
}

// Store_presign_with_content_type, PUT upload URL'ini belirli bir Content-Type ile imzalar.
func (sr S3_serv) Store_presign_with_content_type(path string, contentType string, expr time.Duration) (uploadURL string, err error) {
	if !sr.Is_init {
		err = fmt.Errorf("s3 is not init")
		return
	}

	req, _ := sr.Serv.PutObjectRequest(&s3.PutObjectInput{
		Bucket:      aws.String(sr.Inf.Bucket),
		Key:         aws.String(path),
		ContentType: aws.String(contentType),
	})

	uploadURL, err = req.Presign(expr)
	return
}

func (sr S3_serv) Rm(path string) (err error) {
	if !sr.Is_init {
		err = fmt.Errorf("s3 is not init")
		return
	}

	_, err = sr.Serv.DeleteObject(&s3.DeleteObjectInput{
		Bucket: aws.String(sr.Inf.Bucket),
		Key:    aws.String(path),
	})
	if err != nil {
		return
	}

	return
}

func (sr S3_serv) Ls(path string) ([]string, error) {
	if !sr.Is_init {
		return nil, fmt.Errorf("s3 is not init")
	}

	res, err := sr.Serv.ListObjects(&s3.ListObjectsInput{
		Bucket: aws.String(sr.Inf.Bucket),
		Prefix: aws.String(path),
	})

	if err != nil {
		return nil, err
	}

	flist := make([]string, 0)

	for i := 0; i < len(res.Contents); i++ {
		flist = append(flist, *res.Contents[i].Key)
	}

	return flist, nil
}

// Ls_all, Ls gibi prefix altindaki anahtarlari dondurur ama sayfa sayfa ilerler.
// Ls tek ListObjects cagrisiyla en fazla 1000 anahtar verir; tumunu silmek gereken
// yerde (etkinligi kalici silme) eksik kalmasin diye ayri.
func (sr S3_serv) Ls_all(prefix string) ([]string, error) {
	if !sr.Is_init {
		return nil, fmt.Errorf("s3 is not init")
	}

	keys := make([]string, 0)
	err := sr.Serv.ListObjectsPages(&s3.ListObjectsInput{
		Bucket: aws.String(sr.Inf.Bucket),
		Prefix: aws.String(prefix),
	}, func(page *s3.ListObjectsOutput, lastPage bool) bool {
		for _, obj := range page.Contents {
			if obj.Key != nil {
				keys = append(keys, *obj.Key)
			}
		}
		return true
	})
	if err != nil {
		return nil, err
	}
	return keys, nil
}

type S3_object_info struct {
	Key          string
	Size         int64
	LastModified time.Time
}

// Ls_all_info, Ls_all ile ayni sekilde sayfa sayfa listeler; anahtarin yaninda boyut ve son
// degisiklik zamanini da dondurur (export zip'lerinin yasini olcmek icin).
func (sr S3_serv) Ls_all_info(prefix string) ([]S3_object_info, error) {
	if !sr.Is_init {
		return nil, fmt.Errorf("s3 is not init")
	}

	objects := make([]S3_object_info, 0)
	err := sr.Serv.ListObjectsPages(&s3.ListObjectsInput{
		Bucket: aws.String(sr.Inf.Bucket),
		Prefix: aws.String(prefix),
	}, func(page *s3.ListObjectsOutput, lastPage bool) bool {
		for _, obj := range page.Contents {
			if obj.Key == nil {
				continue
			}
			objects = append(objects, S3_object_info{
				Key:          *obj.Key,
				Size:         aws.Int64Value(obj.Size),
				LastModified: aws.TimeValue(obj.LastModified),
			})
		}
		return true
	})
	if err != nil {
		return nil, err
	}
	return objects, nil
}

// Exists, verilen key'in bucket icinde var olup olmadigini kontrol eder.
func (sr S3_serv) Exists(path string) (bool, error) {
	if !sr.Is_init {
		return false, fmt.Errorf("s3 is not init")
	}

	_, err := sr.Serv.HeadObject(&s3.HeadObjectInput{
		Bucket: aws.String(sr.Inf.Bucket),
		Key:    aws.String(path),
	})
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "not found") {
			return false, nil
		}
		return false, err
	}

	return true, nil
}

// Stat, key'in boyutunu doner; nesne yoksa exists=false ve hata yok.
// Neden: misafir yuklemesi dosyasi S3'e ulasmadan galeriye girmesin diye (upload_receipt)
// yalnizca varlik yetmiyor -- iOS Safari okuyamadigi dosyayi bos govdeyle PUT edip 200 aliyor,
// geriye 0 baytlik bir nesne kaliyor (21 Eylul, bir misafirin 100 dosyasi).
func (sr S3_serv) Stat(path string) (size int64, exists bool, err error) {
	size, _, exists, err = sr.Head(path)
	return
}

// Head, Stat gibi ama nesnenin ETag'ini de doner. Presign'li tek parcali PUT'ta ETag icerigin
// MD5'idir; ayni dosyanin ikinci kez yuklendigini baytlari indirmeden anlamaya yeter (AM-07).
// AWS'de 403 de "yok" sayilir: ListBucket yetkisi olmayan kimlige S3 eksik key icin 404 yerine 403
// doner; GetObject yetkimiz oldugu icin var olan bir nesnede 403 almayiz. Diger endpoint'lerde
// (R2, MinIO) 403 hatadir.
func (sr S3_serv) Head(path string) (size int64, etag string, exists bool, err error) {
	if !sr.Is_init {
		err = fmt.Errorf("s3 is not init")
		return
	}

	out, err := sr.Serv.HeadObject(&s3.HeadObjectInput{
		Bucket: aws.String(sr.Inf.Bucket),
		Key:    aws.String(path),
	})
	if err != nil {
		if reqErr, ok := err.(awserr.RequestFailure); ok {
			code := reqErr.StatusCode()
			// 403 yalniz AWS'de "yok" demek; R2/MinIO eksik anahtara 404 doner, 403 orada yanlis
			// kimlik ya da yetki demektir ve "yok" sayilirsa yukleme taramasi satirlari siler.
			if code == http.StatusNotFound || (code == http.StatusForbidden && sr.Inf.isAWS()) {
				return 0, "", false, nil
			}
		}
		return 0, "", false, err
	}

	return aws.Int64Value(out.ContentLength), aws.StringValue(out.ETag), true, nil
}

func (sr S3_serv) Url(key string) string {
	if sr.Inf.PublicURL != "" {
		return strings.TrimRight(sr.Inf.PublicURL, "/") + path.Join("/", key)
	}

	path_ar := make([]string, 0)
	path_ar = append(path_ar, sr.Inf.s3Host())
	path_ar = append(path_ar, strings.Split(key, "/")...)

	return "https://" + filepath.Join(path_ar...)
}

// publicHosts: herkese acik URL'lerde kabul edilen host'lar ve her birinin yol oneki
// (public_url bir yol icerebilir, ornek yerel MinIO "http://127.0.0.1:9100/memboxpub-local").
func (sr S3_serv) publicHosts() map[string]string {
	hosts := map[string]string{}
	for _, h := range sr.Inf.LegacyPublicHosts {
		if h = strings.TrimSpace(h); h != "" {
			hosts[strings.ToLower(h)] = ""
		}
	}
	if sr.Inf.isAWS() && sr.Inf.Bucket != "" && sr.Inf.Region != "" {
		hosts[strings.ToLower(sr.Inf.s3Host())] = ""
	}
	if sr.Inf.PublicURL != "" {
		if u, err := url.Parse(sr.Inf.PublicURL); err == nil && u.Host != "" {
			hosts[strings.ToLower(u.Host)] = strings.TrimRight(u.Path, "/")
		}
	}
	return hosts
}

// IsPublicHost: host bu bucket'in herkese acik adreslerinden biri mi (yeni ya da eski).
func (sr S3_serv) IsPublicHost(host string) bool {
	_, ok := sr.publicHosts()[strings.ToLower(host)]
	return ok
}

// Decode_url: bu bucket'a ait herkese acik bir URL'den anahtari "/" ile baslayan yol olarak doner.
func (sr S3_serv) Decode_url(uri string) (key string, err error) {
	u, err := url.Parse(uri)
	if err != nil {
		return
	}

	prefix, ok := sr.publicHosts()[strings.ToLower(u.Host)]
	if !ok || (prefix != "" && !strings.HasPrefix(u.Path, prefix+"/")) {
		err = fmt.Errorf("%s: path url does not belong to this bucket", sr.Inf.Bucket)
		return
	}
	key = strings.TrimPrefix(u.Path, prefix)
	return
}
