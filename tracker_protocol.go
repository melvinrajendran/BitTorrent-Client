/*
 * Functions to handle the Tracker HTTP/HTTPS Protocol.
 */

package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/marksamman/bencode"
)

// Number of bytes that the client has uploaded, has downloaded, and has to download, respectively
var (
	uploaded   int64 = 0
	downloaded int64 = 0
	left       int64
)
// Channel via which to send the last TCP connection that was established
var connChannel = make(chan net.Conn)
// Last tracker response received
var trackerResponse map[string]interface{}

// Sends a tracker request to the scrape URL, receives the tracker response, and prints it.
func scrapeTracker() {

	// Initialize all query parameters
	params := url.Values {
		"info_hash": {string(infoHash)},
	}

	// Get the index of the last slash in the announce URL
	lastSlashIndex := strings.LastIndex(announce, "/")

	// If (1) there is no slash, or (2) the text immediately following the last slash is not 'announce', return
	if lastSlashIndex == -1 || len(announce) < lastSlashIndex + 9 || announce[lastSlashIndex + 1:lastSlashIndex + 9] != "announce" {
		return
	}

	// Compute the scrape URL
	scrapeURL := announce[:lastSlashIndex + 1] + "scrape" + announce[lastSlashIndex + 9:]

	// Parse the scrape URL
	parsedURL, err := url.Parse(scrapeURL)
	assert(err == nil, "Error parsing the scrape URL")

	// Establish a TCP connection to the tracker
	conn, err := net.Dial("tcp", parsedURL.Host)
	assert(err == nil, "Error creating a TCP connection to the tracker")
	defer conn.Close()

	// Create the HTTP tracker request
	request := "GET " + scrapeURL[strings.LastIndex(scrapeURL, "/"):] + "?" + params.Encode() + " HTTP/1.1\r\n" +
		"Host: " + parsedURL.Host + "\r\n" +
		"Connection: close\r\n" +
		"\r\n"

	// Store the tracker request in a buffer
	buffer := new(bytes.Buffer)
	buffer.WriteString(request)

	// Send the tracker request
	_, err = conn.Write(buffer.Bytes())
	assert(err == nil, "Error sending the tracker request")

	// Receive the tracker response from the tracker
	buffer = readLoop(conn)

	// Parse the tracker scrape response
	scrapeResponse := parseTrackerResponse(buffer, true)

	// Get the files dictionary
	files, ok := scrapeResponse["files"].(map[string]interface{})
	assert(ok, "Error getting the files dictionary")

	// Get the flags dictionary
	flags, ok := scrapeResponse["flags"].(map[string]interface{})

	// Print the tracker scrape response
	fmt.Println("====================== Tracker Scrape ======================")
	fmt.Println("Files Dictionary:")
	for key, value := range files {
		valueMap := value.(map[string]interface{})
		fmt.Printf("\tKey:\n\t\t%v\n\tValue:\n\t\tComplete: %v\n\t\tDownloaded: %v\n\t\tIncomplete: %v\n", key, valueMap["complete"], valueMap["downloaded"], valueMap["incomplete"])
	}
	if ok {
		fmt.Println("Flags:")
		for key, value := range flags {
			fmt.Printf("\tKey: %v, Value: %v\n", key, value)
		}
	}
}

// Sends a tracker request with the parameter event.
func sendTrackerRequest(event string) {

	// Initialize all query parameters
	params := url.Values{}
	params.Add("info_hash", string(infoHash))
	params.Add("peer_id", peerID)
	params.Add("port", strconv.FormatInt(port, 10))
	params.Add("uploaded", strconv.FormatInt(uploaded, 10))
	params.Add("downloaded", strconv.FormatInt(downloaded, 10))
	params.Add("left", strconv.FormatInt(left, 10))
	if compact {
		params.Add("compact", "1")
	} else {
		params.Add("compact", "0")
	}
	if event != "" {
		assert(event == "started" || event == "stopped" || event == "completed", "Invalid event, must be one of 'started', 'stopped', or 'completed'")
		params.Add("event", event)
	}

	// Parse the announce URL
	parsedURL, err := url.Parse(announce)
	assert(err == nil, "Error parsing the announce URL")

	// Attempt to establish a TCP connection to the tracker
	conn, err := net.Dial("tcp", parsedURL.Host)
	assert(err == nil, "Error forming a TCP connection to the tracker")

	// Send the connection into the channel
	connChannel <- conn

	// Create the HTTP tracker request
	request := "GET " + "/announce?" + params.Encode() + " HTTP/1.1\r\n" +
		"Host: " + parsedURL.Host + "\r\n" +
		"Connection: close\r\n" +
		"\r\n"

	// Store the tracker request in a buffer
	buffer := new(bytes.Buffer)
	buffer.WriteString(request)

	// Send the tracker request
	_, err = conn.Write(buffer.Bytes())
	assert(err == nil, "Error sending the tracker request")
}

