package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
)

func okResponse() []byte {
	return []byte("{status: \"ok\"}")
}
func writeJson(w http.ResponseWriter, obj interface{}) {
	respStr, err := json.Marshal(obj)
	if err != nil {
		log.Printf("Error during JSON marshal: %v\n", err)
		return
	}
	w.Write([]byte(respStr))
}

// true if there was an error that we handled
func handleErr(err error, w http.ResponseWriter) bool {
	if err != nil {
		log.Printf("Error: %v", err.Error())
		w.WriteHeader(500)
		writeJson(w, struct {
			status  string
			message string
		}{
			status:  "error",
			message: err.Error(),
		})
		return true
	}
	return false
}

const bucketName = "pottery-log"

func Upload(svc *s3.S3) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, req *http.Request) {
		deviceId := req.FormValue("deviceId")
		if deviceId == "" {
			handleErr(errors.New("Missing required field deviceId"), w)
			return
		}
		imageFile, imageFileHeader, err := req.FormFile("image")
		if imageFile == nil {
			handleErr(errors.New("Missing required field image"), w)
			return
		}
		if handleErr(err, w) {
			return
		}
		imageData, err := ioutil.ReadAll(imageFile)
		if handleErr(err, w) {
			return
		}
		fileName := fmt.Sprintf("%v/%v", deviceId, imageFileHeader.Filename)

		params := &s3.PutObjectInput{
			Bucket:       aws.String(bucketName), // Required
			Key:          aws.String(fileName),   // Required
			ACL:          aws.String("public-read"),
			Body:         bytes.NewReader(imageData),
			CacheControl: aws.String("max-age=31556926"), // cachable forever
			ContentType:  aws.String(imageFileHeader.Header.Get("Content-Type")),
			Expires:      aws.Time(time.Now().Add(time.Hour * 24 * 365)),
			StorageClass: aws.String("STANDARD_IA"), // Infrequent Access
			//ContentDisposition: aws.String("ContentDisposition"),
			//ContentEncoding:    aws.String("ContentEncoding"),
			//ContentLanguage:    aws.String("ContentLanguage"),
			//ContentLength:      aws.Int64(1),
			//GrantFullControl:   aws.String("GrantFullControl"),
			//GrantRead:          aws.String("GrantRead"),
			//GrantReadACP:       aws.String("GrantReadACP"),
			//GrantWriteACP:      aws.String("GrantWriteACP"),
			//Metadata: map[string]*string{
			//    "Key": aws.String("MetadataValue"), // Required
			//    // More values...
			//},
			//RequestPayer:            aws.String("RequestPayer"),
			//SSECustomerAlgorithm:    aws.String("SSECustomerAlgorithm"),
			//SSECustomerKey:          aws.String("SSECustomerKey"),
			//SSECustomerKeyMD5:       aws.String("SSECustomerKeyMD5"),
			//SSEKMSKeyId:             aws.String("SSEKMSKeyId"),
			//ServerSideEncryption:    aws.String("ServerSideEncryption"),
			//Tagging:                 aws.String("TaggingHeader"),
			//WebsiteRedirectLocation: aws.String("WebsiteRedirectLocation"),
		}
		_, err = svc.PutObject(params)
		if awserr, ok := err.(awserr.Error); err != nil && ok {
			log.Printf("AWS Error: %+v\n", awserr)
		}
		if handleErr(err, w) {
			return
		}

		url := fmt.Sprintf("https://%s.s3.amazonaws.com/%s", bucketName, fileName)
		writeJson(w, struct {
			Status string `json:"status"`
			Uri    string `json:"uri"`
		}{
			Status: "ok",
			Uri:    url,
		})
		log.Printf("Uploaded image to %s\n", url)
	}
}

func Get(svc *s3.S3) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, req *http.Request) {
		w.Write(okResponse())
		log.Print("get")
	}
}

func Delete(svc *s3.S3) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, req *http.Request) {
		w.Write(okResponse())
		log.Printf("Delete request to ")
	}
}

func Copy(svc *s3.S3) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, req *http.Request) {
		w.Write(okResponse())
		log.Print("copy")
	}
}

func setupS3() *s3.S3 {
	sess := session.Must(session.NewSessionWithOptions(session.Options{
		Config: aws.Config{
			Region: aws.String("us-east-2"),
			CredentialsChainVerboseErrors: aws.Bool(true),
			//Credentials: credentials.NewSharedCredentials()
		},
		Profile: "pottery-log-server",
	}))
	return s3.New(sess)
}

func main() {
	port := flag.Int("port", 9000, "port to listen on")
	flag.Parse()
	serveStr := fmt.Sprintf(":%v", *port)
	log.Printf("Serving at localhost%v", serveStr)
	svc := setupS3()
	pfx := "/pottery-log-images/"
	http.HandleFunc(pfx+"upload", Upload(svc))
	http.HandleFunc(pfx+"get", Get(svc))
	http.HandleFunc(pfx+"delete", Delete(svc))
	http.HandleFunc(pfx+"copy", Copy(svc))
	log.Fatal(http.ListenAndServe(serveStr, nil))
}
