package main

import (
	"archive/zip"
	"errors"
	"io"
	"log"
	"mime/multipart"
	"os"
	"sync"
)

const metadataFileName = "metadata.json"

var exps = NewExports()

type export struct {
	mu       sync.Mutex
	f        *os.File
	w        *zip.Writer
	finished bool
}

type exports struct {
	mu      sync.Mutex
	exports map[string]*export
}

// NewExports sets up the exports
func NewExports() *exports {
	return &exports{
		mu:      sync.Mutex{},
		exports: make(map[string]*export),
	}
}

func (e *exports) Start(deviceID, metadata string) error {
	exp, err := NewExport(deviceID, metadata)
	if err != nil {
		return err
	}

	e.mu.Lock()
	e.exports[deviceID] = exp
	e.mu.Unlock()

	return nil
}

func (e *exports) Get(deviceID string) *export {
	e.mu.Lock()
	defer e.mu.Unlock()

	return e.exports[deviceID]
}

func (e *exports) Remove(deviceID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.exports, deviceID)

}

// NewExport adds & sets up an export
func NewExport(deviceID, metadata string) (*export, error) {
	location := "/tmp/pottery-log-exports/" + deviceID + ".zip"
	log.Printf("Starting export at %v\n", location)

	saveMetadataFile(metadata, deviceID)

	// Truncates if the file exists
	file, err := os.Create(location)
	if err != nil {
		return nil, err
	}
	exp := &export{
		mu:       sync.Mutex{},
		f:        file,
		w:        zip.NewWriter(file),
		finished: false,
	}

	metadataFile, err := exp.w.Create(metadataFileName)
	if err != nil {
		exp.f.Close()
		return nil, err
	}

	_, err = metadataFile.Write([]byte(metadata))
	if err != nil {
		exp.f.Close()
		return nil, err
	}

	return exp, nil
}

func (e *export) AddImage(imageFile multipart.File, imageFileHeader *multipart.FileHeader) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.finished {
		return errors.New("The export has finished")
	}

	zipWriter, err := e.w.CreateHeader(&zip.FileHeader{
		Name:    imageFileHeader.Filename,
		Method:  zip.Deflate,
		Comment: imageFileHeader.Header.Get("Content-Type"),
	})
	if err != nil {
		return err
	}

	_, err = io.Copy(zipWriter, imageFile)
	return err
}

func (e *export) Finish() (*os.File, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.finished {
		return nil, errors.New("The export has finished")
	}
	e.finished = true

	err := e.w.Close()
	if err != nil {
		e.f.Close()
		return nil, err
	}

	_, err = e.f.Seek(0, 0)
	if err != nil {
		e.f.Close()
		return nil, err
	}

	return e.f, nil
}

func saveMetadataFile(metadata, deviceID string) error {
	location := "/tmp/pottery-log-exports/metadata/" + deviceID + ".json"

	// Truncates if the file exists
	file, err := os.Create(location)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = file.Write([]byte(metadata))
	if err != nil {
		return err
	}

	return nil
}
