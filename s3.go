package main

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
)

const imageBucketName = "pottery-log"
const importBucketName = "pottery-log-exports"

var svc *s3.S3

func init() {
	sess := session.Must(session.NewSessionWithOptions(session.Options{
		Config: aws.Config{
			Region:                        aws.String("us-east-2"),
			CredentialsChainVerboseErrors: aws.Bool(true),
			//Credentials: credentials.NewSharedCredentials()
		},
		Profile: "pottery-log-server",
	}))
	svc = s3.New(sess)
}

func downloadImport(urlString string, localFile string) error {

	s3url, err := url.Parse(urlString)
	if err != nil {
		return err
	}
	if s3url.Host != fmt.Sprintf("%s.s3.amazonaws.com", importBucketName) {
		return errors.New("The link must be a Pottery Log export link")
	}
	log.Printf("Downloading %v to %v\n", urlString, localFile)
	path := s3url.Path

	downloader := s3manager.NewDownloaderWithClient(svc)

	file, err := os.Create(localFile)
	defer file.Close()
	_, err = downloader.Download(file,
		&s3.GetObjectInput{
			Bucket: aws.String(importBucketName),
			Key:    aws.String(path),
		})
	log.Println("Finished downloading file")

	if awserr, ok := err.(awserr.Error); err != nil && ok {
		log.Printf("AWS Error: %+v\n", awserr)
	}

	return err
}

func uploadImage(imageFile multipart.File, imageFileHeader *multipart.FileHeader, deviceID string) (string, error) {
	return uploadFile(imageBucketName, imageFile, imageFileHeader.Filename, imageFileHeader.Header.Get("Content-Type"), deviceID)
}

func uploadImportedImage(imageFile *zip.File, deviceID string) (string, error) {
	imageReader, err := imageFile.Open()
	if err != nil {
		log.Print("Error opening image file")
		return "", err
	}
	return uploadFile(importBucketName, imageReader, imageFile.Name, imageFile.Comment, deviceID)
}

func uploadFile(bucketName string, file io.Reader, fileName, contentType, deviceID string) (string, error) {

	fullFileName := fmt.Sprintf("%v/%v", deviceID, fileName)
	if objectExists(bucketName, fullFileName) {
		fmt.Printf("Image %s already in s3\n", fullFileName)
		return objectUrl(bucketName, fullFileName), nil
	}

	var reader io.ReadSeeker
	if fr, ok := file.(io.ReadSeeker); ok {
		reader = fr
	} else {
		data, err := ioutil.ReadAll(file)
		if err != nil {
			log.Print("Cannot read the file into memory\n")
			return "", err
		}
		if !strings.HasPrefix(contentType, "image/") {
			contentType = http.DetectContentType(data)
		}
		reader = bytes.NewReader(data)
	}


	params := &s3.PutObjectInput{
		// Params copied to uploadMultipart CreateMultipartUpload
		Bucket:       aws.String(bucketName),   // Required
		Key:          aws.String(fullFileName), // Required
		ACL:          aws.String("public-read"),
		Body:         reader,
		CacheControl: aws.String("max-age=31556926"), // cachable forever
		ContentType:  aws.String(contentType),
		Expires:      aws.Time(time.Now().Add(time.Hour * 24 * 365)),
	}
	_, err := svc.PutObject(params)
	if awserr, ok := err.(awserr.Error); err != nil && ok {
		log.Printf("AWS Error: %+v\n", awserr)
	}
	if err != nil {
		log.Print("Non-AWS error from svc.PutObject\n")
		return "", err
	}

	return objectUrl(bucketName, fullFileName), nil
}

const MIN_MULTIPART_SIZE = 1_000_000_000 // 1GB
const PART_SIZE = 500_000_000 // 500 MB

