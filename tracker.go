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
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/marksamman/bencode"
)

// Number of bytes that the client has uploaded, has downloaded, and has to download, respectively
var (
	uploaded   int64 = 0
	downloaded int64 = 0
	left       int64
)

// Last tracker response received
var trackerResponse map[string]interface{}

// Create a channel via which to send the last TCP connection that was established
var connChannel = make(chan net.Conn)

// Contains the query parameters to be used in the HTTP tracker request.
type QueryParams struct {
	infoHash   []byte
	peerID     string
	port       int64
	uploaded   int64
	downloaded int64
	left       int64
	compact    bool
	event      string
}

// Returns the corresponding query string for the parameter query parameters.
func (queryParams *QueryParams) toQueryString() string {

	// Initialize the URL values
	values := url.Values{}

	// Add all query parameters to the URL values
	values.Add("info_hash", string(queryParams.infoHash))
	values.Add("peer_id", queryParams.peerID)
	values.Add("port", strconv.FormatInt(queryParams.port, 10))
	values.Add("uploaded", strconv.FormatInt(queryParams.uploaded, 10))
	values.Add("downloaded", strconv.FormatInt(queryParams.downloaded, 10))
	values.Add("left", strconv.FormatInt(queryParams.left, 10))
	if queryParams.compact {
		values.Add("compact", "1")
	} else {
		values.Add("compact", "0")
	}
	if queryParams.event != "" {
		assert(queryParams.event == "started" || queryParams.event == "stopped" || queryParams.event == "completed", "Invalid event, must be one of 'started', 'stopped', or 'completed'")
		values.Add("event", queryParams.event)
	}

	// Return the encoded query string
	return values.Encode()
}

