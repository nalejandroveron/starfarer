using Godot;
using Starfarer.Protocol.V1;

public partial class World : Node3D
{
    private PackedScene _playerScene = null!;
    private Node3D _playersRoot = null!;
    private TransportProbe _client = null!;

    private readonly Dictionary<ulong, Node3D> _players = new();

    public override void _Ready()
    {
        _playerScene = GD.Load<PackedScene>("res://Scenes/Player.tscn");
        _playersRoot = GetNode<Node3D>("Players");
        _client = GetNode<TransportProbe>("HUD/TransportProbe");

        _client.SnapshotReceived += ApplySnapshot;
        _client.SessionClosed += ClearPlayers;
    }

    public void ApplySnapshot(WorldSnapshot snapshot)
    {
        var present = new HashSet<ulong>();

        foreach (var state in snapshot.Players)
        {
            present.Add(state.PlayerId);

            if (!_players.TryGetValue(state.PlayerId, out var node))
            {
                node = _playerScene.Instantiate<Node3D>();
                node.Name = $"Player_{state.PlayerId}";

                _playersRoot.AddChild(node);
                _players.Add(state.PlayerId, node);
            }

            node.Position = new Vector3(
                state.PositionX,
                0,
                state.PositionZ
            );
        }

        // Primero reunimos los IDs; después modificamos el diccionario.
        var removed = new List<ulong>();

        foreach (var id in _players.Keys)
        {
            if (!present.Contains(id))
                removed.Add(id);
        }

        foreach (var id in removed)
        {
            _players[id].QueueFree();
            _players.Remove(id);
        }
    }

    public void ClearPlayers()
    {
        foreach (var node in _players.Values)
            node.QueueFree();

        _players.Clear();
    }

    public override void _ExitTree()
    {
        if (_client != null)
        {
            _client.SnapshotReceived -= ApplySnapshot;
            _client.SessionClosed -= ClearPlayers;
        }
    }
}
