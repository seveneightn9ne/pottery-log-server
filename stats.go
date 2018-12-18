package main

import (
	"encoding/json"
	"log"
	"net/http"
	"net/url"
)

var statChan chan map[string]interface{}

func init() {
	statChan = make(chan map[string]interface{}, 1000)
}

func logEvent(name, deviceID string, tags ...interface{}) {
	event := make(map[string]interface{})
	event["event_type"] = name

	if deviceID == "" {
		deviceID = "1"
	}
	event["device_id"] = deviceID

	for i := 0; i < len(tags); i += 2 {
		if len(tags) > i+1 {
			if key, ok := tags[i].(string); ok {
				event[key] = tags[i+1]
			} else {
				log.Printf("Bad tag name for event %v: %v\n", name, tags[i])
				continue
			}
		}
	}

	statChan <- event
}

func sendToAmplitude(apiKey string) {
	if apiKey == "" {
		log.Print("Skipping Amplitude logging because no api_key provided.\n")
		return
	}

	client := &http.Client{}
	url := url.URL{
		Scheme: "https",
		Host:   "api.amplitude.com",
		Path:   "/httpapi",
	}
	query := url.Query()
	for {
		event := <-statChan
		jsonEvent, err := json.Marshal(event)
		if err != nil {
			log.Printf("Error during Amplitude event marshal: %v\n", err)
			continue
		}

		query.Set("api_key", apiKey)
		query.Set("event", string(jsonEvent))
		url.RawQuery = query.Encode()

		resp, err := client.Get(url.String())
		if err != nil {
			log.Printf("Error sending Amplitude request: %v\n", err)
			continue
		}
		if resp.StatusCode > 204 {
			log.Printf("Amplitude returned status %v\n", resp.StatusCode)
		}
	}
}
