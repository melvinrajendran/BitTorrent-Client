/*
 * Functions to handle the peer wire TCP protocol.
 */

package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math/rand"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Bitfield of the client
var bitfield []byte
// Channel via which the indexes of complete pieces are sent
var completePieceChannel = make(chan uint32)
// Whether the client is in end game
var inEndGame = false
// Whether the client downloaded the file
var downloadedFile = false
// Array of the top-four downloaders
var downloaders = make([]*Connection, 0)
// Optimistic unchoke
var optimisticUnchoke *Connection

// Stores an unfulfilled request
type Request struct {
	index    uint32
	begin    uint32
	length   uint32
	time time.Time
}

func newRequest(index uint32, begin uint32, length uint32) *Request {
	return &Request {
		index:    index,
		begin:    begin,
		length:   length,
		time: time.Now(),
	}
}

// Returns true if the parameter request queue contains the parameter request, or false otherwise.
func contains(requestQueue []*Request, request *Request) bool {

	// Iterate across the request queue
	for _, r := range requestQueue {

		// Check if the request is in the queue
		if r.index == request.index && r.begin == request.begin && r.length == request.length {
			return true
		}
	}

	return false
}

// Handles a successful connection with a peer.
func handleSuccessfulConnection(conn net.Conn, peerID string) {

	// Initialize a new connection and add it to the array
	connection := newConnection(conn)
	connectionsMu.Lock()
	connections = append(connections, connection)
	connectionsMu.Unlock()

	// Serialize and send the handshake message
	handshakeMessage := newHandshakeMessage()
	sendMessage(connection, handshakeMessage.serialize(), "handshake", fmt.Sprintf("[%s] Sent handshake message", conn.RemoteAddr()))

	// Receive and deserialize the peer's handshake message
	handshakeBuffer := make([]byte, 68)
	_, err := io.ReadFull(conn, handshakeBuffer)
	if err != nil {
		return
	}
	receivedHandshakeMessage, err := deserializeHandshakeMessage(bytes.NewReader(handshakeBuffer))
	if err != nil || !bytes.Equal(receivedHandshakeMessage.infoHash, infoHash) || (peerID != "" && strconv.QuoteToASCII(receivedHandshakeMessage.peerID) != strings.Replace(strconv.QuoteToASCII(peerID), `\u00`, `\x`, -1)) {
		if verbose {
			fmt.Printf("[%s] Received invalid handshake message\n", conn.RemoteAddr())
		}
		return
	}
	if verbose {
		fmt.Printf("[%s] Received handshake message\n", conn.RemoteAddr())
	}

	// Update the peer connection's handshake status
	connection.completedHandshake = true

	// Check if at least 1 piece has been completed
	if numPiecesCompleted >= 1 {

		// Serialize and send the bitfield message
		bitfieldMessage := newBitfieldMessage()
		sendMessage(connection, bitfieldMessage.serialize(), "bitfield", fmt.Sprintf("[%s] Sent bitfield message", conn.RemoteAddr()))
	}

	// Start goroutines to send request messages and piece messages, respectively
	go handleRequestMessages(connection)
	go handlePieceMessages(connection)

	// Loop indefinitely
	for {

		// Initialize a buffer to store the length of messages received from the peer
		lengthBuffer := make([]byte, 4)

		// Read the message length from the connection
		_, err = io.ReadFull(conn, lengthBuffer)
		if err != nil {
			return
		}

		// Initialize the message length
		length := binary.BigEndian.Uint32(lengthBuffer)

		// Initialize a buffer to store the message received from the peer
		messageBuffer := make([]byte, length)

		// Read the message from the connection
		_, err = io.ReadFull(conn, messageBuffer)
		if err != nil {
			return
		}

		// Deserialize the message
		message, err := deserializeMessage(length, messageBuffer)
		if err != nil {
			return
		}

		// Update the peer's last-received time
		connection.lastReceivedTime = time.Now()

		// Switch on the message type
		switch message := message.(type) {
			case KeepAliveMessage:
				if verbose {
					fmt.Printf("[%s] Received keep-alive message\n", conn.RemoteAddr())
				}
				break

			case ConnectionMessage:
				// Switch on the message ID
				switch message.id {
					case messageIDChoke:
						if verbose {
							fmt.Printf("[%s] Received choke message\n", conn.RemoteAddr())
						}

						// The peer choked the client
						connection.peerChoking = true
						break

					case messageIDUnchoke:
						if verbose {
							fmt.Printf("[%s] Received unchoke message\n", conn.RemoteAddr())
						}

						// The peer unchoked the client
						connection.peerChoking = false
						break

					case messageIDInterested:
						if verbose {
							fmt.Printf("[%s] Received interested message\n", conn.RemoteAddr())
						}

						// The peer became interested in the client
						connection.peerInterested = true

						// Check if the client is not choking the peer
						if !connection.amChoking {

							// Iterate across the downloaders
							for i, downloader := range downloaders {

								// Check if the peer has a faster download/upload speed than the current downloader
								if getSpeed(connection) > getSpeed(downloader) {
									
									// Check if there are less than 4 downloaders
									if len(downloaders) < 4 {
										
										// Update the array of downloaders
										for _, conn := range connections {
											if conn == connection {
												tempDownloaders := downloaders
												downloaders = tempDownloaders[:i]
												downloaders = append(downloaders, connection)
												downloaders = append(downloaders, tempDownloaders[i:]...)
												break
											}
										}
									} else {

										// Check if the slowest downloader is not the optimistic unchoke
										if downloaders[3] != optimisticUnchoke {

											// Serialize and send a choke message
											chokeMessage := newConnectionMessage(messageIDChoke)
											sendMessage(downloaders[3], chokeMessage.serialize(), "choke", fmt.Sprintf("[%s] Sent choke message", downloaders[3].conn.RemoteAddr()))

											// The client choked the peer
											downloaders[3].amChoking = true

											// Update the array of downloaders
											for _, conn := range connections {
												if conn == connection {
													tempDownloaders := downloaders
													downloaders = tempDownloaders[:i]
													downloaders = append(downloaders, connection)
													downloaders = append(downloaders, tempDownloaders[i:3]...)
													break
												}
											}
										} else {

											// Serialize and send a choke message
											chokeMessage := newConnectionMessage(messageIDChoke)
											sendMessage(downloaders[2], chokeMessage.serialize(), "choke", fmt.Sprintf("[%s] Sent choke message", downloaders[2].conn.RemoteAddr()))

											// The client choked the peer
											downloaders[2].amChoking = true

											// Update the array of downloaders
											for _, conn := range connections {
												if conn == connection {
													tempDownloaders := downloaders
													downloaders = tempDownloaders[:i]
													downloaders = append(downloaders, connection)
													downloaders = append(downloaders, tempDownloaders[i:2]...)
													downloaders = append(downloaders, optimisticUnchoke)
													break
												}
											}
										}
									}
									
									break
								}
							}
						}

						break

					case messageIDNotInterested:
						if verbose {
							fmt.Printf("[%s] Received not interested message\n", conn.RemoteAddr())
						}

						// The peer became not interested in the client
						connection.peerInterested = false

						break

					default:
						if verbose {
							fmt.Printf("[%s] Received invalid message ID\n", conn.RemoteAddr())
						}
						return
				}
				break

			case HaveMessage:
				if verbose {
					fmt.Printf("[%s] Received have message with piece index %d\n", conn.RemoteAddr(), message.pieceIndex)
				}

				// Update the peer's bitfield
				byteIndex := message.pieceIndex / 8
				bitOffset := 7 - uint(message.pieceIndex % 8)
				connection.bitfield[byteIndex] |= (1 << bitOffset)

				// Compare the bitfields of the client and peer
				compareBitfields(connection)

				break

			case BitfieldMessage:
				if verbose {
					fmt.Printf("[%s] Received bitfield message\n", conn.RemoteAddr())
				}

				// Update the peer's bitfield
				connection.bitfield = message.bitfield

				// Compare the bitfields of the client and peer
				compareBitfields(connection)

				break

			case RequestOrCancelMessage:
				// Switch on the message ID
				switch message.id {
					case messageIDRequest:
						if verbose {
							fmt.Printf("[%s] Received request message with index %d, begin %d, and length %d\n", conn.RemoteAddr(), message.index, message.begin, message.length)
						}

						// Initialize a new request
						request := newRequest(message.index, message.begin, message.length)

						// Wait while the received request queue is full
						for len(connection.receivedRequestQueue) >= maxRequestQueueSize {}

						// Check if the request is not already in the queue
						if !contains(connection.receivedRequestQueue, request) {

							// Add the request to the queue
							connection.receivedRequestQueue = append(connection.receivedRequestQueue, request)
						}

						break

					case messageIDCancel:
						if verbose {
							fmt.Printf("[%s] Received cancel message with index %d, begin %d, and length %d\n", conn.RemoteAddr(), message.index, message.begin, message.length)
						}

						// Iterate across all of the requests in the received request queue
						for i, request := range connection.receivedRequestQueue {

							// Check if the current request's fields match those of the cancel message
							if request.index == message.index && request.begin == message.begin && request.length == message.length {

								// Remove the current request from the queue
								connection.receivedRequestQueue = append(connection.receivedRequestQueue[:i], connection.receivedRequestQueue[i + 1:]...)
								break
							}
						}

						break

					default:
						if verbose {
							fmt.Printf("[%s] Received invalid message ID\n", conn.RemoteAddr())
						}
						return
				}
				break

			case PieceMessage:
				if verbose {
					fmt.Printf("[%s] Received piece message with index %d and begin %d\n", conn.RemoteAddr(), message.index, message.begin)
				}

				// Check if the client has not downloaded the file
				if !downloadedFile {

					// Iterate across the request queue
					for _, request := range connection.sentRequestQueue {

						// Check if a request was sent for block and the block has not been received
						if request.index == message.index && request.begin == message.begin && request.length == uint32(len(message.block)) && !pieces[message.index].blocks[(int64(message.begin) / maxBlockSize)].isReceived {

							// Update the block in the corresponding piece
							pieces[message.index].blocks[(int64(message.begin) / maxBlockSize)].isReceived = true

							// Update the corresponding piece
							for i := uint32(0); i < uint32(len(message.block)); i++ {
								pieces[message.index].data[message.begin + i] = message.block[i]
							}
							pieces[message.index].numBlocksReceived++

							// Check if all blocks have been received
							if pieces[message.index].numBlocksReceived == pieces[message.index].numBlocks {

								// Get the correct hash of the piece
								correctHash := pieces[message.index].hash

								// Compute the hash of the received piece
								pieceHash := getSHA1Hash(pieces[message.index].data)

								// Check if the hashes are equal
								if bytes.Equal(pieceHash, correctHash) {

									// The piece has been completed
									pieces[message.index].isComplete = true
									numPiecesCompleted++

									// Update the client's bitfield
									byteIndex := message.index / 8
									bitOffset := 7 - uint(message.index % 8)
									bitfield[byteIndex] |= (1 << bitOffset)

									// Compare the bitfields of the client and peer
									compareBitfields(connection)

									// Update the number of bytes downloaded and left
									downloaded += int64(pieces[message.index].length)
									left -= int64(pieces[message.index].length)

									// Update the number of bytes downloaded from the peer
									connection.bytesDownloaded += int64(pieces[message.index].length)

									// Send the index of the complete piece into the channel
									completePieceChannel <- message.index

									if !verbose {
										fmt.Printf("\r[CLIENT] Downloading from %-" + fmt.Sprint(len(strconv.Itoa(len(trackerResponse["peers"].([]interface{}))))) + "d of %-" + fmt.Sprint(len(strconv.Itoa(len(trackerResponse["peers"].([]interface{}))))) + "d peers (%.2f%%)", len(connections), len(trackerResponse["peers"].([]interface{})), float64(numPiecesCompleted)/float64(numPieces) * 100)
										if numPiecesCompleted == numPieces {
											fmt.Println()
										}
									} else {
										fmt.Printf("[CLIENT] Downloaded piece %-" + fmt.Sprint(len(strconv.Itoa(int(numPieces)))) + "d from %-" + fmt.Sprint(len(strconv.Itoa(len(trackerResponse["peers"].([]interface{}))))) + "d of %-" + fmt.Sprint(len(strconv.Itoa(len(trackerResponse["peers"].([]interface{}))))) + "d peers (%.2f%%)\n", message.index, len(connections), len(trackerResponse["peers"].([]interface{})), float64(numPiecesCompleted)/float64(numPieces) * 100)
									}

									// Check if the client is in end game
									if inEndGame {
										connectionsMu.RLock()

										// Iterate across the peer connections
										for _, connection := range connections {

											// Serialize and send cancel message
											cancelMessage := newRequestOrCancelMessage(messageIDCancel, message.index, message.begin / uint32(maxBlockSize))
											sendMessage(connection, cancelMessage.serialize(), "cancel", fmt.Sprintf("[%s] Sent cancel message with index %d, begin %d, and length %d", connection.conn.RemoteAddr(), message.index, message.begin / uint32(maxBlockSize), pieces[message.index].blocks[message.begin / uint32(maxBlockSize)].length))
										}

										connectionsMu.RUnlock()
									}
								} else {
									// Revert the blocks of the piece
									for _, block := range pieces[message.index].blocks {
										block.isReceived = false
									}

									// Revert the piece
									pieces[message.index].numBlocksReceived = 0

									if verbose {
										fmt.Println("[CLIENT] Failed to validate hash of piece %-" + fmt.Sprint(len(strconv.Itoa(int(numPieces)))) + "d", message.index)
									}
								}
							}
							
							// Remove the request from the queue
							for j, r := range connection.sentRequestQueue {

								// Check if the request is in the queue
								if r.index == message.index && r.begin == message.begin && r.length == uint32(len(message.block)) {
									connection.sentRequestQueue = append(connection.sentRequestQueue[:j], connection.sentRequestQueue[j + 1:]...)
									break
								}
							}

							break
						}
					}
				}

				break

			default:
				if verbose {
					fmt.Printf("[%s] Received invalid message type\n", conn.RemoteAddr())
				}
				return
		}
	}
}

