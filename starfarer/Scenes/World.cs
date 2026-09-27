using Godot;
using Starfarer.Protocol.V1;
using Starfarer.Netcode;

public partial class World : Node3D
{
    private PackedScene _playerScene = null!;
    private Node3D _playersRoot = null!;
    private TransportProbe _client = null!;

    private readonly Dictionary<ulong, Node3D> _players = new();
    private readonly Dictionary<ulong, SnapshotBuffer<Vector3>>
        _remoteSamples = new();

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

            var position = new Vector3(
                state.PositionX,
                0,
                state.PositionZ
            );
            if (state.PlayerId == _client.LocalPlayerId)
            {
                node.Position = _client.LocalVisualPosition;
            }
            else
            {
                if (!_remoteSamples.TryGetValue(state.PlayerId, out var buffer))
                {
                    buffer = new SnapshotBuffer<Vector3>();
                    _remoteSamples.Add(state.PlayerId, buffer);
                    node.Position = position;
                }
                buffer.Add(snapshot.ServerTime, position);
            }
        }

        // A complete AOI baseline describes visibility, not entity destruction.
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
            _remoteSamples.Remove(id);
        }
    }

    public void ClearPlayers()
    {
        foreach (var node in _players.Values)
            node.QueueFree();

        _players.Clear();
        _remoteSamples.Clear();
    }

    public override void _Process(double delta)
    {
        double renderTime = _client.RenderTime;
        if (_players.TryGetValue(_client.LocalPlayerId, out var local))
            local.Position = _client.LocalVisualPosition;

        foreach (var entry in _remoteSamples)
        {
            if (!_players.TryGetValue(entry.Key, out var node))
                continue;

            if (entry.Value.TrySample(renderTime,
                (from, to, weight) => from.Lerp(to, weight), out var position))
                node.Position = position;
        }
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
