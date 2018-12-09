package main

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

func okResponse() []byte {
	return []byte("{\"status\": \"ok\"}")
}
func writeJSON(w http.ResponseWriter, obj interface{}) {
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
		log.Printf("Error: %v\n", err.Error())
		w.WriteHeader(500)
		writeJSON(w, struct {
			Status  string `json:"status"`
			Message string `json:"message"`
		}{
			Status:  "error",
			Message: err.Error(),
		})
		return true
	}
	return false
}

func Upload(w http.ResponseWriter, req *http.Request) {
	deviceID := req.FormValue("deviceId")
	if deviceID == "" {
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

	url, err := uploadImage(imageFile, imageFileHeader, deviceID)
	if handleErr(err, w) {
		return
	}

	writeJSON(w, struct {
		Status string `json:"status"`
		URI    string `json:"uri"`
	}{
		Status: "ok",
		URI:    url,
	})
	log.Printf("Uploaded image to %s\n", url)
}

func Delete(w http.ResponseWriter, req *http.Request) {
	uri := req.FormValue("uri")
	if uri == "" {
		handleErr(errors.New("Missing required field uri"), w)
		return
	}
	parts := strings.Split(uri, "s3.amazonaws.com/")
	if len(parts) != 2 {
		handleErr(errors.New("Can't parse uri "+uri), w)
		return
	}
	fileName := parts[1]

	err := deleteImage(fileName)
	if handleErr(err, w) {
		return
	}

	w.WriteHeader(200)
	log.Printf("Deleted image %s\n", fileName)
}

func StartExport(w http.ResponseWriter, req *http.Request) {
	deviceID := req.FormValue("deviceId")
	metadata := req.FormValue("metadata")
	if deviceID == "" || metadata == "" {
		handleErr(errors.New("Missing required field"), w)
		return
	}

	err := exps.Start(deviceID, metadata)
	if handleErr(err, w) {
		return
	}

	w.Write(okResponse())
}

func FinishExport(w http.ResponseWriter, req *http.Request) {
	deviceID := req.FormValue("deviceId")
	if deviceID == "" {
		handleErr(errors.New("Missing required field"), w)
		return
	}
	exp := exps.Get(deviceID)
	if exp == nil {
		handleErr(errors.New("There is no export"), w)
		return
	}

	exps.Remove(deviceID)

	zipFile, err := exp.Finish()
	if handleErr(err, w) {
		return
	}
	defer zipFile.Close()

	fileName := "pottery_log_export_" + time.Now().Format("2006_01_02") + ".zip"
	uri, err := uploadFile(importBucketName, zipFile, fileName, "application/zip", deviceID)

	if handleErr(err, w) {
		return
	}

	writeJSON(w, struct {
		Status string `json:"status"`
		URI    string `json:"uri"`
	}{
		Status: "ok",
		URI:    uri,
	})

	log.Printf("Finished the export for device %s available at %s.\n", deviceID, uri)
}

func ExportImage(w http.ResponseWriter, req *http.Request) {
	deviceID := req.FormValue("deviceId")
	imageFile, imageFileHeader, err := req.FormFile("image")
	if handleErr(err, w) {
		return
	}
	if deviceID == "" || imageFile == nil {
		handleErr(errors.New("Missing required field"), w)
		return
	}

	exp := exps.Get(deviceID)
	if exp == nil {
		handleErr(errors.New("There is no export"), w)
		return
	}

	err = exp.AddImage(imageFile, imageFileHeader)
	if handleErr(err, w) {
		return
	}

	w.Write(okResponse())
	log.Printf("Exported an image for device %s.\n", deviceID)
}

func Import(w http.ResponseWriter, req *http.Request) {
	deviceID := req.FormValue("deviceId")
	zipFile, zipFileHeader, err := req.FormFile("import")
	if handleErr(err, w) {
		return
	}
	if deviceID == "" || zipFile == nil {
		handleErr(errors.New("Missing required field"), w)
		return
	}
	defer zipFile.Close()

	r, err := zip.NewReader(zipFile, zipFileHeader.Size)
	if handleErr(err, w) {
		return
	}
	/*
		importName := deviceId
		parts := strings.Split(zipFileHeader.Filename, ".")
		if len(parts) == 2 {
			importName = parts[0]
		}
		fileName := importName + ""
	*/
	imageMap := make(map[string]string)
	var metadata []byte
	for _, f := range r.File {
		if f.Name == metadataFileName {
			metadataFile, err := f.Open()
			if handleErr(err, w) {
				return
			}
			metadata, err = ioutil.ReadAll(metadataFile)
			if handleErr(err, w) {
				return
			}
		} else {
			// Image file
			uri, err := uploadImportedImage(f, deviceID)
			if handleErr(err, w) {
				return
			}
			imageMap[f.Name] = uri
		}
	}

	if metadata == nil {
		handleErr(errors.New("No "+metadataFileName+" found in the zip file"), w)
		return
	}

	writeJSON(w, struct {
		Status   string            `json:"status"`
		Metadata string            `json:"metadata"`
		ImageMap map[string]string `json:"image_map"`
	}{
		Status:   "ok",
		Metadata: string(metadata),
		ImageMap: imageMap,
	})
	log.Printf("Imported %s for device %s.\n", zipFileHeader.Filename, deviceID)
}

func main() {
	port := flag.Int("port", 9292, "port to listen on")
	flag.Parse()
	os.MkdirAll("/tmp/pottery-log-exports", 0777)
	serveStr := fmt.Sprintf(":%v", *port)
	log.Printf("Serving at localhost%v", serveStr)

	http.HandleFunc("/pottery-log-images/upload", Upload)
	http.HandleFunc("/pottery-log-images/delete", Delete)

	http.HandleFunc("/pottery-log/export", StartExport)
	http.HandleFunc("/pottery-log/export-image", ExportImage)
	http.HandleFunc("/pottery-log/finish-export", FinishExport)
	http.HandleFunc("/pottery-log/import", Import)

	log.Fatal(http.ListenAndServe(serveStr, nil))
}