// Compares the client bitfield and the parameter peer's bitfield, sending interested/not interested messages as necessary.
func compareBitfields(connection *Connection) {

	// Initialize a variable to store if the client is interested in the peer
	amInterested := false

	// Iterate across the bytes in the bitfields of the client and peer
	for i := 0; i < len(connection.bitfield); i++ {

		// Iterate across the bits in the current byte
		for j := 0; j < 8; j++ {

			// Get the current bit in the bitfields of the client and peer
			clientBit := (bitfield[i] >> uint8(j)) & 1
			peerBit := (connection.bitfield[i] >> uint8(j)) & 1

			// Check if the peer has a piece that the client does not and the client is not interested in the peer
			if clientBit == 0 && peerBit == 1 {

				amInterested = true

				goto peerHasPiece
			}
		}
	}

	peerHasPiece:

	// Check if the client became interested in the peer
	if !connection.amInterested && amInterested {

		// Update the connection state
		connection.amInterested = true

		// Serialize and send an interested message
		interestedMessage := newConnectionMessage(messageIDInterested)
		sendMessage(connection, interestedMessage.serialize(), "interested", fmt.Sprintf("[%s] Sent interested message", connection.conn.RemoteAddr()))
	
		// The client became interested in the peer
		connection.amInterested = true

		return
	}
	
	// Check if the client became not interested in the peer
	if connection.amInterested && !amInterested {

		// Update the connection state
		connection.amInterested = false

		// Serialize and send a not interested message
		notInterestedMessage := newConnectionMessage(messageIDNotInterested)
		sendMessage(connection, notInterestedMessage.serialize(), "not interested", fmt.Sprintf("[%s] Sent not interested message", connection.conn.RemoteAddr()))
	
		// The client became not interested in the peer
		connection.amInterested = false
	}
}

