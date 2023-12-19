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
	"sync"
	"time"
)

// Variable to check if download is complete
var downloadComplete = false
// Track start time of download
var downloadStartTime time.Time = time.Now()
// Number of requests to send out per connection
const requestsPerConnection = 10
// Number of random block requests per piece
const blockRequestsPerPiece = 5
// Number of pieces downloaded
var numPiecesDownloaded = 0
// Indicates if the client has entered endgame
var endGame = false
var endGameMissingPieces []int
// Array of attempted connections
var attemptedConnections []string
// Array of connection states
var connectionStates []*ConnectionState
// Number of TCP connections that the client currently has to peers
var numConns = 0

// Stores received requests
var requestQueue []Request

// Stores requests sent to other peers
var (
	pendingRequests []Request
	mutexPending    sync.Mutex
)

var downloaders []*ConnectionState
var optimisticUnchoked *ConnectionState

// Request to be stored in queue
type Request struct {
	index 		int32
	begin 		int32
	length 		int32
	connState 	ConnectionState
}

func newRequest(connState ConnectionState, msg RequestMessage) Request {
	return Request {
		index: msg.Index,
		begin: msg.Begin,
		length: msg.Length,
		connState: connState,
	}
}

type ConnectionState struct {
	am_choking       bool
	am_interested    bool
	peer_choking     bool
	peer_interested  bool
	startTime        time.Time
	lastSentTime     time.Time
	lastReceivedTime time.Time
	bytesDownloaded  int64
	download_speed   float64
	bytesUploaded    int64
	upload_speed     float64
	conn 			       net.Conn
}

func newConnectionState(conn net.Conn) *ConnectionState {
	state := ConnectionState{
		am_choking:      true,
		am_interested:   false,
		peer_choking:    true,
		peer_interested: false,
		conn: conn,
		bytesDownloaded: 0,
		startTime: time.Now(),
		lastSentTime: time.Now(),
		lastReceivedTime: time.Now(),
	}

	return &state
}

func removeRequest(connection net.Conn, received PieceMessage) {
	for i, pendingReq := range pendingRequests {
    if pendingReq.connState.conn == connection && pendingReq.index == received.Index && pendingReq.begin == received.Begin {
      // Move all elements after i one index to the left
      copy(pendingRequests[i:], pendingRequests[i+1:])
      // Slice off the last element
      pendingRequests = pendingRequests[:len(pendingRequests)-1]
      if verbose {
        fmt.Printf("[%s] Removed pending request for piece %d\n", pendingReq.connState.conn.RemoteAddr(), pendingReq.index)
      }
      return // Exit the function after removing the request
  	}
  }
}

func cancelPendingRequests(received PieceMessage) {

	canceled := []net.Conn{}

	for _, pendingReq := range pendingRequests {
		if pendingReq.index == received.Index &&
			pendingReq.begin == received.Begin {
			canceled = append(canceled, pendingReq.connState.conn)

			cancel := NewCancelMessage(pendingReq.index, pendingReq.begin, pendingReq.length)
			pendingReq.connState.conn.Write(cancel.Serialize())

		}
	}

	for _, canceledReqs := range canceled {
		removeRequest(canceledReqs, received)
	}
}

// Handle actively forming connections to other peers.
func handleFormingConnections() {

	// Loop indefinitely
	for {

		// Check if the tracker response is not nil
		if trackerResponse != nil {

			// Get the list of peers
			peers, ok := trackerResponse["peers"].([]interface{})
			assert(ok, "Error getting the peers")

			// Iterate across the list of peers
			for _, peer := range peers {

				// Get the current peer's fields
				curr := peer.(map[string]interface{})
				peerAddr := curr["ip"]
				peerPort := curr["port"]

				// Compute the peer's address-port pair
				peerAddrPort := fmt.Sprintf("%s:%d", peerAddr, peerPort)

				// Determine if there has already been an attempt to connect to the peer
				didAttempt := false
				for _, addrPort := range attemptedConnections {
					if addrPort == peerAddrPort {
						didAttempt = true
						break
					}
				}

				// Check if there has not been an attempt to connect to the peer, and there are less than 30 peer connections
				if !didAttempt && numConns < 30 {

					// Add the peer's address-port pair to the array of attempted connections
					attemptedConnections = append(attemptedConnections, peerAddrPort)

					// Start a goroutine to attempt to form a connection
					go attemptFormingConnection(peerAddrPort)
				}
			}
		}
	}
}

