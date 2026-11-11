package s3wrap

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Bugunku canli yapilandirma: S3, yeni anahtarlar yok.
var prodS3 = S3_init_inp{
	Bucket:   "memboxpub-qo1gff2e",
	Region:   "eu-north-1",
	Endpoint: "s3.eu-north-1.amazonaws.com",
}

// Tasimadan sonraki yapilandirma ornegi: R2, ozel alan adi, eski S3 host'u kabul listesinde.
var r2 = S3_init_inp{
	Bucket:            "addmoments-media",
	Region:            "auto",
	Endpoint:          "https://ACCOUNT.eu.r2.cloudflarestorage.com",
	PublicURL:         "https://media.addmoments.com.ua",
	LegacyPublicHosts: []string{"memboxpub-qo1gff2e.s3.eu-north-1.amazonaws.com"},
}

func TestUrlS3Unchanged(t *testing.T) {
	sr := S3_serv{Inf: prodS3}
	cases := map[string]string{
		"/ui/assets/lang/en.json":      "https://memboxpub-qo1gff2e.s3.eu-north-1.amazonaws.com/ui/assets/lang/en.json",
		"addon_banner/p/x.jpg":         "https://memboxpub-qo1gff2e.s3.eu-north-1.amazonaws.com/addon_banner/p/x.jpg",
		"/events//a/b.zip":             "https://memboxpub-qo1gff2e.s3.eu-north-1.amazonaws.com/events/a/b.zip",
		"/ui":                          "https://memboxpub-qo1gff2e.s3.eu-north-1.amazonaws.com/ui",
		"advertorial/addmoments/u.png": "https://memboxpub-qo1gff2e.s3.eu-north-1.amazonaws.com/advertorial/addmoments/u.png",
	}
	for in, want := range cases {
		if got := sr.Url(in); got != want {
			t.Errorf("Url(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUrlPublic(t *testing.T) {
	sr := S3_serv{Inf: r2}
	cases := map[string]string{
		"/ui/assets/lang/en.json": "https://media.addmoments.com.ua/ui/assets/lang/en.json",
		"addon_banner/p/x.jpg":    "https://media.addmoments.com.ua/addon_banner/p/x.jpg",
		"/events//a/b.zip":        "https://media.addmoments.com.ua/events/a/b.zip",
		"/ui":                     "https://media.addmoments.com.ua/ui",
	}
	for in, want := range cases {
		if got := sr.Url(in); got != want {
			t.Errorf("Url(%q) = %q, want %q", in, got, want)
		}
	}
	withSlash := S3_serv{Inf: S3_init_inp{PublicURL: "https://media.addmoments.com.ua/"}}
	if got := withSlash.Url("/a"); got != "https://media.addmoments.com.ua/a" {
		t.Errorf("trailing slash public_url: %q", got)
	}
}

func TestDecodeUrl(t *testing.T) {
	type c struct {
		inf  S3_init_inp
		in   string
		want string
		ok   bool
	}
	minio := S3_init_inp{Bucket: "memboxpub-local", Region: "eu-north-1", Endpoint: "http://127.0.0.1:9100", PublicURL: "http://127.0.0.1:9100/memboxpub-local"}
	cases := []c{
		{prodS3, "https://memboxpub-qo1gff2e.s3.eu-north-1.amazonaws.com/events/x/y.jpg", "/events/x/y.jpg", true},
		{prodS3, "https://evil.com/events/x/y.jpg", "", false},
		{prodS3, "https://media.addmoments.com.ua/events/x/y.jpg", "", false},
		{r2, "https://media.addmoments.com.ua/events/x/y.jpg", "/events/x/y.jpg", true},
		{r2, "https://MEDIA.addmoments.com.ua/events/x/y.jpg", "/events/x/y.jpg", true},
		{r2, "https://memboxpub-qo1gff2e.s3.eu-north-1.amazonaws.com/events/x/y.jpg", "/events/x/y.jpg", true},
		{r2, "https://addmoments-media.s3.auto.amazonaws.com/events/x/y.jpg", "", false},
		{r2, "https://ACCOUNT.eu.r2.cloudflarestorage.com/addmoments-media/events/x", "", false},
		{minio, "http://127.0.0.1:9100/memboxpub-local/events/x/y.jpg", "/events/x/y.jpg", true},
		{minio, "http://127.0.0.1:9100/other/events/x/y.jpg", "", false},
	}
	for _, tc := range cases {
		sr := S3_serv{Inf: tc.inf}
		got, err := sr.Decode_url(tc.in)
		if (err == nil) != tc.ok || got != tc.want {
			t.Errorf("%s Decode_url(%q) = %q, %v; want %q ok=%v", tc.inf.Bucket, tc.in, got, err, tc.want, tc.ok)
		}
	}
}

func TestIsPublicHost(t *testing.T) {
	if !(S3_serv{Inf: prodS3}).IsPublicHost("memboxpub-qo1gff2e.s3.eu-north-1.amazonaws.com") {
		t.Error("prod S3 host must be accepted")
	}
	if (S3_serv{Inf: prodS3}).IsPublicHost("media.addmoments.com.ua") {
		t.Error("public host must not be accepted before it is configured")
	}
	sr := S3_serv{Inf: r2}
	for _, h := range []string{"media.addmoments.com.ua", "memboxpub-qo1gff2e.s3.eu-north-1.amazonaws.com"} {
		if !sr.IsPublicHost(h) {
			t.Errorf("%s must be accepted on r2", h)
		}
	}
	if sr.IsPublicHost("addmoments-media.s3.auto.amazonaws.com") {
		t.Error("derived s3 host must not be accepted for a non-AWS endpoint")
	}
}

// Head: 404 her yerde "yok"; 403 yalniz AWS'de "yok", baska endpoint'te hata.
func TestHeadStatusMapping(t *testing.T) {
	status := http.StatusNotFound
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/present") {
			w.Header().Set("ETag", `"abc"`)
			w.Header().Set("Content-Length", "5")
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(status)
	}))
	defer srv.Close()

	var sr S3_serv
	sr.Init(S3_init_inp{Key_id: "k", Key_secret: "s", Bucket: "b", Region: "auto", Endpoint: srv.URL})

	size, etag, exists, err := sr.Head("/events/present")
	if err != nil || !exists || size != 5 || etag != `"abc"` {
		t.Fatalf("present: %d %q %v %v", size, etag, exists, err)
	}

	status = http.StatusNotFound
	if _, _, exists, err = sr.Head("/events/missing"); err != nil || exists {
		t.Errorf("404 on non-AWS: exists=%v err=%v, want missing without error", exists, err)
	}

	status = http.StatusForbidden
	if _, _, exists, err = sr.Head("/events/missing"); err == nil || exists {
		t.Errorf("403 on non-AWS: exists=%v err=%v, want an error", exists, err)
	}

	// Ayni sunucu, AWS gibi davranan yapilandirmayla: 403 "yok" sayilir (bugunku davranis).
	if !(S3_init_inp{Endpoint: "s3.eu-north-1.amazonaws.com"}).isAWS() || (S3_init_inp{Endpoint: srv.URL}).isAWS() {
		t.Fatal("isAWS mismatch")
	}
	awsLike := S3_serv{Serv: sr.Serv, Inf: S3_init_inp{Bucket: "b", Endpoint: "s3.eu-north-1.amazonaws.com"}, Is_init: true}
	if _, _, exists, err = awsLike.Head("/events/missing"); err != nil || exists {
		t.Errorf("403 on AWS: exists=%v err=%v, want missing without error", exists, err)
	}
}