// Sends request messages.
func handleRequestMessages(connection *Connection) {

	// Loop until the client has downloaded the file
	for !downloadedFile {

		// If the client is choked by the peer or not interested in the peer, continue
		if connection.peerChoking || !connection.amInterested {
			continue
		}

		// Check if the client is not in end game
		if !inEndGame {

			// Select a random piece
			pieceIndex := uint32(rand.Intn(int(numPieces)))

			// If the piece is complete, continue
			if pieces[pieceIndex].isComplete {
				continue
			}
			
			// Compute the byte index and bit offset of the piece in the peer's bitfield
			byteIndex := pieceIndex / 8
			bitOffset := 7 - uint(pieceIndex % 8)

			// If the peer does not have the piece, continue
			if connection.bitfield[byteIndex] & (1 << bitOffset) == 0 {
				continue
			}

			// Iterate across the blocks in the piece
			for blockIndex, block := range pieces[pieceIndex].blocks {

				// If the block has been received, continue
				if block.isReceived {
					continue
				}

				// Initialize a new request
				request := newRequest(pieceIndex, uint32(blockIndex) * uint32(maxBlockSize), uint32(block.length))

				// Wait while the request queue is full
				for len(connection.sentRequestQueue) >= maxRequestQueueSize {}

				// Check if the request is not already in the queue
				if !contains(connection.sentRequestQueue, request) {

					// Add the request to the queue
					connection.sentRequestQueue = append(connection.sentRequestQueue, request)
					
					// Serialize and send the request message
					requestMessage := newRequestOrCancelMessage(messageIDRequest, pieceIndex, uint32(blockIndex))
					sendMessage(connection, requestMessage.serialize(), "request", fmt.Sprintf("[%s] Sent request message with index %d, begin %d, and length %d", connection.conn.RemoteAddr(), pieceIndex, int64(blockIndex) * maxBlockSize, block.length))
				
					// The block has been requested
					pieces[pieceIndex].blocks[blockIndex].isRequested = true
				}
			}

			// Iterate across all of pieces
			for _, piece := range pieces {

				// Iterate across all of the blocks of the current piece
				for _, block := range piece.blocks {

					// If the current block has not been requested, go to the label
					if !block.isRequested {
						goto allBlocksNotRequested
					}
				}
			}

			// Enter end game
			inEndGame = true

			if verbose {
				fmt.Println("[CLIENT] Entered end game")
			}
		} else {

			// Iterate across all of the pieces
			for pieceIndex, piece := range pieces {

				// If the piece has been completed, continue
				if piece.isComplete {
					continue
				}

				// Iterate across all of the blocks of the current piece
				for blockIndex, block := range piece.blocks {

					// If the block has been received, continue
					if block.isReceived {
						continue
					}

					// Initialize a new request
					request := newRequest(uint32(pieceIndex), uint32(blockIndex) * uint32(maxBlockSize), uint32(block.length))
				
					// Wait while the request queue is full
					for len(connection.sentRequestQueue) >= maxRequestQueueSize {}

					// Check if the request is not already in the queue
					if !contains(connection.sentRequestQueue, request) {

						// Add the request to the queue
						connection.sentRequestQueue = append(connection.sentRequestQueue, request)
						
						// Serialize and send the request message
						requestMessage := newRequestOrCancelMessage(messageIDRequest, uint32(pieceIndex), uint32(blockIndex))
						sendMessage(connection, requestMessage.serialize(), "request", fmt.Sprintf("[%s] Sent request message with index %d, begin %d, and length %d", connection.conn.RemoteAddr(), pieceIndex, int64(blockIndex) * maxBlockSize, block.length))
					}
				}
			}

			// Sleep for 1 second
			time.Sleep(1 * time.Second)
		}

		allBlocksNotRequested:
	}
}