// Attempts to form a TCP connection with the peer with the parameter address-port pair.
func attemptFormingConnection(peerAddrPort string) {

	// Attempt to establish a TCP connection to the peer
	conn, err := net.Dial("tcp", peerAddrPort)
	if err == nil {
		// Handle a successful connection
		go handleAcceptedConnection(conn)

		// Increment the number of peer connections
		numConns++

		if verbose {
			fmt.Printf("[%s] Actively formed a TCP connection\n", conn.RemoteAddr())
		}
	} else {
		// Handle a failed connection
		if verbose {
			fmt.Printf("[%s] Error actively forming a TCP connection\n", peerAddrPort)
		}
	}
}

// Handles incoming connections from other peers.
func handleIncomingConnections() {

	// Compute the client's address-port pair
	clientAddrPort := fmt.Sprintf(":%d", port)

	// Listen for incoming connections
	listener, err := net.Listen("tcp", clientAddrPort)
	assert(err == nil, "Error listening for incoming connections")
	defer listener.Close()

	// Loop indefinitely
	for {

		// Check if the number of peer connections is less than 55
		if numConns < 55 {

			// Accept an incoming connection
			conn, err := listener.Accept()
			if err == nil {
				// Handle a successful connection
				go handleAcceptedConnection(conn)

				// Increment the number of peer connections
				numConns++

				if verbose {
					fmt.Printf("[%s] Accepted an incoming TCP connection\n", conn.RemoteAddr())
				}
			} else {
				// Handle a failed connection
				if verbose {
					fmt.Println("[UNKNOWN] Error accepting an incoming TCP connection")
				}
			}
		}
	}
}

