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
	"strconv"
	"strings"
	"time"
)

/*
 * TODO
 * Update request queue, store in connection?
 */

// Maximum number of unfulfilled requests to queue
const requestQueueSize = 10

// Stores an unfulfilled request
type Request struct {
	index    uint32
	begin    uint32
	length   uint32
	sentTime time.Time
}

func newRequest(index uint32, begin uint32, length uint32) *Request {
	return &Request {
		index: index,
		begin: begin,
		length: length,
		sentTime: time.Now(),
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

// Bitfield of the client
var bitfield []byte
// Array of connections that the client has with remote peers
var connections []*Connection
// Array of address-port pairs of connections that the client attempted to form with remote peers
var attemptedConnections []string

// Stores the state of a connection that the client has with a remote peer
type Connection struct {
	conn 			       net.Conn
	amChoking        bool
	amInterested     bool
	peerChoking      bool
	peerInterested   bool
	bitfield         []byte
	bytesDownloaded  int64
	bytesUploaded    int64
	startTime        time.Time
	lastSentTime     time.Time
	lastReceivedTime time.Time
}

func newConnection(conn net.Conn) *Connection {
	return &Connection {
		conn:             conn,
		amChoking:        true,
		amInterested:     false,
		peerChoking:      true,
		peerInterested:   false,
		bitfield:         make([]byte, numPieces),
		bytesDownloaded:  0,
		bytesUploaded:    0,
		startTime:        time.Now(),
		lastSentTime:     time.Now(),
		lastReceivedTime: time.Now(),
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
			if !ok {
				continue
			}

			// Iterate across the list of peers
			for _, peer := range peers {

				// Compute the current peer's address-port pair
				peerDict := peer.(map[string]interface{})
				peerID := peerDict["peer id"].(string)
				peerAddrPort := fmt.Sprintf("%s:%d", peerDict["ip"], peerDict["port"])

				shouldAttempt := true

				// Determine if there has already been an attempt to connect to the peer
				for _, addrPort := range attemptedConnections {
					if addrPort == peerAddrPort {
						shouldAttempt = false
						break
					}
				}

				// Determine if there is already a connection to the peer
				for _, connection := range connections {
					if connection.conn.RemoteAddr().String() == peerAddrPort {
						shouldAttempt = false
						break
					}
				}

				// Check if forming a connection to the peer should be attempted, and there are less than 30 peer connections
				if shouldAttempt && len(connections) < 30 {

					// Add the peer's address-port pair to the array of attempted connections
					attemptedConnections = append(attemptedConnections, peerAddrPort)

					// Start a goroutine to attempt to form a connection
					go attemptFormingConnection(peerAddrPort, peerID)
				}
			}
		}
	}
}

// Attempts to form a TCP connection with the peer with the parameter address-port pair.
func attemptFormingConnection(peerAddrPort string, peerID string) {

	// Initialize the number of attempts
	numAttempts := 0

	// Loop up to 5 times
	for {

		// Attempt to establish a TCP connection to the peer
		conn, err := net.Dial("tcp4", peerAddrPort)
		
		// Increment the number of attempts
		numAttempts++

		// Check if an error occurred
		if err != nil {
			if numAttempts < 5 {
				// If there are attempts remaining, retry after 5 seconds
				time.Sleep(5 * time.Second)
				continue
			} else {
				// Else, break
				if verbose {
					fmt.Printf("[%s] Error actively forming a TCP connection\n", peerAddrPort)
				}
				break
			}
		}

		// Handle a successful connection
		go handleSuccessfulConnection(conn, peerID)

		if verbose {
			fmt.Printf("[%s] Actively formed a TCP connection\n", conn.RemoteAddr())
		}

		break
	}
}

// Handles incoming connections from other peers.
func handleIncomingConnections() {

	// Compute the client's address-port pair
	clientAddrPort := fmt.Sprintf(":%d", port)

	// Listen for incoming connections
	listener, err := net.Listen("tcp4", clientAddrPort)
	assert(err == nil, "Error listening for incoming connections")
	defer listener.Close()

	// Loop indefinitely
	for {

		// Check if there are less than 55 peer connections
		if len(connections) < 55 {

			// Accept an incoming connection
			conn, err := listener.Accept()
			if err != nil {
				// Handle a failed connection
				if verbose {
					fmt.Println("[UNKNOWN] Error accepting an incoming TCP connection")
				}
				continue
			}
			
			// Handle a successful connection
			go handleSuccessfulConnection(conn, "")

			if verbose {
				fmt.Printf("[%s] Accepted an incoming TCP connection\n", conn.RemoteAddr())
			}
		}
	}
}

// Handles successful connections with other peers.
func handleSuccessfulConnection(conn net.Conn, peerID string) {

	// Initialize a new connection and add it to the array
	connection := newConnection(conn)
	connections = append(connections, connection)

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

	// Check if at least 1 piece is completed
	if numPiecesCompleted >= 1 {

		// Serialize and send the bitfield message
		bitfieldMessage := newBitfieldMessage()
		sendMessage(connection, bitfieldMessage.serialize(), "bitfield", fmt.Sprintf("[%s] Sent bitfield message with bitfield %08b", conn.RemoteAddr(), bitfield))
	}

	// Initialize a queue to store unfulfilled requests
	var requestQueue []*Request

	go handleRequestMessages(connection, &requestQueue)

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

				// Check if the peer has a piece that the client does not and the client is not interested in the peer
				if bitfield[message.pieceIndex] == 0 && !connection.amInterested {

					// The client became interested in the peer
					connection.amInterested = true

					// Serialize and send an interested message
					interestedMessage := newConnectionMessage(messageIDInterested)
					sendMessage(connection, interestedMessage.serialize(), "interested", fmt.Sprintf("[%s] Sent interested message", conn.RemoteAddr()))
				}

				break

			case BitfieldMessage:
				if verbose {
					fmt.Printf("[%s] Received bitfield message with bitfield %08b\n", conn.RemoteAddr(), message.bitfield)
				}

				// Update the peer's bitfield
				connection.bitfield = message.bitfield

				// Iterate across the bytes in the bitfields of the client and peer
				for i := 0; i < len(connection.bitfield); i++ {

					// Iterate across the bits in the current byte
					for j := 0; j < 8; j++ {

						// Get the current bit in the bitfields of the client and peer
						clientBit := (bitfield[i] >> uint8(j)) & 1
						peerBit := (connection.bitfield[i] >> uint8(j)) & 1
	
						// Check if the peer has a piece that the client does not and the client is not interested in the peer
						if clientBit == 0 && peerBit == 1 && !connection.amInterested {

							// The client became interested in the peer
							connection.amInterested = true

							// Serialize and send an interested message
							interestedMessage := newConnectionMessage(messageIDInterested)
							sendMessage(connection, interestedMessage.serialize(), "interested", fmt.Sprintf("[%s] Sent interested message", conn.RemoteAddr()))

							goto sentInterested
						}
					}
				}

				sentInterested:

				break

			case RequestOrCancelMessage:
				// Switch on the message ID
				switch message.id {
					case messageIDRequest:
						if verbose {
							fmt.Printf("[%s] Received request message with index %d, begin %d, and length %d\n", conn.RemoteAddr(), message.index, message.begin, message.length)
						}
						break

					case messageIDCancel:
						if verbose {
							fmt.Printf("[%s] Received cancel message with index %d, begin %d, and length %d\n", conn.RemoteAddr(), message.index, message.begin, message.length)
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

				// Iterate across the request queue
				for i, request := range requestQueue {

					// Check if a request was sent for block and the block has not been received
					if request.index == message.index && request.begin == message.begin && request.length == uint32(len(message.block)) && !pieces[message.index].blocks[(int64(message.begin) / blockSize)].isReceived {
						
						// Update the block in the corresponding piece
						pieces[message.index].blocks[(int64(message.begin) / blockSize)].isReceived = true
						pieces[message.index].blocks[(int64(message.begin) / blockSize)].data = message.block

						// Update the corresponding piece
						pieces[message.index].numBlocksReceived++

						// Check if all blocks have been received
						if pieces[message.index].numBlocksReceived == pieces[message.index].numBlocks {

							// Get the correct hash of the piece
							correctHash := pieceHashes[message.index]

							// Compute the hash of the received piece
							buffer := new(bytes.Buffer)
							for _, block := range pieces[message.index].blocks {
								buffer.Write(block.data)
							}
							pieceHash := getSHA1Hash(buffer.Bytes())

							// Check if the hashes are equal
							if bytes.Equal(pieceHash, correctHash) {

								// The piece has been completed
								pieces[message.index].isComplete = true
								numPiecesCompleted++

								fmt.Printf("[CLIENT] Completed %d/%d pieces\n", numPiecesCompleted, numPieces)
							} else {
								// Revert the blocks of the piece
								for _, block := range pieces[message.index].blocks {
									block.isReceived = false
								}

								// Revert the piece
								pieces[message.index].numBlocksReceived = 0
							}
						}
						
						// Remove the request from the queue
						requestQueue = append(requestQueue[:i], requestQueue[i + 1:]...)
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

// Sends request messages.
func handleRequestMessages(connection *Connection, requestQueue *[]*Request) {

	// Loop until all pieces have been completed
	for numPiecesCompleted < numPieces {

		// Check if the client is unchoked by the peer
		if !connection.peerChoking {

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
				request := newRequest(pieceIndex, uint32(blockIndex) * uint32(blockSize), uint32(block.length))

				// Check if the request is not already in the queue
				if !contains(*requestQueue, request) {

					// Wait while the request queue is full
					for len(*requestQueue) >= requestQueueSize {}

					// Add the request to the queue
					*requestQueue = append(*requestQueue, request)
					
					// Serialize and send the request message
					requestMessage := newRequestOrCancelMessage(messageIDRequest, pieceIndex, uint32(blockIndex))
					sendMessage(connection, requestMessage.serialize(), "request", fmt.Sprintf("[%s] Sent request message with index %d, begin %d, and length %d", connection.conn.RemoteAddr(), pieceIndex, int64(blockIndex) * blockSize, block.length))
				}
			}
		}
	}
}

// Sends keep-alive messages periodically.
func handleKeepAliveMessages() {

	// Loop indefinitely
	for {

		// Get the current time
		currentTime := time.Now()

		// Iterate across the peer connections
		for _, connection := range connections {

			// Check if at least 1 minute has passed since the client sent a message
			if currentTime.Sub(connection.lastSentTime) >= 1 * time.Minute {

				// Serialize and send keep-alive message
				keepAliveMessage := newKeepAliveMessage()
				sendMessage(connection, keepAliveMessage.serialize(), "keep-alive", fmt.Sprintf("[%s] Sent keep-alive message", connection.conn.RemoteAddr()))
			}
		}
	}
}

// Closes the parameter connection and removes it the global array.
func closeConnection(connection *Connection) {

	// Close the connection
	connection.conn.Close()

	// Remove the connection from the global array
	for i, conn := range connections {
		if conn.conn == connection.conn {
			connections = append(connections[:i], connections[i + 1:]...)
			break
		}
	}

	if verbose {
		fmt.Printf("[%s] Closed the TCP connection\n", connection.conn.RemoteAddr())
	}
}

// Handles timed-out connections by closing them.
func handleTimeouts() {

	// Loop indefinitely
	for {

		// Get the current time
		currentTime := time.Now()

		// Iterate across the peer connections
		for _, connection := range connections {

			// Check if at least 2 minutes have passed since the client received a message
			if currentTime.Sub(connection.lastReceivedTime) >= 2 * time.Minute {

				// Close the connection
				closeConnection(connection)

				break
			}
		}
	}
}