// Receives a tracker response and returns the corresponding decoded dictionary.
func receiveTrackerResponse(conn net.Conn) {
	defer conn.Close()
	
	// Read the tracker response from the connection
	buffer := readLoop(conn)

	// Parse the tracker response and update the last tracker response received
	trackerResponse = parseTrackerResponse(buffer, false)

	if verbose {
		// Print the tracker response
		printTrackerResponse(trackerResponse)
	}
}

// Parses the parameter buffer containing an HTTP tracker response and returns the decoded dictionary.
func parseTrackerResponse(buffer *bytes.Buffer, isScrape bool) map[string]interface{} {

	// Get the header and body of the tracker response
	header := buffer.Bytes()[:(bytes.Index(buffer.Bytes(), []byte("\r\n\r\n")))]
	body := buffer.Bytes()[(bytes.Index(buffer.Bytes(), []byte("\r\n\r\n")) + 4):]

	// Assert that the HTTP request succeeded
	assert(strings.Split(strings.Split(string(header), "\r\n")[0], " ")[2] == "OK", "Unsuccessful tracker request")

	// Check if the response's transfer encoding is chunked
	if bytes.Contains(header, []byte("Transfer-Encoding: chunked")) {

		// Initialize a buffer for the complete response body
		completeBody := new(bytes.Buffer)

		// Iterate while there are chunks remaining
		for {

			// Compute the length of the current chunk in bytes
			len, err := strconv.ParseInt(string(body[:(bytes.Index(body, []byte("\r\n")))]), 16, 64)
			assert(err == nil, "Error converting the chunk length to an integer")

			// If the length is not positive, all chunks have been processed
			if len <= 0 {
				break
			}

			// Update the chunked and complete response bodies
			body = body[(bytes.Index(body, []byte("\r\n")) + 2):]
			completeBody.Write(body[:len])
			body = body[(len + 2):]
		}

		// Reset the buffer and store the complete tracker response body
		buffer.Reset()
		buffer.Write(completeBody.Bytes())
	} else {
		// Reset the buffer and store the complete tracker response body
		buffer.Reset()
		buffer.Write(body)
	}

	// Decode the tracker response
	dict, err := bencode.Decode(buffer)
	assert(err == nil, "Error decoding the tracker response")

	// Check if the tracker request is not a scrape and the tracker response is compact
	if !isScrape && compact {

		// Initialize the peers string and peers list of dictionaries
		peersStr := dict["peers"].(string)
		var peersList []interface{}

		// Iterate across the peers in the peers string
		for i := 0; i < len(peersStr); i += 6 {

			// Get the current peer's address and port
			address := peersStr[i : i + 4]
			port := peersStr[i + 4 : i + 6]

			// Append the current peer to the peer list
			peersList = append(peersList, map[string]interface{}{
				"peer id": "",
				"ip":      net.IP(address).String(),
				"port":    int(binary.BigEndian.Uint16([]byte(port))),
			})
		}

		// Replace the peers string with the peers list of dictionaries
		dict["peers"] = peersList
	}

	return dict
}

// Prints the parameter tracker response.
func printTrackerResponse(trackerResponse map[string]interface{}) {
	fmt.Println("===================== Tracker Response =====================")
	for key, value := range trackerResponse {
		if key == "peers" {
			fmt.Println("Peer Dictionary:")
			peers := trackerResponse["peers"].([]interface{})
			for i, peer := range peers {
				p := peer.(map[string]interface{})
				fmt.Printf("\tPeer %v:\tIP: %v\tPort: %v\n", i, p["ip"], p["port"])
			}
		} else {
			fmt.Printf("Key: %v, Value: %v\n", key, value)
		}
	}
	fmt.Println("===================== Transfer Details =====================")
}

// Sends tracker requests on an interval.
func handleTrackerRequests() {

	if verbose {
		// Send, receive, and print a tracker scrape
		scrapeTracker()
	}

	// Initialize the number of bytes that the client has to download
	left = fileLength

	// Loop until the client receives a tracker response
	for {

		// Check if the client has not received a tracker response
		if trackerResponse == nil {

			// Send a tracker request containing the event 'started'
			sendTrackerRequest("started")

			// Sleep for 10 seconds
			time.Sleep(10 * time.Second)
		} else {
			break
		}
	}

	// Loop indefinitely
	for {

		// Sleep for the interval specified in the last tracker response
		time.Sleep(time.Duration(trackerResponse["interval"].(int64)) * time.Second)

		// Send a periodic tracker request
		sendTrackerRequest("")
	}
}

// Recieves tracker requests indefinitely.
func handleTrackerResponses() {

	// Loop indefinitely
	for {

		// Receive a connection from the channel
		conn, ok := <-connChannel

		// If the channel is closed, exit the loop
		if !ok {
			break
		}

		// Receive the tracker response
		receiveTrackerResponse(conn)
	}
}