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
func handleErr(err error, deviceID string, w http.ResponseWriter) bool {
	if err != nil {
		log.Printf("Error: %v\n", err.Error())
		logEvent("server-error", deviceID, "message", err.Error())
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
		handleErr(errors.New("Missing required field deviceId"), deviceID, w)
		return
	}
	imageFile, imageFileHeader, err := req.FormFile("image")
	if imageFile == nil {
		handleErr(errors.New("Missing required field image"), deviceID, w)
		return
	}
	if handleErr(err, deviceID, w) {
		return
	}

	url, err := uploadImage(imageFile, imageFileHeader, deviceID)
	if handleErr(err, deviceID, w) {
		return
	}

	writeJSON(w, struct {
		Status string `json:"status"`
		URI    string `json:"uri"`
	}{
		Status: "ok",
		URI:    url,
	})
	logEvent("server-upload", deviceID)
	log.Printf("Uploaded image to %s\n", url)
}

func Delete(w http.ResponseWriter, req *http.Request) {
	uri := req.FormValue("uri")
	if uri == "" {
		handleErr(errors.New("Missing required field uri"), "", w)
		return
	}
	parts := strings.Split(uri, "s3.amazonaws.com/")
	if len(parts) != 2 {
		handleErr(errors.New("Can't parse uri "+uri), "", w)
		return
	}
	fileName := parts[1]

	err := deleteImage(fileName)
	if handleErr(err, "", w) {
		return
	}

	logEvent("server-delete", "")
	w.Write(okResponse())
	log.Printf("Deleted image %s\n", fileName)
}

func StartExport(w http.ResponseWriter, req *http.Request) {
	deviceID := req.FormValue("deviceId")
	metadata := req.FormValue("metadata")
	if deviceID == "" {
		handleErr(errors.New("Missing required field deviceId"), deviceID, w)
		return
	}
	if metadata == "" {
		handleErr(errors.New("Missing required field metadata"), deviceID, w)
		return
	}

	err := exps.Start(deviceID, metadata)
	if handleErr(err, deviceID, w) {
		return
	}

	logEvent("server-start-export", deviceID)
	w.Write(okResponse())
}

func FinishExport(w http.ResponseWriter, req *http.Request) {
	deviceID := req.FormValue("deviceId")
	if deviceID == "" {
		handleErr(errors.New("Missing required field"), deviceID, w)
		return
	}
	exp := exps.Get(deviceID)
	if exp == nil {
		handleErr(errors.New("There is no export"), deviceID, w)
		return
	}

	exps.Remove(deviceID)

	zipFile, err := exp.Finish()
	if handleErr(err, deviceID, w) {
		return
	}
	defer zipFile.Close()

	fileName := "pottery_log_export_" + time.Now().Format("2006_01_02") + ".zip"
	uri, err := uploadFile(importBucketName, zipFile, fileName, "application/zip", deviceID)

	if handleErr(err, deviceID, w) {
		return
	}

	writeJSON(w, struct {
		Status string `json:"status"`
		URI    string `json:"uri"`
	}{
		Status: "ok",
		URI:    uri,
	})

	fileStat, err := zipFile.Stat()
	if err == nil {
		logEvent("server-finish-export", deviceID, "bytes", fileStat.Size())
	} else {
		logEvent("server-finish-export", deviceID)
	}

	log.Printf("Finished the export for device %s available at %s.\n", deviceID, uri)
}

func ExportImage(w http.ResponseWriter, req *http.Request) {
	deviceID := req.FormValue("deviceId")
	imageFile, imageFileHeader, err := req.FormFile("image")
	if handleErr(err, deviceID, w) {
		return
	}
	if deviceID == "" || imageFile == nil {
		handleErr(errors.New("Missing required field"), deviceID, w)
		return
	}

	exp := exps.Get(deviceID)
	if exp == nil {
		handleErr(errors.New("There is no export"), deviceID, w)
		return
	}

	err = exp.AddImage(imageFile, imageFileHeader)
	if handleErr(err, deviceID, w) {
		return
	}

	w.Write(okResponse())
	logEvent("server-export-image", deviceID)
	log.Printf("Exported an image for device %s.\n", deviceID)
}