// Sends a tracker request with the parameter event.
func sendTrackerRequest(event string) {

	// Initialize the query parameters
	params := QueryParams{
		infoHash:   infoHash,
		peerID:     peerID,
		port:       port,
		uploaded:   uploaded,
		downloaded: downloaded,
		left:       left,
		compact:    compact,
		event:      event,
	}

	// Parse the announce URL
	parsedURL, err := url.Parse(announce)
	assert(err == nil, "Error parsing the announce URL")

	// Establish a TCP connection to the tracker
	conn, err := net.Dial("tcp", parsedURL.Host)
	assert(err == nil, "Error creating a TCP connection to the tracker")

	// Send the connection into the channel
	connChannel <- conn

	// Create the HTTP tracker request
	request := "GET " + "/announce?" + params.toQueryString() + " HTTP/1.1\r\n" +
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

// Sends a tracker request to the scrape URL, receives the tracker response, and prints it.
func sendTrackerScrape() {

	// Initialize the URL values
	values := url.Values {
		"info_hash": {string(infoHash)},
	}

	// Get the index of the last slash in the announce URL
	lastSlashIndex := strings.LastIndex(announce, "/")

	// If there is no slash, return
	if lastSlashIndex == -1 {
		return
	}

	// If the text immediately following the last slash is not 'announce', return
	if len(announce) < lastSlashIndex + 9 || announce[lastSlashIndex + 1:lastSlashIndex + 9] != "announce" {
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
	request := "GET " + scrapeURL[strings.LastIndex(scrapeURL, "/"):] + "?" + values.Encode() + " HTTP/1.1\r\n" +
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
	buffer = new(bytes.Buffer)
	temp := make([]byte, 1024)

	for {
		// Read bytes from the connection into the temporary buffer
		n, err := conn.Read(temp)
		if err != nil {
			break
		}

		// Append the read bytes to the buffer
		buffer.Write(temp[:n])
	}

	// Get the header and body of the tracker response
	header := buffer.Bytes()[:(bytes.Index(buffer.Bytes(), []byte("\r\n\r\n")))]
	body := buffer.Bytes()[(bytes.Index(buffer.Bytes(), []byte("\r\n\r\n")) + 4):]

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

	// Decode the tracker scrape
	dict, err := bencode.Decode(buffer)
	assert(err == nil, "Error decoding the tracker scrape")

	// Get the files dictionary
	files, ok := dict["files"].(map[string]interface{})
	assert(ok, "Error getting the files dictionary")

	// Get the flags dictionary
	flags, ok := dict["flags"].(map[string]interface{})

	fmt.Println("====================== Tracker Scrape ======================")
	fmt.Println("Files Dictionary:")
	for key, value := range files {
		valueMap := value.(map[string]interface{})
		fmt.Printf("\tKey:\n\t\t%s\n\tValue:\n\t\tComplete: %#v\n\t\tDownloaded: %#v\n\t\tIncomplete: %#v\n", key, valueMap["complete"], valueMap["downloaded"], valueMap["incomplete"])
	}
	if ok {
		fmt.Println("Flags:")
		for key, value := range flags {
			fmt.Printf("\tKey: %s, Value: %#v\n", key, value)
		}
	}
}

// Receives a tracker response and returns the corresponding decoded dictionary.
func receiveTrackerResponse(conn net.Conn) {
	defer conn.Close()
	
	// Receive the tracker response from the tracker
	buffer := new(bytes.Buffer)
	temp := make([]byte, 1024)

	for {
		// Read bytes from the connection into the temporary buffer
		n, err := conn.Read(temp)
		if err != nil {
			break
		}

		// Append the read bytes to the buffer
		buffer.Write(temp[:n])
	}

	// Get the header and body of the tracker response
	header := buffer.Bytes()[:(bytes.Index(buffer.Bytes(), []byte("\r\n\r\n")))]
	body := buffer.Bytes()[(bytes.Index(buffer.Bytes(), []byte("\r\n\r\n")) + 4):]

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

	// Check if the tracker response is compact
	if compact {

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

	// Update the last tracker response received
	trackerResponse = dict

	if verbose {
		// Print the current tracker response
		printTrackerResponse(trackerResponse)
	}
}

// Sends tracker requests on an interval.
func handleTrackerRequests() {

	if verbose {
		// Send a tracker scrape
		sendTrackerScrape()
	}

	// Initialize the number of bytes that the client has to downloaded
	left = fileLength

	// Send a tracker request containing the event 'started'
	sendTrackerRequest("started")

	// Loop indefinitely
	for {

		// Check if the client has received a tracker response
		if trackerResponse != nil {

			// Sleep for the interval specified in the last tracker response
			time.Sleep(time.Duration(trackerResponse["interval"].(int64)) * time.Second)

			// Send a periodic tracker request
			sendTrackerRequest("")
		}
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

// Handles gracefully shutting down the client.
func handleShuttingDown(wg *sync.WaitGroup) {
	defer wg.Done()

	// Create a channel via which to receive OS signals
	sigChannel := make(chan os.Signal, 1)

	// Notify the channel on receiving the interrupt signal (Ctrl + C)
	signal.Notify(sigChannel, os.Interrupt, syscall.SIGINT)

	// Block until a signal is received
	<-sigChannel

	// Send a tracker request containing the event 'stopped'
	sendTrackerRequest("stopped")

	// Close the connection channel
	close(connChannel)
}

// Prints the parameter tracker response.
func printTrackerResponse(trackerResponse map[string]interface{}) {
	fmt.Println("===================== Tracker Response =====================")
	for key, value := range trackerResponse {
		if key == "peers" {
			fmt.Println("Peer List:")
			peers := trackerResponse["peers"].([]interface{})
			for idx, peer := range peers {
				fmt.Printf("\tPeer %2d:    ", idx+1)
				peerMap := peer.(map[string]interface{})
				if !compact {
					fmt.Printf("Peer ID: %s\tIP: %15s\tPort: %5d\n", peerMap["peer id"], peerMap["ip"], peerMap["port"])
				} else {
					fmt.Printf("IP: %15s\tPort: %5d\n", peerMap["ip"], peerMap["port"])
				}
			}
		} else {
			fmt.Printf("Key: %s, Value: %#v\n", key, value)
		}
	}
	fmt.Println("==================== Peer Wire Protocol ====================")
}
