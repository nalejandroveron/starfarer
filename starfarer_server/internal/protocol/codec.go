package protocol

import (
	"fmt"

	pb "starfarer/internal/protocol/v1"
	"starfarer/netcode"

	"google.golang.org/protobuf/proto"
)

const Version uint32 = 2
const MaxDatagramSize = netcode.MaxDatagramSize

// DecodeClientEvents validates the envelope before dispatching it.
func DecodeClientEvents(data []byte) (*pb.ClientEvents, error) {
	if len(data) == 0 || len(data) > MaxDatagramSize {
		return nil, fmt.Errorf("invalid datagram size: %d", len(data))
	}
	message := new(pb.ClientEvents)

	if err := proto.Unmarshal(data, message); err != nil {
		return nil, fmt.Errorf("decode client message: %w", err)
	}

	if message.ProtocolVersion != Version || message.Event == nil {
		return nil, fmt.Errorf("unsupported protocol version or missing event")
	}
	return message, nil
}

// EncodeServerEvents serializes an outgoing event.
func EncodeServerEvents(message *pb.ServerEvents) ([]byte, error) {
	if message == nil {
		return nil, fmt.Errorf("cannot encode a nil server message")
	}
	if message.ProtocolVersion != Version || message.Event == nil {
		return nil, fmt.Errorf("unsupported protocol version or missing event")
	}

	data, err := proto.Marshal(message)
	if err != nil {
		return nil, fmt.Errorf("encode server message: %w", err)
	}
	if len(data) > MaxDatagramSize {
		return nil, fmt.Errorf("event exceeds datagram limit")
	}

	return data, nil
}
