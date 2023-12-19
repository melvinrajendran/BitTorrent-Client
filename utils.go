/*
 * Utility functions that are used in the BitTorrent protocols.
 */

package main

import (
	"crypto/sha1"
	"fmt"
	"math/rand"
)

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
	for i := 0; i < pieces[pieceIdx].numBlocks; i++ {
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