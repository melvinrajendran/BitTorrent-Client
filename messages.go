package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

const (
	MessageIDChoke         byte = 0
	MessageIDUnChoke       byte = 1
	MessageIDInterested    byte = 2
	MessageIDNotInterested byte = 3
	MessageIDHave          byte = 4
	MessageIDBitfield      byte = 5
	MessageIDRequest       byte = 6
	MessageIDPiece         byte = 7
	MessageIDCancel        byte = 8
	MessageIDPort          byte = 9

	Pstr    string = "BitTorrent protocol"
	Pstrlen byte   = 19
)

type Handshake struct {
	PstrlenVal byte
	PstrVal    string
	Reserved   []byte
	InfoHash   []byte
	PeerID     string
}

func (msg *Handshake) Serialize() []byte {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.BigEndian, msg.PstrlenVal)
	buf.WriteString(msg.PstrVal)
	buf.Write(msg.Reserved)
	buf.Write(msg.InfoHash)
	buf.WriteString(msg.PeerID)

	return buf.Bytes()
}

func DeserializeHandshake(r io.Reader) (Handshake, error) {
	var msg Handshake
	binary.Read(r, binary.BigEndian, &msg.PstrlenVal)

	// pstr is a slice with length PstrlenVal
	pstr := make([]byte, msg.PstrlenVal)

	// reads exactly len(pstr) number of bytes from the io.Reader and stores them in r
	io.ReadFull(r, pstr)
	msg.PstrVal = string(pstr) // convert the read pstr bytes to a string

	msg.Reserved = make([]byte, 8)
	io.ReadFull(r, msg.Reserved)

	msg.InfoHash = make([]byte, 20)
	if _, err := io.ReadFull(r, msg.InfoHash); err != nil {
		return Handshake{}, err
	}

	peerID := make([]byte, 20)
	if _, err := io.ReadFull(r, peerID); err != nil {
		return Handshake{}, err
	}
	msg.PeerID = string(peerID)

	return msg, nil
}

func NewHandshake(peerID string, infoHash []byte) Handshake {
	return Handshake{
		PstrVal:    Pstr,
		PstrlenVal: Pstrlen,
		Reserved:   make([]byte, 8),
		InfoHash:   infoHash,
		PeerID:     peerID,
	}
}

// ------------------------------
type KeepAliveMessage struct {
	LengthPrefix int32
}

func (msg *KeepAliveMessage) Serialize() []byte {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.BigEndian, msg.LengthPrefix)

	return buf.Bytes()
}

func NewKeepAliveMessage() KeepAliveMessage {
	return KeepAliveMessage{
		LengthPrefix: 0,
	}
}

// ------------------------------

// Choke, Unchoke, Interested, and NotInterested messages all have the
// same form of: <length prefix><message ID>.
// Simple message accounts for all 4.
type SimpleMessage struct {
	LengthPrefix uint32
	MessageID    byte
}

func (msg *SimpleMessage) Serialize() []byte {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.BigEndian, msg.LengthPrefix)
	binary.Write(buf, binary.BigEndian, msg.MessageID)

	return buf.Bytes()
}

func NewSimpleMessage(messageID byte) SimpleMessage {
	return SimpleMessage{
		LengthPrefix: 1,
		MessageID:    messageID,
	}
}

// ------------------------------
type HaveMessage struct {
	LengthPrefix int32
	MessageID    byte // 4 for Have message
	PieceIndex   int32
}

func (msg *HaveMessage) Serialize() []byte {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.BigEndian, msg.LengthPrefix)
	binary.Write(buf, binary.BigEndian, msg.MessageID)
	binary.Write(buf, binary.BigEndian, msg.PieceIndex)

	return buf.Bytes()
}

func DeserializeHaveMessage(r io.Reader) (HaveMessage, error) {
	var msg HaveMessage
	if err := binary.Read(r, binary.BigEndian, &msg.PieceIndex); err != nil {
		return HaveMessage{}, err
	}
	msg.LengthPrefix = 5
	msg.MessageID = MessageIDHave
	return msg, nil
}

func NewHaveMessage(pieceIndex int32) HaveMessage {
	return HaveMessage{
		LengthPrefix: 5,
		MessageID:    MessageIDHave,
		PieceIndex:   pieceIndex,
	}
}