// Removes timed-out requests from each connection's sent request queue.
func handleRequestTimeouts() {

	// Loop until the client has downloaded the file
	for !downloadedFile {

		connectionsMu.Lock()

		// Iterate across the connections
		for _, connection := range connections {

			// Iterate across the requests in the queue of the current connection
			for i, request := range connection.sentRequestQueue {
										
				// Check if at least 5 seconds have passed since the current request was sent
				if time.Since(request.time) >= 5 * time.Second {

					// Remove the request from the queue
					connection.sentRequestQueue = append(connection.sentRequestQueue[:i], connection.sentRequestQueue[i + 1:]...)

					break
				}
			}
		}

		connectionsMu.Unlock()
	}
}

// Downloads the file after all pieces have been completed
func handleDownloadingFile() {

	// Check if the file has not been downloaded
	if !downloadedFile {

		// Create the .part file
		file, err := os.Create(fileName + ".part")
		assert(err == nil, "Error creating file")
		defer file.Close()

		// Set the size of the file
		err = file.Truncate(fileLength)
		assert(err == nil, "Error truncating file")

		// Loop until the file has been downloaded
		for !downloadedFile {

			// Receive the index of a complete piece from the channel
			pieceIndex, ok := <- completePieceChannel
			assert(ok, "Error reading from the complete piece channel")

			// Seek to the position of the piece
			_, err = file.Seek(int64(pieceIndex) * maxPieceLength, 0)
			assert(err == nil, "Error seeking file")

			// Write the piece to the file
			_, err := file.Write(pieces[pieceIndex].data)
			assert(err == nil, "Error writing to file")

			// Check if all of the pieces have been completed
			if numPiecesCompleted == numPieces {

				// Exit end game
				inEndGame = false

				if verbose {
					fmt.Println("[CLIENT] Exited end game")
				}

				// Remove the .part extension from the file
				os.Rename(fileName + ".part", fileName)

				// Close the channel
				close(completePieceChannel)

				// Download the file
				downloadedFile = true

				fmt.Printf("[CLIENT] Successfully downloaded the file \"%s\" in %v!\n", fileName, formatSeconds(time.Since(startTime).Seconds()))

				// Send a tracker request containing the event 'completed'
				sendTrackerRequest("completed")

				break
			}
		}
	}
}

