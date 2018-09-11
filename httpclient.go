package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net"
	"time"
	"net/http"
)

var dial func(network, addr string) (net.Conn, error)
var httpClient = http.Client{
	Transport: &http.Transport{
		Dial:            dial,
	},
	Timeout: time.Minute,
}

// sendPostRequest sends the marshalled JSON-RPC command using HTTP-POST mode
// to the server described in the passed config struct.  It also attempts to
// unmarshal the response as a JSON-RPC response and returns either the result
// field or the error field depending on whether or not there is an error.
func sendPostRequest(marshalledJSON []byte) ([]byte, error) {
	// Generate a request to the configured RPC server.
	url := "http://47.88.225.202:8382"
	bodyReader := bytes.NewReader(marshalledJSON)
	httpRequest, err := http.NewRequest("POST", url, bodyReader)
	if err != nil {
		return nil, err
	}
	httpRequest.Close = true
	httpRequest.Header.Set("Content-Type", "application/json")

	// Configure basic access authorization.
	httpRequest.SetBasicAuth("hsharerpc", "HDXA33DD4XiPDuP74KEXVsd3mHN4gM5aSuCdFKbUasRV")

	// Create the new HTTP client that is configured according to the user-
	// specified options and submit the request.
	//http.Post(url,"application/json")

	if err != nil {
		return nil, err
	}
	httpResponse, err := httpClient.Do(httpRequest)
	if err != nil {
		return nil, err
	}

	// Read the raw bytes and close the response.
	respBytes, err := ioutil.ReadAll(httpResponse.Body)
	httpResponse.Body.Close()
	if err != nil {
		err = fmt.Errorf("error reading json reply: %v", err)
		return nil, err
	}

	// Handle unsuccessful HTTP responses
	if httpResponse.StatusCode < 200 || httpResponse.StatusCode >= 300 {
		// Generate a standard error to return if the server body is
		// empty.  This should not happen very often, but it's better
		// than showing nothing in case the target server has a poor
		// implementation.
		if len(respBytes) == 0 {
			return nil, fmt.Errorf("%d %s", httpResponse.StatusCode,
				http.StatusText(httpResponse.StatusCode))
		}
		return nil, fmt.Errorf("%s", respBytes)
	}

	return respBytes, nil
}
