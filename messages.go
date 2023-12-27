/*
 * Structs and functions for messages sent in the peer wire TCP protocol.
 */

package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"time"
)

// Length and name of the BitTorrent protocol
const (
	pStrLen byte   = 19
	pStr    string = "BitTorrent protocol"
)

// Message IDs
const (
	messageIDChoke         byte = 0
	messageIDUnchoke       byte = 1
	messageIDInterested    byte = 2
	messageIDNotInterested byte = 3
	messageIDHave          byte = 4
	messageIDBitfield      byte = 5
	messageIDRequest       byte = 6
	messageIDPiece         byte = 7
	messageIDCancel        byte = 8
)

type HandshakeMessage struct {
	pStrLen  byte
	pStr     string
	reserved []byte
	infoHash []byte
	peerID   string
}

func newHandshakeMessage() *HandshakeMessage {
	return &HandshakeMessage {
		pStr:     pStr,
		pStrLen:  pStrLen,
		reserved: make([]byte, 8),
		infoHash: infoHash,
		peerID:   peerID,
	}
}

func (message *HandshakeMessage) serialize() []byte {
	buffer := new(bytes.Buffer)
	binary.Write(buffer, binary.BigEndian, message.pStrLen)
	buffer.WriteString(message.pStr)
	buffer.Write(message.reserved)
	buffer.Write(message.infoHash)
	buffer.WriteString(message.peerID)

	return buffer.Bytes()
}

func deserializeHandshakeMessage(reader io.Reader) (HandshakeMessage, error) {
	var message HandshakeMessage

	err := binary.Read(reader, binary.BigEndian, &message.pStrLen)
	if err != nil {
		return message, err
	}

	ps := make([]byte, message.pStrLen)
	_, err = io.ReadFull(reader, ps)
	if err != nil {
		return message, err
	}
	message.pStr = string(ps)

	message.reserved = make([]byte, 8)
	io.ReadFull(reader, message.reserved)
	if err != nil {
		return message, err
	}

	message.infoHash = make([]byte, 20)
	io.ReadFull(reader, message.infoHash)
	if err != nil {
		return message, err
	}

	pid := make([]byte, 20)
	io.ReadFull(reader, pid)
	if err != nil {
		return message, err
	}
	message.peerID = string(pid)

	return message, nil
}

type KeepAliveMessage struct {
	len uint32
}

func newKeepAliveMessage() *KeepAliveMessage {
	return &KeepAliveMessage{
		len: 0,
	}
}

func (message *KeepAliveMessage) serialize() []byte {
	buffer := new(bytes.Buffer)
	binary.Write(buffer, binary.BigEndian, message.len)

	return buffer.Bytes()
}

type ConnectionMessage struct {
	len uint32
	id  byte
}

func newConnectionMessage(id byte) *ConnectionMessage {
	return &ConnectionMessage {
		len: 1,
		id:  id,
	}
}

func (message *ConnectionMessage) serialize() []byte {
	buffer := new(bytes.Buffer)
	binary.Write(buffer, binary.BigEndian, message.len)
	binary.Write(buffer, binary.BigEndian, message.id)

	return buffer.Bytes()
}

type HaveMessage struct {
	len        uint32
	id         byte
	pieceIndex uint32
}

func newHaveMessage(pieceIndex uint32) *HaveMessage {
	return &HaveMessage {
		len:        5,
		id:         messageIDHave,
		pieceIndex: pieceIndex,
	}
}

func (message *HaveMessage) serialize() []byte {
	buffer := new(bytes.Buffer)
	binary.Write(buffer, binary.BigEndian, message.len)
	binary.Write(buffer, binary.BigEndian, message.id)
	binary.Write(buffer, binary.BigEndian, message.pieceIndex)

	return buffer.Bytes()
}

func deserializeHaveMessage(reader io.Reader, len uint32) (HaveMessage, error) {
	var message HaveMessage

	if len != 5 {
		return message, errors.New("Received invalid have message length")
	}
	message.len = 5

	message.id = messageIDHave

	err := binary.Read(reader, binary.BigEndian, &message.pieceIndex)
	if err != nil {
		return message, err
	}

	return message, nil
}

type BitfieldMessage struct {
	len      uint32
	id       byte
	bitfield []byte
}

func newBitfieldMessage() *BitfieldMessage {
	return &BitfieldMessage {
		len:      uint32(1 + len(bitfield)),
		id:       messageIDBitfield,
		bitfield: bitfield,
	}
}

func (message *BitfieldMessage) serialize() []byte {
	buffer := new(bytes.Buffer)
	binary.Write(buffer, binary.BigEndian, message.len)
	binary.Write(buffer, binary.BigEndian, message.id)
	binary.Write(buffer, binary.BigEndian, message.bitfield)

	return buffer.Bytes()
}

func deserializeBitfieldMessage(reader io.Reader, len uint32) (BitfieldMessage, error) {
	var message BitfieldMessage

	if (int64(len - 1) != int64(math.Ceil(float64(numPieces) / float64(8)))) {
		return message, errors.New("Received invalid bitfield message length")
	}
	message.len = len

	message.id = messageIDBitfield

	message.bitfield = make([]byte, message.len - 1)
	err := binary.Read(reader, binary.BigEndian, &message.bitfield)
	if err != nil {
		return message, err
	}
	for i := 0; i < int(message.len - 1); i++ {
		for j := 0; j < 8; j++ {
			bit := (message.bitfield[i] >> uint8(j)) & 1

			if int64(i * 8 + (7 - j)) >= numPieces && bit == 1 {
				return message, errors.New("Received invalid bitfield message")
			}
		}
	}

	return message, nil
}