func uploadMultipart(bucketName string, file *os.File, fileName, contentType, deviceID string) (string, error) {

	// Fall back to uploadFile for small files
	stat, _ := file.Stat()
	fileSize := stat.Size()
	if fileSize < MIN_MULTIPART_SIZE {
		return uploadFile(bucketName, file, fileName, contentType, deviceID)
	}

	// Bail if file already exists
	fullFileName := fmt.Sprintf("%v/%v", deviceID, fileName)
	if objectExists(bucketName, fullFileName) {
		fmt.Printf("Image %s already in s3\n", fullFileName)
		return objectUrl(bucketName, fullFileName), nil
	}

	// Initiate multipart upload
	upl, err := svc.CreateMultipartUpload(&s3.CreateMultipartUploadInput{
		// Params copied from uploadFile PutObjectInput
		Bucket:       aws.String(bucketName),   // Required
		Key:          aws.String(fullFileName), // Required
		ACL:          aws.String("public-read"),
		CacheControl: aws.String("max-age=31556926"), // cachable forever
		ContentType:  aws.String(contentType),
		Expires:      aws.Time(time.Now().Add(time.Hour * 24 * 365)),
	})

	if awserr, ok := err.(awserr.Error); err != nil && ok {
		log.Printf("AWS Error: %+v\n", awserr)
	}
	if err != nil {
		return "", err
	}

	var completedParts []*s3.CompletedPart
	partBytes := make([]byte, PART_SIZE)
	partNum := 1
	for {
		n, err := file.Read(partBytes)
		if n == 0 && err == io.EOF {
			break
		}
		if err != nil && err != io.EOF {
			abortMultipartUpload(upl)
			return "", err
		}
		if n == 0 {
			continue
		}
		partResp, err := svc.UploadPart(&s3.UploadPartInput{
			Body: bytes.NewReader(partBytes[:n]),
			Bucket: upl.Bucket,
			Key: upl.Key,
			PartNumber: aws.Int64(int64(partNum)),
			UploadId: upl.UploadId,
			ContentLength: aws.Int64(int64(n)),
		})
		if awserr, ok := err.(awserr.Error); err != nil && ok {
			log.Printf("UploadPart: AWS Error: %+v\n", awserr)
		}
		if err != nil {
			abortMultipartUpload(upl)
			return "", err
		}
		completedParts = append(completedParts, &s3.CompletedPart{
			ETag: partResp.ETag,
			PartNumber: aws.Int64(int64(partNum)),
		})
		partNum++
	}

	// Complete upload
	_, err = svc.CompleteMultipartUpload(&s3.CompleteMultipartUploadInput{
		Bucket: upl.Bucket,
		Key: upl.Key,
		UploadId: upl.UploadId,
		MultipartUpload: &s3.CompletedMultipartUpload{
			Parts: completedParts,
		},
	})
	if awserr, ok := err.(awserr.Error); err != nil && ok {
		log.Printf("CompleteMultipartUpload: AWS Error: %+v\n", awserr)
	}
	if err != nil {
		return "", err
	}

	return objectUrl(bucketName, fullFileName), nil
}

func abortMultipartUpload(upl *s3.CreateMultipartUploadOutput) {
	_, err := svc.AbortMultipartUpload(&s3.AbortMultipartUploadInput{
		Bucket: upl.Bucket,
		Key: upl.Key,
		UploadId: upl.UploadId,
	})
	if awserr, ok := err.(awserr.Error); err != nil && ok {
		log.Printf("AbortMultipartUpload: AWS Error: %+v\n", awserr)
	} else if err != nil {
		log.Printf("AbortMultipartUpload: Error: %+v\n", err);
	}
}

func deleteImage(fileName string) error {
	params := &s3.DeleteObjectInput{
		Bucket: aws.String(imageBucketName),
		Key:    aws.String(fileName),
	}
	_, err := svc.DeleteObject(params)
	if awserr, ok := err.(awserr.Error); err != nil && ok {
		log.Printf("AWS Error: %+v\n", awserr)
	}
	return err
}

func objectExists(bucketName, fileName string) bool {
	params := &s3.HeadObjectInput{
		Bucket: aws.String(bucketName),
		Key: aws.String(fileName),
	}
	_, err := svc.HeadObject(params)
	return err == nil
}

func objectUrl(bucketName, fileName string) string {
	return fmt.Sprintf("https://%s.s3.amazonaws.com/%s", bucketName, fileName)
}