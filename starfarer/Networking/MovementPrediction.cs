using System.Numerics;
using Starfarer.Netcode;

namespace Starfarer.Networking;

// Game controller shared mathematically with Go's internal/world/player.go.
public readonly record struct MoveInput(float X, float Z);
public sealed class MovementPrediction
{
    public const uint InputHz = 60;
    public const float Speed = 5;
    private readonly PredictionTimeline<MoveInput, Vector2> _timeline = new(Vector2.Zero, Step);
    public ulong LastTick => _timeline.LastTick;
    public int Pending => _timeline.Pending;
    public Vector2 Position => _timeline.State;
    public static Vector2 Step(Vector2 state, MoveInput input)
    {
        double length = Math.Sqrt((double)input.X * input.X + (double)input.Z * input.Z);
        if (!double.IsFinite(length)) return state;
        float x = input.X, z = input.Z;
        if (length > 1) { x = (float)(x / length); z = (float)(z / length); }
        return state + new Vector2(x * Speed / InputHz, z * Speed / InputHz);
    }
    public void Reset(ulong tick, Vector2 state) => _timeline.Reset(tick, state);
    public void Reconcile(ulong tick, Vector2 state) => _timeline.Reconcile(tick, state);
    public void Generate(ulong target, MoveInput direction) => _timeline.Generate(target, direction);
    public KeyValuePair<ulong, MoveInput>[] Recent(int count) => _timeline.Recent(count);
}