type RequestOrCancelMessage struct {
	len    uint32
	id     byte
	index  uint32
	begin  uint32
	length uint32
}

func newRequestOrCancelMessage(id byte, index uint32, blockIndex uint32) *RequestOrCancelMessage {

	// Serialize and send Request message for a block of the target piece
	begin := uint32(int64(blockIndex) * maxBlockSize)
	length := uint32(pieces[index].blocks[blockIndex].length)

	return &RequestOrCancelMessage {
		len:    13,
		id:     id,
		index:  index,
		begin:  begin,
		length: length,
	}
}

func (message *RequestOrCancelMessage) serialize() []byte {
	buffer := new(bytes.Buffer)
	binary.Write(buffer, binary.BigEndian, message.len)
	binary.Write(buffer, binary.BigEndian, message.id)
	binary.Write(buffer, binary.BigEndian, message.index)
	binary.Write(buffer, binary.BigEndian, message.begin)
	binary.Write(buffer, binary.BigEndian, message.length)

	return buffer.Bytes()
}

func deserializeRequestOrCancelMessage(reader io.Reader, len uint32, id byte) (RequestOrCancelMessage, error) {

	var message RequestOrCancelMessage
	
	if len != 13 {
		return message, errors.New("Received invalid request or cancel message length")
	}
	message.len = 13

	message.id = id

	err := binary.Read(reader, binary.BigEndian, &message.index)
	if err != nil {
		return message, err
	}

	err = binary.Read(reader, binary.BigEndian, &message.begin)
	if err != nil {
		return message, err
	}

	err = binary.Read(reader, binary.BigEndian, &message.length)
	if err != nil {
		return message, err
	}
	if message.length > 131072 {
		return message, errors.New("Received invalid request message length")
	}

	return message, nil
}

type PieceMessage struct {
	len   uint32
	id    byte
	index uint32
	begin uint32
	block []byte
}

func newPieceMessage(index uint32, begin uint32, block []byte) *PieceMessage {
	return &PieceMessage {
		len:   uint32(9 + len(block)),
		id:    messageIDPiece,
		index: index,
		begin: begin,
		block: block,
	}
}

func (message *PieceMessage) serialize() []byte {
	buffer := new(bytes.Buffer)
	binary.Write(buffer, binary.BigEndian, message.len)
	binary.Write(buffer, binary.BigEndian, message.id)
	binary.Write(buffer, binary.BigEndian, message.index)
	binary.Write(buffer, binary.BigEndian, message.begin)
	binary.Write(buffer, binary.BigEndian, message.block)

	return buffer.Bytes()
}

func deserializePieceMessage(reader io.Reader, len uint32) (PieceMessage, error) {
	var message PieceMessage

	if len < 10 {
		return message, errors.New("Received invalid piece message length")
	}
	message.len = len

	message.id = messageIDPiece

	err := binary.Read(reader, binary.BigEndian, &message.index)
	if err != nil {
		return message, err
	}

	err = binary.Read(reader, binary.BigEndian, &message.begin)
	if err != nil {
		return message, err
	}

	message.block = make([]byte, message.len - 9)
	_, err = io.ReadFull(reader, message.block)
	if err != nil {
		return message, err
	}

	return message, nil
}

// Sends the parameter serialized message via the parameter connection.
func sendMessage(connection *Connection, serializedMessage []byte, messageType string, successMessage string) {

	// Send the message
	_, err := connection.conn.Write(serializedMessage)
	if err != nil {
		
		// Check if the peer has closed the connection
		if opErr, ok := err.(*net.OpError); ok && opErr.Err.Error() == "write: broken pipe" {
			if verbose {
				fmt.Printf("[%s] Peer closed the TCP connection\n", connection.conn.RemoteAddr())
			}

			// Close the connection
			closeConnection(connection)
		} else {
			if verbose {
				fmt.Printf("[%s] Error sending the %s message\n", connection.conn.RemoteAddr(), messageType)
			}
		}
	} else {
		// Print the success message
		if verbose {
			fmt.Println(successMessage)
		}

		// Update the peer's last-sent time
		connection.lastSentTime = time.Now()
	}
}

// Reads from the parameter buffer and both deserializes and returns the message.
func deserializeMessage(len uint32, buffer []byte) (interface{}, error) {

	// Initialize a reader
	reader := bytes.NewReader(buffer)

	// If the message length is 0, return a keep-alive message
	if len == 0 {
		return KeepAliveMessage {}, nil
	}

	// Get the message ID
	var id byte
	err := binary.Read(reader, binary.BigEndian, &id)
	if err != nil {
		return nil, err
	}

	// Switch on the ID
	switch id {
		case messageIDChoke, messageIDUnchoke, messageIDInterested, messageIDNotInterested:
			if len != 1 {
				return nil, errors.New("Received invalid connection state message length")
			}
			return ConnectionMessage {len: len, id: id}, nil

		case messageIDHave:
			return deserializeHaveMessage(reader, len)

		case messageIDBitfield:
			return deserializeBitfieldMessage(reader, len)

		case messageIDRequest, messageIDCancel:
			return deserializeRequestOrCancelMessage(reader, len, id)

		case messageIDPiece:
			return deserializePieceMessage(reader, len)
			
		default:
			return nil, errors.New("Received invalid message ID")
	}
}