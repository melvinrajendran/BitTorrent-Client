/*
 * Utility functions that are used in the BitTorrent client.
 */

package main

import (
	"bytes"
	"crypto/sha1"
	"flag"
	"fmt"
	"net"
	"os"
)

// Indicates that the client accepts a compact tracker response
var	compact bool
// Port number that the client is listening on
var	port int64
// Torrent file that contains metadata about the file to be distributed
var	torrentFile *os.File
// Indicates that the client prints detailed logs
var	verbose bool

// Checks if the parameter condition is false, in which case an error message is printed before panicking.
func assert(condition bool, message string) {
	if !condition {
		panic(fmt.Sprintf("Assertion failed: %s", message))
	}
}

// Returns the SHA1 hash of the parameter byte array.
func getSHA1Hash(bytes []byte) []byte {

	// Initialize SHA1, the block size, total number of bytes to hash, and number of blocks
	sha1 := sha1.New()
	maxBlockSize := 64
	totalBytes := len(bytes)
	numBlocks := (totalBytes + maxBlockSize - 1) / maxBlockSize

	// Iterate across the blocks of the byte array
	for i := 0; i < numBlocks; i++ {

		// Compute the start and end bytes of the current block
		start := i * maxBlockSize
		end := (i + 1) * maxBlockSize
		if end > totalBytes {
			end = totalBytes
		}

		// Get the current block
		block := bytes[start:end]

		// Write the current block to the hash
		sha1.Write(block)
	}

	// Return the hash
	return sha1.Sum(nil)
}

// Parses and validates the command-line flags.
func parseFlags() {
	
	// Define the command-line flags
	var tfp string
	var p int
	flag.BoolVar(&compact, "compact", false, "Indicates that the client accepts a compact response")
	flag.IntVar(&p, "port", 6881, "Port that the client will bind to and listen on")
	flag.StringVar(&tfp, "torrent", "artofwar.torrent", "Path to the torrent file that contains metadata about the file to be distributed")
	flag.BoolVar(&verbose, "verbose", false, "Indicates that the client prints detailed logs")

	// Parse the flags
	flag.Parse()

	// Validate the port flag
	port = int64(p)
	assert(6881 <= port && port <= 6889, "Invalid port, must be an integer between 6881 and 6889")

	// Validate the torrent file path flag by attempting to open the torrent file
	tf, err := os.Open(tfp)
	assert(err == nil, "Invalid torrent file path")
	torrentFile = tf
}

// Iteratively reads from the parameter connection and returns a buffer containing the read bytes.
func readLoop(conn net.Conn) *bytes.Buffer {

	// Initialize a buffer and temporary buffer
	buffer := new(bytes.Buffer)
	temp := make([]byte, 1024)

	// Loop while there are bytes to be read
	for {

		// Read bytes from the connection into the temporary buffer
		n, err := conn.Read(temp)
		if err != nil {
			break
		}

		// Append the read bytes to the buffer
		buffer.Write(temp[:n])
	}

	return buffer
}