// Handles accepted connections with other peers.
func handleAcceptedConnection(conn net.Conn) {

	// Store an array of the indices of pieces that the peer has and the client needs
	var neededPeerPieces []int 

	// Initialize a new connection state and add it to the array
	connState := newConnectionState(conn)
	connectionStates = append(connectionStates, connState)

	// Set a read deadline of 2 minutes
	err := conn.SetReadDeadline(time.Now().Add(2 * time.Minute))
	assert(err == nil, "Error setting a read deadline")

	// Serialize and send the handshake
	handshake := NewHandshake(peerID, infoHash)
	sendMessage(connState, handshake.Serialize(), "handshake", fmt.Sprintf("[%s] Sent handshake", conn.RemoteAddr()))

	// Receive and deserialize the peer's handshake
	handshakeBuffer := make([]byte, 68)
	_, err = io.ReadFull(conn, handshakeBuffer)
	if err != nil {
		return
	}
	handshake, err = DeserializeHandshake(bytes.NewReader(handshakeBuffer))
	if err != nil || !bytes.Equal(handshake.InfoHash, infoHash) {
		return
	}

	if verbose {
		fmt.Printf("[%s] Received handshake\n", conn.RemoteAddr())
	}

	// If the client already has at least one piece, send the bitfield message
	if numPiecesDownloaded >= 1 {

		// Initialize the bitfield message
		bitfield := make([]byte, (numPieces + 7) / 8)

		// Set the bits in the bitfield according to their piece indexes
		for index, piece := range pieces {
			if piece.isComplete {
				byteIndex := index / 8
				offset := index % 8
				bitfield[byteIndex] |= 1 << (7 - offset)
			}
		}

		// Serialize and send the bitfield message
		bitfieldMsg := NewBitfieldMessage(bitfield)
		sendMessage(connState, bitfieldMsg.Serialize(), "bitfield", fmt.Sprintf("[%s] Sent bitfield message with bitfield %08b", conn.RemoteAddr(), bitfield))
	}

	// Initialize a buffer to store the length of messages received from the peer
	lengthBuffer := make([]byte, 4)

	// Loop indefinitely
	for {

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
		message, err := DeserializeMessage(length, messageBuffer)
		assert(err == nil, "Error deserializing the message")

		// Update the peer's last-received time
		connState.lastReceivedTime = time.Now()

		// Switch on the message type
		switch msg := message.(type) {

			case KeepAliveMessage:
				if verbose {
					fmt.Printf("[%s] Received keep-alive message\n", conn.RemoteAddr())
				}

			case SimpleMessage:

				// Switch on the message ID
				switch msg.MessageID {

					case MessageIDChoke:
						if verbose {
							fmt.Printf("[%s] Received choke message\n", conn.RemoteAddr())
						}
						connState.peer_choking = true

					case MessageIDUnChoke:
						if verbose {
							fmt.Printf("[%s] Received unchoke message\n", conn.RemoteAddr())
						}
						connState.peer_choking = false

						// Remove piece indices from neededPeerPieces that have already been downloaded from other connections
						for i, pieceIdx := range neededPeerPieces {
							if pieces[pieceIdx].isComplete {
								neededPeerPieces = append(neededPeerPieces[:i], neededPeerPieces[i+1:]...)
								break
							}
						}

						// Resend peer messages if client is still interested in this peer
						if connState.am_interested && !endGame{

							// Send requests for random pieces
							randomPieceIndices := getRandomSubsetOfPieceIndices(neededPeerPieces, requestsPerConnection)
							
							for _, pieceIdx := range randomPieceIndices {

								randomBlockIndices := getRandomBlockIndices(pieceIdx, blockRequestsPerPiece)
								
								for _, blockIdx := range randomBlockIndices {

									requestMsg := NewRequestMessage(pieceIdx, blockIdx)
									sendMessage(connState, requestMsg.Serialize(), "request", fmt.Sprintf("[%s] Sent request message for block %d in piece %d", conn.RemoteAddr(), blockIdx, pieceIdx))

								}
							}
						}

					case MessageIDInterested:
						if verbose {
							fmt.Printf("[%s] Received interested message\n", conn.RemoteAddr())
						}
						connState.peer_interested = true

						if downloaders != nil && len(downloaders) >= 1 && connState.download_speed > downloaders[len(downloaders) - 1].download_speed {

							// Send a choke message
							sendMsg := NewSimpleMessage(MessageIDChoke)
							sendMessage(downloaders[len(downloaders) - 1], sendMsg.Serialize(), "choke", fmt.Sprintf("[%s] Sent choke message", downloaders[len(downloaders) - 1].conn.RemoteAddr()))
						
							downloaders[len(downloaders) - 1].am_choking = true
						}

					case MessageIDNotInterested:
						if verbose {
							fmt.Printf("[%s] Received not interested message\n", conn.RemoteAddr())
						}
						connState.peer_interested = false
						
					default:
						if verbose {
							fmt.Printf("[%s] Received unknown message\n", conn.RemoteAddr())
						}
						return
				}

			case HaveMessage:
				if verbose {
					fmt.Printf("[%s] Received have message with piece %d\n", conn.RemoteAddr(), msg.PieceIndex)
				}

				// Check if completion status is false (client does not already have this piece)
				if !pieces[msg.PieceIndex].isComplete {
					neededPeerPieces = append(neededPeerPieces, int(msg.PieceIndex))

					if !connState.am_interested {
						connState.am_interested = true
						// Serialize and send Interested message
						sendMsg := NewSimpleMessage(MessageIDInterested)
						sendMessage(connState, sendMsg.Serialize(), "interested", fmt.Sprintf("[%s] Sent interested message", conn.RemoteAddr()))
					}

					// Serialize and send Request message for blocks of the new piece
					if !connState.peer_choking && !endGame{

						randomBlockIndices := getRandomBlockIndices(int(msg.PieceIndex), blockRequestsPerPiece)
						
						for _, blockIdx := range randomBlockIndices {

							requestMsg := NewRequestMessage(int(msg.PieceIndex), blockIdx)
							sendMessage(connState, requestMsg.Serialize(), "request", fmt.Sprintf("[%s] Sent request message for block %d in piece %d", conn.RemoteAddr(), blockIdx, msg.PieceIndex))
						}
					}
				} else if len(neededPeerPieces) == 0 {
					// Resend not interested message if client still does not want any pieces from the peer after the have message
					connState.am_interested = false
					// Serialize and send Not Interested message
					sendMsg := NewSimpleMessage(MessageIDNotInterested)
					sendMessage(connState, sendMsg.Serialize(), "not interested", fmt.Sprintf("[%s] Sent not interested message", conn.RemoteAddr()))
				}

			case BitfieldMessage:
				if verbose {
					fmt.Printf("[%s] Received bitfield message with bitfield %08b\n", conn.RemoteAddr(), msg.Bitfield)
				}

				// Check if the bitfield is the wrong length
				if (int(length - 1) != len(msg.Bitfield)) {
					return
				}

				var peerPieces []int

				// Iterate through each byte of the bitfield
				for byteIndex, b := range msg.Bitfield {
					// Iterate through each bit of the byte
					for bitIndex := 7; bitIndex >= 0; bitIndex-- {
						// Check if the bit is set to 1
						pieceIdx := byteIndex*8 + (7 - bitIndex)
						if b&(1<<bitIndex) != 0 {
							// Piece is available
							// fmt.Printf("Piece %d is available\n", pieceIdx)
							peerPieces = append(peerPieces, pieceIdx)
						} else {
							if pieceIdx < numPieces {
								// Piece is missing
								// fmt.Printf("Piece %d is missing\n", pieceIdx)
							}
						}
					}
				}

				connState.am_interested = false
				for _, pieceIdx := range peerPieces {
					// Check if completion status is false (client does not already have this piece)
					if !pieces[pieceIdx].isComplete {
						neededPeerPieces = append(neededPeerPieces, pieceIdx)
						connState.am_interested = true
					}
				}

				if connState.am_interested {
					// Serialize and send Interested message
					sendMsg := NewSimpleMessage(MessageIDInterested)
					sendMessage(connState, sendMsg.Serialize(), "interested", fmt.Sprintf("[%s] Sent interested message", conn.RemoteAddr()))
				} else { // Serialize and send Not Interested message if peer has no pieces the peer is interested in
					sendMsg := NewSimpleMessage(MessageIDNotInterested)
					sendMessage(connState, sendMsg.Serialize(), "not interested", fmt.Sprintf("[%s] Sent not interested message", conn.RemoteAddr()))
				}

			case RequestMessage:
				// Note: Assume client will only receive this message type if it has the entire piece being requested for
				if verbose {
					fmt.Printf("[%s] Received request message for block with begin %d and length %d in piece %d\n", conn.RemoteAddr(), msg.Begin, msg.Length, msg.Index)
				}
				
				// Create a new request message
				req := newRequest(*connState, msg)
				
				// Queue the request to be processed
				requestQueue = append(requestQueue, req)

			case PieceMessage:
				if downloadComplete {
					continue
				}

				// Assume client receives bytes for pieces in sequential order
				if verbose {
					fmt.Printf("[%s] Received piece message with index %d and begin %d\n", conn.RemoteAddr(), msg.Index, msg.Begin)
				}
				// fmt.Printf("Piece message: %#v\n", msg)

				// Check if Piece message is valid
				targetPiece := pieces[int(msg.Index)]
				if (msg.Begin % int32(blockSize) != 0) {
					// Close connection
					if verbose {
						fmt.Printf("[%s] Received block of piece %d with invalid begin field. Ignoring block...\n", conn.RemoteAddr(), msg.Index)
					}
					return
				}

				blockIdx := int(msg.Begin / int32(blockSize))				
				if targetPiece.blocks[blockIdx].isReceived && !endGame{
					if verbose {
						fmt.Printf("[%s] Received block of piece %d that was already received\n", conn.RemoteAddr(), msg.Index)
					}

					// Re-transmit new request for the Piece message
					if connState.am_interested && !connState.peer_choking{
						if !pieces[msg.Index].isComplete {

							randomBlockIndices := getRandomBlockIndices(int(msg.Index), 1)
							for _, blockIdx := range randomBlockIndices {

								requestMsg := NewRequestMessage(int(msg.Index), blockIdx)
								sendMessage(connState, requestMsg.Serialize(), "request", fmt.Sprintf("[%s] Sent new request message for block %d in piece %d", conn.RemoteAddr(), blockIdx, msg.Index))

							}
						} else { // Send request for new Piece message if original piece was completed
							newPieceIndex := getRandomPieceIndex(neededPeerPieces)
							if newPieceIndex != -1 {
								randomBlockIndices := getRandomBlockIndices(int(newPieceIndex), 1)
								for _, blockIdx := range randomBlockIndices {
									requestMsg := NewRequestMessage(int(newPieceIndex), blockIdx)
									sendMessage(connState, requestMsg.Serialize(), "request", fmt.Sprintf("[%s] Completed piece %d. Sent request message for new piece %d...", conn.RemoteAddr(), msg.Index, newPieceIndex))

								}
							}
						}
					}
					continue
				}

				// Write block into the data field of the corresponding block
				copy(pieces[msg.Index].data[msg.Begin:], msg.Block)
				targetPiece.blocks[blockIdx].isReceived = true

				addedByteCount := len(msg.Block)
				if verbose {
					fmt.Printf("[CLIENT] Added %d bytes to piece %d\n", addedByteCount, msg.Index)
				}

				// Increment the number of bytes downloaded
				downloaded += int64(addedByteCount)
				// Decrement the number of bytes left
				left -= int64(addedByteCount)

				pieces[msg.Index].numBlocksReceived += 1

				// Remove the corresponding request for the received piece message from PendingRequests
				if endGame {
					mutexPending.Lock()
					removeRequest(conn, msg)
					// Send Cancel messages
					cancelPendingRequests(msg)
					mutexPending.Unlock()
				}

				// Check if piece was completed
				if pieces[msg.Index].numBlocksReceived == pieces[msg.Index].numBlocks {

					// Compare hash of completed piece with piece hash in info dictionary for validation check'
					correctHash := pieceHashes[msg.Index]

					// Compute the hash of the completed piece
					pieceHash := getSHA1Hash(pieces[msg.Index].data)

					if !bytes.Equal(pieceHash, correctHash) && !endGame{
						fmt.Printf("[CLIENT] Failed to verify hash of piece %d\n", msg.Index)
						
						// Reset progress on current piece
						pieces[msg.Index] = newPiece(int64(len(pieces[msg.Index].data)))

						// Re-transmit requests for the Piece message
						randomBlockIndices := getRandomBlockIndices(int(msg.Index), blockRequestsPerPiece)
						for _, blockIdx := range randomBlockIndices {

							requestMsg := NewRequestMessage(int(msg.Index), blockIdx)
							sendMessage(connState, requestMsg.Serialize(), "request", fmt.Sprintf("[%s] Sent new request message for block %d in piece %d", conn.RemoteAddr(), blockIdx, msg.Index))

							// Add sent request to list of pending requests
							if endGame {
								pendingRequests = append(pendingRequests, newRequest(*connState, requestMsg))
							}
						}
						continue
					}
					
					if verbose {
						fmt.Printf("[CLIENT] Successfully verified hash of piece %d\n", msg.Index)
					}

					// Update piece status and remove piece from neededPeerPieces
					numPiecesDownloaded += 1
					pieces[msg.Index].isComplete = true
					for i, pieceIdx := range neededPeerPieces {
						if pieceIdx == int(msg.Index) {
							neededPeerPieces = append(neededPeerPieces[:i], neededPeerPieces[i+1:]...)
							break
						}
					}

					// Serialize and send Have message to all peers
					sendHaveToAllPeers(msg.Index)

					if verbose {
						fmt.Printf("[CLIENT] Completed piece with index %d\n", msg.Index)
					}
					fmt.Printf("[CLIENT] Number of pieces completed: %d/%d\n", numPiecesDownloaded, numPieces)
					// torrentDownloadSpeed := float64(downloaded) / time.Since(downloadStartTime).Seconds()
					// fmt.Printf("[CLIENT] Current download speed: %f bytes/second\n", torrentDownloadSpeed)
					
					// Enter end game when 90% of pieces are downloaded
					percentComplete := (float64(numPiecesDownloaded) / float64(numPieces)) * 100
					// fmt.Printf("%.2f%%\n", percentComplete)
					if (percentComplete >= 90.0 && !endGame){
						fmt.Println("[CLIENT] Entered Endgame")
						findMissingPieces()
						endGame = true
					}

					// Assemble file if all pieces have been obtained
					if numPiecesDownloaded == numPieces {
						endGame = false
						file, err := os.Create(fileName)
						assert(err == nil, "Error creating file to assemble pieces")
						defer file.Close()

						// Build the file using the byte slices
						for i := 0; i < numPieces; i++ {
							_, err := file.Write(pieces[i].data)
							assert(err == nil, "Error writing the pieces to file")
						}
						fmt.Printf("[CLIENT] Successfully downloaded file %s in %f seconds!\n", fileName, time.Since(downloadStartTime).Seconds())
						downloadComplete = true

						// Send a tracker request containing the event 'completed'
						sendTrackerRequest("completed")
					} else { // Otherwise, send request for new Piece message if original piece was completed
						if !endGame {
							newPieceIndex := getRandomPieceIndex(neededPeerPieces)
							if newPieceIndex != -1 {
								randomBlockIndices := getRandomBlockIndices(int(newPieceIndex), blockRequestsPerPiece)
								for _, blockIdx := range randomBlockIndices {
									requestMsg := NewRequestMessage(int(newPieceIndex), blockIdx)
									sendMessage(connState, requestMsg.Serialize(), "request", fmt.Sprintf("[%s] Completed piece %d. Sent request message for new piece %d...", conn.RemoteAddr(), msg.Index, newPieceIndex))

									// Add sent request to list of pending requests
									if endGame {
										pendingRequests = append(pendingRequests, newRequest(*connState, requestMsg))
									}
								}
							}
						}
					}
				} else if connState.am_interested && !connState.peer_choking && !endGame{ // peer finishes downloading one block and sends request for new block in piece

						// Send request for next block of piece if client is still interested in this peer
						randomBlockIndices := getRandomBlockIndices(int(msg.Index), 1)
						for _, blockIdx := range randomBlockIndices {

							requestMsg := NewRequestMessage(int(msg.Index), blockIdx)
							sendMessage(connState, requestMsg.Serialize(), "request", fmt.Sprintf("[%s] Sent request message for block %d in new piece %d", conn.RemoteAddr(), blockIdx, msg.Index))
					}
					connState.bytesDownloaded += int64(length - 9)
					connState.download_speed = float64(connState.bytesDownloaded) / time.Since(connState.startTime).Seconds()
					// fmt.Printf("Current download speed: %f bytes/sec\n", connState.download_speed)
					// connState.handleFastest()
				}

			case CancelMessage:
				if verbose {
					fmt.Printf("[%s] Received cancel message\n", conn.RemoteAddr())
				}

				canceled := Request {
					index: msg.Index,
					begin: msg.Begin,
					length: msg.Length,
					connState: *connState,
				}
				
				// Iterate and remove the canceled request from the queue
				for index, req := range requestQueue {
					if req == canceled {
						if len(requestQueue) > 1 {
							requestQueue = append(requestQueue[:index],  requestQueue[(index + 1):]...)
						} else {
							requestQueue = []Request{}
						}
					}
				}

			default:
				if verbose {
					fmt.Printf("[%s] Received unknown message\n", conn.RemoteAddr())
				}
				return
		}
	}
}

