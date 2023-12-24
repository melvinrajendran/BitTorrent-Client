/*
 * Functions to handle the torrent (metainfo) file, which contains metadata
 * necessary to perform the tracker HTTP/HTTPS protocol.
 */

package main

import (
	"fmt"
	"math"
	"os"

	"github.com/marksamman/bencode"
)

// Block size
const maxBlockSize int64 = 16384

// 20-byte SHA1 hash of the encoded info dictionary
var infoHash []byte
// Number of bytes in each piece
var pieceLength int64
// Number of pieces in the file
var numPieces int64
// Array of 20-byte SHA1 piece hash values
var pieceHashes [][]byte
// Array of pieces to be aggregated into a file
var pieces []*Piece
// Number of pieces completed
var numPiecesCompleted int64 = 0
// File name
var fileName string
// File length
var fileLength int64
// Announce URL of the tracker
var announce string

// Stores the status and length of a block in a Piece
type Block struct {
	isReceived bool
	length     int64
	data       []byte
}

// Stores the status and data of a piece in the file
type Piece struct {
	blocks            []Block
	isComplete        bool
	numBlocks         int64
	numBlocksReceived int64
}

// Initializes and returns a new Piece.
func newPiece(length int64) *Piece {

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
			isReceived: false,
			data:       make([]byte, blockLen),
			length:     blockLen,
		}
	}

	// Return the new Piece
	return &Piece {
		isComplete:        false,
		blocks:            blocks,
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

	// Get the number of bytes in each piece
	pieceLength, ok = info["piece length"].(int64)
	assert(ok, "Invalid piece length in torrent file")

	// Compute the number of pieces
	numPieces = int64(math.Ceil(float64(fileLength) / float64(pieceLength)))

	// Initialize the bitfield of the client
	bitfield = make([]byte, int64(math.Ceil(float64(numPieces) / float64(8))))

	// Get the SHA1 hashes of the pieces as an array of 20-byte hash values
	piecesStr, ok := info["pieces"].(string)
	assert(ok, "Invalid pieces in torrent file")
	for i := 0; i < len(piecesStr); i += 20 {
		piece := []byte(piecesStr[i : i + 20])
		pieceHashes = append(pieceHashes, piece)
	}

	// Get the announce URL of the tracker
	announce = decodedTorrentFile["announce"].(string)

	// Iterate as many times as the number of pieces
	for i := int64(0); i < numPieces; i++ {

		// Compute the length of the current piece, where the last piece may be irregular
		pieceLen := pieceLength
		if i == numPieces - 1 {
			pieceLen = fileLength - (int64(numPieces) - 1) * pieceLength
		}

		// Add a new piece to the array
		pieces = append(pieces, newPiece(pieceLen))
	}

	// Print the torrent file
	printTorrentFile(decodedTorrentFile)

	return nil
}

// Prints the contents of the parameter torrent file.
func printTorrentFile(torrentFile map[string]interface{}) {
	fmt.Println("======================= Torrent File =======================")
	for key, value := range torrentFile {
		if key == "info" {
			fmt.Println("Info Dictionary:")
			info := torrentFile["info"].(map[string]interface{})
			for key, value := range info {
				if key != "pieces" {
					fmt.Printf("\tKey: %v, Value: %v\n", key, value)
				}
			}
		} else {
			fmt.Printf("Key: %v, Value: %v\n", key, value)
		}
	}
	if !verbose {
		fmt.Println("===================== Transfer Details =====================")
	}
}