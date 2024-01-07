/*
 * Functions to handle the torrent (metainfo) file, which contains metadata
 * necessary to perform the tracker HTTP/HTTPS protocol.
 */

package main

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"os"
	"time"

	"github.com/marksamman/bencode"
)

// Block size
const maxBlockSize int64 = 16384

// 20-byte SHA1 hash of the encoded info dictionary
var infoHash []byte
// File name
var fileName string
// File length
var fileLength int64
// Maximum number of bytes in each piece
var maxPieceLength int64
// Number of pieces in the file
var numPieces int64
// Number of pieces that have been completed
var numPiecesCompleted int64 = 0
// Array of pieces to be aggregated into a file
var pieces []*Piece
// Announce URL of the tracker
var announce string

// Stores the status and length of a block in a Piece
type Block struct {
	isRequested bool
	isReceived  bool
	length      int64
}

// Stores the status and data of a piece in the file
type Piece struct {
	blocks            []Block
	data              []byte
	hash              []byte
	isComplete        bool
	length            int64
	numBlocks         int64
	numBlocksReceived int64
}

// Initializes and returns a new Piece.
func newPiece(hash []byte, length int64) *Piece {

	// Compute the number of blocks and the last block's length, which may be irregular
	numBlocks := int64(math.Ceil(float64(length) / float64(maxBlockSize)))
	lastBlockSize := length - (int64(numBlocks) - 1) * maxBlockSize

	// Initialize the blocks of the new Piece
	blocks := make([]Block, numBlocks)
	for i := int64(0); i < numBlocks; i++ {
		blockLen := maxBlockSize
		if i == numBlocks - 1 {
			blockLen = lastBlockSize
		}

		blocks[i] = Block {
			isRequested: false,
			isReceived:  false,
			length:      blockLen,
		}
	}

	// Return the new Piece
	return &Piece {
		blocks:            blocks,
		data:              make([]byte, length),
		hash:              hash,
		isComplete:        false,
		length:            length,
		numBlocks:         numBlocks,
		numBlocksReceived: 0,
	}
}

// Prints the fields of the parameter piece.
func printPiece(piece Piece) {
	fmt.Println("\tBlocks:")
	for i, block := range piece.blocks {
		fmt.Printf("\t\tBlock %v:\tIs Received: %v\tLength: %v\n", i, block.isReceived, block.length)
	}
	fmt.Printf("\tIs Complete: %v\n", piece.isComplete)
	fmt.Printf("\tNumber of Blocks: %v\n", piece.numBlocks)
	fmt.Printf("\tNumber of Blocks Received: %v\n", piece.numBlocksReceived)
}

// Prints the fields of all pieces.
func printPieces() {
	fmt.Println("========================== Pieces ==========================")
	for i, piece := range pieces {
		fmt.Printf("Piece %v:\n", i)
		printPiece(*piece)
	}
}

// Parses the parameter torrent file.
func parseTorrentFile(torrentFile *os.File) error {

	// Decode the torrent file
	decodedTorrentFile, err := bencode.Decode(torrentFile)
	assert(err == nil, "Error decoding the torrent file")

	// Get the info dictionary
	info, ok := decodedTorrentFile["info"].(map[string]interface{})
	assert(ok, "Invalid info dictionary in torrent file")

	// Compute the 20-byte SHA1 hash of the encoded info dictionary
	infoHash = getSHA1Hash(bencode.Encode(info))

	// Get the file name
	fileName, ok = info["name"].(string)
	assert(ok, "Invalid file name in torrent file")

	// Get the length of the file in bytes
	fileLength, ok = info["length"].(int64)
	assert(ok, "Invalid length in torrent file")

	// Initialize the number of bytes that the client has to download
	left = fileLength

	// Get the number of bytes in each piece
	maxPieceLength, ok = info["piece length"].(int64)
	assert(ok, "Invalid piece length in torrent file")

	// Compute the number of pieces
	numPieces = int64(math.Ceil(float64(fileLength) / float64(maxPieceLength)))

	// Initialize the bitfield of the client
	bitfield = make([]byte, int64(math.Ceil(float64(numPieces) / float64(8))))

	// Get the SHA1 hashes of the pieces as an array of 20-byte hash values
	piecesStr, ok := info["pieces"].(string)
	assert(ok, "Invalid pieces in torrent file")

	// Iterate as many times as the number of pieces
	for i := int64(0); i < numPieces; i++ {

		// Compute the length of the current piece, where the last piece may be irregular
		pieceLength := maxPieceLength
		if i == numPieces - 1 {
			pieceLength = fileLength - (int64(numPieces) - 1) * maxPieceLength
		}

		// Get the SHA1 hash of the current piece
		pieceHash := []byte(piecesStr[(i * 20):(i * 20 + 20)])

		// Add a new piece to the array
		pieces = append(pieces, newPiece(pieceHash, pieceLength))
	}

	// Get the announce URL of the tracker
	announce = decodedTorrentFile["announce"].(string)

	// Print the torrent file
	printTorrentFile(decodedTorrentFile)

	// Check if the client downloaded the file already
	_, err = os.Stat(fileName)
	if err == nil {

		// Open the file
		file, err := os.Open(fileName)
		assert(err == nil, "Error opening the file")
		defer file.Close()

		// Get the file information
		fileInfo, err := file.Stat()
		assert(err == nil, "Error getting file information")

		// Check if the file length is incorrect
		if fileInfo.Size() != fileLength {
			goto clientDoesNotHaveFile
		}

		// Seek to the beginning of the file
		_, err = file.Seek(0, io.SeekStart)
		assert(err == nil, "Error seeking to the beginning of the file")

		// Iterate as many times as the number of pieces
		for i := int64(0); i < numPieces; i++ {

			// Initialize a buffer to store the current piece
			buffer := make([]byte, pieces[i].length)

			// Read the current piece into the buffer
			io.ReadFull(file, buffer)
			assert(err == nil, "Error reading a piece of the file")

			// Check if the piece hash is incorrect
			if !bytes.Equal(getSHA1Hash(buffer), pieces[i].hash) {
				goto clientDoesNotHaveFile
			}

			// Update the piece
			for _, block := range pieces[i].blocks {
				block.isRequested = true
				block.isReceived = true
			}
			pieces[i].data = buffer
			pieces[i].isComplete = true
			pieces[i].numBlocksReceived = pieces[i].numBlocks
		}

		// Update the number of bytes downloaded and left
		downloaded = int64(fileLength)
		left = 0

		// Update the bitfield
		for i := range bitfield {
			bitfield[i] = 255
		}

		// Update the number of completed pieces and the download status
		numPiecesCompleted = numPieces
		downloadedFile = true
	}

	clientDoesNotHaveFile:

	return nil
}

// Prints the contents of the parameter torrent file.
func printTorrentFile(torrentFile map[string]interface{}) {
	fmt.Println("======================= Torrent File =======================")
	creationDate := time.Unix(torrentFile["creation date"].(int64), 0)
	fmt.Printf("Announce: %v\nComment: %v\nCreated By: %v\nCreation Date: %v\n", torrentFile["announce"], torrentFile["comment"], torrentFile["created by"], creationDate)
	info := torrentFile["info"].(map[string]interface{})
	fmt.Printf("Info Dictionary:\n\tLength: %v\n\tName: %v\n\tPiece Length: %v\n", formatBytes(info["length"].(int64)), info["name"], formatBytes(info["piece length"].(int64)))
	if !verbose {
		fmt.Println("===================== Transfer Details =====================")
	}
}