// Sends a have message to all peers.
func sendHaveToAllPeers(pieceIndex int32) {

	// Initialize the have message
	haveMsg := NewHaveMessage(pieceIndex)

	// Iterate across the peer connections
	for _, connState := range connectionStates {

		sendMessage(connState, haveMsg.Serialize(), "have", fmt.Sprintf("[%s] Sent have message with piece %d", connState.conn.RemoteAddr(), pieceIndex))
	}
}

// Closes the parameter connection and removes the parameter connection state from all global arrays.
func closeConnection(connState *ConnectionState) {

	// Close the connection
	connState.conn.Close()

	// Remove the connection from the array of connection states
	for i, cs := range connectionStates {
		if cs == connState {
			connectionStates = append(connectionStates[:i], connectionStates[i + 1:]...)
			break
		}
	}

	// Decrement the number of peer connections
	numConns--

	if verbose {
		fmt.Printf("[%s] Closed the TCP connection\n", connState.conn.RemoteAddr())
	}
}

// Sends keep-alive messages periodically.
func handleKeepAliveMessages() {

	// Loop indefinitely
	for {

		// Get the current time
		currentTime := time.Now()

		// Iterate across the peer connections 
		for _, cs := range connectionStates {

			// Check if at least 1 minute has passed since the client sent a message
			if currentTime.Sub(cs.lastSentTime) >= 1 * time.Minute {

				// Serialize and send keep-alive message
				keepAliveMsg := NewKeepAliveMessage()
				sendMessage(cs, keepAliveMsg.Serialize(), "keep-alive", fmt.Sprintf("[%s] Sent keep-alive message", cs.conn.RemoteAddr()))
			}
		}
	}
}

