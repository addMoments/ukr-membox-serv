package s3wrap

import (
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
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

func (sr S3_serv) Get_presign(path string, expr time.Duration) (download_url string, err error) {
	if !sr.Is_init {
		err = fmt.Errorf("s3 is not init")
		return
	}

	req, _ := sr.Serv.GetObjectRequest(&s3.GetObjectInput{
		Bucket: aws.String(sr.Inf.Bucket),
		Key:    aws.String(path),
	})

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

func (sr S3_serv) Url(path string) string {
	path_ar := make([]string, 0)
	path_ar = append(path_ar, sr.Inf.Bucket+".s3."+sr.Inf.Region+".amazonaws.com")
	path_ar = append(path_ar, strings.Split(path, "/")...)

	return "https://" + filepath.Join(path_ar...)
}

func (sr S3_serv) Decode_url(uri string) (path string, err error) {
	url, err := url.Parse(uri)
	if err != nil {
		return
	}

	host_split := strings.Split(url.Host, ".")

	if len(host_split) < 3 {
		err = fmt.Errorf("invalid decode url format")
		return
	}

	if host_split[0] != sr.Inf.Bucket {
		err = fmt.Errorf(sr.Inf.Bucket + ": path url does not belong to this bucket")
		return
	}

	if host_split[2] != sr.Inf.Region {
		err = fmt.Errorf(sr.Inf.Bucket + ": invalid path url reigon")
		return
	}

	path = url.Path
	return
}
