package s3wrap

import (
	"archive/zip"
	"io"
	"time"
)

type s3_zip_streamer struct {
	Zw    *zip.Writer
	Close func() error
}

func (sr S3_serv) New_zip_streamer(out_path string) *s3_zip_streamer {
	pw := sr.StoreStream(out_path)
	zw := zip.NewWriter(pw)

	errfunc := func() (err error) {
		err = zw.Close()
		if err != nil {
			return
		}
		return pw.Close()
	}

	return &s3_zip_streamer{
		Zw:    zw,
		Close: errfunc,
	}
}

// Add_file zip'e yeni bir girdi acar ve reader'i ona kopyalar.
// Modified bilerek dolu: zip.Writer.Create tarihi bos birakir (DOS 0 = 1980-00-00, gun 0 / ay 0
// gecersiz) ve Windows'un yerlesik ayiklayicisi gecersiz tarihli girdileri listelemez -- host
// export zip'ini "ici bos" goruyordu (macOS Archive Utility ve unzip aldirmaz, o yuzden bizde
// gorunmedi). Gercek yukleme tarihi yerine simdiki zaman yeter; uploads.xlsx zaten created_at tasir.
func (sr s3_zip_streamer) Add_file(zip_path string, reader io.Reader) (n int64, err error) {
	entry, err := sr.Zw.CreateHeader(&zip.FileHeader{
		Name:     zip_path,
		Method:   zip.Deflate,
		Modified: time.Now(),
	})
	if err != nil {
		return 0, err
	}

	return io.Copy(entry, reader)
}