// Periodically sends unchoke messages to the peers with the top-four download/upload speeds, as well as choke messages to other peers.
func handleChoking() {

	// Loop indefinitely
	for {

		// Sort the peer connnections in decreasing order by download/upload speed
		connectionsMu.Lock()
		sort.Slice(connections, func(i, j int) bool {
			return getSpeed(connections[i]) > getSpeed(connections[j])
		})
		connectionsMu.Unlock()

		// Initialize a temporary array of downloaders
		tempDownloaders := make([]*Connection, 0)

		// Iterate across the connections
		for _, connection := range connections {

			// Check if the current peer is interested and choked by the client
			if connection.peerInterested {

				// Add the current peer to the array of downloaders
				tempDownloaders = append(tempDownloaders, connection)

				// Check if the curent peer is choked by the client
				if connection.amChoking {

					// Serialize and send an unchoke message
					unchokeMessage := newConnectionMessage(messageIDUnchoke)
					sendMessage(connection, unchokeMessage.serialize(), "unchoke", fmt.Sprintf("[%s] Sent unchoke message", connection.conn.RemoteAddr()))
				
					// The client unchoked the peer
					connection.amChoking = false
				}
			}

			// If there are 4 downloaders, break
			if optimisticUnchoke == nil && len(tempDownloaders) == 4 || optimisticUnchoke != nil && optimisticUnchoke.peerInterested && len(tempDownloaders) == 3 {
				break
			}
		}

		// Check if the optimistic unchoke is interested in the client
		if optimisticUnchoke != nil && optimisticUnchoke.peerInterested {

			// Add the optimistic unchoke to the array of downloaders
			if getSpeed(optimisticUnchoke) < getSpeed(tempDownloaders[len(tempDownloaders) - 1]) {
				tempDownloaders = append(tempDownloaders, optimisticUnchoke)
			} else {
				for i, downloader := range tempDownloaders {
					if getSpeed(optimisticUnchoke) > getSpeed(downloader) {
						tempDownloaders = tempDownloaders[:i]
						tempDownloaders = append(tempDownloaders, optimisticUnchoke)
						tempDownloaders = append(tempDownloaders, tempDownloaders[i:]...)
						break
					}
				}
			}
		}

		// Update the global array of downloaders
		downloaders = tempDownloaders

		// Iterate across the connections
		for _, connection := range connections {

			// Compute if the current peer is a downloader
			isDownloader := false
			for _, downloader := range downloaders {
				if connection == downloader {
					isDownloader = true
					break
				}
			}

			// Check if the peer is not a downloader
			if !isDownloader {

				// Check if the client is choking the peer
				if connection.amChoking {

					// Iterate across the downloaders
					for _, downloader := range downloaders {

						// Check if the peer has a faster download/upload speed than the current downloader
						if getSpeed(connection) > getSpeed(downloader) {

							// Serialize and send an unchoke message
							unchokeMessage := newConnectionMessage(messageIDUnchoke)
							sendMessage(connection, unchokeMessage.serialize(), "unchoke", fmt.Sprintf("[%s] Sent unchoke message", connection.conn.RemoteAddr()))
						
							// The client unchoked the peer
							connection.amChoking = false

							break
						}
					}
				} else {

					// Check if the peer has a slower download/upload speed than the slowest downloader and is not the optimistic unchoke
					if (len(downloaders) == 0 || getSpeed(connection) < getSpeed(downloaders[len(downloaders) - 1])) && connection != optimisticUnchoke {

						// Serialize and send a choke message
						chokeMessage := newConnectionMessage(messageIDChoke)
						sendMessage(connection, chokeMessage.serialize(), "choke", fmt.Sprintf("[%s] Sent choke message", connection.conn.RemoteAddr()))

						// The client choked the peer
						connection.amChoking = true
					}
				}
			}
		}

		// Sleep for 10 seconds
		time.Sleep(10 * time.Second)
	}
}

