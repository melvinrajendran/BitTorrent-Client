package main

import (
	"flag"
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