/*
 * BitTorrent client, implemented according to the v1.0 protocol specification:
 * https://wiki.theory.org/BitTorrentSpecification.
 */

package main

import (
	"fmt"
	"math/rand"
)

// Peer ID of the client
var peerID string

func main() {

	// Parse and validate the command-line flags
	parseFlags()
	defer torrentFile.Close()

	// Generate the peer ID of the client
	peerID = generatePeerID()

	// Print the client's details
	printClientDetails()

	// Parse the torrent file
	parseTorrentFile(torrentFile)

	// // Declare and add a single goroutine to a wait group
	// var wg sync.WaitGroup
	// wg.Add(1)

	// // Start goroutines to send and receive tracker requests, respectively
	// go handleTrackerRequests()
	// go handleTrackerResponses()

	// // Start goroutines to actively form new connections and handle incoming connections, respectively
	// go handleFormingConnections()
	// go handleIncomingConnections()

	// // Start goroutine to periodically send unchoke messages and perform optimistic unchoking, respectively
	// go handleUnchokeMessages()
	// go handleOptimisticUnchoking()

	// // Start a goroutine to process incoming request messages
	// go handleRequestMessages()

	// // Start goroutine to periodically send keep-alive messages and close timed-out connections, respectively
	// go handleKeepAliveMessages()
	// go handleTimeouts()

	// go handleEndGame()
	// // Start a goroutine to handle shutting down gracefully
	// go handleShuttingDown(&wg)
	
	// // Wait for the shutdown goroutine to finish
	// wg.Wait()
}

// Generates and returns a peer ID for the client.
func generatePeerID() string {

	// Generate a random 12-digit integer
	randomInt := rand.Intn(1000000000000)

	// Format the random integer as a string
	randomStr := fmt.Sprintf("%012d", randomInt)

	// Compute the peer ID of the client by concatenating the client ID and client version to the random string
	peerID := "-ML0001-" + randomStr

	return peerID
}

// Prints the client's command-line flags and peer ID.
func printClientDetails() {
	fmt.Println("====================== Client Details ======================")
	fmt.Printf("Compact: %v\nPeer ID: %s\nPort: %d\nVerbose: %v\n", compact, peerID, port, verbose)
}