//------------------------------

type BitfieldMessage struct {
	LengthPrefix uint32
	MessageID    byte // 5 for Bitfield message
	Bitfield     []byte
}

func (msg *BitfieldMessage) Serialize() []byte {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.BigEndian, msg.LengthPrefix)
	binary.Write(buf, binary.BigEndian, msg.MessageID)
	binary.Write(buf, binary.BigEndian, msg.Bitfield)

	return buf.Bytes()
}

func DeserializeBitfieldMessage(r io.Reader, lengthPrefix uint32) (BitfieldMessage, error) {
	var msg BitfieldMessage
	msg.LengthPrefix = lengthPrefix

	var bitfieldLen = int32(msg.LengthPrefix - 1) // - 1 because lengthPrefix includes byte for MessageID

	msg.Bitfield = make([]byte, bitfieldLen) //creates a byte array of size bitfieldLen
	binary.Read(r, binary.BigEndian, &msg.Bitfield)

	msg.MessageID = MessageIDBitfield
	return msg, nil
}

func NewBitfieldMessage(bitfield []byte) BitfieldMessage {
	return BitfieldMessage{
		LengthPrefix: uint32(1 + len(bitfield)),
		MessageID:    MessageIDBitfield,
		Bitfield:     bitfield,
	}
}

//------------------------------

type RequestMessage struct {
	LengthPrefix int32
	MessageID    byte // 6 for Request message
	Index        int32
	Begin        int32
	Length       int32
}

func (msg *RequestMessage) Serialize() []byte {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.BigEndian, msg.LengthPrefix)
	binary.Write(buf, binary.BigEndian, msg.MessageID)
	binary.Write(buf, binary.BigEndian, msg.Index)
	binary.Write(buf, binary.BigEndian, msg.Begin)
	binary.Write(buf, binary.BigEndian, msg.Length)

	return buf.Bytes()
}

func NewRequestMessage(pieceIdx int, blockIdx int) RequestMessage {

	// Serialize and send Request message for a block of the target piece
	begin := int64(blockIdx) * blockSize
	length := pieces[pieceIdx].blocks[blockIdx].length

	return RequestMessage{
		LengthPrefix: 13,
		MessageID:    MessageIDRequest,
		Index:        int32(pieceIdx),
		Begin:        int32(begin),
		Length:       int32(length),
	}
}

//-----------------------------

type CancelMessage struct {
	LengthPrefix int32
	MessageID    byte // 8 for Cancel message
	Index        int32
	Begin        int32
	Length       int32
}

func (msg *CancelMessage) Serialize() []byte {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.BigEndian, msg.LengthPrefix)
	binary.Write(buf, binary.BigEndian, msg.MessageID)
	binary.Write(buf, binary.BigEndian, msg.Index)
	binary.Write(buf, binary.BigEndian, msg.Begin)
	binary.Write(buf, binary.BigEndian, msg.Length)

	return buf.Bytes()
}

func DeserializeRequestOrCancelMessage(r io.Reader, messageID byte) (RequestMessage, error) {
	var msg RequestMessage
	msg.LengthPrefix = 13
	msg.MessageID = messageID

	binary.Read(r, binary.BigEndian, &msg.Index)
	binary.Read(r, binary.BigEndian, &msg.Begin)
	binary.Read(r, binary.BigEndian, &msg.Length)

	return msg, nil
}

func NewCancelMessage(index int32, begin int32, length int32) CancelMessage {
	return CancelMessage{
		LengthPrefix: 13,
		MessageID:    MessageIDCancel,
		Index:        index,
		Begin:        begin,
		Length:       length,
	}
}

//------------------------------

type PieceMessage struct {
	LengthPrefix uint32
	MessageID    byte // 7 for Piece message
	Index        int32
	Begin        int32
	Block        []byte
}

func (msg *PieceMessage) Serialize() []byte {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.BigEndian, msg.LengthPrefix)
	binary.Write(buf, binary.BigEndian, msg.MessageID)
	binary.Write(buf, binary.BigEndian, msg.Index)
	binary.Write(buf, binary.BigEndian, msg.Begin)
	binary.Write(buf, binary.BigEndian, msg.Block)

	return buf.Bytes()
}

