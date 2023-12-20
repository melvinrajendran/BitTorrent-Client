/*
 * Functions to handle the peer wire TCP protocol.
 */

package main

import (
	"fmt"
	"net"
	"time"
)

// Array of connections that the client has with remote peers
var connections []Connection
// Array of address-port pairs of connections that the client attempted to form with remote peers
var attemptedConnections []string

// Stores the state of a connection that the client has with a remote peer
type Connection struct {
	conn 			       net.Conn
	amChoking        bool
	amInterested     bool
	peerChoking      bool
	peerInterested   bool
	bytesDownloaded  int64
	bytesUploaded    int64
	startTime        time.Time
	lastSentTime     time.Time
	lastReceivedTime time.Time
}

func newConnection(conn net.Conn) Connection {
	connection := Connection {
		conn:             conn,
		amChoking:        true,
		amInterested:     false,
		peerChoking:      true,
		peerInterested:   false,
		bytesDownloaded:  0,
		bytesUploaded:    0,
		startTime:        time.Now(),
		lastSentTime:     time.Now(),
		lastReceivedTime: time.Now(),
	}

	return connection
}

// Handle actively forming connections to other peers.
func handleFormingConnections() {

	// Loop indefinitely
	for {

		// Check if the tracker response is not nil
		if trackerResponse != nil {

			// Get the list of peers
			peers, ok := trackerResponse["peers"].([]interface{})
			if !ok {
				continue
			}

			// Iterate across the list of peers
			for _, peer := range peers {

				// Compute the current peer's address-port pair
				peerDict := peer.(map[string]interface{})
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
					go attemptFormingConnection(peerAddrPort)
				}
			}
		}
	}
}

// Attempts to form a TCP connection with the peer with the parameter address-port pair.
func attemptFormingConnection(peerAddrPort string) {

	// Initialize the number of attempts
	numAttempts := 0

	// Loop up to 5 times
	for {

		// Attempt to establish a TCP connection to the peer
		conn, err := net.Dial("tcp", peerAddrPort)
		
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
		go handleAcceptedConnection(conn, true)

		if verbose {
			fmt.Printf("[%s] Actively formed a TCP connection\n", conn.RemoteAddr())
		}

		break
	}
}

// Handles incoming connections from other peers.
func handleIncomingConnections() {

	// Compute the client's address-port pair
	clientAddrPort := fmt.Sprintf(":%d", port)

	// Listen for incoming connections
	listener, err := net.Listen("tcp", clientAddrPort)
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
			go handleAcceptedConnection(conn, false)

			if verbose {
				fmt.Printf("[%s] Accepted an incoming TCP connection\n", conn.RemoteAddr())
			}
		}
	}
}

// Handles accepted connections with other peers.
func handleAcceptedConnection(conn net.Conn, formedConnection bool) {

	// Initialize a new connection and add it to the array
	connection := newConnection(conn)
	connections = append(connections, connection)

	
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