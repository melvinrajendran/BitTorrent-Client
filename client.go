/*
 * BitTorrent client, implemented according to the v1.0 protocol specification:
 * https://wiki.theory.org/BitTorrentSpecification.
 */

package main

import (
	"fmt"
	"math/rand"
	"os"
	"os/signal"
	"syscall"
)

// Peer ID of the client
var peerID string

func main() {

	// Parse and validate the command-line flags
	parseFlags()
	defer torrentFile.Close()

	// Generate the peer ID of the client
	generatePeerID()

	// Print the client's details
	printClientDetails()

	// Parse the torrent file
	parseTorrentFile(torrentFile)

	// Start goroutines to send tracker requests and receive tracker responses, respectively
	go handleTrackerRequests()
	go handleTrackerResponses()

	// Start goroutines to actively form new connections and handle incoming connections, respectively
	go handleFormingConnections()
	go handleIncomingConnections()

	// Start goroutine to periodically send keep-alive messages and close timed-out connections, respectively
	go handleKeepAliveMessages()
	go handleTimeouts()

	// // Start goroutine to periodically send unchoke messages and perform optimistic unchoking, respectively
	// go handleUnchokeMessages()
	// go handleOptimisticUnchoking()

	// // Start a goroutine to process incoming request messages
	// go handleRequestMessages()

	// go handleEndGame()

	// Handle shutting down gracefully
	handleShuttingDown()
}

// Generates and returns a peer ID for the client.
func generatePeerID() {

	// Generate a random 12-digit integer
	randomInt := rand.Intn(1000000000000)

	// Compute the peer ID of the client by concatenating the client ID and version and the random integer
	pid := "-ML0001-" + fmt.Sprintf("%012d", randomInt)

	// Initialize the peer ID of the client
	peerID = pid
}

// Prints the client's command-line flags and peer ID.
func printClientDetails() {
	fmt.Println("====================== Client Details ======================")
	fmt.Printf("Compact: %v\nPeer ID: %v\nPort: %v\nVerbose: %v\n", compact, peerID, port, verbose)
	if !verbose {
		fmt.Println("===================== Transfer Details =====================")
	}
}

// Handles gracefully shutting down the client.
func handleShuttingDown() {

	// Create a channel via which to receive OS signals
	sigChannel := make(chan os.Signal, 1)

	// Notify the channel on receiving the interrupt signal (Ctrl + C)
	signal.Notify(sigChannel, os.Interrupt, syscall.SIGINT)

	// Block until a signal is received
	<-sigChannel

	// Send a tracker request containing the event 'stopped'
	sendTrackerRequest("stopped")
}