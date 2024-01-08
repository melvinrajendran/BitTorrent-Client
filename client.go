/*
 * BitTorrent client, implemented according to the v1.0 protocol specification:
 * https://wiki.theory.org/BitTorrentSpecification.
 *
 * SHA1 Hashes:
 * 888987a1d69639deeb67fe976cb291aa2121486f  artofwar.txt
 * dc735158ca683697c9fdeb2b3b560230ce027bf3  debian-12.4.0-amd64-netinst.iso
 */

package main

import (
	"fmt"
	"math/rand"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// Peer ID of the client
var peerID string
// Start time of the download
var startTime time.Time

func main() {

	// Initialize the start time of the download
	startTime = time.Now()

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
	go handleConnectionTimeouts()

	// Start a goroutine to remove timed-out requests from each connection's sent request queue
	go handleRequestTimeouts()

	// Start a goroutine to download the file after all of the pieces have been completed
	go handleDownloadingFile()

	// Start goroutines to periodically unchoke the top-four downloaders and a random peer, respectively
	go handleChoking()
	go handleOptimisticUnchoking()

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
	fmt.Printf("Compact: %v\nPeer ID: %s\nPort: %v\nVerbose: %v\n", compact, peerID, port, verbose)
}

// Handles gracefully shutting down the client.
func handleShuttingDown() {

	// Create a channel via which to receive OS signals
	sigChannel := make(chan os.Signal, 1)

	// Notify the channel on receiving the interrupt signal (Ctrl + C)
	signal.Notify(sigChannel, os.Interrupt, syscall.SIGINT)

	// Block until a signal is received
	<- sigChannel

	// Send a tracker request containing the event 'stopped'
	sendTrackerRequest("stopped")
}