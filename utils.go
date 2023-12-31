/*
 * Utility functions that are used in the BitTorrent client.
 */

package main

import (
	"bytes"
	"crypto/sha1"
	"flag"
	"fmt"
	"math"
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

// Formats the parameter number of bytes as a string.
func formatBytes(bytes int64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
	)

	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

// Formats the parameter number of seconds as a string.
func formatSeconds(seconds float64) string {

	// If the number of seconds is less than 60, return only seconds
	if seconds < 60 {
		return fmt.Sprintf("%.2f seconds", seconds)
	}

	// Calculate the number of minutes and remaining seconds
	minutes := int(seconds) / 60
	remainingSeconds := int(math.Round(seconds - float64(minutes) * 60))

	// Return the time in minutes and seconds
	return fmt.Sprintf("%d minutes and %d seconds", minutes, remainingSeconds)
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
	flag.StringVar(&tfp, "torrent", "debian-12.4.0-amd64-netinst.iso.torrent", "Path to the torrent file that contains metadata about the file to be distributed")
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