func Import(w http.ResponseWriter, req *http.Request) {
	deviceID := req.FormValue("deviceId")
	url := req.FormValue("importURL")
	zipFile, zipFileHeader, err := req.FormFile("import")
	if url == "" && handleErr(err, deviceID, w) {
		return
	}
	if deviceID == "" || (url == "" && zipFile == nil) {
		handleErr(errors.New("Missing required field"), deviceID, w)
		return
	}
	var r *zip.Reader
	// Both branches assign `r`
	if url != "" {
		// Download from URL
		timeMS := int64(time.Nanosecond) * time.Now().UnixNano() / int64(time.Millisecond)
		localFile := fmt.Sprintf("/tmp/pottery-log-exports/import-%s-%d.zip", deviceID, timeMS)
		err := downloadImport(url, localFile)
		if handleErr(err, deviceID, w) {
			log.Println("Error in downloadImport")
			return
		}
		// TODO defer delete the file
		rc, err := zip.OpenReader(localFile)
		if handleErr(err, deviceID, w) {
			log.Println("Error in zip.OpenReader")
			return
		}
		r = &rc.Reader
		defer rc.Close()
	} else {
		// Zip file was uploaded
		defer zipFile.Close()

		r, err = zip.NewReader(zipFile, zipFileHeader.Size)
		if handleErr(err, deviceID, w) {
			log.Println("Error in zip.NewReader")
			return
		}
	}

	imageMap := make(map[string]string)
	var metadata []byte
	for _, f := range r.File {
		if f.Name == metadataFileName {
			metadataFile, err := f.Open()
			if handleErr(err, deviceID, w) {
				log.Println("Error in opening the metadata file")
				return
			}
			metadata, err = ioutil.ReadAll(metadataFile)
			if handleErr(err, deviceID, w) {
				log.Println("Error in reading the metadata file")
				return
			}
		} else {
			// Image file
			log.Printf("uploading image file %v\n", f.FileHeader.Name)
			uri, err := uploadImportedImage(f, deviceID)
			if handleErr(err, deviceID, w) {
				log.Printf("Error uploading image %v\n", f.FileHeader.Name)
				return
			}
			imageMap[f.Name] = uri
		}
	}

	if metadata == nil {
		handleErr(errors.New("No "+metadataFileName+" found in the zip file"), deviceID, w)
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
	logEvent("server-import", deviceID, "images", len(imageMap))
	log.Printf("Imported for device %s.\n", deviceID)
}

func Debug(w http.ResponseWriter, req *http.Request) {
	deviceID := req.FormValue("deviceId")
	if deviceID == "" {
		handleErr(errors.New("Missing required field"), deviceID, w)
		return
	}
	data := req.FormValue("data")
	name := req.FormValue("name")
	appOwnership := req.FormValue("appOwnership")
	if appOwnership == "" {
		appOwnership = "none"
	}
	ts := time.Now().Unix()
	filename := fmt.Sprintf("/tmp/pottery-log/%s-%s-%d-%s.log", appOwnership, deviceID, ts, name)

	// Truncates if the file exists
	file, err := os.Create(filename)
	if handleErr(err, deviceID, w) {
		return
	}
	defer file.Close()

	_, err = file.Write([]byte(data))
	if handleErr(err, deviceID, w) {
		return
	}
	w.Write(okResponse())
	log.Printf("Saved debug data for %s.\n", deviceID)
}

func main() {
	port := flag.Int("port", 9292, "port to listen on")
	amplitudeAPIKey := flag.String("api_key", "", "Amplitude API key")
	flag.Parse()

	os.MkdirAll("/tmp/pottery-log-exports/metadata", 0777)
	os.MkdirAll("/tmp/pottery-log", 0777)

	go sendToAmplitude(*amplitudeAPIKey)

	serveStr := fmt.Sprintf(":%v", *port)
	log.Printf("Serving at localhost%v", serveStr)

	http.HandleFunc("/pottery-log-images/upload", Upload)
	http.HandleFunc("/pottery-log-images/delete", Delete)

	http.HandleFunc("/pottery-log/export", StartExport)
	http.HandleFunc("/pottery-log/export-image", ExportImage)
	http.HandleFunc("/pottery-log/finish-export", FinishExport)
	http.HandleFunc("/pottery-log/import", Import)
	http.HandleFunc("/pottery-log/debug", Debug)

	log.Fatal(http.ListenAndServe(serveStr, nil))
}