// Periodically updates the optimistic unchoke.
func handleOptimisticUnchoking() {

	// Loop indefinitely
	for {

		// Check if there is at least 1 peer connection
		if len(connections) >= 1 {

			// Initialize an array to store the weights of the choked peer connections
			var connectionWeights []*Connection

			connectionsMu.RLock()

			// Iterate across the peer connections
			for _, connection := range connections {

				// Check if the handshake has been completed and the client is choking the current peer
				if connection.completedHandshake && connection.amChoking {

					// Append the current peer's index to the array
					connectionWeights = append(connectionWeights, connection)

					// Check if less than 1 minute has passed since the current peer connection started
					if time.Since(connection.startTime) < 1 * time.Minute {

						// Append the current peer's index to the array 2 more times
						connectionWeights = append(connectionWeights, connection)
						connectionWeights = append(connectionWeights, connection)
					}
				}
			}

			connectionsMu.RUnlock()

			if len(connectionWeights) == 0 {
				continue
			}

			// Select a random choked peer, where newly-connected peers are 3 times more likely to be selected
			randomIndex := rand.Intn(len(connectionWeights))

			// Update the optimistic unchoke
		  optimisticUnchoke = connectionWeights[randomIndex]

			if verbose {
				fmt.Printf("[%s] Set as optimistic unchoke\n", optimisticUnchoke.conn.RemoteAddr())
			}

			// Serialize and send an unchoke message
			unchokeMessage := newConnectionMessage(messageIDUnchoke)
			sendMessage(optimisticUnchoke, unchokeMessage.serialize(), "unchoke", fmt.Sprintf("[%s] Sent unchoke message", optimisticUnchoke.conn.RemoteAddr()))
		
			// The client unchoked the optimistic unchoke
			optimisticUnchoke.amChoking = false

			// Sleep for 30 seconds
			time.Sleep(30 * time.Second)

			// Compute if the optimistic unchoke is a downloader
			isDownloader := false
			for _, downloader := range downloaders {
				if downloader == optimisticUnchoke {
					isDownloader = true
					break
				}
			}

			// Check if the optimistic unchoke is not a downloader and it has a slower download/upload speed than the slowest downloader
			if !isDownloader && (len(downloaders) == 0 || getSpeed(optimisticUnchoke) < getSpeed(downloaders[len(downloaders) - 1])) {

				// Serialize and send a choke message
				chokeMessage := newConnectionMessage(messageIDChoke)
				sendMessage(optimisticUnchoke, chokeMessage.serialize(), "choke", fmt.Sprintf("[%s] Sent choke message", optimisticUnchoke.conn.RemoteAddr()))

				// The client choked the optimistic unchoke
				optimisticUnchoke.amChoking = true
			}
		}
	}
}

// Sends piece messages.
func handlePieceMessages(connection *Connection) {

	// Loop indefinitely
	for {

		// If the peer is not interested in the client, continue
		if !connection.peerInterested {
			continue
		}

		// If the received request queue is empty, continue
		if len(connection.receivedRequestQueue) == 0 {
			continue
		}

		// Remove a request from the queue
		request := connection.receivedRequestQueue[0]
		connection.receivedRequestQueue = connection.receivedRequestQueue[1:]

		// If the client is choking the peer or does not have the piece, continue
		if connection.amChoking || !pieces[request.index].isComplete {
			continue
		}

		// Serialize and send the piece message
		pieceMessage := newPieceMessage(request.index, request.begin, pieces[request.index].data[request.begin:(request.begin + request.length)])
		sendMessage(connection, pieceMessage.serialize(), "piece", fmt.Sprintf("[%s] Sent piece message with index %d and begin %d", connection.conn.RemoteAddr(), request.index, request.begin))
	
		// Update the number of bytes uploaded to the peer
		connection.bytesUploaded += int64(request.length)
	}
}