// Handles timed-out connections by closing them.
func handleTimeouts() {

	// Loop indefinitely
	for {

		// Get the current time
		currentTime := time.Now()

		// Iterate across the peer connections 
		for _, cs := range connectionStates {

			// Check if at least 2 minutes have passed since the client received a message
			if currentTime.Sub(cs.lastReceivedTime) >= 2 * time.Minute {

				// Close the connection
				closeConnection(cs)

				break
			}
		}
	}
}

// Process any incoming requests
func handleRequestMessages() {

	// Loop forever
	for {
		// If the queue is not empty process the first request and send
		if len(requestQueue) > 0 {
			req := requestQueue[0]
			connState := req.connState

			// Data transfer takes place whenever one side is interested and the other side is not choking
			if !connState.am_choking && connState.peer_interested {

				// Get block to upload
				var blockToUpload []byte
				var length int
				for index, piece := range pieces {
					if index == int(req.index) {
						pieceData := piece.data
						length = min(len(pieceData), int(req.length))
						blockToUpload = pieceData[req.begin : int(req.begin) + length]
						
						break
					}
				}
				assert(blockToUpload != nil, "Error getting block to upload")

				// Serialize and send Piece message
				pieceMsg := NewPieceMessage(req.index, req.begin, blockToUpload)
				sendMessage(&connState, pieceMsg.Serialize(), "piece", fmt.Sprintf("[%s] Sent piece message with index %d and begin %d", connState.conn.RemoteAddr(), req.index, req.begin))
			
				// Increment the number of bytes uploaded
				uploaded += int64(length)

				// Update the client's upload speed to the peer
				connState.bytesUploaded += int64(length - 9)
				connState.upload_speed = float64(connState.bytesUploaded) / time.Since(connState.startTime).Seconds()
			}
			// Dequeue the first request
			if len(requestQueue) > 1 {
				requestQueue = requestQueue[1:]
			} else {
				requestQueue = []Request{}
			}
		}
	}
}