func DeserializePieceMessage(r io.Reader, lengthPrefix uint32) (PieceMessage, error) {
	var msg PieceMessage
	msg.MessageID = MessageIDPiece
	msg.LengthPrefix = lengthPrefix
	var blockSize = msg.LengthPrefix - 9

	binary.Read(r, binary.BigEndian, &msg.Index)
	binary.Read(r, binary.BigEndian, &msg.Begin)

	//msg.Block = make([]byte, blockSize)
	//binary.Read(r, binary.BigEndian, &msg.Block)

	msg.Block = make([]byte, blockSize)
	io.ReadFull(r, msg.Block)

	return msg, nil
}

func NewPieceMessage(index int32, begin int32, block []byte) PieceMessage {
	return PieceMessage{
		LengthPrefix: uint32(9 + len(block)),
		MessageID:    MessageIDPiece,
		Index:        index,
		Begin:        begin,
		Block:        block,
	}
}

//------------------------------

type PortMessage struct {
	LengthPrefix int32
	MessageID    byte // 9 for Port message
	ListenPort   int16
}

func (msg *PortMessage) Serialize() []byte {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.BigEndian, msg.LengthPrefix)
	binary.Write(buf, binary.BigEndian, msg.MessageID)
	binary.Write(buf, binary.BigEndian, msg.ListenPort)

	return buf.Bytes()
}

func DeserializePortMessage(r io.Reader) (PortMessage, error) {
	var msg PortMessage
	msg.LengthPrefix = 3
	msg.MessageID = MessageIDPort

	binary.Read(r, binary.BigEndian, &msg.ListenPort)

	return msg, nil
}

func NewPort(portNum int16) PortMessage {
	return PortMessage{
		LengthPrefix: 3,
		MessageID:    MessageIDPort,
		ListenPort:   portNum,
	}
}

//------------------------------

// DeserializeMessage reads from an io.Reader (like a net.Conn) and deserializes the message.
func DeserializeMessage(length uint32, messageBuffer []byte) (interface{}, error) {
	r := bytes.NewReader(messageBuffer)

	// If the message's length is 0, it must be a keep-alive message
	if length == 0 {
		return KeepAliveMessage{}, nil
	}

	var messageID byte
	if err := binary.Read(r, binary.BigEndian, &messageID); err != nil {
		return nil, err
	}

	switch messageID {
	case MessageIDChoke, MessageIDUnChoke, MessageIDInterested, MessageIDNotInterested:
		// Handle SimpleMessage types, which don't have a payload beyond the message ID.
		return SimpleMessage{LengthPrefix: length, MessageID: messageID}, nil
	case MessageIDHave:
		// Deserialize a Have message.
		return DeserializeHaveMessage(r)
	case MessageIDBitfield:
		return DeserializeBitfieldMessage(r, length)
	case MessageIDCancel, MessageIDRequest:
		return DeserializeRequestOrCancelMessage(r, messageID)
	case MessageIDPiece:
		return DeserializePieceMessage(r, length)
	case MessageIDPort:
		return DeserializePortMessage(r)
	default:
		// Unknown message type
		return nil, errors.New("unknown message ID")
	}
}

// Sends the parameter serialized message via the parameter connection. Returns 1 if the message was sent successfully, and 0 if not.
func sendMessage(connState *ConnectionState, serializedMessage []byte, messageType string, successMessage string) int {
	sentMessage := 0
	
	// Send the message
	_, err := connState.conn.Write(serializedMessage)
	if err != nil {
		
		// Check if the peer has closed the connection
		if opErr, ok := err.(*net.OpError); ok && opErr.Err.Error() == "write: broken pipe" {
			if verbose {
				fmt.Printf("[%s] Peer closed the TCP connection\n", connState.conn.RemoteAddr())
			}

			// Close the connection
			closeConnection(connState)
		} else {
			if verbose {
				fmt.Printf("[%s] Error sending the %s message\n", connState.conn.RemoteAddr(), messageType)
			}
		}
	} else {
		// Print the success message
		if verbose {
			fmt.Println(successMessage)
		}

		sentMessage = 1
	}

	// Update the peer's last-sent time
	connState.lastSentTime = time.Now()

	return sentMessage
}