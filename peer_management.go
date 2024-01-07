/*
 * Functions to manage connections to peers in the swarm.
 */

package main

import (
	"fmt"
	"math"
	"net"
	"sync"
	"time"
)

// Maximum number of unfulfilled requests to queue
const maxRequestQueueSize = 10

// Array of address-port pairs of connections that the client attempted to form with remote peers
var attemptedConnections []string
// Array of connections that the client has with remote peers
var connections = make([]*Connection, 0)
// Mutex for the array of connections
var connectionsMu sync.RWMutex

// Stores the state of a connection that the client has with a remote peer
type Connection struct {
	conn 			           net.Conn
	completedHandshake   bool
	amChoking            bool
	amInterested         bool
	peerChoking          bool
	peerInterested       bool
	bitfield             []byte
	bytesDownloaded      int64
	bytesUploaded        int64
	sentRequestQueue     []*Request
	receivedRequestQueue []*Request
	startTime            time.Time
	lastSentTime         time.Time
	lastReceivedTime     time.Time
}

func newConnection(conn net.Conn) *Connection {
	return &Connection {
		conn:                 conn,
		completedHandshake:   false,
		amChoking:            true,
		amInterested:         false,
		peerChoking:          true,
		peerInterested:       false,
		bitfield:             make([]byte, int64(math.Ceil(float64(numPieces) / float64(8)))),
		bytesDownloaded:      0,
		bytesUploaded:        0,
		sentRequestQueue:     make([]*Request, 0),
		receivedRequestQueue: make([]*Request, 0),
		startTime:            time.Now(),
		lastSentTime:         time.Now(),
		lastReceivedTime:     time.Now(),
	}
}

// Returns the download/upload speed of the parameter connection.
func getSpeed(connection *Connection) float64 {
	if !downloadedFile {
		return float64(connection.bytesDownloaded) / time.Since(connection.startTime).Seconds()
	}

	return float64(connection.bytesUploaded) / time.Since(connection.startTime).Seconds()
}

// Handle actively forming connections to other peers.
func handleFormingConnections() {

	// Wait while a tracker response has not been received
	for trackerResponse == nil {}

	// Loop indefinitely
	for {

		// Get the list of peers
		peers, ok := trackerResponse["peers"].([]interface{})
		if !ok {
			continue
		}

		// Iterate across the list of peers
		for _, peer := range peers {

			// Compute the current peer's address-port pair
			peerDict := peer.(map[string]interface{})
			peerID := peerDict["peer id"].(string)
			peerAddrPort := fmt.Sprintf("%s:%d", peerDict["ip"], peerDict["port"])

			shouldAttempt := true

			// Determine if there has already been an attempt to connect to the peer
			for _, addrPort := range attemptedConnections {
				if addrPort == peerAddrPort {
					shouldAttempt = false
					break
				}
			}

			// Determine if there is already a connection to the peer
			for _, connection := range connections {
				if connection.conn.RemoteAddr().String() == peerAddrPort {
					shouldAttempt = false
					break
				}
			}

			// Check if forming a connection to the peer should be attempted, and there are less than 30 peer connections
			if shouldAttempt && len(connections) < 30 {

				// Add the peer's address-port pair to the array of attempted connections
				attemptedConnections = append(attemptedConnections, peerAddrPort)

				// Start a goroutine to attempt to form a connection
				go attemptFormingConnection(peerAddrPort, peerID)
			}
		}
	}
}

// Attempts to form a TCP connection with the peer with the parameter address-port pair.
func attemptFormingConnection(peerAddrPort string, peerID string) {

	// Initialize the number of attempts
	numAttempts := 0

	// Loop up to 5 times
	for {

		// Attempt to establish a TCP connection to the peer
		conn, err := net.Dial("tcp4", peerAddrPort)
		
		// Increment the number of attempts
		numAttempts++

		// Check if an error occurred
		if err != nil {
			if numAttempts < 5 {
				// If there are attempts remaining, retry after 5 seconds
				time.Sleep(5 * time.Second)
				continue
			} else {
				// Else, break
				if verbose {
					fmt.Printf("[%s] Error actively forming a TCP connection\n", peerAddrPort)
				}
				break
			}
		}

		// Handle a successful connection
		go handleSuccessfulConnection(conn, peerID)

		if verbose {
			fmt.Printf("[%s] Actively formed a TCP connection\n", conn.RemoteAddr())
		}

		break
	}

	// Remove the peer's address-port pair from the list of attempted connections
	for i, attemptedConnection := range attemptedConnections {
		if attemptedConnection == peerAddrPort {
			attemptedConnections = append(attemptedConnections[:i], attemptedConnections[i + 1:]...)
			break
		}
	}
}

// Handles incoming connections from other peers.
func handleIncomingConnections() {

	// Compute the client's address-port pair
	clientAddrPort := fmt.Sprintf(":%d", port)

	// Listen for incoming connections
	listener, err := net.Listen("tcp4", clientAddrPort)
	assert(err == nil, "Error listening for incoming connections")
	defer listener.Close()

	// Loop indefinitely
	for {

		// Check if there are less than 55 peer connections
		if len(connections) < 55 {

			// Accept an incoming connection
			conn, err := listener.Accept()
			if err != nil {
				// Handle a failed connection
				if verbose {
					fmt.Println("[UNKNOWN] Error accepting an incoming TCP connection")
				}
				continue
			}
			
			// Handle a successful connection
			go handleSuccessfulConnection(conn, "")

			if verbose {
				fmt.Printf("[%s] Accepted an incoming TCP connection\n", conn.RemoteAddr())
			}
		}
	}
}

// Sends keep-alive messages periodically.
func handleKeepAliveMessages() {

	// Loop indefinitely
	for {

		connectionsMu.RLock()

		// Iterate across the peer connections
		for _, connection := range connections {

			// Check if at least 1 minute has passed since the client sent a message
			if time.Since(connection.lastSentTime) >= 1 * time.Minute {

				// Serialize and send keep-alive message
				keepAliveMessage := newKeepAliveMessage()
				sendMessage(connection, keepAliveMessage.serialize(), "keep-alive", fmt.Sprintf("[%s] Sent keep-alive message", connection.conn.RemoteAddr()))
			}
		}

		connectionsMu.RUnlock()
	}
}

// Closes the parameter connection and removes it the global array.
func closeConnection(connection *Connection) {

	// Close the connection
	connection.conn.Close()

	// Remove the connection from the global array
	for i, conn := range connections {
		if conn.conn == connection.conn {
			connections = append(connections[:i], connections[i + 1:]...)
			break
		}
	}

	if verbose {
		fmt.Printf("[%s] Closed the TCP connection\n", connection.conn.RemoteAddr())
	}
}

// Handles timed-out connections by closing them.
func handleConnectionTimeouts() {

	// Loop indefinitely
	for {

		connectionsMu.Lock()

		// Iterate across the peer connections
		for _, connection := range connections {

			// Check if at least 2 minutes have passed since the client received a message
			if time.Since(connection.lastReceivedTime) >= 2 * time.Minute {

				// Close the connection
				closeConnection(connection)

				break
			}
		}

		connectionsMu.Unlock()
	}
}