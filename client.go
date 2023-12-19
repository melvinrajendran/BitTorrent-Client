/*
 * BitTorrent client.
 */

package main

import (
	"fmt"
	"sync"
)

// Port number that the client is listening on
var port int
// Indicates that the client accepts a compact tracker response
var compact bool
// Indicates that the client prints detailed logs
var verbose bool

func main() {

	// Parse and validate the command-line flags
	parseFlags()

	// Print the pieces
	if verbose {
		printPieces()
	}

	// Declare and add a single goroutine to a wait group
	var wg sync.WaitGroup
	wg.Add(1)

	// Start goroutines to send and receive tracker requests, respectively
	go handleTrackerRequests()
	go handleTrackerResponses()

	// Start goroutines to actively form new connections and handle incoming connections, respectively
	go handleFormingConnections()
	go handleIncomingConnections()

	// Start goroutine to periodically send unchoke messages and perform optimistic unchoking, respectively
	go handleUnchokeMessages()
	go handleOptimisticUnchoking()

	// Start a goroutine to process incoming request messages
	go handleRequestMessages()

	// Start goroutine to periodically send keep-alive messages and close timed-out connections, respectively
	go handleKeepAliveMessages()
	go handleTimeouts()

	go handleEndGame()
	// Start a goroutine to handle shutting down gracefully
	go handleShuttingDown(&wg)
	
	// Wait for the shutdown goroutine to finish
	wg.Wait()
}

func printClientDetails() {
	fmt.Println("====================== Client Details ======================")
	fmt.Printf("Port: %d\nCompact: %v\nVerbose: %v\nPeer ID: %s\n", port, compact, verbose, peerID)
	if !verbose {
		fmt.Println("===================== Transfer Details =====================")
	}
}