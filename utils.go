/*
 * Utility functions that are used in the BitTorrent protocols.
 */

package main

import (
	"crypto/sha1"
	"flag"
	"fmt"
	"math"
	"math/rand"
	"os"

	"github.com/marksamman/bencode"
)

// Checks if the parameter condition is false, in which case an error message is printed before panicking.
func assert(condition bool, message string) {
	if !condition {
		panic(fmt.Sprintf("Assertion failed: %s", message))
	}
}

// Generates and returns the peer ID of the client.
func getPeerID() string {

	// Generate a random 12-digit integer
	randomInt := rand.Intn(1000000000000)

	// Format the random integer as a string
	randomStr := fmt.Sprintf("%012d", randomInt)

	// Compute the peer ID of the client by concatenating the client ID and client version to the random string
	peerID := "-CN0001-" + randomStr

	return peerID
}

// Returns the SHA1 hash of the parameter byte array.
func getSHA1Hash(bytes []byte) []byte {

	// Initialize SHA1, the block size, total number of bytes to hash, and number of blocks
	sha1 := sha1.New()
	blockSize := 64
	totalBytes := len(bytes)
	numBlocks := (totalBytes + blockSize - 1) / blockSize

	// Iterate across the blocks of the byte array
	for i := 0; i < numBlocks; i++ {

		// Compute the start and end bytes of the current block
		start := i * blockSize
		end := (i + 1) * blockSize
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
	var torrentFilePath string
	flag.StringVar(&torrentFilePath, "torrent", "artofwar.torrent", "Path to the torrent file that contains metadata about the file/folder to be distributed")
	flag.IntVar(&port, "port", 6881, "Port that the client will bind to and listen on")
	flag.BoolVar(&compact, "compact", false, "Indicates that the client accepts a compact response")
	flag.BoolVar(&verbose, "verbose", false, "Indicates that the client prints detailed logs")

	// Parse the flags
	flag.Parse()

	// Validate the torrent file path flag by attempting to open the file
	torrentFile, err := os.Open(torrentFilePath)
	defer torrentFile.Close()
	assert(err == nil, "Invalid torrent file path")

	// Decode the torrent file
	decodedTorrentFile, err := bencode.Decode(torrentFile)
	assert(err == nil, "Error decoding the torrent file")

	// Validate the port flag
	assert(6881 <= port && port <= 6889, "Invalid port, must be an integer between 6881 and 6889")

	// Parse the torrent file
	parseTorrentFile(decodedTorrentFile)
}

// Parses the parameter torrent file and initializes global variables.
func parseTorrentFile(torrentFile map[string]interface{}) {

	// Get the info dictionary
	info, ok := torrentFile["info"].(map[string]interface{})
	assert(ok, "Error getting the info dictionary")

	// Get the file name
	fileName, ok = info["name"].(string)
	assert(ok, "Error getting the file name")

	// Get the SHA1 hashes of the pieces as an array of 20-byte hash values
	piecesStr, ok := info["pieces"].(string)
	assert(ok, "Invalid 'pieces' key in torrent file")
	for i := 0; i < len(piecesStr); i += 20 {
		piece := []byte(piecesStr[i : i + 20])
		pieceHashes = append(pieceHashes, piece)
	}

	// Get the length of the file in bytes
	fileLength, ok = info["length"].(int64)
	assert(ok, "Invalid length in torrent file")

	// Get the length of each piece in bytes
	pieceLength, ok = info["piece length"].(int64)
	assert(ok, "Invalid piece length in torrent file")

	// Compute the number of pieces
	numPieces = int(math.Ceil(float64(fileLength) / float64(pieceLength)))

	// Get the announce URL of the tracker
	announce = torrentFile["announce"].(string)

	// Compute the 20-byte SHA1 hash of the encoded info dictionary
	infoHash = getSHA1Hash(bencode.Encode(info))

	// Get the peer ID of the client
	peerID = getPeerID()

	// Print the client details
	printClientDetails()

	// Initialize the number of bytes that the client has to downloaded
	left = info["length"].(int64)

	// Iterate as many times as the number of pieces
	for i := int64(0); i < int64(numPieces); i++ {

		// Compute the length of the current piece, handling the final piece differently because it is irregular
		length := pieceLength
		if i == int64(numPieces) - 1 {
			length = fileLength - (int64(numPieces) - 1) * pieceLength
		}

		// Add a new piece to the array of fully-completed pieces
		pieces = append(pieces, newPiece(length))
	}

	if verbose {
		printTorrentFile(torrentFile)
	}
}

// Prints the contents of the parameter torrent file.
func printTorrentFile(torrentFileDict map[string]interface{}) {
	fmt.Println("======================= Torrent File =======================")
	for key, value := range torrentFileDict {
		if key == "info" {
			fmt.Println("Info Dictionary:")
			infoDict := torrentFileDict["info"].(map[string]interface{})
			for key, value := range infoDict {
				fmt.Printf("\tKey: %s, Value: %#v\n", key, value)
			}
		} else {
			fmt.Printf("Key: %s, Value: %#v\n", key, value)
		}
	}
}

func getRandomPieceIndex(pieceIndices []int) (int32) {
	if len(pieceIndices) == 0 {
		return -1
	}

	index := rand.Intn(len(pieceIndices))

	return int32(pieceIndices[index])
}

func getRandomSubsetOfPieceIndices(pieceIndices []int, subsetSize int) ([]int) {
	if len(pieceIndices) < subsetSize { // Return entire list of indices if there are less than subsetSize 
		return pieceIndices
	}

	// Shuffle the indices slice
	shuffledSlice := make([]int, len(pieceIndices))
	copy(shuffledSlice, pieceIndices)

	for i := len(shuffledSlice) - 1; i > 0; i-- {
		j := rand.Intn(i + 1)
		shuffledSlice[i], shuffledSlice[j] = shuffledSlice[j], shuffledSlice[i]
	}

	// Return the first 'subsetSize' elements as the random subset
	return shuffledSlice[:subsetSize]
}

func getRandomBlockIndices(pieceIdx int, numIndices int) []int {

	var indices []int // contains only indices of blocks that have not yet been received
	for i := 0; i < pieces[pieceIdx].totalBlockCount; i++ {
		if !pieces[pieceIdx].blocks[i].isReceived {
			indices = append(indices, i)
		}
	}

	if len(indices) < numIndices { // Return entire list of indices if there are less than subsetSize 
		return indices
	}

	// Shuffle the indices slice
	shuffledSlice := make([]int, len(indices))
	copy(shuffledSlice, indices)

	for i := len(shuffledSlice) - 1; i > 0; i-- {
		j := rand.Intn(i + 1)
		shuffledSlice[i], shuffledSlice[j] = shuffledSlice[j], shuffledSlice[i]
	}

	// Return the shuffled slice
	return shuffledSlice[:numIndices]
}