// Handles unchoking and choking peers.
func handleUnchokeMessages() {

	// Loop indefinitely
	for {

		if !downloadComplete {
			// Sorts the connection state list in decreasing order by download speed
			sort.Slice(connectionStates, func(i, j int) bool {
				return connectionStates[i].download_speed > connectionStates[j].download_speed
			})
		} else {
			// Sorts the connection state list in decreasing order by upload speed
			sort.Slice(connectionStates, func(i, j int) bool {
				return connectionStates[i].upload_speed > connectionStates[j].upload_speed
			})
		}

		var dldrs []*ConnectionState

		// If the optimistic unchoked is interested, include it as a downloader
		if optimisticUnchoked != nil && optimisticUnchoked.peer_interested {
			dldrs = append(dldrs, optimisticUnchoked)
		}

		for _, connState := range connectionStates {

			// Check if the peer is interested and choked
			if connState.peer_interested && connState.am_choking {
				dldrs = append(dldrs, connState)

				// Send a unchoke message
				sendMsg := NewSimpleMessage(MessageIDUnChoke)
				sendMessage(connState, sendMsg.Serialize(), "unchoke", fmt.Sprintf("[%s] Sent unchoke message", connState.conn.RemoteAddr()))
			
				connState.am_choking = false
			} else if connState.peer_interested {
				dldrs = append(dldrs, connState)
			}

			if len(dldrs) == 4 {
				break
			}
		}

		downloaders = dldrs
		
		// Iterate across the other connections
		for _, connState := range connectionStates {

			// Compute if the current peer is a downloader
			isDownloader := false
			for _, cs := range downloaders {
				if connState == cs {
					isDownloader = true
					break
				}
			}

			// Check if the peer is not a downloader and is faster than one and is choked
			if !isDownloader && len(downloaders) >= 1 && connState.am_choking {
				if (!downloadComplete && connState.download_speed > downloaders[len(downloaders) - 1].download_speed) || (downloadComplete && connState.upload_speed > downloaders[len(downloaders) - 1].upload_speed) {
					// Send a unchoke message
					sendMsg := NewSimpleMessage(MessageIDUnChoke)
					sendMessage(connState, sendMsg.Serialize(), "unchoke", fmt.Sprintf("[%s] Sent unchoke message", connState.conn.RemoteAddr()))
					connState.am_choking = false
				}
			} else if !isDownloader && !connState.am_choking {

				// Send a choke message
				sendMsg := NewSimpleMessage(MessageIDChoke)
				sendMessage(connState, sendMsg.Serialize(), "choke", fmt.Sprintf("[%s] Sent choke message", connState.conn.RemoteAddr()))
				connState.am_choking = true
			}
		}

		// Sleep for 10 seconds
		time.Sleep(10 * time.Second)
	}
}

