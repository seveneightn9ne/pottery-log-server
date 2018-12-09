package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
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

func uploadImage(imageFile multipart.File, imageFileHeader *multipart.FileHeader, deviceID string) (string, error) {
	return uploadFile(imageBucketName, imageFile, imageFileHeader.Filename, imageFileHeader.Header.Get("Content-Type"), deviceID)
}

func uploadImportedImage(imageFile *zip.File, deviceID string) (string, error) {
	imageReader, err := imageFile.Open()
	if err != nil {
		return "", err
	}
	return uploadFile(importBucketName, imageReader, imageFile.Name, imageFile.Comment, deviceID)
}

func uploadFile(bucketName string, file io.Reader, fileName, contentType, deviceID string) (string, error) {
	var reader io.ReadSeeker
	if fr, ok := file.(io.ReadSeeker); ok {
		reader = fr
	} else {
		data, err := ioutil.ReadAll(file)
		if err != nil {
			return "", err
		}
		if !strings.HasPrefix(contentType, "image/") {
			contentType = http.DetectContentType(data)
		}
		reader = bytes.NewReader(data)
	}

	fullFileName := fmt.Sprintf("%v/%v", deviceID, fileName)

	params := &s3.PutObjectInput{
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
		return "", err
	}

	url := fmt.Sprintf("https://%s.s3.amazonaws.com/%s", bucketName, fullFileName)
	return url, nil
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
