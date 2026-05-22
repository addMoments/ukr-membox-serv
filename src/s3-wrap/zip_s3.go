package s3wrap

import (
	"archive/zip"
	"io"
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

func (sr s3_zip_streamer) Add_file(zip_path string, reader io.Reader) (n int64, err error) {
	entry, err := sr.Zw.Create(zip_path)
	if err != nil {
		return 0, err
	}

	return io.Copy(entry, reader)
}

func (sr s3_zip_streamer) Open_writer(zip_path string) (w *io.PipeWriter, err error) {
	reader, writer := io.Pipe()

	go (func() {
		_, err := sr.Add_file(zip_path, reader)
		if err != nil {
			reader.CloseWithError(err)
		}
	})()

	return writer, nil
}