// Performs optimistic unchoking on peers.
func handleOptimisticUnchoking() {

	// Loop indefinitely
	for {
  
	  // Check if there is at least one connection
	  if len(connectionStates) >= 1 {
  
		currentTime := time.Now()
  
		// Determine the weights of each peer being selected
		var weightedConnectionStates []*ConnectionState
		for _, connState := range connectionStates {
		  if connState.am_choking {
			weightedConnectionStates = append(weightedConnectionStates, connState)
  
			if currentTime.Sub(connState.startTime) < 1 * time.Minute {
				weightedConnectionStates = append(weightedConnectionStates, connState)
				weightedConnectionStates = append(weightedConnectionStates, connState)
			}
		  }
		}
  
		// Select a random choked peer, where newly-connected peers are three times more likely to be selected
		randomIndex := rand.Intn(len(weightedConnectionStates))
  
		if randomIndex >= 0 {
		  // Set the optimistic unchoked
		  optimisticUnchoked = weightedConnectionStates[randomIndex]
  
		  if verbose {
			fmt.Printf("[%s] Set to optimistic unchoked\n", optimisticUnchoked.conn.RemoteAddr())
		  }
  
		  // Send a unchoke message
		  sendMsg := NewSimpleMessage(MessageIDUnChoke)
		  sendMessage(optimisticUnchoked, sendMsg.Serialize(), "unchoke", fmt.Sprintf("[%s] Sent unchoke message", optimisticUnchoked.conn.RemoteAddr()))
		  optimisticUnchoked.am_choking = false
  
		  // Sleep for 30 seconds
		  time.Sleep(30 * time.Second)
  
		  // Send a choke message
		  sendMsg = NewSimpleMessage(MessageIDChoke)
		  sendMessage(optimisticUnchoked, sendMsg.Serialize(), "choke", fmt.Sprintf("[%s] Sent choke message", optimisticUnchoked.conn.RemoteAddr()))
		  optimisticUnchoked.am_choking = true
		}
	  }
	}
  }  

// Handles end game protocol
func handleEndGame() {
	blockIdx := 0
	for {
		if endGame {
			for _, connState := range connectionStates {
				for _, pieceIdx := range endGameMissingPieces {
					begin := int64(blockIdx) * blockSize
					if begin > pieceLength || blockIdx > len(pieces[pieceIdx].blocks) - 1{
						blockIdx = 0
					}
					requestMsg := NewRequestMessage(pieceIdx, blockIdx)
					sendMessage(connState, requestMsg.Serialize(), "request", fmt.Sprintf("[%s] Sent request message for block %d in piece %d", connState.conn.RemoteAddr(), blockIdx, pieceIdx))
					
					mutexPending.Lock()
					pendingRequests = append(pendingRequests, newRequest(*connState, requestMsg))
					mutexPending.Unlock()
				}
			}
			blockIdx++
			// time.Sleep(5 * time.Second)
		}
	}
}

func findMissingPieces() {
	for idx, piece := range pieces {
		if !piece.isComplete {
			endGameMissingPieces = append(endGameMissingPieces, idx)
		}
	}
}