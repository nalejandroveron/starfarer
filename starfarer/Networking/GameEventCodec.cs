using Google.Protobuf;
using Starfarer.Netcode;
using Starfarer.Protocol.V1;

namespace Starfarer.Networking;

// Protobuf messages belong to the game; the netcode channel only sees this interface.
public sealed class GameEventCodec : IEventCodec<ClientEvents, ServerEvents>
{
    public const uint Version = 2;
    public byte[] Encode(ClientEvents message)
    {
        message.ProtocolVersion = Version;
        if (message.EventCase == ClientEvents.EventOneofCase.None)
            throw new FormatException("Client event is missing.");
        return message.ToByteArray();
    }

    public ServerEvents Decode(byte[] data)
    {
        try
        {
            var message = ServerEvents.Parser.ParseFrom(data);
            if (message.ProtocolVersion != Version || message.EventCase == ServerEvents.EventOneofCase.None)
                throw new FormatException("Unsupported protocol version or missing server event.");
            return message;
        }
        catch (InvalidProtocolBufferException ex) { throw new FormatException("Invalid Protobuf.", ex); }
    }
}
