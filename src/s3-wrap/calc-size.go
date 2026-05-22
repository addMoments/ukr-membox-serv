package s3wrap

import (
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/s3"
)

func (sr S3_serv) Calc_size(path string) (mb float64, err error) {
	if !sr.Is_init {
		err = fmt.Errorf("s3 is not init")
		return
	}

	var totalBytes int64

	err = sr.Serv.ListObjectsPages(&s3.ListObjectsInput{
		Bucket: aws.String(sr.Inf.Bucket),
		Prefix: aws.String(path),
	}, func(page *s3.ListObjectsOutput, lastPage bool) bool {
		for _, obj := range page.Contents {
			if obj.Size != nil {
				totalBytes += *obj.Size
			}
		}
		return true // continue to next page
	})

	if err != nil {
		return
	}

	mb = float64(totalBytes) / (1024 * 1024)
	return
}

// Calc_object_size, tekil bir S3 objesinin boyutunu MB cinsinden HeadObject ile
// hesaplar. Event silme snapshot'inda prefix toplamamak icin kullanilir; boylece
// QR/export gibi upload disi dosyalar upload MB metrigine karismaz.
func (sr S3_serv) Calc_object_size(path string) (mb float64, exists bool, err error) {
	if !sr.Is_init {
		err = fmt.Errorf("s3 is not init")
		return
	}

	res, err := sr.Serv.HeadObject(&s3.HeadObjectInput{
		Bucket: aws.String(sr.Inf.Bucket),
		Key:    aws.String(path),
	})
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			switch aerr.Code() {
			case s3.ErrCodeNoSuchKey, "NotFound", "404":
				return 0, false, nil
			}
		}
		return 0, false, err
	}

	if res.ContentLength == nil {
		return 0, true, nil
	}

	mb = float64(*res.ContentLength) / (1024 * 1024)
	return mb, true, nil
}
